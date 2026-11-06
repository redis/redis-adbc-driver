// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Notify a fresh index of complete final row images, without changing any image
// or directory. Validate the entire batch before the first notification. Missing
// HASHes must never be recreated as row-ID-only documents. This is not replayed
// after an uncertain reply: publication fails and the private output is retired.
const snapshotIndexNotifyLua = snapshotRowLayoutLua + `
local catalog,graph,directory=KEYS[1],KEYS[2],KEYS[3]
local workspace,token,data=ARGV[1],ARGV[2],ARGV[3]
if #KEYS<4 or #KEYS>259 or #ARGV~=#KEYS or
 catalog~='` + snapshotCatalogKey + `' or graph~='` + snapshotMetadataGraph + `' or
 directory~='` + snapshotPrefix + `members:'..data then
 return redis.error_reply('invalid snapshot index notification batch')
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
local owner=cjson.decode(owned)
local prefix=owner.prefix
if owner.owner~=workspace or owner.state~='live' or not valid_data_prefix(prefix,data) then
 return redis.error_reply('snapshot index notification prefix is not owned')
end
local bytes=0
for _,key in ipairs(KEYS) do bytes=bytes+#key end
for _,value in ipairs(ARGV) do bytes=bytes+#value end
if bytes>67108864 or (#KEYS>4 and bytes>1048576) then
 return redis.error_reply('snapshot index notification byte limit exceeded')
end
local ids,seen={},{}
for i=4,#KEYS do
 local member=ARGV[i]
 if #member~=20 or not string.match(member,'^%d+$') or
 member=='00000000000000000000' or member>'09223372036854775807' or seen[member] then
  return redis.error_reply('invalid snapshot index directory member')
 end
 seen[member]=true
 local id=string.gsub(member,'^0+','')
 if KEYS[i]~=prefix..id or redis.call('ZSCORE',directory,member)~='0' or
 redis.call('TYPE',KEYS[i]).ok~='hash' or redis.call('HGET',KEYS[i],'` + rowIDField + `')~=id then
  return redis.error_reply('snapshot index row identity unavailable')
 end
 if not redis.acl_check_cmd('HSET',KEYS[i],'` + rowIDField + `',id) then
  return redis.error_reply('NOPERM snapshot index notification requires HSET')
 end
 ids[#ids+1]=id
end
for i,id in ipairs(ids) do redis.call('HSET',KEYS[i+3],'` + rowIDField + `',id) end
return #ids
`

func (s *store) notifySnapshotIndexBatch(ctx context.Context, prefix string, members []string) error {
	scope := snapshotOf(ctx)
	data := snapshotDataID(prefix)
	if scope == nil || !scope.write || scope.pin == nil || !snapshotOwns(ctx, prefix) || data == "" || prefix != snapshotRowPrefix(prefix) || len(members) == 0 || len(members) > 256 {
		return errorf(adbc.StatusInvalidState, "invalid snapshot index notification context")
	}
	args := []any{"EVAL", snapshotIndexNotifyLua, 3 + len(members), snapshotCatalogKey, snapshotMetadataGraph, snapshotMembers(data)}
	for _, member := range members {
		n, err := strconv.ParseInt(member, 10, 64)
		if err != nil || n < 1 || len(member) != 20 || member != formatSnapshotIndexMember(n) {
			return errorf(adbc.StatusIO, "invalid snapshot index directory member")
		}
		args = append(args, prefix+strconv.FormatInt(n, 10))
	}
	args = append(args, scope.id, scope.pin.token, data)
	for _, member := range members {
		args = append(args, member)
	}
	n, err := once(ctx, s.redis(ctx), args...).Int64()
	if err != nil {
		return wrapRedis(transportContextError(ctx, err), "snapshot index notification failed; operation was not replayed")
	}
	if n != int64(len(members)) {
		return errorf(adbc.StatusIO, "invalid snapshot index notification outcome")
	}
	return nil
}
func formatSnapshotIndexMember(n int64) string {
	id := strconv.FormatInt(n, 10)
	return "00000000000000000000"[:20-len(id)] + id
}

// Traverse only the registered private directory, in bounded lexicographic
// pages. No SCAN or database-wide initial indexing pass is needed. SQL/input
// execution has finished; no row mutations can interleave on this workspace.
func (s *store) populateSnapshotIndex(ctx context.Context, meta *tableMeta) error {
	scope := snapshotOf(ctx)
	prefix := meta.prefix()
	data := snapshotDataID(prefix)
	if scope == nil || scope.pin == nil || !snapshotOwns(ctx, prefix) {
		return errorf(adbc.StatusInvalidState, "snapshot index output is not owned")
	}
	directory := snapshotMembers(data)
	expected, err := s.redis(ctx).ZCard(ctx, directory).Result()
	if err != nil {
		return wrapRedis(err, "failed to count final index directory")
	}
	var total int64
	min := "-"
	baseBytes := len(snapshotCatalogKey) + len(snapshotMetadataGraph) + len(directory) + len(scope.id) + len(scope.pin.token) + len(data)
	for {
		if err := cancelled(ctx); err != nil {
			return err
		}
		members, err := s.redis(ctx).ZRangeByLex(ctx, directory, &goredis.ZRangeBy{Min: min, Max: "+", Count: 256}).Result()
		if err != nil {
			return wrapRedis(err, "failed to read final index directory")
		}
		if len(members) == 0 {
			break
		}
		if total+int64(len(members)) > expected {
			return errorf(adbc.StatusIO, "final index directory changed")
		}
		for start := 0; start < len(members); {
			end, bytes := start, baseBytes
			for end < len(members) {
				size := len(prefix) + len(members[end]) + len(members[end]) // safe upper bound including decimal suffix
				if end > start && bytes+size > snapshotWriteBatchBytes {
					break
				}
				bytes += size
				end++
			}
			if err := s.notifySnapshotIndexBatch(ctx, prefix, members[start:end]); err != nil {
				return err
			}
			start = end
		}
		total += int64(len(members))
		min = "(" + members[len(members)-1]
	}
	actual, err := s.redis(ctx).ZCard(ctx, directory).Result()
	if err != nil {
		return wrapRedis(err, "failed to verify final index directory")
	}
	if total != expected || actual != expected {
		return errorf(adbc.StatusIO, "final index directory changed")
	}
	return nil
}

// A principal allowed to write/index but not query keeps its original readiness
// contract. This rebuilds only private index state; SQL and row inputs are not
// repeated. The old index cannot be reused as a completion proof for the new one.
func (s *store) replaceSnapshotIndexWithScan(ctx context.Context, meta *tableMeta) error {
	old := meta.index()
	meta.IndexName = snapshotIndexPrefix(snapshotDataID(meta.prefix())) + snapshotID()
	if err := s.searchDo(ctx, meta.index(), snapshotScannedIndexArgs(meta)...).Err(); err != nil {
		return wrapRedis(err, "failed to build final index for write-only principal")
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := s.redis(ctx).Set(ctx, metaKey(meta.Schema, meta.Name), payload, 0).Err(); err != nil {
		return err
	}
	if err := s.searchDo(ctx, old, "FT.DROPINDEX", old).Err(); err != nil && !isUnknownIndex(err) {
		return wrapRedis(err, "failed to retire private directory-built index")
	}
	scope := snapshotOf(ctx)
	scope.mu.Lock()
	if scope.scannedFinalIndexes == nil {
		scope.scannedFinalIndexes = map[string]bool{}
	}
	scope.scannedFinalIndexes[meta.index()] = true
	scope.mu.Unlock()
	return nil
}

// Preserve the raw Redis error until the caller has classified a definite
// permission refusal. General ADBC wrapping intentionally drops server types.
func (s *store) snapshotIndexedRowCount(ctx context.Context, index string) (int64, error) {
	reply, err := s.searchDo(ctx, index, "FT.SEARCH", index, "@"+rowIDField+":[1 +inf]", "LIMIT", 0, 0, "TIMEOUT", 0, "DIALECT", 2).Result()
	if err != nil {
		return 0, err
	}
	if snapshotReplyWarning(reply) {
		return 0, errorf(adbc.StatusIO, "snapshot index proof returned a partial-result warning")
	}
	return parseSearchCount(reply)
}
