// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

const snapshotWriteBatchRows = 256
const snapshotWriteBatchBytes = 1 << 20
const snapshotIngestConvertRows = 4096

// All row images and their exact directory entries precede the durable outcome.
// An uncertain reply is resolved by reading that outcome, never replaying input.
const snapshotWriteBatchLua = snapshotRowLayoutLua + `
-- snapshot insert row batch
local catalog,graph,directory=KEYS[1],KEYS[2],KEYS[3]
local workspace,token,data,operation=ARGV[1],ARGV[2],ARGV[3],ARGV[4]
if catalog~='` + snapshotCatalogKey + `' or graph~='` + snapshotMetadataGraph + `' or
 directory~='` + snapshotPrefix + `members:'..data or #KEYS<4 or #KEYS>259 or
 #operation~=32 or string.find(operation,'[^0-9a-f]') then
 return redis.error_reply('invalid snapshot insert batch')
end
local raw=redis.call('HGET',catalog,'w:'..workspace)
if not raw then return redis.error_reply('snapshot write generation was reclaimed') end
local w=cjson.decode(raw)
local t=redis.call('TIME')
local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if w.state~='staging' or w.token~=token or w.expires<=now then
 return redis.error_reply('snapshot writer lease lost')
end
local owned=redis.call('HGET',graph,'d:'..data)
if not owned then return redis.error_reply('snapshot data generation was reclaimed') end
local d=cjson.decode(owned)
if d.owner~=workspace or d.state~='live' or not valid_data_prefix(d.prefix,data) then
 return redis.error_reply('shared snapshot data is immutable')
end
if w.row_batch and w.row_batch.id==operation then return w.row_batch.rows end
local dirtype=redis.call('TYPE',directory).ok
if dirtype~='none' and dirtype~='zset' then return redis.error_reply('invalid snapshot row directory type') end
local rows,members,seen={},{},{}
local offset,bytes=5,0
for _,key in ipairs(KEYS) do bytes=bytes+#key end
for _,value in ipairs(ARGV) do bytes=bytes+#value end
if bytes>67108864 or (#KEYS>4 and bytes>1048576) then
 return redis.error_reply('snapshot insert batch byte limit exceeded')
end
for i=4,#KEYS do
 local member,count=ARGV[offset],tonumber(ARGV[offset+1])
 if not member or #member~=20 or not string.match(member,'^%d+$') or
    member=='00000000000000000000' or member>'09223372036854775807' or seen[member] or
    not count or count<2 or count%2~=0 or count>#ARGV-offset-1 then
  return redis.error_reply('invalid snapshot insert row')
 end
 seen[member]=true
 local suffix=string.gsub(member,'^0+','')
 if KEYS[i]~=d.prefix..suffix or redis.call('EXISTS',KEYS[i])~=0 or redis.call('ZSCORE',directory,member) then
  return redis.error_reply('snapshot insert row identity unavailable')
 end
 local fields={}
 for n=1,count do fields[n]=ARGV[offset+1+n] end
 if fields[1]~='` + rowIDField + `' or fields[2]~=suffix then return redis.error_reply('invalid snapshot insert row ID') end
 if not redis.acl_check_cmd('HSET',KEYS[i],unpack(fields)) then
  return redis.error_reply('NOPERM snapshot insert batch requires HSET')
 end
 rows[#rows+1]=fields
 members[#members+1]=member
 offset=offset+2+count
end
if offset~=#ARGV+1 then return redis.error_reply('invalid snapshot insert batch arguments') end
w.row_batch={id=operation,rows=#rows}
local outcome=cjson.encode(w)
local registration={}
for _,member in ipairs(members) do registration[#registration+1]=0;registration[#registration+1]=member end
if not redis.acl_check_cmd('ZADD',directory,unpack(registration)) or
   not redis.acl_check_cmd('HSET',catalog,'w:'..workspace,outcome) then
 return redis.error_reply('NOPERM snapshot insert batch requires ZADD and control HSET')
end
-- Register first: even an unexpected later runtime failure leaves every possible
-- row image discoverable by ownership-aware cleanup; it cannot publish.
redis.call('ZADD',directory,unpack(registration))
for i,fields in ipairs(rows) do redis.call('HSET',KEYS[i+3],unpack(fields)) end
redis.call('HSET',catalog,'w:'..workspace,outcome)
return #rows
`

type snapshotInsertRow struct {
	key, member string
	fields      []any
}

func (s *store) insertSnapshotBatch(ctx context.Context, prefix string, rows []snapshotInsertRow) error {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write || scope.pin == nil || len(rows) == 0 || len(rows) > snapshotWriteBatchRows {
		return errorf(adbc.StatusInvalidState, "invalid snapshot insert batch context")
	}
	// One outcome slot per workspace, bounded independently of total operation size.
	scope.rowBatchMu.Lock()
	defer scope.rowBatchMu.Unlock()
	data := snapshotDataID(prefix)
	scope.mu.Lock()
	owned := scope.owned[data]
	scope.mu.Unlock()
	if data == "" || !owned || prefix != snapshotRowPrefix(prefix) {
		return errorf(adbc.StatusInvalidState, "snapshot insert prefix is not owned")
	}
	operation := snapshotID()
	args := []any{"EVAL", snapshotWriteBatchLua, len(rows) + 3, snapshotCatalogKey, snapshotMetadataGraph, snapshotMembers(data)}
	for _, row := range rows {
		args = append(args, row.key)
	}
	args = append(args, scope.id, scope.pin.token, data, operation)
	for _, row := range rows {
		args = append(args, row.member, len(row.fields))
		args = append(args, row.fields...)
	}
	n, err := once(ctx, s.client, args...).Int64()
	if err == nil {
		if n != int64(len(rows)) {
			return errorf(adbc.StatusIO, "invalid snapshot insert batch outcome")
		}
		return nil
	}
	// Read only, using a separate bounded context if cancellation raced the
	// acknowledged server execution. Missing/unknown outcomes fail the statement.
	resolve, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, readErr := s.client.HGet(resolve, snapshotCatalogKey, "w:"+scope.id).Bytes()
	var outcome struct {
		Token string `json:"token"`
		Batch struct {
			ID   string `json:"id"`
			Rows int    `json:"rows"`
		} `json:"row_batch"`
	}
	if readErr == nil && json.Unmarshal(raw, &outcome) == nil && outcome.Token == scope.pin.token && outcome.Batch.ID == operation && outcome.Batch.Rows == len(rows) {
		return nil
	}
	return wrapRedis(err, "snapshot insert batch failed; input was not replayed")
}

func (s *store) writeSnapshotRows(ctx context.Context, meta *tableMeta, a rowAlloc, rows [][]Value, fieldNames []any) (int64, error) {
	batch := make([]snapshotInsertRow, 0, snapshotWriteBatchRows)
	bytes, written := 0, 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.checkWritten(ctx, meta); err != nil {
			return err
		}
		if err := s.insertSnapshotBatch(ctx, meta.prefix(), batch); err != nil {
			return err
		}
		written += len(batch)
		if testHookRowsWritten != nil {
			testHookRowsWritten(meta, written)
		}
		batch, bytes = batch[:0], 0
		return nil
	}
	for r, row := range rows {
		id := strconv.FormatInt(a.first+int64(r), 10)
		key := meta.prefix() + id
		fields := []any{rowIDField, id}
		if meta.SnapshotSourceKey && len(a.sourceKeys) > 0 && !a.sourceKeys[r].Null {
			fields = append(fields, snapshotSourceKeyField, a.sourceKeys[r].S)
		}
		for i, value := range row {
			if !value.Null {
				fields = append(fields, fieldNames[i], encodeStored(value))
			}
		}
		fields = append(fields, a.added...)
		// Include framing/control/directory bytes conservatively. A single wide
		// row retains the previous 64 MiB command ceiling; ordinary pages target 1 MiB.
		size := len(key) + 128
		for _, field := range fields {
			size += len(fmt.Sprint(field))
		}
		if len(batch) > 0 && (len(batch) >= snapshotWriteBatchRows || bytes+size+1024 > snapshotWriteBatchBytes) {
			if err := flush(); err != nil {
				return 0, err
			}
		}
		batch = append(batch, snapshotInsertRow{key: key, member: snapshotDirectoryMember(key), fields: fields})
		bytes += size
	}
	if err := flush(); err != nil {
		return 0, err
	}
	if err := s.checkKeys(ctx, meta, true); err != nil {
		return 0, err
	}
	return int64(written), nil
}
