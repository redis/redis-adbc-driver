// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func snapshotMetadataDirectory(id, slot string) string {
	return snapshotPrefix + "metadata:fields:" + id + ":" + slot
}

// A partial target is registered in its root immediately. Its construction edge
// takes over the removed root-to-source edge until completion/retirement. Both
// the partial target and source therefore remain discoverable by ordinary GC.
// Each call copies <=64 references, targeting 1 MiB of field/owner bytes; one
// oversized field may exceed the target. The checkpoint is a logical key.
const snapshotMetadataCloneBucket = `
if KEYS[2]~='` + snapshotPrefix + `metadata:root:'..ARGV[1] or KEYS[3]~='` + snapshotPrefix + `metadata:bucket:'..ARGV[4]..':'..ARGV[3] or KEYS[4]~='` + snapshotPrefix + `metadata:bucket:'..ARGV[1]..':'..ARGV[3] or KEYS[7]~='` + snapshotPrefix + `metadata:fields:'..ARGV[4]..':'..ARGV[3] or KEYS[8]~='` + snapshotPrefix + `metadata:fields:'..ARGV[1]..':'..ARGV[3] then return redis.error_reply('invalid snapshot metadata bucket keys') end
local raw=redis.call('HGET',KEYS[1],'w:'..ARGV[1])
if not raw then return redis.error_reply('snapshot writer lease lost') end
local w=cjson.decode(raw);local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if w.state~='staging' or w.token~=ARGV[2] or w.expires<=now then return redis.error_reply('snapshot writer lease lost') end
local owner=redis.call('HGET',KEYS[2],ARGV[3]) or ''
local buildfield='c:'..ARGV[1]..':'..ARGV[3]
local buildraw=redis.call('HGET',KEYS[5],buildfield)
local build=nil
if owner==ARGV[1] then
 if not buildraw then return 1 end
 build=cjson.decode(buildraw)
 if build.source~=ARGV[4] then return redis.error_reply('snapshot metadata construction source changed') end
elseif owner~=ARGV[4] then return redis.error_reply('snapshot metadata root changed') end
local expected=0
if ARGV[4]~='' then
 expected=tonumber(redis.call('HGET',KEYS[5],'n:'..ARGV[4]..':'..ARGV[3]) or '-1')
 if expected<0 or expected~=redis.call('HLEN',KEYS[3]) or expected~=redis.call('ZCARD',KEYS[7]) or tonumber(redis.call('HGET',KEYS[5],'b:'..ARGV[4]..':'..ARGV[3]) or '0')<=0 or redis.call('HEXISTS',KEYS[5],'c:'..ARGV[4]..':'..ARGV[3])==1 then return redis.error_reply('snapshot metadata bucket incomplete') end
end
if not build then
 if redis.call('EXISTS',KEYS[4],KEYS[8])~=0 or redis.call('HEXISTS',KEYS[5],'b:'..ARGV[1]..':'..ARGV[3])~=0 then return redis.error_reply('snapshot metadata construction target already exists') end
 redis.call('HSET',KEYS[2],ARGV[3],ARGV[1])
 redis.call('HSET',KEYS[5],'b:'..ARGV[1]..':'..ARGV[3],1,'n:'..ARGV[1]..':'..ARGV[3],0)
 if expected==0 then
  if ARGV[4]~='' and redis.call('HINCRBY',KEYS[5],'b:'..ARGV[4]..':'..ARGV[3],-1)==0 then redis.call('ZADD',KEYS[6],0,ARGV[4]..':'..ARGV[3]) end
  return 1
 end
 build={source=ARGV[4],after=''}
 redis.call('HSET',KEYS[5],buildfield,cjson.encode(build))
 w.metadata_builds=(w.metadata_builds or 0)+1
 redis.call('HSET',KEYS[1],'w:'..ARGV[1],cjson.encode(w))
end
local count=tonumber(redis.call('HGET',KEYS[5],'n:'..ARGV[1]..':'..ARGV[3]) or '-1')
if count<0 or count~=redis.call('HLEN',KEYS[4]) or count~=redis.call('ZCARD',KEYS[8]) or count>expected then return redis.error_reply('snapshot metadata partial bucket incomplete') end
local fields,values,bytes={},{},0
local after=build.after
for i=1,64 do
 local minimum='-';if after~='' then minimum='('..after end
 local page=redis.call('ZRANGEBYLEX',KEYS[7],minimum,'+','LIMIT',0,1)
 if #page==0 then break end
 local field=page[1]
 if #field>67108864 then return redis.error_reply('snapshot metadata field exceeds staging limit') end
 local value=redis.call('HGET',KEYS[3],field)
 if not value or #value~=32 or string.find(value,'[^0-9a-f]') or tonumber(redis.call('HGET',KEYS[5],'v:` + snapshotPrefix + `workspace:'..value..':'..field) or '0')<=0 or redis.call('HEXISTS',KEYS[4],field)~=0 then return redis.error_reply('snapshot metadata value unavailable') end
 if #fields>0 and bytes+#field+#value>1048576 then break end
 table.insert(fields,field);table.insert(values,value);bytes=bytes+#field+#value;after=field
end
if #fields==0 or count+#fields>expected then return redis.error_reply('snapshot metadata directory is incomplete') end
for i,field in ipairs(fields) do
 redis.call('HINCRBY',KEYS[5],'v:` + snapshotPrefix + `workspace:'..values[i]..':'..field,1)
 redis.call('HSET',KEYS[4],field,values[i]);redis.call('ZADD',KEYS[8],0,field)
end
count=count+#fields
redis.call('HSET',KEYS[5],'n:'..ARGV[1]..':'..ARGV[3],count)
if count==expected then
 if redis.call('HINCRBY',KEYS[5],'b:'..ARGV[4]..':'..ARGV[3],-1)==0 then redis.call('ZADD',KEYS[6],0,ARGV[4]..':'..ARGV[3]) end
 redis.call('HDEL',KEYS[5],buildfield);w.metadata_builds=w.metadata_builds-1
 redis.call('HSET',KEYS[1],'w:'..ARGV[1],cjson.encode(w));return 1
end
build.after=after;redis.call('HSET',KEYS[5],buildfield,cjson.encode(build));return 0
`

func (s *store) snapshotMetadataBucketStep(ctx context.Context, slot, source string) (int, error) {
	scope := snapshotOf(ctx)
	return once(ctx, s.redis(ctx), "EVAL", snapshotMetadataCloneBucket, 8, snapshotCatalogKey, snapshotMetadataMap(scope.id), snapshotMetadataBucket(source, slot), snapshotMetadataBucket(scope.id, slot), snapshotMetadataGraph, snapshotMetadataBucketQueue, snapshotMetadataDirectory(source, slot), snapshotMetadataDirectory(scope.id, slot), scope.id, scope.pin.token, slot, source).Int()
}

// Caller holds metadataMu. A new scope can resume an interrupted private bucket
// by reading its source/checkpoint; no Redis SCAN cursor survives a reconnect.
func (s *store) ownSnapshotMetadataBucket(ctx context.Context, slot, owner string) error {
	scope := snapshotOf(ctx)
	m := &scope.metadata
	if m.ready[slot] {
		return nil
	}
	source := owner
	if owner == scope.id {
		raw, err := s.redis(ctx).HGet(ctx, snapshotMetadataGraph, "c:"+scope.id+":"+slot).Bytes()
		if errors.Is(err, goredis.Nil) {
			m.ready[slot] = true
			delete(m.building, slot)
			return nil
		}
		if err != nil {
			return err
		}
		var build struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(raw, &build); err != nil {
			return err
		}
		source = build.Source
		if !snapshotMetadataID(source) {
			return errorf(adbc.StatusIO, "invalid metadata construction source")
		}
	}
	m.building[slot] = source
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := s.snapshotMetadataBucketStep(ctx, slot, source)
		if err != nil {
			return err
		}
		if done == 1 {
			m.roots[slot] = scope.id
			m.ready[slot] = true
			return nil
		}
	}
}
