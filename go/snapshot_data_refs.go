// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

const snapshotDataQueue = snapshotPrefix + "metadata:gc:data"

// Native descriptor objects retain their row segments once, independently of
// the number of buckets that alias the object. Validate complete edge changes
// before mutating: Redis script errors do not roll back earlier commands.
const snapshotDataEdges = snapshotRowLayoutLua + `
local function data_live(graph, refs)
 for _,id in ipairs(refs) do
  local raw=redis.call('HGET',graph,'d:'..id)
  local count=tonumber(redis.call('HGET',graph,'r:'..id) or '-1')
  if not raw or cjson.decode(raw).state~='live' or not valid_data_prefix(cjson.decode(raw).prefix,id) or count<0 then error('snapshot data generation unavailable') end
 end
end
local function data_releasable(graph, refs)
 for _,id in ipairs(refs) do
  if tonumber(redis.call('HGET',graph,'r:'..id) or '0')<=0 then error('snapshot data reference missing') end
 end
end
local function data_retain(graph, refs)
 for _,id in ipairs(refs) do redis.call('HINCRBY',graph,'r:'..id,1) end
end
local function data_release(graph, queue, refs)
 for _,id in ipairs(refs) do
  if redis.call('HINCRBY',graph,'r:'..id,-1)==0 then redis.call('ZADD',queue,0,id) end
 end
end
`

// Only descriptor mutations need to parse metadata. Keep this out of row-write
// and control scripts, whose cost must not grow with descriptor handling.
const snapshotDescriptorRefs = `
local function descriptor_refs(value,graph)
 local m=cjson.decode(value)
 local refs,seen={},{}
 local function add(prefix,index)
  local parsed,id,access=snapshot_row_key(prefix)
  if not parsed or parsed~=prefix or access~=m.snapshot_access_id or type(index)~='string' or string.sub(index,1,#('adbc_snapshot16_{meta}_'..id..'_'))~='adbc_snapshot16_{meta}_'..id..'_' then error('invalid snapshot segment identity') end
  local raw=redis.call('HGET',graph,'d:'..id)
  if not raw or cjson.decode(raw).prefix~=prefix then error('snapshot descriptor physical prefix is not owned') end
  if not seen[id] then table.insert(refs,id);seen[id]=true end
 end
 add(m.key_prefix,m.index_name)
 if m.snapshot_segments then
  if type(m.snapshot_segments)~='table' or #m.snapshot_segments>=8 then error('invalid snapshot segment manifest') end
  for _,seg in ipairs(m.snapshot_segments) do add(seg.prefix,seg.index) end
 end
 return refs
end
`
