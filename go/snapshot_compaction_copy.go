// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

type snapshotCompactionCopyOutcome struct {
	ID     string `json:"id"`
	Before string `json:"before"`
	After  string `json:"after"`
	Rows   int    `json:"rows"`
}

// The output directory, complete immutable-source images and source checkpoint
// commit together. A pending marker preserves discoverability after an unexpected
// Lua runtime failure; no partial output can advance the checkpoint or install.
var snapshotCompactionCopyLua = snapshotRowLayoutLua + `
-- snapshot atomic compaction copy batch
local catalog,graph,directory=KEYS[1],KEYS[2],KEYS[3]
local workspace,token,data,before,after,operation=ARGV[1],ARGV[2],ARGV[3],ARGV[4],ARGV[5],ARGV[6]
local lease=tonumber(ARGV[7])
local function member(value)
 return type(value)=='string' and #value==20 and string.match(value,'^%d+$') and
 value~='00000000000000000000' and value<='09223372036854775807'
end
if #KEYS<3 or #KEYS>67 or #ARGV<7 or catalog~='` + snapshotCatalogKey + `' or
 graph~='` + snapshotMetadataGraph + `' or directory~='` + snapshotPrefix + `members:'..data or
 #operation~=32 or string.find(operation,'[^0-9a-f]') or
 (before~='' and not member(before)) or not member(after) or after<=before or
 lease~=` + strconv.FormatInt(snapshotLease.Milliseconds(), 10) + ` then
 return redis.error_reply('invalid compaction copy batch')
end
local raw=redis.call('HGET',catalog,'compaction')
local wr=redis.call('HGET',catalog,'w:'..workspace)
if not raw or not wr then return redis.error_reply('compaction copy ownership missing') end
local job,w=cjson.decode(raw),cjson.decode(wr)
local clock=redis.call('TIME');local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
if job.id~=workspace or job.token~=token or job.data~=data or job.phase~='copy' or
 w.state~='compacting' or w.token~=token or w.expires<=now then
 return redis.error_reply('compaction copy lease lost')
end
if (job.after or '')~=before then return redis.error_reply('compaction copy checkpoint changed') end
local owned=redis.call('HGET',graph,'d:'..data)
if not owned then return redis.error_reply('compaction output ownership missing') end
local owner=cjson.decode(owned)
if owner.owner~=workspace or owner.state~='live' or owner.prefix~=job.prefix or
 not valid_data_prefix(owner.prefix,data) then return redis.error_reply('compaction copy prefix is not owned') end
local dirtype=redis.call('TYPE',directory).ok
if dirtype~='none' and dirtype~='zset' then return redis.error_reply('invalid compaction copy directory type') end
local rows,registration={},{}
local offset,bytes,previous=8,0,before
for i=4,#KEYS do bytes=bytes+#KEYS[i] end
for i=8,#ARGV do bytes=bytes+#ARGV[i] end
if bytes>67108864 or (#KEYS>4 and bytes>1048576) then
 return redis.error_reply('compaction copy image byte limit exceeded')
end
for i=4,#KEYS do
 local id,count=ARGV[offset],tonumber(ARGV[offset+1])
 if not member(id) or id<=previous or id>after or not count or count<2 or count%2~=0 or count>#ARGV-offset-1 then
  return redis.error_reply('invalid compaction copy row')
 end
 previous=id
 local suffix=string.gsub(id,'^0+','')
 if KEYS[i]~=owner.prefix..suffix then return redis.error_reply('invalid compaction copy row identity') end
 local present=redis.call('ZSCORE',directory,id)
 local rowtype=redis.call('TYPE',KEYS[i]).ok
 if (present and tonumber(present)~=0) or (rowtype~='none' and rowtype~='hash') or
 (rowtype=='hash' and not present) then return redis.error_reply('compaction copy output is not registered') end
 local fields,seen={},{}
 for n=1,count,2 do
  local field,value=ARGV[offset+1+n],ARGV[offset+2+n]
  if seen[field] or (field=='` + snapshotDeletedField + `' and value=='1') then
   return redis.error_reply('invalid compaction copy fields')
  end
  seen[field]=true;fields[n]=field;fields[n+1]=value
 end
 if fields[1]~='` + rowIDField + `' or fields[2]~=suffix then return redis.error_reply('invalid compaction copy row ID') end
 if not redis.acl_check_cmd('HSET',KEYS[i],unpack(fields)) then return redis.error_reply('NOPERM compaction copy requires HSET') end
 rows[#rows+1]=fields;registration[#registration+1]=0;registration[#registration+1]=id
 offset=offset+2+count
end
if offset~=#ARGV+1 then return redis.error_reply('invalid compaction copy arguments') end
local outcome={id=operation,before=before,after=after,rows=#rows}
job.copy_pending=outcome
local pending=cjson.encode(job)
job.copy_pending=nil;job.copy_batch=outcome;job.after=after;job.last_error=nil
local completed=cjson.encode(job)
w.expires=now+lease
local renewed=cjson.encode(w)
if (#rows>0 and not redis.acl_check_cmd('ZADD',directory,unpack(registration))) or
 not redis.acl_check_cmd('HSET',catalog,'compaction',pending) or
 not redis.acl_check_cmd('HSET',catalog,'compaction',completed,'w:'..workspace,renewed) then
 return redis.error_reply('NOPERM compaction copy requires directory and checkpoint writes')
end
redis.call('HSET',catalog,'compaction',pending)
if #rows>0 then redis.call('ZADD',directory,unpack(registration)) end
for i,fields in ipairs(rows) do redis.call('HSET',KEYS[i+3],unpack(fields)) end
redis.call('HSET',catalog,'compaction',completed,'w:'..workspace,renewed)
return #rows
`

func (s *store) commitSnapshotCompactionCopy(ctx context.Context, j *snapshotCompactionJob, after string, rows []snapshotInsertRow) error {
	operation := snapshotID()
	args := []any{"EVAL", snapshotCompactionCopyLua, 3 + len(rows), snapshotCatalogKey, snapshotMetadataGraph, snapshotMembers(j.Data)}
	for _, row := range rows {
		args = append(args, row.key)
	}
	args = append(args, j.ID, j.Token, j.Data, j.After, after, operation, snapshotLease.Milliseconds())
	for _, row := range rows {
		args = append(args, row.member, len(row.fields))
		args = append(args, row.fields...)
	}
	n, err := once(ctx, s.client, args...).Int64()
	if err == nil {
		if n != int64(len(rows)) {
			return errorf(adbc.StatusIO, "invalid compaction copy outcome")
		}
		j.CopyBatch = &snapshotCompactionCopyOutcome{ID: operation, Before: j.After, After: after, Rows: len(rows)}
		j.After, j.LastError, j.CopyPending = after, "", nil
		return nil
	}
	// A definite server refusal is not a lost reply. In particular, an old
	// owner must not report success just because its successor advanced farther.
	var serverErr interface {
		error
		RedisError()
	}
	if errors.As(err, &serverErr) {
		return wrapRedis(err, "compaction copy refused before completion")
	}
	// Reading progress is safe even when the reply or caller context was lost.
	// Never resend this mutation. A later owner resumes the immutable source at
	// its durable checkpoint, including repair of explicitly incomplete output.
	resolve, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	durable, readErr := s.readSnapshotCompaction(resolve)
	if readErr == nil && durable != nil && durable.ID == j.ID && durable.Data == j.Data && durable.Prefix == j.Prefix && durable.After >= after {
		*j = *durable
		return nil
	}
	return wrapRedis(transportContextError(ctx, err), "compaction copy outcome unknown or incomplete; batch was not replayed")
}
