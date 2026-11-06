// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotMetadataGraph = snapshotPrefix + "metadata:graph"
const snapshotMetadataBucketQueue = snapshotPrefix + "metadata:gc:buckets"
const snapshotMetadataValueQueue = snapshotPrefix + "metadata:gc:values"

func snapshotMetadataMap(id string) string { return snapshotPrefix + "metadata:root:" + id }
func snapshotMetadataBucket(id, slot string) string {
	return snapshotPrefix + "metadata:bucket:" + id + ":" + slot
}
func snapshotMetadataSlot(key string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf("%02x", h.Sum32()&255)
}
func snapshotMetadataID(id string) bool {
	return len(id) == 32 && snapshotDataID(snapshotRows(id)+"1") == id
}

type snapshotMetadataState struct {
	building      map[string]string
	roots         map[string]string
	buckets       map[string]map[string]string
	checked       map[string]bool
	loaded        map[string]bool
	bucketChecked map[string]bool
	ready         map[string]bool
	dirty         map[string]bool
	prune         map[string]bool
	reads         map[string]string // logical key -> original immutable value owner (empty means absent)
	changed       map[string]bool
}

// Both maps contain only references. Copying a root touches at most 256 slots;
// bucket/value references live independently of the original writer manifest.
const snapshotMetadataCloneRoot = `
if KEYS[2]~='` + snapshotPrefix + `metadata:root:'..ARGV[3] or KEYS[3]~='` + snapshotPrefix + `metadata:root:'..ARGV[1] or KEYS[5]~='` + snapshotPrefix + `workspace:'..ARGV[3]..':__keys' then return redis.error_reply('invalid snapshot metadata root keys') end
local raw=redis.call('HGET',KEYS[1],'w:'..ARGV[1])
if not raw then return redis.error_reply('snapshot writer lease lost') end
local w=cjson.decode(raw)
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local pinraw=redis.call('HGET',KEYS[1],'p:'..ARGV[2])
if w.state~='staging' or w.token~=ARGV[2] or w.expires<=now or not pinraw then return redis.error_reply('snapshot writer lease lost') end
local pin=cjson.decode(pinraw)
if pin.root~=ARGV[3] or pin.expires<=now then return redis.error_reply('snapshot base pin expired') end
if w.metadata_ready then return 1 end
local baseRaw=redis.call('HGET',KEYS[1],'w:'..ARGV[3])
if not baseRaw then return redis.error_reply('snapshot metadata manifest unavailable') end
local baseWork=cjson.decode(baseRaw)
local entries=redis.call('HGETALL',KEYS[2])
if baseWork.state~='committed' or (baseWork.metadata_builds or 0)>0 or baseWork.metadata_buckets~=#entries/2 or baseWork.metadata_keys~=redis.call('ZCARD',KEYS[5]) then return redis.error_reply('snapshot metadata base is incomplete') end
if #entries>512 then return redis.error_reply('invalid snapshot metadata root') end
for i=1,#entries,2 do
 local field='b:'..entries[i+1]..':'..entries[i]
 if tonumber(redis.call('HGET',KEYS[4],field) or '0')<=0 then return redis.error_reply('snapshot metadata bucket unavailable') end
end
for i=1,#entries,2 do
 redis.call('HINCRBY',KEYS[4],'b:'..entries[i+1]..':'..entries[i],1)
 redis.call('HSET',KEYS[3],entries[i],entries[i+1])
end
w.metadata_ready=true;redis.call('HSET',KEYS[1],'w:'..ARGV[1],cjson.encode(w))
return 1
`

const snapshotMetadataOwnValue = snapshotDataEdges + `
if KEYS[10]~='` + snapshotPrefix + `metadata:fields:'..ARGV[1]..':'..ARGV[3] or KEYS[2]~='` + snapshotPrefix + `metadata:root:'..ARGV[1] or KEYS[3]~='` + snapshotPrefix + `metadata:bucket:'..ARGV[1]..':'..ARGV[3] or KEYS[4]~='` + snapshotPrefix + `workspace:'..ARGV[5]..':'..ARGV[4] or KEYS[5]~='` + snapshotPrefix + `workspace:'..ARGV[1]..':'..ARGV[4] then return redis.error_reply('invalid snapshot metadata value keys') end
local raw=redis.call('HGET',KEYS[1],'w:'..ARGV[1])
if not raw then return redis.error_reply('snapshot writer lease lost') end
local w=cjson.decode(raw);local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if w.state~='staging' or w.token~=ARGV[2] or w.expires<=now then return redis.error_reply('snapshot writer lease lost') end
if (w.metadata_builds or 0)>0 then return redis.error_reply('snapshot metadata construction is incomplete') end
if redis.call('HGET',KEYS[2],ARGV[3])~=ARGV[1] then return redis.error_reply('snapshot metadata bucket is not private') end
local owner=redis.call('HGET',KEYS[3],ARGV[4]) or ''
if owner==ARGV[1] then return 1 end
if owner~=ARGV[5] then return redis.error_reply('snapshot metadata value changed') end
local present=0
if owner~='' then
 local expected=redis.call('HGET',KEYS[6],'e:'..KEYS[4])
 if not expected or tonumber(expected)~=redis.call('EXISTS',KEYS[4]) then return redis.error_reply('snapshot metadata value is missing or corrupt') end
 present=tonumber(expected)
 if present==1 then
  local edges=redis.call('HGET',KEYS[6],'t:'..KEYS[4])
  if string.sub(ARGV[4],1,6)=='table:' and not edges then return redis.error_reply('snapshot descriptor references missing') end
  local refs=cjson.decode(edges or '[]')
  data_live(KEYS[6],refs)
  redis.call('COPY',KEYS[4],KEYS[5],'REPLACE')
  data_retain(KEYS[6],refs)
  if #refs>0 then redis.call('HSET',KEYS[6],'t:'..KEYS[5],cjson.encode(refs)) end
 end
end
redis.call('HSET',KEYS[3],ARGV[4],ARGV[1]);redis.call('ZADD',KEYS[10],0,ARGV[4])
redis.call('HSET',KEYS[6],'v:'..KEYS[5],1,'e:'..KEYS[5],present,'n:'..ARGV[1]..':'..ARGV[3],redis.call('HLEN',KEYS[3]))
if present==1 then redis.call('ZADD',KEYS[8],0,KEYS[5]) end
if owner~='' and redis.call('HINCRBY',KEYS[6],'v:'..KEYS[4],-1)==0 then redis.call('ZADD',KEYS[7],0,KEYS[4]) end
return 1
`

func (s *store) cloneSnapshotMetadata(baseCtx, ctx context.Context, base, id string) error {
	if err := s.validateSnapshotMetadata(baseCtx, base); err != nil {
		return err
	}
	return wrapRedis(once(ctx, s.client, "EVAL", snapshotMetadataCloneRoot, 5, snapshotCatalogKey, snapshotMetadataMap(base), snapshotMetadataMap(id), snapshotMetadataGraph, snapshotMetadataKeys(base), id, snapshotOf(ctx).pin.token, base).Err(), "failed to share snapshot metadata root")
}

// Called only from the key-position hook. Internal protocol keys bypass this
// resolver, so its Redis calls cannot recursively enter the metadata lock.
func resolveSnapshotMetadata(ctx context.Context, suffix string, writing bool, command ...string) (string, error) {
	scope := snapshotOf(ctx)
	s := scope.store
	scope.metadataMu.Lock()
	defer scope.metadataMu.Unlock()
	m := &scope.metadata
	if m.roots == nil {
		roots, err := s.redis(ctx).HGetAll(ctx, snapshotMetadataMap(scope.id)).Result()
		if err != nil {
			return "", err
		}
		if len(roots) > 256 {
			return "", errorf(adbc.StatusIO, "invalid snapshot metadata root")
		}
		m.roots = roots
		m.buckets = map[string]map[string]string{}
		m.checked = map[string]bool{}
		m.loaded = map[string]bool{}
		m.bucketChecked = map[string]bool{}
		m.ready = map[string]bool{}
		m.building = map[string]string{}
		m.dirty = map[string]bool{}
		m.prune = map[string]bool{}
		m.changed = map[string]bool{}
		if m.reads == nil {
			m.reads = map[string]string{}
		}
	}
	slot := snapshotMetadataSlot(suffix)
	owner := m.roots[slot]
	if owner != "" && !snapshotMetadataID(owner) {
		return "", errorf(adbc.StatusIO, "invalid snapshot metadata bucket identity")
	}
	if scope.write && !m.ready[slot] {
		if source, pending := m.building[slot]; pending {
			if err := s.ownSnapshotMetadataBucket(ctx, slot, source); err != nil {
				return "", err
			}
			owner = m.roots[slot]
		}
	}
	if scope.write && owner == scope.id && !m.ready[slot] {
		if err := s.ownSnapshotMetadataBucket(ctx, slot, owner); err != nil {
			return "", err
		}
	}
	bucket := m.buckets[slot]
	if bucket == nil {
		bucket = map[string]string{}
		m.buckets[slot] = bucket
	}
	if !m.loaded[suffix] {
		if owner != "" {
			var value string
			var err error
			if !m.bucketChecked[slot] {
				p := s.redis(ctx).Pipeline()
				proof := p.HMGet(ctx, snapshotMetadataGraph, "n:"+owner+":"+slot, "b:"+owner+":"+slot)
				count := p.HLen(ctx, snapshotMetadataBucket(owner, slot))
				fields := p.ZCard(ctx, snapshotMetadataDirectory(owner, slot))
				entry := p.HGet(ctx, snapshotMetadataBucket(owner, slot), suffix)
				if _, err := p.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
					return "", err
				}
				values := proof.Val()
				expected, e := strconv.ParseInt(fmt.Sprint(values[0]), 10, 64)
				refs, r := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
				if e != nil || r != nil || refs <= 0 || expected != count.Val() || expected != fields.Val() {
					return "", errorf(adbc.StatusIO, "snapshot metadata bucket is incomplete (valid proof=%t, expected=%d, references=%d, entries=%d, fields=%d)", e == nil && r == nil, expected, refs, count.Val(), fields.Val())
				}
				m.bucketChecked[slot] = true
				value, err = entry.Result()
			} else {
				value, err = s.redis(ctx).HGet(ctx, snapshotMetadataBucket(owner, slot), suffix).Result()
			}
			if err != nil && !errors.Is(err, goredis.Nil) {
				return "", err
			}
			if err == nil {
				bucket[suffix] = value
			}
		}
		m.loaded[suffix] = true
	}
	if _, recorded := m.reads[suffix]; !recorded && !(scope.write && bucket[suffix] == scope.id) {
		m.reads[suffix] = bucket[suffix]
	}
	if writing {
		m.changed[suffix] = true
		if len(command) > 0 {
			switch command[0] {
			case "del", "unlink", "srem", "hdel", "zrem", "expire", "pexpire":
				m.prune[suffix] = true
			}
		}
		if strings.HasPrefix(suffix, "table:") {
			m.dirty[suffix] = true
		}
		if !scope.write {
			return "", errorf(adbc.StatusInvalidState, "published snapshot generations are read-only")
		}
		if owner != scope.id {
			if err := s.ownSnapshotMetadataBucket(ctx, slot, owner); err != nil {
				return "", err
			}
			owner = scope.id
		}
		valueOwner := bucket[suffix]
		if valueOwner != scope.id {
			if valueOwner != "" && !snapshotMetadataID(valueOwner) {
				return "", errorf(adbc.StatusIO, "invalid snapshot metadata value identity")
			}
			source := snapshotWorkspace(valueOwner) + suffix
			target := snapshotWorkspace(scope.id) + suffix
			if err := once(ctx, s.redis(ctx), "EVAL", snapshotMetadataOwnValue, 10, snapshotCatalogKey, snapshotMetadataMap(scope.id), snapshotMetadataBucket(scope.id, slot), source, target, snapshotMetadataGraph, snapshotMetadataValueQueue, snapshotMetadataKeys(scope.id), snapshotDataQueue, snapshotMetadataDirectory(scope.id, slot), scope.id, scope.pin.token, slot, suffix, valueOwner).Err(); err != nil {
				return "", err
			}
			bucket[suffix] = scope.id
		}
		return snapshotWorkspace(scope.id) + suffix, nil
	}
	valueOwner := bucket[suffix]
	if valueOwner == "" {
		return snapshotPrefix + "absent:" + scope.id + ":" + suffix, nil
	}
	if !snapshotMetadataID(valueOwner) {
		return "", errorf(adbc.StatusIO, "invalid snapshot metadata value identity")
	}
	key := snapshotWorkspace(valueOwner) + suffix
	if !(scope.write && valueOwner == scope.id) && !m.checked[key] {
		p := s.redis(ctx).Pipeline()
		e := p.HMGet(ctx, snapshotMetadataGraph, "e:"+key, "v:"+key)
		exists := p.Exists(ctx, key)
		if _, err := p.Exec(ctx); err != nil {
			return "", wrapRedis(err, "snapshot metadata object unavailable")
		}
		proof := e.Val()
		expected, err := strconv.ParseInt(fmt.Sprint(proof[0]), 10, 64)
		refs, referr := strconv.ParseInt(fmt.Sprint(proof[1]), 10, 64)
		if err != nil || referr != nil || refs <= 0 || expected != exists.Val() {
			return "", errorf(adbc.StatusIO, "snapshot metadata object is missing or corrupt")
		}
		m.checked[key] = true
	}
	return key, nil
}

// Root retirement detaches references atomically. Empty-reference buckets and
// objects are drained separately; no surviving manifest needs the old writer.
const snapshotMetadataRetireRoot = `
if KEYS[2]~='` + snapshotPrefix + `metadata:root:'..ARGV[1] or redis.call('HGET',KEYS[1],'root')==ARGV[1] then return redis.error_reply('invalid snapshot metadata retirement') end
local raw=redis.call('HGET',KEYS[1],'w:'..ARGV[1])
if not raw or cjson.decode(raw).state~='deleting' then return redis.error_reply('snapshot metadata retirement not admitted') end
local entries=redis.call('HGETALL',KEYS[2])
for i=1,#entries,2 do
 local id=entries[i+1]..':'..entries[i]
 if tonumber(redis.call('HGET',KEYS[3],'b:'..id) or '0')<=0 then return redis.error_reply('snapshot metadata reference is missing') end
 local build=redis.call('HGET',KEYS[3],'c:'..id)
 if build then
  if entries[i+1]~=ARGV[1] or tonumber(redis.call('HGET',KEYS[3],'b:'..cjson.decode(build).source..':'..entries[i]) or '0')<=0 then return redis.error_reply('snapshot construction source reference is missing') end
 end
end
for i=1,#entries,2 do
 local id=entries[i+1]..':'..entries[i]
 if redis.call('HINCRBY',KEYS[3],'b:'..id,-1)==0 then redis.call('ZADD',KEYS[4],0,id) end
 local build=redis.call('HGET',KEYS[3],'c:'..id)
 if build then
  local source=cjson.decode(build).source..':'..entries[i]
  if redis.call('HINCRBY',KEYS[3],'b:'..source,-1)==0 then redis.call('ZADD',KEYS[4],0,source) end
  redis.call('HDEL',KEYS[3],'c:'..id)
 end
end
redis.call('DEL',KEYS[2]);return 1
`
const snapshotMetadataCollectBucket = `
if not redis.call('ZSCORE',KEYS[3],ARGV[1]) then return 1 end
local refs=redis.call('HGET',KEYS[2],'b:'..ARGV[1])
if not refs then return redis.error_reply('snapshot metadata bucket reference missing') end
if refs and tonumber(refs)>0 then return redis.error_reply('snapshot metadata bucket is referenced') end
local entries=redis.call('HSCAN',KEYS[1],0,'COUNT',128)[2]
local count=math.min(#entries,256)
for i=1,count,2 do
 local key='` + snapshotPrefix + `workspace:'..entries[i+1]..':'..entries[i]
 if tonumber(redis.call('HGET',KEYS[2],'v:'..key) or '0')<=0 then return redis.error_reply('snapshot metadata object reference missing') end
end
for i=1,count,2 do
 local key='` + snapshotPrefix + `workspace:'..entries[i+1]..':'..entries[i]
 if redis.call('HINCRBY',KEYS[2],'v:'..key,-1)==0 then redis.call('ZADD',KEYS[4],0,key) end
 redis.call('HDEL',KEYS[1],entries[i]);redis.call('ZREM',KEYS[5],entries[i])
end
if redis.call('HLEN',KEYS[1])==0 then
 redis.call('DEL',KEYS[5]);redis.call('HDEL',KEYS[2],'b:'..ARGV[1],'n:'..ARGV[1]);redis.call('ZREM',KEYS[3],ARGV[1])
end
return 1
`
const snapshotMetadataCollectValue = snapshotDataEdges + `
if not redis.call('ZSCORE',KEYS[3],KEYS[1]) then return 1 end
local refs=redis.call('HGET',KEYS[2],'v:'..KEYS[1])
if not refs then return redis.error_reply('snapshot metadata object reference missing') end
if refs and tonumber(refs)>0 then return redis.error_reply('snapshot metadata object is referenced') end
local refs=cjson.decode(redis.call('HGET',KEYS[2],'t:'..KEYS[1]) or '[]')
data_releasable(KEYS[2],refs)
data_release(KEYS[2],KEYS[5],refs)
redis.call('DEL',KEYS[1]);redis.call('ZREM',KEYS[4],KEYS[1])
redis.call('HDEL',KEYS[2],'v:'..KEYS[1],'e:'..KEYS[1],'t:'..KEYS[1]);redis.call('ZREM',KEYS[3],KEYS[1]);return 1
`

func (s *store) retireSnapshotMetadata(ctx context.Context, id string) error {
	return once(ctx, s.client, "EVAL", snapshotMetadataRetireRoot, 4, snapshotCatalogKey, snapshotMetadataMap(id), snapshotMetadataGraph, snapshotMetadataBucketQueue, id).Err()
}
func (s *store) collectSnapshotMetadata(ctx context.Context) error {
	ids, err := s.client.ZRange(ctx, snapshotMetadataBucketQueue, 0, 15).Result()
	if err != nil {
		return err
	}
	for _, id := range ids {
		parts := strings.Split(id, ":")
		if len(parts) != 2 || !snapshotMetadataID(parts[0]) || len(parts[1]) != 2 {
			return errorf(adbc.StatusIO, "invalid retiring metadata bucket")
		}
		if _, err := strconv.ParseUint(parts[1], 16, 8); err != nil {
			return err
		}
		if err := once(ctx, s.client, "EVAL", snapshotMetadataCollectBucket, 5, snapshotMetadataBucket(parts[0], parts[1]), snapshotMetadataGraph, snapshotMetadataBucketQueue, snapshotMetadataValueQueue, snapshotMetadataDirectory(parts[0], parts[1]), id).Err(); err != nil {
			return err
		}
	}
	keys, err := s.client.ZRange(ctx, snapshotMetadataValueQueue, 0, 15).Result()
	if err != nil {
		return err
	}
	for _, key := range keys {
		rest, ok := strings.CutPrefix(key, snapshotPrefix+"workspace:")
		if !ok || len(rest) < 34 || rest[32] != ':' || !snapshotMetadataID(rest[:32]) {
			return errorf(adbc.StatusIO, "invalid retiring metadata object")
		}
		if err := once(ctx, s.client, "EVAL", snapshotMetadataCollectValue, 5, key, snapshotMetadataGraph, snapshotMetadataValueQueue, snapshotMetadataKeys(rest[:32]), snapshotDataQueue).Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) snapshotDirtyTables(ctx context.Context) ([]*tableMeta, error) {
	scope := snapshotOf(ctx)
	scope.metadataMu.Lock()
	keys := make([]string, 0, len(scope.metadata.dirty))
	for suffix := range scope.metadata.dirty {
		keys = append(keys, metaPrefix+suffix)
	}
	scope.metadataMu.Unlock()
	var tables []*tableMeta
	for _, key := range keys {
		readAt := time.Now()
		raw, err := s.redis(ctx).Get(ctx, key).Bytes()
		if errors.Is(err, goredis.Nil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var m tableMeta
		if err := json.Unmarshal(raw, &m); err != nil || metaKey(m.Schema, m.Name) != key {
			return nil, errorf(adbc.StatusIO, "invalid private snapshot table descriptor")
		}
		// The scoped GET above already performed native key authorization and
		// metadata ownership checks. Reuse that exact decoded descriptor, while
		// retaining the normal local policy and complete snapshot validation.
		if err := s.hydrateTableMetadata(ctx, &m, readAt); err != nil {
			return nil, err
		}
		if err := s.validateSnapshotTable(ctx, &m); err != nil {
			return nil, err
		}
		tables = append(tables, &m)
	}
	return tables, nil
}

// Deletions remove the mapping itself before publication. Keeping absent-object
// tombstones in every future root would grow metadata with deleted object names.
const snapshotMetadataPruneValue = `
local raw=redis.call('HGET',KEYS[1],'w:'..ARGV[1])
if not raw then return redis.error_reply('snapshot writer lease lost') end
local w=cjson.decode(raw);local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if w.state~='staging' or w.token~=ARGV[2] or w.expires<=now then return redis.error_reply('snapshot writer lease lost') end
if (w.metadata_builds or 0)>0 then return redis.error_reply('snapshot metadata construction is incomplete') end
if KEYS[6]~='` + snapshotPrefix + `metadata:fields:'..ARGV[1]..':'..ARGV[3] or KEYS[2]~='` + snapshotPrefix + `metadata:bucket:'..ARGV[1]..':'..ARGV[3] or KEYS[3]~='` + snapshotPrefix + `workspace:'..ARGV[1]..':'..ARGV[4] then return redis.error_reply('invalid snapshot metadata pruning keys') end
if redis.call('HGET',KEYS[2],ARGV[4])~=ARGV[1] then return 0 end
if redis.call('EXISTS',KEYS[3])~=0 then return 0 end
if redis.call('HGET',KEYS[4],'e:'..KEYS[3])~='0' or tonumber(redis.call('HGET',KEYS[4],'v:'..KEYS[3]) or '0')~=1 then return redis.error_reply('invalid private metadata deletion') end
redis.call('HDEL',KEYS[2],ARGV[4]);redis.call('ZREM',KEYS[6],ARGV[4]);redis.call('HSET',KEYS[4],'v:'..KEYS[3],0,'n:'..ARGV[1]..':'..ARGV[3],redis.call('HLEN',KEYS[2]))
redis.call('ZADD',KEYS[5],0,KEYS[3]);return 1
`

func (s *store) pruneSnapshotMetadata(ctx context.Context) error {
	scope := snapshotOf(ctx)
	scope.metadataMu.Lock()
	defer scope.metadataMu.Unlock()
	for suffix := range scope.metadata.prune {
		slot := snapshotMetadataSlot(suffix)
		n, err := once(ctx, s.redis(ctx), "EVAL", snapshotMetadataPruneValue, 6, snapshotCatalogKey, snapshotMetadataBucket(scope.id, slot), snapshotWorkspace(scope.id)+suffix, snapshotMetadataGraph, snapshotMetadataValueQueue, snapshotMetadataDirectory(scope.id, slot), scope.id, scope.pin.token, slot, suffix).Int()
		if err != nil {
			return err
		}
		if n == 1 {
			delete(scope.metadata.buckets[slot], suffix)
		}
	}
	clear(scope.metadata.prune)
	return nil
}
