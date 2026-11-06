// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Progress is the remaining directory, not a persisted SCAN cursor. Validate
// every argument and permission before mutating: Lua errors do not roll back.
const snapshotGCRowBatchLua = snapshotRowLayoutLua + `
-- snapshot row reclamation batch
local id,prefix=ARGV[1],ARGV[2]
local graph,queue,directory=KEYS[1],KEYS[2],KEYS[3]
if #KEYS<4 or #KEYS>259 or #ARGV~=#KEYS-1 or
 graph~='` + snapshotMetadataGraph + `' or
 queue~='` + snapshotDataQueue + `' or
 directory~='` + snapshotPrefix + `members:'..id or
 not valid_data_prefix(prefix,id) then
 return redis.error_reply('invalid snapshot reclamation batch')
end
local raw=redis.call('HGET',graph,'d:'..id)
if not raw then
 -- Another collector may have completed the generation. Do not infer row
 -- ownership from the stale caller's key list.
 if redis.call('HEXISTS',graph,'r:'..id)==0 and
    not redis.call('ZSCORE',queue,id) and redis.call('ZCARD',directory)==0 then return 0 end
 return redis.error_reply('snapshot reclamation owner missing')
end
local owner=cjson.decode(raw)
if owner.state~='deleting' or owner.prefix~=prefix or
 redis.call('HGET',graph,'r:'..id)~='0' or not redis.call('ZSCORE',queue,id) then
 return redis.error_reply('snapshot reclamation not admitted')
end
local rows,members,seen={},{},{}
for i=4,#KEYS do
 local member=ARGV[i-1]
 if #member~=20 or not string.match(member,'^%d+$') or
    member=='00000000000000000000' or member>'09223372036854775807' or seen[member] then
  return redis.error_reply('invalid snapshot row directory member')
 end
 seen[member]=true
 -- IDs must never pass through Lua doubles (including values above 2^53).
 local suffix=string.gsub(member,'^0+','')
 if KEYS[i]~=prefix..suffix then return redis.error_reply('invalid snapshot reclamation row') end
 local score=redis.call('ZSCORE',directory,member)
 if score then
  if tonumber(score)~=0 then return redis.error_reply('invalid snapshot row directory score') end
  rows[#rows+1]=KEYS[i]
  members[#members+1]=member
 end
end
if #rows==0 then return 0 end
if not redis.acl_check_cmd('DEL',unpack(rows)) or
   not redis.acl_check_cmd('ZREM',directory,unpack(members)) then
 return redis.error_reply('NOPERM snapshot reclamation batch requires DEL and ZREM')
end
local deleted=redis.call('DEL',unpack(rows))
redis.call('ZREM',directory,unpack(members))
return deleted
`

func (s *store) deleteSnapshotRowBatch(ctx context.Context, prefix string, members []string) (int64, error) {
	id := snapshotDataID(prefix)
	if id == "" || prefix != snapshotRowPrefix(prefix) || len(members) == 0 || len(members) > 256 {
		return 0, errorf(adbc.StatusIO, "invalid snapshot reclamation batch")
	}
	args := []any{"EVAL", snapshotGCRowBatchLua, 3 + len(members), snapshotMetadataGraph, snapshotDataQueue, snapshotMembers(id)}
	for _, member := range members {
		n, err := strconv.ParseInt(member, 10, 64)
		if err != nil || n < 1 || len(member) != 20 {
			return 0, errorf(adbc.StatusIO, "invalid snapshot row directory member")
		}
		args = append(args, prefix+strconv.FormatInt(n, 10))
	}
	args = append(args, id, prefix)
	for _, member := range members {
		args = append(args, member)
	}
	return once(ctx, s.client, args...).Int64()
}
