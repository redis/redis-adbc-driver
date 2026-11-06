-- Copyright (c) 2026 ADBC Drivers Contributors
-- Licensed under the Apache License, Version 2.0.
-- Frozen protocol15 collector from 95643ed; intentionally not regenerated.

local function snapshot_row_key(key)
 if type(key)~='string' then return nil end
 local schema,id,suffix=string.match(key,'^([^:{}]+):{meta}:snapshot:rows:([0-9a-f]+):(.*)$')
 if not schema or schema=='adbc' or #id~=32 then return nil end
 return schema..':{meta}:snapshot:rows:'..id..':',id
end
local function valid_data_prefix(prefix,id)
 local parsed,data=snapshot_row_key(prefix)
 return parsed~=nil and parsed==prefix and data==id
end

-- snapshot row reclamation batch
local id,prefix=ARGV[1],ARGV[2]
local graph,queue,directory=KEYS[1],KEYS[2],KEYS[3]
if #KEYS<4 or #KEYS>259 or #ARGV~=#KEYS-1 or
 graph~='adbc:{meta}:snapshot:metadata:graph' or
 queue~='adbc:{meta}:snapshot:metadata:gc:data' or
 directory~='adbc:{meta}:snapshot:members:'..id or
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
