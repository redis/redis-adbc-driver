// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

// Snapshot catalogs use private metadata manifests and shared immutable table
// generations. Legacy metadata commands are scoped to one generation at the client boundary. Row HASHes and
// metadata share {meta}; publication never spans Redis hash slots. Published
// generations are immutable. The ordinary SQL planner and scan implementations
// consequently read precisely the same physical objects for an entire statement.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const (
	// OptionStringStatementSnapshots reports the mandatory managed-table
	// consistency guarantee. It cannot be disabled. See docs/snapshots.md.
	OptionStringStatementSnapshots = "adbc.redis.statement_snapshots"
	snapshotPrefix                 = metaPrefix + "snapshot:"
	snapshotCatalogKey             = snapshotPrefix + "catalog"
	snapshotLease                  = 2 * time.Minute
	snapshotTimeout                = 15 * time.Minute
	snapshotMaxBytes               = 64 << 20
	snapshotMaxWrites              = 100_000
)

type snapshotContextKey struct{}

type snapshotScope struct {
	compactionUpdates   string
	store               *store
	metadataMu          sync.Mutex
	metadata            snapshotMetadataState
	id                  string
	pin                 *snapshotPin
	maintenance         bool
	tempDead            string
	write               bool
	owned               map[string]bool
	deferredIndexes     map[string]*snapshotIndexState // data ID -> final index layout
	scannedFinalIndexes map[string]bool                // compatibility fallback for non-querying writers
	validated           *sync.Map                      // immutable segment descriptor -> *snapshotSegmentValidation
	mu                  sync.Mutex
	rowBatchMu          sync.Mutex
	bytes, writes       int64
	batchedWrites       int64 // acknowledged row images, outside the cumulative command quota
}

func snapshotOf(ctx context.Context) *snapshotScope {
	s, _ := ctx.Value(snapshotContextKey{}).(*snapshotScope)
	return s
}

func snapshotWorkspace(id string) string    { return snapshotPrefix + "workspace:" + id + ":" }
func snapshotMetadataKeys(id string) string { return snapshotWorkspace(id) + "__keys" }
func snapshotIndexPrefix(id string) string  { return "adbc_snapshot_{meta}_" + id + "_" }
func snapshotID() string                    { return strings.ReplaceAll(uuid.NewString(), "-", "") }

// The bounded catalog contains at most 16 generations and 128 live pins. Redis
// TIME governs leases. Root acquisition and pin creation, publication/outcome,
// and reclamation claims each have one metadata-slot linearization point.
const snapshotControl = snapshotDataEdges + `
local k = KEYS[1]
local graph, dataqueue = KEYS[#KEYS-3], KEYS[#KEYS-2]
local compactionqueue, candidates = KEYS[#KEYS-1], KEYS[#KEYS]
local op, token, id, base = ARGV[1], ARGV[2], ARGV[3], ARGV[4]
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
local expiry = now + tonumber(ARGV[5])
local function owns_pin()
  local raw = redis.call('HGET', k, 'p:'..token)
  if not raw then return false end
  local pin = cjson.decode(raw)
  return pin.root == base and pin.expires > now
end
if op == 'init' then
  if redis.call('EXISTS', k) == 0 then
    if ARGV[6] ~= 'import' and redis.call('DBSIZE') ~= 0 then return redis.error_reply('managed-table snapshots require a fresh empty database; existing storage is not upgraded or modified') end
    redis.call('HSET', k, 'format', '15', 'root', id, 'w:'..id, cjson.encode({state='committed',metadata_keys=0,metadata_buckets=0}), 'r:'..id, '[]')
  end
  if redis.call('HGET', k, 'format') ~= '15' then return redis.error_reply('unsupported snapshot catalog format') end
  return 1
end
if redis.call('HGET', k, 'format') ~= '15' then return redis.error_reply('missing or unsupported snapshot catalog') end
-- Lease operations need only their own records. Admission and reclamation
-- below still enumerate/prune expired pins before accounting or collection.
if op == 'renew' or op == 'valid' then
  local raw = redis.call('HGET', k, 'p:'..token)
  if not raw then return 0 end
  local pin = cjson.decode(raw)
  if pin.root ~= base or pin.expires <= now then return 0 end
  if op == 'renew' then
    pin.expires = expiry
    redis.call('HSET', k, 'p:'..token, cjson.encode(pin))
    if id ~= '' then
      local wr = redis.call('HGET', k, 'w:'..id)
      if not wr then return 0 end
      local w = cjson.decode(wr)
      if (w.state ~= 'staging' and w.state ~= 'sealed') or w.token ~= token or w.expires <= now then return 0 end
      w.expires = expiry
      redis.call('HSET', k, 'w:'..id, cjson.encode(w))
    end
  end
  return 1
elseif op == 'release' then
  redis.call('HDEL', k, 'p:'..token)
  return 1
end
local entries = redis.call('HGETALL', k)
local pins, works, protected, owners = 0, 0, {}, {}
for n=1,#entries,2 do
  local field = entries[n]
  if string.sub(field,1,2) == 'p:' then
    local pin = cjson.decode(entries[n+1])
    if pin.expires > now then
      pins = pins + 1
      protected[pin.root] = true
      owners[string.sub(field,3)] = pin.root
    else redis.call('HDEL', k, field) end
  elseif string.sub(field,1,2) == 'w:' then works = works + 1 end
end
if op == 'compact_candidates' then
  local members=redis.call('ZRANGEBYSCORE',compactionqueue,'-inf',now,'LIMIT',0,16)
  local result={}
  for _,key in ipairs(members) do
    local value=redis.call('HGET',candidates,key)
    if not value then return redis.error_reply('compaction candidate payload missing') end
    table.insert(result,key);table.insert(result,value)
  end
  return result
elseif op == 'compact_discard' or op == 'compact_defer' then
  if redis.call('HGET',candidates,id)~=ARGV[6] then return 0 end
  if op=='compact_discard' then redis.call('ZREM',compactionqueue,id);redis.call('HDEL',candidates,id)
  else redis.call('ZADD',compactionqueue,'XX',now+1000,id) end
  return 1
elseif op == 'compact_start' then
  if redis.call('HEXISTS', k, 'compaction') == 1 or works >= 14 or not owns_pin() then return 0 end
  local job = cjson.decode(ARGV[6])
  local candidate=redis.call('HGET',candidates,job.queue_key)
  if not candidate then return 0 end
  candidate=cjson.decode(candidate)
  if candidate.oid~=job.queue_oid or candidate.lineage~=job.source.snapshot_lineage or type(candidate.references)~='table' or #candidate.references<#job.references then return 0 end
  local source_start=job.source_start or 0
  if type(source_start)~='number' or source_start<0 or source_start%1~=0 or
     source_start+#job.references>#candidate.references or
     (source_start>0 and #job.references<2) then return 0 end
  for i,data in ipairs(job.references) do if candidate.references[source_start+i]~=data then return 0 end end
  for _,data in ipairs(job.references) do
    local raw = redis.call('HGET', graph, 'd:'..data)
    if not raw or cjson.decode(raw).state ~= 'live' then return 0 end
  end
  if redis.call('HEXISTS', graph, 'd:'..job.data) == 1 then return 0 end
  data_live(graph,job.references)
  local refs = job.references
  table.insert(refs, job.data)
  if not valid_data_prefix(job.prefix,job.data) then return redis.error_reply('invalid compaction output prefix') end
  redis.call('HSET',graph,'d:'..job.data,cjson.encode({owner=id,state='live',prefix=job.prefix}),'r:'..job.data,0)
  data_retain(graph,refs)
  redis.call('HSET', k, 'compaction', ARGV[6], 'w:'..id,
    cjson.encode({state='compacting',token=job.token,expires=expiry}),
    'r:'..id,cjson.encode(refs))
  -- Retain selective work until publication updates its candidate. An older
  -- worker safely declines non-prefix sources; it must still discover a full
  -- merge rather than strand writers at the hard segment limit.
  if source_start==0 then
    redis.call('ZREM',compactionqueue,job.queue_key);redis.call('HDEL',candidates,job.queue_key)
  end
  return 1
elseif op == 'compact_acquire' then
  local raw = redis.call('HGET', k, 'compaction')
  if not raw then return '' end
  local job = cjson.decode(raw)
  local wr = redis.call('HGET', k, 'w:'..job.id)
  if not wr then return redis.error_reply('compaction ownership record is missing') end
  local w = cjson.decode(wr)
  if w.state ~= 'compacting' then return redis.error_reply('invalid compaction state') end
  if w.expires > now and w.token ~= token then return '' end
  w.token, w.expires, job.token = token, expiry, token
  if job.phase == 'install' then w.publish_until = now + 5000 end
  local result = cjson.encode(job)
  redis.call('HSET', k, 'w:'..job.id,cjson.encode(w),'compaction',result)
  return result
elseif op == 'compact_checkpoint' or op == 'compact_error' or op == 'compact_release' or op == 'compact_finish' then
  local raw = redis.call('HGET', k, 'compaction')
  if not raw then return 0 end
  local job = cjson.decode(raw)
  local wr = redis.call('HGET', k, 'w:'..id)
  if job.id ~= id or not wr then return 0 end
  local w = cjson.decode(wr)
  if w.state ~= 'compacting' or w.token ~= token or w.expires <= now then return 0 end
  if op == 'compact_checkpoint' then
    local update=cjson.decode(ARGV[6])
    local function same_value(a,b)
      if type(a)~=type(b) then return false end
      if type(a)~='table' then return a==b end
      for key,value in pairs(a) do if not same_value(value,b[key]) then return false end end
      for key,_ in pairs(b) do if a[key]==nil then return false end end
      return true
    end
    local phases={copy=1,index=2,install=3}
    local prior_phase,next_phase=phases[job.phase],phases[update.phase]
    if update.id~=job.id or update.token~=token or update.data~=job.data or update.prefix~=job.prefix or
       (update.source_start or 0)~=(job.source_start or 0) or
       not same_value(update.source,job.source) or not same_value(update.references,job.references) or
       update.queue_key~=job.queue_key or update.queue_version~=job.queue_version or update.queue_oid~=job.queue_oid or
       not prior_phase or not next_phase or next_phase<prior_phase or next_phase>prior_phase+1 or
       type(update.after)~='string' or update.after<(job.after or '') or
       job.copy_pending then return 0 end
    w.expires = expiry
    if update.phase == 'install' then w.publish_until = now + 5000 end
    redis.call('HSET', k, 'compaction',ARGV[6],'w:'..id,cjson.encode(w))
  elseif op == 'compact_error' then
    job.last_error=ARGV[6]
    redis.call('HSET',k,'compaction',cjson.encode(job))
  elseif op == 'compact_release' then
    w.expires, w.publish_until = 0, 0
    redis.call('HSET', k, 'w:'..id,cjson.encode(w))
  else
    redis.call('HSET', k, 'w:'..id,cjson.encode({state='deleting'}))
    redis.call('HDEL', k, 'compaction')
  end
  return 1
elseif op == 'retain_partitions' then
  if not owns_pin() then return redis.error_reply('partition producer snapshot expired') end
  if pins >= 128 then return redis.error_reply('snapshot reader limit (128) reached') end
  local until_ms = now + tonumber(ARGV[6])
  redis.call('HSET',k,'p:'..id,cjson.encode({root=base,expires=until_ms,partition=true}))
  return tostring(until_ms)
elseif op == 'acquire_partition' then
  local raw = redis.call('HGET',k,'p:'..id)
  if not raw then return redis.error_reply('partition snapshot expired or released') end
  local group = cjson.decode(raw)
  if not group.partition or group.root ~= base or group.expires <= now then
    return redis.error_reply('partition snapshot expired, released or belongs to another database')
  end
  if pins >= 128 then return redis.error_reply('snapshot reader limit (128) reached') end
  redis.call('HSET',k,'p:'..token,cjson.encode({root=base,expires=expiry}))
  return base
elseif op == 'release_partitions' then
  local raw = redis.call('HGET',k,'p:'..token)
  if raw and cjson.decode(raw).partition then redis.call('HDEL',k,'p:'..token) end
  return 1
elseif op == 'acquire' then
  if ARGV[6] == 'write' then
    local jr = redis.call('HGET', k, 'compaction')
    if jr then
      local job = cjson.decode(jr)
      local wr = redis.call('HGET', k, 'w:'..job.id)
      if job.phase == 'install' and wr then
        local w = cjson.decode(wr)
        if w.state == 'compacting' and w.expires > now and (w.publish_until or 0) > now then
          return redis.error_reply('snapshot compaction publication pending')
        end
      end
    end
  end
  local prior = redis.call('HGET', k, 'p:'..token)
  if prior then return cjson.decode(prior).root end
  if pins >= 128 then return redis.error_reply('snapshot reader limit (128) reached') end
  local root = redis.call('HGET', k, 'root')
  redis.call('HSET', k, 'p:'..token, cjson.encode({root=root,expires=expiry,writing=(ARGV[6] == 'write')}))
  return root
elseif op == 'admit_ingest' then
  if not owns_pin() then return -1 end
  local pin=cjson.decode(redis.call('HGET',k,'p:'..token))
  if not pin.writing then return -1 end
  local target=ARGV[6]
  if #target~=64 or string.find(target,'[^0-9a-f]') then return redis.error_reply('invalid snapshot ingest admission target') end
  if pin.ingest_target and pin.ingest_target~=target then return redis.error_reply('snapshot ingest target already admitted') end
  if redis.call('HGET',k,'root')~=base then return 0 end
  for n=1,#entries,2 do
    if string.sub(entries[n],1,2)=='p:' and entries[n]~='p:'..token then
      local other=cjson.decode(entries[n+1])
      if other.writing and other.expires>now and other.ingest_target==target then return 0 end
    end
  end
  pin.ingest_target=target
  redis.call('HSET',k,'p:'..token,cjson.encode(pin))
  return 1
elseif op == 'reserve' then
  local prior = redis.call('HGET', k, 'w:'..id)
  if prior then
    local w = cjson.decode(prior)
    if (w.state == 'staging' or w.state == 'sealed') and w.token == token then return 1 end
    return 0
  end
  if works >= 16 then return redis.error_reply('snapshot generation limit (16) reached; release old readers or retry cleanup') end
  if not owns_pin() then return 0 end
  redis.call('HSET', k, 'w:'..id, cjson.encode({state='staging',token=token,expires=expiry,base=base}))
  redis.call('HSET', k, 'r:'..id, '[]')
  return 1
elseif op == 'own_data' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if not raw or not owns_pin() then return redis.error_reply('snapshot writer lease lost') end
  local w = cjson.decode(raw)
  if w.state ~= 'staging' or w.token ~= token or w.expires <= now then return redis.error_reply('snapshot writer lease lost') end
  local request=cjson.decode(ARGV[6])
  local data,prefix=request.id,request.prefix
  if not valid_data_prefix(prefix,data) then return redis.error_reply('invalid snapshot data prefix') end
  local prior = redis.call('HGET', graph, 'd:'..data)
  if prior then
    local d = cjson.decode(prior)
    if d.owner == id and d.state == 'live' and d.prefix==prefix then return 1 end
    return redis.error_reply('snapshot data generation unavailable')
  end
  local refs = cjson.decode(redis.call('HGET', k, 'r:'..id) or '[]')
  table.insert(refs, data)
  redis.call('HSET',graph,'d:'..data,cjson.encode({owner=id,state='live',prefix=prefix}),'r:'..data,1)
  redis.call('HSET', k, 'r:'..id,cjson.encode(refs))
  return 1
elseif op == 'seal' then
  local raw=redis.call('HGET',k,'w:'..id)
  if not raw or not owns_pin() then return 0 end
  local w=cjson.decode(raw)
  if (w.state~='staging' and w.state~='sealed') or w.token~=token or w.expires<=now then return 0 end
  if w.import_pending then return redis.error_reply('incomplete import batch cannot publish') end
  if (w.metadata_builds or 0)>0 then return redis.error_reply('snapshot metadata construction is incomplete') end
  w.state='sealed';redis.call('HSET',k,'w:'..id,cjson.encode(w));return 1
elseif op == 'publish' or op == 'publish_compaction' or op == 'publish_temp' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if not raw then return -1 end
  local w = cjson.decode(raw)
  if w.state == 'committed' then return 1 end
  if (w.state ~= 'staging' and w.state ~= 'sealed') or w.token ~= token or w.expires <= now or not owns_pin() then return -1 end
  if w.import_pending then return redis.error_reply('incomplete import batch cannot publish') end
  if (w.metadata_builds or 0)>0 then return redis.error_reply('snapshot metadata construction is incomplete') end
  local queueupdates=cjson.decode(ARGV[7])
  for _,update in ipairs(queueupdates) do
    if type(update.key)~='string' or string.sub(update.key,1,6)~='table:' then return redis.error_reply('invalid compaction queue update') end
    local candidate=update.candidate
    if candidate and (type(candidate)~='table' or candidate.key~=update.key or candidate.version~=id or type(candidate.schema)~='string' or type(candidate.name)~='string' or type(candidate.oid)~='string' or type(candidate.lineage)~='string' or type(candidate.references)~='table' or #candidate.references<4 or #candidate.references>8) then return redis.error_reply('invalid compaction queue candidate') end
    if candidate then for _,data in ipairs(candidate.references) do if type(data)~='string' or #data~=32 or string.find(data,'[^0-9a-f]') then return redis.error_reply('invalid compaction queue data identity') end end end
  end
  -- Seal before the first CAS refusal. The uncontended path seals and commits
  -- in this same operation, without an extra round trip.
  if w.state=='staging' then w.state='sealed';redis.call('HSET',k,'w:'..id,cjson.encode(w)) end
  local merge=nil
  local expected=base
  if ARGV[6]~='' then
    merge=cjson.decode(ARGV[6]);expected=merge.base
    local mr=redis.call('HGET',k,'w:'..merge.id)
    local pr=redis.call('HGET',k,'p:'..merge.token)
    if not mr or not pr then return -1 end
    local mw,mp=cjson.decode(mr),cjson.decode(pr)
    if mw.state~='staging' or mw.base~=merge.base or mw.token~=merge.token or mw.expires<=now or mp.root~=merge.base or mp.expires<=now or not mw.metadata_ready or (mw.metadata_builds or 0)>0 or #(cjson.decode(redis.call('HGET',k,'r:'..merge.id) or '[]'))~=0 then return -1 end
    if KEYS[#KEYS-4]~='` + snapshotPrefix + `metadata:root:'..merge.id then return redis.error_reply('invalid snapshot merge map') end
  end
  if redis.call('HGET', k, 'root') ~= expected then return 0 end
  if op == 'publish_temp' and redis.call('EXISTS',KEYS[4]) ~= 0 then return -4 end
  if op == 'publish_compaction' then
    for n=1,#entries,2 do
      if string.sub(entries[n],1,2) == 'p:' then
        local pin = cjson.decode(entries[n+1])
        if pin.writing and pin.expires > now then return 0 end
      end
      if string.sub(entries[n],1,2) == 'w:' and entries[n] ~= 'w:'..id then
        local other = cjson.decode(entries[n+1])
        if (other.state == 'staging' or other.state == 'sealed') and other.expires > now and (not merge or entries[n]~='w:'..merge.id) then return 0 end
      end
    end
  end
  local refs = cjson.decode(redis.call('HGET',k,'r:'..id) or '[]')
  data_live(graph,refs)
  data_releasable(graph,refs)
  if merge then
    local oldmap=redis.call('HGETALL',KEYS[3])
    local newmap=redis.call('HGETALL',KEYS[#KEYS-4])
    if #oldmap>512 or #newmap>512 then return redis.error_reply('invalid snapshot merge root') end
    for i=1,#oldmap,2 do
      if tonumber(redis.call('HGET',graph,'b:'..oldmap[i+1]..':'..oldmap[i]) or '0')<=0 then return redis.error_reply('snapshot metadata reference missing') end
    end
    for i=1,#newmap,2 do
      if tonumber(redis.call('HGET',graph,'b:'..newmap[i+1]..':'..newmap[i]) or '0')<=0 then return redis.error_reply('snapshot metadata reference missing') end
    end
    for i=1,#oldmap,2 do
      local field=oldmap[i+1]..':'..oldmap[i]
      if redis.call('HINCRBY',graph,'b:'..field,-1)==0 then redis.call('ZADD',KEYS[#KEYS-5],0,field) end
    end
    redis.call('DEL',KEYS[3])
    for i=1,#newmap,2 do redis.call('HSET',KEYS[3],newmap[i],newmap[i+1]) end
    redis.call('DEL',KEYS[#KEYS-4])
    redis.call('HSET',k,'w:'..merge.id,cjson.encode({state='deleting'}))
  end
  redis.call('HSET', k, 'root', id, 'w:'..id, cjson.encode({state='committed',metadata_keys=redis.call('ZCARD',KEYS[2]),metadata_buckets=redis.call('HLEN',KEYS[3])}), 'r:'..id, '[]')
  data_release(graph,dataqueue,refs)
  for _,update in ipairs(queueupdates) do
    if update.candidate then
      redis.call('HSET',candidates,update.key,cjson.encode(update.candidate))
      redis.call('ZADD',compactionqueue,'NX',now,update.key)
    else redis.call('ZREM',compactionqueue,update.key);redis.call('HDEL',candidates,update.key) end
  end
  local pr = redis.call('HGET', k, 'p:'..token)
  if pr then
    local pin = cjson.decode(pr)
    pin.writing = false
    redis.call('HSET', k, 'p:'..token,cjson.encode(pin))
  end
  return 1
elseif op == 'outcome' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if not raw then return 'unknown' end
  return cjson.decode(raw).state
elseif op == 'abort' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if raw then
    local w = cjson.decode(raw)
    if (w.state == 'staging' or w.state == 'sealed') and w.token == token then
      redis.call('HSET', k, 'w:'..id, cjson.encode({state='deleting'}))
    end
  end
  return 1
elseif op == 'claim' or op == 'peek' then
  local root = redis.call('HGET', k, 'root')
  for n=1,#entries,2 do
    local field = entries[n]
    if string.sub(field,1,2) == 'w:' then
      local candidate = string.sub(field,3)
      local w = cjson.decode(entries[n+1])
      -- A lost reservation reply can strand an ownerless pre-clone workspace.
      -- Established work retains its independent lease and reference lifetime,
      -- even if its base pin stops. Claiming atomically fences a late clone.
      -- Durable compaction jobs have separate ownership and remain excluded.
      local abandoned = w.state == 'staging' and not w.metadata_ready and owners[w.token] ~= w.base
      if candidate ~= root and not protected[candidate] and w.state ~= 'compacting' and ((w.state ~= 'staging' and w.state ~= 'sealed') or w.expires <= now or abandoned) then
        if op=='claim' then redis.call('HSET', k, field, cjson.encode({state='deleting'})) end
        return candidate
      end
    end
  end
  return ''
elseif op == 'reclaimed' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if raw and cjson.decode(raw).state == 'deleting' then
    local refs=cjson.decode(redis.call('HGET',k,'r:'..id) or '[]')
    data_releasable(graph,refs)
    data_release(graph,dataqueue,refs)
    redis.call('HDEL', k, 'w:'..id, 'r:'..id)
  end
  return 1
elseif op == 'claim_data' then
  local candidates=redis.call('ZRANGE',dataqueue,0,15)
  for _,data in ipairs(candidates) do
    local raw=redis.call('HGET',graph,'d:'..data)
    local count=tonumber(redis.call('HGET',graph,'r:'..data) or '-1')
    if not raw or count<0 then return redis.error_reply('snapshot data reference missing') end
    if count==0 then
      local d=cjson.decode(raw)
      if not valid_data_prefix(d.prefix,data) then return redis.error_reply('invalid snapshot reclamation prefix') end
      d.state='deleting'
      redis.call('HSET',graph,'d:'..data,cjson.encode(d))
      return d.prefix
    end
    redis.call('ZREM',dataqueue,data)
  end
  return ''
elseif op == 'reclaimed_data' then
  local raw = redis.call('HGET',graph,'d:'..id)
  if raw and cjson.decode(raw).state == 'deleting' then
    redis.call('HDEL',graph,'d:'..id,'r:'..id)
    redis.call('ZREM',dataqueue,id)
  end
  return 1
end
return redis.error_reply('unknown snapshot control operation')
`

func (s *store) snapshotControl(ctx context.Context, op, token, id, base string, extra ...string) *goredis.Cmd {
	// Unknown replies are never resent. A definite digest/command refusal may
	// precede one full-body attempt; publication still resolves its durable outcome.
	arg := ""
	if len(extra) != 0 {
		arg = extra[0]
	}
	keys := []any{snapshotCatalogKey}
	if op == "publish" || op == "publish_compaction" || op == "publish_temp" {
		keys = append(keys, snapshotMetadataKeys(id), snapshotMetadataMap(id))
	}
	if op == "publish_temp" && len(extra) > 1 {
		keys = append(keys, tempAliveKey(extra[1]))
	}
	if len(extra) > 2 && extra[2] != "" {
		keys = append(keys, snapshotMetadataBucketQueue, snapshotMetadataMap(extra[2]))
	}
	keys = append(keys, snapshotMetadataGraph, snapshotDataQueue, snapshotCompactionQueue, snapshotCompactionCandidates)
	updates := "[]"
	if len(extra) > 3 && extra[3] != "" {
		updates = extra[3]
	}
	return s.controlScript.run(ctx, s.client, keys, op, token, id, base, snapshotLease.Milliseconds(), arg, updates)
}

type snapshotPin struct {
	ctx          context.Context
	store        *store
	token, root  string
	mu           sync.Mutex
	workspace    string
	err          error
	materialized bool // under mu; preserve the materialized-reader expiry SQLSTATE
	stop         chan struct{}
	done         chan struct{}
	once         sync.Once
}

func (s *store) acquireSnapshot(ctx context.Context, writing ...bool) (context.Context, *snapshotPin, error) {
	return s.acquireSnapshotMode(ctx, true, writing...)
}

func (s *store) acquireSnapshotMode(ctx context.Context, reap bool, writing ...bool) (context.Context, *snapshotPin, error) {
	if s == nil || !s.snapshots || snapshotOf(ctx) != nil {
		return ctx, nil, nil
	}
	if reap {
		s.reapSnapshot(ctx)
	}
	p := &snapshotPin{ctx: ctx, store: s, token: snapshotID(), stop: make(chan struct{}), done: make(chan struct{})}
	mode := ""
	if len(writing) > 0 && writing[0] {
		mode = "write"
	}
	var root string
	var err error
	delay := 5 * time.Millisecond
	for {
		root, err = s.snapshotControl(ctx, "acquire", p.token, "", "", mode).Text()
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "snapshot compaction publication pending") {
			return ctx, nil, wrapRedis(transportContextError(ctx, err), "failed to acquire statement snapshot")
		}
		// Only a known admission refusal is polled. SQL planning/execution and
		// Arrow consumption have not begun; transport errors are never replayed.
		s.wakeSnapshotCompactor()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx, nil, cancelled(ctx)
		case <-timer.C:
		}
		delay = min(delay*2, 100*time.Millisecond)
	}

	p.root = root
	go p.heartbeat()
	sctx := context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: root, pin: p, store: s})
	if err := s.validateSnapshotRead(sctx); err != nil {
		p.release()
		return ctx, nil, err
	}
	return sctx, p, nil
}

func (p *snapshotPin) heartbeat() {
	defer close(p.done)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.store.snapshotControl(ctx, "release", p.token, "", p.root).Err()
	}()
	tick := time.NewTicker(snapshotLease / 4)
	defer tick.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-p.ctx.Done():
			p.mu.Lock()
			p.err = cancelled(p.ctx)
			p.mu.Unlock()
			return
		case <-tick.C:
			p.mu.Lock()
			id := p.workspace
			p.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			n, err := p.store.snapshotControl(ctx, "renew", p.token, id, p.root).Int()
			cancel()
			if err != nil || n != 1 {
				p.mu.Lock()
				p.err = errorf(adbc.StatusIO, "statement snapshot lease lost; retry the statement")
				if err == nil && id == "" && p.materialized {
					p.err = matviewLeaseExpired()
				}
				p.mu.Unlock()
				return
			}
		}
	}
}

func (p *snapshotPin) valid(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if err := cancelled(ctx); err != nil {
		return err
	}
	p.mu.Lock()
	err := p.err
	materialized := p.materialized
	p.mu.Unlock()
	if err != nil {
		return err
	}
	n, err := p.store.snapshotControl(ctx, "valid", p.token, "", p.root).Int()
	if err != nil {
		return wrapRedis(err, "failed to validate statement snapshot")
	}
	if n != 1 {
		if materialized {
			return matviewLeaseExpired()
		}
		return errorf(adbc.StatusIO, "statement snapshot expired; retry the statement")
	}
	return nil
}

func (p *snapshotPin) release() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		close(p.stop)
		<-p.done
	})
}

// snapshotHook scopes only command key positions, never SQL literals, HASH
// values, set members or script bodies. Retried commands are already scoped.
type snapshotHook struct{ cluster *goredis.ClusterClient }

func (snapshotHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}
func (h snapshotHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) (err error) {
		defer func() {
			if err != nil && h.cluster != nil {
				h.cluster.ReloadState(ctx)
			}
		}()
		if err := scopeSnapshotCommand(ctx, cmd); err != nil {
			cmd.SetErr(err)
			return err
		}
		if snapshotSearchMutation(ctx, cmd) {
			return processSnapshotSearch(ctx, cmd, next)
		}
		return next(ctx, fenceSnapshotCommand(ctx, cmd))
	}
}
func (h snapshotHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) (err error) {
		defer func() {
			if err != nil && h.cluster != nil {
				h.cluster.ReloadState(ctx)
			}
		}()
		hasSearch := false
		for _, cmd := range cmds {
			if err := scopeSnapshotCommand(ctx, cmd); err != nil {
				for _, c := range cmds {
					c.SetErr(err)
				}
				return err
			}
			hasSearch = hasSearch || snapshotSearchMutation(ctx, cmd)
		}
		if hasSearch {
			return processSnapshotSearchPipeline(ctx, cmds, next)
		}
		for i, cmd := range cmds {
			cmds[i] = fenceSnapshotCommand(ctx, cmd)
		}
		return next(ctx, cmds)
	}
}

// Each private mutation checks its writer fence on the server in the same
// operation as the mutation. A client paused past its lease cannot resurrect
// rows after reclamation. Embedding Cmder preserves the original typed reply
// parser (including commands queued by MULTI/EXEC).
type snapshotFencedCommand struct {
	goredis.Cmder
	args []any
}

func (c snapshotFencedCommand) Args() []any      { return c.args }
func (c snapshotFencedCommand) Name() string     { return "eval" }
func (c snapshotFencedCommand) FullName() string { return "eval" }
func (c snapshotFencedCommand) NoRetry() bool    { return true }

const snapshotMutationAdmission = `
local raw = redis.call('HGET', KEYS[1], 'w:'..ARGV[1])
if not raw then return redis.error_reply('snapshot write generation was reclaimed') end
local w = cjson.decode(raw)
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
if (w.state ~= 'staging' and w.state ~= 'compacting') or w.token ~= ARGV[2] or w.expires <= now then
  return redis.error_reply('snapshot writer lease lost')
end
local data_prefixes={}
for data in string.gmatch(ARGV[3], '[^,]+') do
 if not data_prefixes[data] then
  local raw = redis.call('HGET', KEYS[2], 'd:'..data)
  if not raw then return redis.error_reply('snapshot data generation was reclaimed') end
  local d = cjson.decode(raw)
  if d.owner ~= ARGV[1] or d.state ~= 'live' or not valid_data_prefix(d.prefix,data) then return redis.error_reply('shared snapshot data is immutable') end
  data_prefixes[data]=d.prefix
 end
end
for i=4,#KEYS do
 local prefix,data=snapshot_row_key(KEYS[i])
 if prefix then
  if data_prefixes[data]~=prefix then return redis.error_reply('snapshot physical prefix is not owned') end
 end
end
local command = string.lower(ARGV[5])
if command=='ft.create' then
 local data=string.match(ARGV[6],'^adbc_snapshot_{meta}_([0-9a-f]+)_')
 local expected=data and data_prefixes[data]
 local found=false
 for i=7,#ARGV do
  local argument=string.lower(ARGV[i])
  if argument=='schema' then break end
  if argument=='prefix' then
   if not expected or tonumber(ARGV[i+1])~=1 or ARGV[i+2]~=expected then return redis.error_reply('snapshot index physical prefix is not owned') end
   found=true;break
  end
 end
 if not found then return redis.error_reply('snapshot index requires its owned physical prefix') end
end
if w.state == 'compacting' then
  local jr = redis.call('HGET', KEYS[1], 'compaction')
  if not jr then return redis.error_reply('compaction job is no longer active') end
  local job = cjson.decode(jr)
  local early_index=false
  if command=='ft.create' and job.phase=='copy' and (job.after or '')=='' and not job.copy_pending then
    local data=string.match(ARGV[6],'^adbc_snapshot_{meta}_([0-9a-f]+)_')
    local skip=false
    for i=7,#ARGV do
      if string.lower(ARGV[i])=='schema' then break end
      if string.lower(ARGV[i])=='skipinitialscan' then skip=true end
    end
    early_index=data==job.data and skip and KEYS[#KEYS]=='` + snapshotPrefix + `members:'..data and
      redis.call('ZCARD',KEYS[#KEYS])==0
  end
  if job.id ~= ARGV[1] or
     (command == 'hset' and job.phase ~= 'copy') or
     (command == 'ft.create' and job.phase ~= 'index' and not early_index) or
     (command ~= 'hset' and command ~= 'ft.create') then
    return redis.error_reply('compaction output is sealed for this operation')
  end
end
`

const snapshotMutation = snapshotDataEdges + snapshotDescriptorRefs + snapshotMutationAdmission + snapshotMutationExecute

// Row operations do not invoke descriptor helpers. Avoid sending their Lua
// definitions for every HASH write in a large staged input.
const snapshotRowMutation = snapshotRowLayoutLua + snapshotMutationAdmission + snapshotMutationExecute

const snapshotMutationExecute = `
local private='` + snapshotPrefix + `workspace:'..ARGV[1]..':'
for i=4,#KEYS do
 if string.sub(KEYS[i],1,string.len('` + snapshotPrefix + `workspace:'))=='` + snapshotPrefix + `workspace:' and i~=#KEYS then
  if string.sub(KEYS[i],1,string.len(private))~=private then return redis.error_reply('shared snapshot metadata is immutable') end
  if tonumber(redis.call('HGET',KEYS[2],'v:'..KEYS[i]) or '0')<=0 then return redis.error_reply('snapshot metadata object is not registered') end
 end
end
local newrefs,oldrefs
local tablekey=ARGV[6]
if (command=='set' or command=='setnx') and string.sub(tablekey,1,#(private..'table:'))==private..'table:' then
 newrefs=descriptor_refs(ARGV[7],KEYS[2])
 oldrefs=cjson.decode(redis.call('HGET',KEYS[2],'t:'..tablekey) or '[]')
 data_live(KEYS[2],newrefs);data_releasable(KEYS[2],oldrefs)
end
if (w.metadata_builds or 0)>0 and string.sub(KEYS[#KEYS],1,#private)==private then return redis.error_reply('snapshot metadata construction is incomplete') end
local result = redis.call(unpack(ARGV,5))
if newrefs and result and (command~='setnx' or result==1) then
 data_retain(KEYS[2],newrefs);data_release(KEYS[2],KEYS[3],oldrefs)
 redis.call('HSET',KEYS[2],'t:'..tablekey,cjson.encode(newrefs))
end
local directory = KEYS[#KEYS]
if string.sub(directory,1,string.len('` + snapshotPrefix + `workspace:')) == '` + snapshotPrefix + `workspace:' then
  for i=4,#KEYS-1 do
    if string.sub(KEYS[i],1,string.len('` + snapshotPrefix + `workspace:'..ARGV[1]..':')) == '` + snapshotPrefix + `workspace:'..ARGV[1]..':' then
      local present=redis.call('EXISTS',KEYS[i])
      redis.call('HSET',KEYS[2],'e:'..KEYS[i],present)
      if present==1 then redis.call('ZADD',directory,0,KEYS[i]) else redis.call('ZREM',directory,KEYS[i]) end
    end
  end
end
if ARGV[4] ~= '' then redis.call('ZADD', KEYS[#KEYS], 0, ARGV[4]) end
-- Lua numbers cannot represent all int64 replies. Re-read integer counters
-- as bulk strings inside this same fence; IntCmd parses them without rounding.
if command == 'incr' or command == 'incrby' then return redis.call('GET', ARGV[6]) end
if command == 'hincrby' then return redis.call('HGET', ARGV[6], ARGV[7]) end
return result
`

func fenceSnapshotCommand(ctx context.Context, cmd goredis.Cmder) goredis.Cmder {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write {
		return cmd
	}
	switch strings.ToLower(fmt.Sprint(cmd.Args()[0])) {
	case "set", "setnx", "incr", "incrby", "hset", "hdel", "hincrby", "sadd", "srem", "zadd", "zrem", "del", "unlink", "restore", "expire", "pexpire", "persist", "ft.create", "ft.dropindex", "ft.alter":
		// Declare every key, including the original command's keys. All
		// share {meta}; no script discovers an undeclared data key.
		keys := []any{snapshotCatalogKey, snapshotMetadataGraph, snapshotDataQueue}
		original := cmd.Args()
		if strings.EqualFold(fmt.Sprint(original[0]), "del") || strings.EqualFold(fmt.Sprint(original[0]), "unlink") {
			keys = append(keys, original[1:]...)
		} else {
			keys = append(keys, original[1])
		}
		member := ""
		if strings.EqualFold(fmt.Sprint(original[0]), "ft.create") {
			if data := snapshotDataID(fmt.Sprint(original[1])); data != "" {
				keys = append(keys, snapshotMembers(data))
			}
		}
		if strings.EqualFold(fmt.Sprint(original[0]), "hset") {
			member = snapshotDirectoryMember(fmt.Sprint(original[1]))
			if member != "" {
				keys = append(keys, snapshotMembers(snapshotDataID(fmt.Sprint(original[1]))))
			}
		}
		for _, key := range keys[1:] {
			if strings.HasPrefix(fmt.Sprint(key), snapshotPrefix+"workspace:") {
				keys = append(keys, snapshotMetadataKeys(scope.id))
				break
			}
		}
		script := snapshotRowMutation
		if strings.HasPrefix(fmt.Sprint(keys[len(keys)-1]), snapshotPrefix+"workspace:") {
			script = snapshotMutation
		}
		args := []any{"EVAL", script, len(keys)}
		args = append(args, keys...)
		data := []string{}
		for _, key := range keys[1:] {
			if id := snapshotDataID(fmt.Sprint(key)); id != "" {
				data = append(data, id)
			}
		}
		args = append(args, scope.id, scope.pin.token, strings.Join(data, ","), member)
		args = append(args, cmd.Args()...)
		// Cmder's private stringArg belongs to the original typed command;
		// Args serializes EVAL. Route by the original key, not envelope slot 3.
		// All original keys and the control key share the catalog hash tag.
		cmd.SetFirstKeyPos(1)
		return snapshotFencedCommand{Cmder: cmd, args: args}
	}
	return cmd
}

func scopeSnapshotCommand(ctx context.Context, cmd goredis.Cmder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	args := cmd.Args()
	if len(args) < 2 {
		return nil
	}
	name := strings.ToLower(fmt.Sprint(args[0]))
	var positions []int
	switch name {
	case "zrange", "zrangebylex", "zscore", "zlexcount", "zcard", "zadd", "zrem", "get", "getrange", "set", "setnx", "incr", "incrby", "hget", "hgetall", "hkeys", "hlen", "hexists", "hmget", "hset", "hdel", "hincrby", "sadd", "srem", "smembers", "sismember", "scard", "dump", "restore", "expire", "pexpire", "persist", "ttl", "pttl":
		positions = []int{1}
	case "del", "unlink", "exists", "watch", "mget":
		for n := 1; n < len(args); n++ {
			positions = append(positions, n)
		}
	case "scan":
		for n := 2; n+1 < len(args); n++ {
			if strings.EqualFold(fmt.Sprint(args[n]), "match") {
				positions = append(positions, n+1)
			}
		}
	case "eval", "evalsha":
		if len(args) > 2 {
			count, _ := strconv.Atoi(fmt.Sprint(args[2]))
			for n := 3; n < 3+count && n < len(args); n++ {
				positions = append(positions, n)
			}
		}
	case "memory":
		if len(args) > 2 {
			positions = []int{2}
		}
	}
	scope := snapshotOf(ctx)
	for _, n := range positions {
		key, ok := args[n].(string)
		if ok && strings.HasPrefix(key, snapshotPrefix+"rows:") {
			return errorf(adbc.StatusInvalidState, "legacy snapshot row layout is unsupported")
		}
		if !ok || !strings.HasPrefix(key, metaPrefix) || strings.HasPrefix(key, snapshotPrefix) || snapshotTempControlKey(key) {
			continue
		}
		if scope == nil {
			return errorf(adbc.StatusInvalidState, "snapshot catalog access requires a statement snapshot")
		}
		if scope.store != nil {
			writing := false
			switch name {
			case "set", "setnx", "incr", "incrby", "hset", "hdel", "hincrby", "sadd", "srem", "zadd", "zrem", "del", "unlink", "restore", "expire", "pexpire", "persist":
				writing = true
			case "watch":
				// WATCH records an immutable input dependency. Copy only when
				// an actual mutation follows; collision probes must not create
				// permanent absent metadata entries in the new catalog.
				writing = false
			}
			resolved, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(key, metaPrefix), writing, name)
			if err != nil {
				return err
			}
			args[n] = resolved
		} else {
			args[n] = snapshotWorkspace(scope.id) + strings.TrimPrefix(key, metaPrefix)
		}
	}
	if scope == nil {
		return nil
	}
	write := false
	switch name {
	case "set", "setnx", "incr", "incrby", "hset", "hdel", "hincrby", "sadd", "srem", "zadd", "zrem", "del", "unlink", "restore", "expire", "pexpire", "persist", "ft.create", "ft.dropindex", "ft.alter":
		write = true
	}
	if write && !scope.write {
		return errorf(adbc.StatusInvalidState, "published snapshot generations are read-only")
	}
	if write {
		scope.mu.Lock()
		defer scope.mu.Unlock()
		for n := 1; n < len(args); n++ {
			scope.bytes += int64(len(fmt.Sprint(args[n])))
		}
		keys := []int{1}
		if name == "del" || name == "unlink" {
			keys = positions
		}
		for _, n := range keys {
			if data := snapshotDataID(fmt.Sprint(args[n])); data != "" && !scope.owned[data] {
				return errorf(adbc.StatusInvalidState, "shared snapshot data is immutable")
			}
		}
		if name == "hset" && len(args) > 1 && snapshotRowPrefix(fmt.Sprint(args[1])) != "" {
			scope.writes++
			if state := scope.deferredIndexes[snapshotDataID(fmt.Sprint(args[1]))]; state != nil {
				for i := 2; i+1 < len(args); i += 2 {
					if fmt.Sprint(args[i]) == snapshotDeletedField && fmt.Sprint(args[i+1]) == "1" {
						state.deleted = true
					}
				}
			}
			if snapshotDirectoryMember(fmt.Sprint(args[1])) != "" {
				scope.bytes += int64(20 + len(snapshotMembers(snapshotDataID(fmt.Sprint(args[1])))))
			}
		}
		if scope.bytes > snapshotMaxBytes || scope.writes > snapshotMaxWrites {
			return errorf(adbc.StatusIO, "snapshot staging budget exceeded (64 MiB command data or 100000 row writes)")
		}
	}
	return nil
}

func snapshotNames(ctx context.Context, schema, table string) (tableNames, bool) {
	ss := snapshotOf(ctx)
	if ss == nil {
		return tableNames{}, false
	}
	id := snapshotID()
	return tableNames{base: rowPrefix(schema, table), prefix: snapshotRows(id, schema), index: snapshotIndexPrefix(id) + "working", n: 1}, true
}

func (s *store) beginSnapshotWrite(ctx context.Context, pin *snapshotPin) (context.Context, string, error) {
	id := snapshotID()
	n, err := s.snapshotControl(ctx, "reserve", pin.token, id, pin.root).Int()
	capacityRefusal := func(err error) bool {
		var serverErr interface {
			error
			RedisError()
		}
		return errors.As(err, &serverErr) && serverErr.Error() == "snapshot generation limit (16) reached; release old readers or retry cleanup"
	}
	// A different writer may consume the slot freed by our previous retirement.
	// Repeat only definite native refusals, before SQL or Arrow consumption, and
	// bound reclamation by the ledger's existing capacity. The workspace ID stays
	// fixed. A transport error, including a lost successful reply, ends admission.
	for attempt := 0; attempt < 16 && capacityRefusal(err); attempt++ {
		maintenance := context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
		retirement, reapErr := s.reapSnapshotManifestOutcome(maintenance)
		if reapErr != nil {
			return ctx, "", wrapRedis(reapErr, "failed to reclaim snapshot generation for admission")
		}
		// Another collector may already have retired the claimed manifest. Its
		// accounting is not ours, but one fresh reservation after the proven
		// refusal is safe. If no retirement was confirmed, do not keep polling.
		n, err = s.snapshotControl(ctx, "reserve", pin.token, id, pin.root).Int()
		if !retirement.capacityChanged {
			break
		}
	}
	if err != nil {
		return ctx, "", wrapRedis(err, "failed to reserve snapshot generation")
	}
	if n != 1 {
		return ctx, "", stateErrorf("40001", "snapshot catalog changed concurrently; retry the statement")
	}
	pin.mu.Lock()
	pin.workspace = id
	pin.mu.Unlock()
	wctx := context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: id, pin: pin, store: s, write: true, owned: map[string]bool{}, validated: snapshotValidationCache(snapshotOf(ctx))})
	baseScope := snapshotOf(ctx)
	baseScope.metadataMu.Lock()
	snapshotOf(wctx).metadata.reads = map[string]string{}
	for key, owner := range baseScope.metadata.reads {
		snapshotOf(wctx).metadata.reads[key] = owner
	}
	baseScope.metadataMu.Unlock()
	if err := s.copySnapshot(ctx, wctx, pin.root, id); err != nil {
		s.abortSnapshot(pin, id)
		return ctx, "", err
	}

	return wctx, id, nil
}

func (s *store) abortSnapshot(pin *snapshotPin, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.snapshotControl(ctx, "abort", pin.token, id, pin.root).Err()
}

func (s *store) publishSnapshot(ctx context.Context, pin *snapshotPin, id string) error {
	defer s.wakeSnapshotCompactor()
	if err := pin.valid(ctx); err != nil {
		return err
	}
	// Document counts alone cannot establish that incremental updates have
	// settled. Populate a fresh index from the final registered row images and
	// require both readiness metadata and a native indexed-row proof.
	if err := s.rebuildSnapshotIndexes(ctx); err != nil {
		return err
	}
	if err := s.validateSnapshot(ctx); err != nil {
		return err
	}
	if err := s.pruneSnapshotMetadata(ctx); err != nil {
		return err
	}
	operation := "publish"
	if snapshotOf(ctx).maintenance {
		operation = "publish_compaction"
	}
	if snapshotOf(ctx).tempDead != "" {
		operation = "publish_temp"
	}
	n, err := s.snapshotControl(ctx, operation, pin.token, id, pin.root, "", snapshotOf(ctx).tempDead, "", snapshotOf(ctx).compactionUpdates).Int()
	if err == nil && n == 0 {
		n, err = s.rebaseSnapshotPublication(ctx, pin, id, operation)
	}
	if err != nil {
		// Resolve with an independent bounded context, including cancellation
		// racing with EXEC. Never replay the SQL or Arrow input.
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		outcome, e := s.snapshotControl(bg, "outcome", pin.token, id, pin.root).Text()
		if e == nil && outcome == "committed" {
			return nil
		}
		if e != nil || outcome == "unknown" {
			return errorf(adbc.StatusIO, "snapshot publication outcome is unknown (operation %s); do not blindly retry", id)
		}
		return wrapRedis(err, "snapshot publication failed")
	}
	if n == 0 {
		return stateErrorf("40001", "snapshot catalog changed concurrently; retry the statement")
	}
	if n == -4 {
		return errOwnerAlive
	}
	if n != 1 {
		return errorf(adbc.StatusIO, "snapshot write lease expired before publication")
	}
	return nil
}

func (s *store) copySnapshot(baseCtx, targetCtx context.Context, base, id string) error {
	return s.cloneSnapshotMetadata(baseCtx, targetCtx, base, id)
}

// prepareSnapshotTable detaches a DML target before planning can capture row
// keys. Read-only source tables continue to use their shared descriptors.
func (s *store) prepareSnapshotTable(ctx context.Context, schema, table string) error {
	return s.prepareSnapshotDelta(ctx, schema, table)
}

func snapshotDataID(key string) string {
	if prefix := snapshotRowPrefix(key); prefix != "" {
		return prefix[len(prefix)-33 : len(prefix)-1]
	}
	for _, prefix := range []string{snapshotPrefix + "members:", "adbc_snapshot_{meta}_"} {
		if rest, ok := strings.CutPrefix(key, prefix); ok {
			if (len(rest) == 32 && prefix == snapshotPrefix+"members:") || (len(rest) >= 33 && (rest[32] == ':' || rest[32] == '_')) {
				id := rest[:32]
				for _, c := range id {
					if !strings.ContainsRune("0123456789abcdef", c) {
						return ""
					}
				}
				return id
			}
		}
	}
	return ""
}

func (s *store) ownSnapshotData(ctx context.Context, data, prefix string) error {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write || data == "" {
		return snapshotUnsupported()
	}
	request, err := json.Marshal(map[string]string{"id": data, "prefix": prefix})
	if err != nil {
		return err
	}
	if err := s.snapshotControl(ctx, "own_data", scope.pin.token, scope.id, scope.pin.root, string(request)).Err(); err != nil {
		return wrapRedis(err, "failed to reserve table generation")
	}
	scope.mu.Lock()
	scope.owned[data] = true
	scope.mu.Unlock()
	return nil
}

func snapshotOwns(ctx context.Context, key string) bool {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write {
		return false
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	return scope.owned[snapshotDataID(key)]
}

func snapshotScannedIndexArgs(meta *tableMeta) []any {
	args := indexCreateArgs(meta)
	for i, arg := range args {
		if arg == "SKIPINITIALSCAN" {
			return append(args[:i], args[i+1:]...)
		}
	}
	return args
}

func (s *store) rebuildSnapshotIndexes(ctx context.Context) error {
	tables, err := s.snapshotDirtyTables(ctx)
	if err != nil {
		return err
	}
	for _, meta := range tables {
		if !snapshotOwns(ctx, meta.prefix()) {
			continue
		}
		old := meta.index()
		overlay, deferred := snapshotDeferredIndex(ctx, meta)
		if deferred {
			meta.SnapshotOverlay = overlay
		}
		meta.IndexName = snapshotIndexPrefix(snapshotDataID(meta.prefix())) + snapshotID()
		if err := s.searchDo(ctx, meta.index(), indexCreateArgs(meta)...).Err(); err != nil {
			return wrapRedis(err, "failed to index final snapshot rows")
		}
		if err := s.populateSnapshotIndex(ctx, meta); err != nil {
			return err
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		pipe := s.client.Pipeline()
		pipe.Set(ctx, metaKey(meta.Schema, meta.Name), raw, 0)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		if deferred {
			finishSnapshotIndex(ctx, meta)
		} else if err := s.searchDo(ctx, old, "FT.DROPINDEX", old).Err(); err != nil && !isUnknownIndex(err) {
			return wrapRedis(err, "failed to retire private working index")
		}
	}
	return nil
}

func (s *store) validateSnapshot(ctx context.Context) error {
	tables, err := s.snapshotDirtyTables(ctx)
	if err != nil {
		return err
	}
	for _, meta := range tables {
		if !snapshotOwns(ctx, meta.prefix()) {
			continue
		}
		count, err := s.redis(ctx).ZCard(ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Result()
		if err != nil {
			return wrapRedis(err, "failed to validate snapshot directory")
		}

		for {
			reply, err := s.searchDo(ctx, meta.index(), "FT.INFO", meta.index()).Result()
			if err != nil && !inconsistentIndexState(err) {
				var serverErr interface {
					error
					RedisError()
				}
				if errors.As(err, &serverErr) && strings.HasPrefix(serverErr.Error(), "NOPERM ") {
					return wrapErr(adbc.StatusUnauthorized, err, "failed to validate snapshot index")
				}
				return wrapRedis(err, "failed to validate snapshot index")
			}
			if err == nil {
				info := indexInfoOf(reply)
				if info.failures != 0 {
					return errorf(adbc.StatusIO, "snapshot index contains indexing failures")
				}
				if !info.indexing && info.percent >= 1 && info.numDocs == count {
					if len(info.prefixes) != 1 || info.prefixes[0] != meta.prefix() {
						return errorf(adbc.StatusIO, "snapshot index has an unexpected row prefix")
					}
					// Every document is a first insertion into this fresh index,
					// from its complete final HASH image. Query the indexed row-ID
					// field as well as checking the registry's document count.
					scope := snapshotOf(ctx)
					scope.mu.Lock()
					scanned := scope.scannedFinalIndexes[meta.index()]
					scope.mu.Unlock()
					if count == 0 || scanned {
						break
					}
					indexed, err := s.snapshotIndexedRowCount(ctx, meta.index())
					if err != nil {
						var denied interface {
							error
							RedisError()
						}
						if errors.As(err, &denied) && strings.HasPrefix(denied.Error(), "NOPERM ") {
							// Keep write-only principals working with the original
							// initial-scan completion proof. Only a definite permission
							// refusal selects it; unknown query outcomes remain errors.
							if err := s.replaceSnapshotIndexWithScan(ctx, meta); err != nil {
								return err
							}
							continue
						}
						return wrapRedis(transportContextError(ctx, err), "failed to prove final snapshot index")
					}
					if indexed > count {
						return errorf(adbc.StatusIO, "final snapshot index contains unexpected rows")
					}
					if indexed == count {
						break
					}
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
		meta.SnapshotRows = &count
		raw, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		if err := s.client.Set(ctx, metaKey(meta.Schema, meta.Name), raw, 0).Err(); err != nil {
			return err
		}
	}
	return prepareSnapshotCompactionQueue(ctx, tables)
}

// Acquisition checks the committed metadata directory. Row sources are checked
// lazily by getTable, so an unrelated missing/rebuilding index cannot disable
// the whole catalog. Per-statement caching is safe only for immutable segments.
func (s *store) validateSnapshotRead(ctx context.Context) error {
	if err := s.validateSnapshotMetadata(ctx, snapshotOf(ctx).id); err != nil {
		return err
	}
	return snapshotOf(ctx).pin.valid(ctx)
}

// A base read and its private workspace share proofs about immutable inputs.
// Sharing a pointer avoids copying a sync.Map after use and duplicate FT.INFO.
func snapshotValidationCache(scope *snapshotScope) *sync.Map {
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.validated == nil {
		scope.validated = &sync.Map{}
	}
	return scope.validated
}

type snapshotSegmentValidation struct {
	done chan struct{} // closed only after every reply in this proof was checked
	err  error
}

func validateSnapshotDescriptor(ctx context.Context, meta *tableMeta) error {
	scope := snapshotOf(ctx)
	if scope == nil {
		return nil
	}
	own := snapshotOwns(ctx, meta.prefix())
	if (!own && meta.SnapshotRows == nil) || meta.Adopted || meta.KeyRows || meta.Foreign || meta.AppIndex || meta.RekeyTo != "" {
		return errorf(adbc.StatusIO, "invalid managed snapshot descriptor")
	}
	if meta.SnapshotSourceKey && meta.SnapshotImportSource == "" {
		return errorf(adbc.StatusIO, "source-key provenance requires a managed import identity")
	}
	if len(meta.SnapshotSegments) >= snapshotMaxSegments || len(meta.SnapshotSegments) > 0 && !meta.SnapshotOverlay {
		return errorf(adbc.StatusIO, "invalid snapshot segment manifest")
	}
	for _, seg := range meta.snapshotSegments() {
		id := snapshotDataID(seg.Prefix)
		if id == "" || seg.Prefix != snapshotRows(id, meta.Schema) || snapshotDataID(seg.Index) != id || seg.Rows < 0 {
			return errorf(adbc.StatusIO, "invalid snapshot segment descriptor")
		}
	}
	return nil
}

func (s *store) validateSnapshotTable(ctx context.Context, meta *tableMeta) error {
	if err := validateSnapshotDescriptor(ctx, meta); err != nil {
		return err
	}
	scope := snapshotOf(ctx)
	if scope == nil {
		return nil
	}
	cache := snapshotValidationCache(scope)
	var checks []*snapshotSegmentValidation
	var pending []snapshotSegmentProbe
	for _, seg := range meta.snapshotSegments() {
		// Private indexes may be unsettled; writer fences and final seal validate
		// these outputs before publication. Shared inputs still need validation.
		if snapshotOwns(ctx, seg.Prefix) {
			continue
		}
		proof := &snapshotSegmentValidation{done: make(chan struct{})}
		value, loaded := cache.LoadOrStore(seg, proof)
		proof = value.(*snapshotSegmentValidation)
		checks = append(checks, proof)
		if !loaded {
			pending = append(pending, snapshotSegmentProbe{segment: seg, proof: proof})
		}
	}
	// Finish our reservations before waiting for another validator. Concurrent
	// readers can reserve different sources; waiting first could deadlock them.
	if len(pending) != 0 {
		s.validateSnapshotSegments(ctx, pending)
	}
	for _, proof := range checks {
		select {
		case <-proof.done:
			if proof.err != nil {
				return proof.err
			}
		case <-ctx.Done():
			return wrapRedis(ctx.Err(), "snapshot segment validation canceled")
		}
	}
	return nil
}

func (s *store) reapSnapshot(parent context.Context) {
	s.reapSnapshotBudget(parent, 5*time.Second)
}
func (s *store) reapSnapshotBudget(parent context.Context, budget time.Duration) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)
	defer cancel()
	_, _ = s.reapSnapshotStep(ctx) // background maintenance remains best effort
}

type snapshotReapResult struct {
	prefix   string
	deleted  int64
	manifest bool
}

type snapshotManifestRetirement struct {
	reclaimed       bool // this collector completed the manifest deletion
	capacityChanged bool // this or another collector is confirmed to have freed a slot
}

// reapSnapshotManifest frees at most one generation slot. Row/index and bucket
// collection remain separate work; admission must not pay for an entire table's
// reclamation. Only this collector's confirmed retirement counts as progress.
func (s *store) reapSnapshotManifest(ctx context.Context) (bool, error) {
	outcome, err := s.reapSnapshotManifestOutcome(ctx)
	return outcome.reclaimed, err
}

func (s *store) reapSnapshotManifestOutcome(ctx context.Context) (snapshotManifestRetirement, error) {
	id, err := s.snapshotControl(ctx, "claim", "", "", "").Text()
	if err != nil {
		return snapshotManifestRetirement{}, err
	}
	if id != "" {
		if err := s.retireSnapshotMetadata(ctx, id); err != nil {
			if !strings.Contains(err.Error(), "snapshot metadata retirement not admitted") {
				return snapshotManifestRetirement{}, err
			}
			// Another collector can finish after both received the same claim.
			// Confirm its durable outcome; do not replay retirement or hide a
			// missing map while an unreclaimed work record still exists.
			pipe := s.client.Pipeline()
			work := pipe.HExists(ctx, snapshotCatalogKey, "w:"+id)
			root := pipe.Exists(ctx, snapshotMetadataMap(id))
			if _, probeErr := pipe.Exec(ctx); probeErr != nil {
				return snapshotManifestRetirement{}, probeErr
			}
			if work.Val() || root.Val() != 0 {
				return snapshotManifestRetirement{}, err
			}
			// Preserve explicit sweep accounting, but admission may continue its
			// bounded loop if a competitor takes this confirmed freed slot.
			return snapshotManifestRetirement{capacityChanged: true}, nil
		} else {
			if err := s.snapshotControl(ctx, "reclaimed", "", id, "").Err(); err != nil {
				return snapshotManifestRetirement{}, err
			}
			return snapshotManifestRetirement{reclaimed: true, capacityChanged: true}, nil
		}
	}
	return snapshotManifestRetirement{}, nil
}

// Explicit maintenance uses the same ownership/retirement protocol, with errors
// and confirmed deletions returned instead of hidden by background maintenance.
func (s *store) reapSnapshotStep(ctx context.Context) (result snapshotReapResult, err error) {
	result.manifest, err = s.reapSnapshotManifest(ctx)
	if err != nil {
		return result, err
	}
	if err := s.collectSnapshotMetadata(ctx); err != nil {
		return result, err
	}
	prefix, err := s.snapshotControl(ctx, "claim_data", "", "", "").Text()
	if err != nil {
		return result, err
	}
	if prefix == "" {
		return result, nil
	}
	data := snapshotDataID(prefix)
	if data == "" || prefix != snapshotRowPrefix(prefix) {
		return result, errorf(adbc.StatusIO, "invalid snapshot reclamation prefix")
	}
	result.prefix = prefix
	indexes, err := s.searchDo(ctx, "", "FT._LIST").Slice()
	if err != nil {
		return result, err
	}
	for _, index := range indexes {
		name := fmt.Sprint(index)
		if strings.HasPrefix(name, snapshotIndexPrefix(data)) {
			if err := s.searchDo(ctx, name, "FT.DROPINDEX", name).Err(); err != nil && !isUnknownIndex(err) {
				return result, err
			}
		}
	}
	result.deleted, err = s.deleteSnapshotKeys(ctx, prefix)
	if err != nil {
		return result, err
	}
	if err := s.client.Del(ctx, snapshotMembers(data)).Err(); err != nil {
		return result, err
	}
	return result, s.snapshotControl(ctx, "reclaimed_data", "", data, "").Err()
}

func (s *store) deleteSnapshotKeys(ctx context.Context, prefix string) (deleted int64, err error) {
	id := snapshotDataID(prefix)
	if id == "" || prefix != snapshotRowPrefix(prefix) {
		return 0, errorf(adbc.StatusIO, "invalid snapshot reclamation prefix")
	}
	err = s.walkSnapshotDirectory(ctx, snapshotMembers(id), func(members []string) error {
		if len(members) == 0 {
			return nil
		}
		n, err := s.deleteSnapshotRowBatch(ctx, prefix, members)
		if err == nil {
			deleted += n
		}
		return err
	})
	return deleted, err
}

func (e *executor) executeSnapshot(ctx context.Context, ps ParsedStmt, params []Value, types []ColType, so *streamOpts) (execResult, error) {
	if drop, ok := ps.Stmt.(*DropPartitionResultStmt); ok {
		if drop.Snapshot {
			return execResult{affected: 0}, e.store.releasePartitionSnapshot(ctx, drop.JobID)
		}
		controlCtx := context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
		store, err := newRetainedStore(e.store.client, e.partitionNamespace, defaultRetainedLimits())
		if err != nil {
			return execResult{}, err
		}
		return execResult{affected: 0}, store.revoke(controlCtx, drop.JobID, "")
	}
	write := false
	switch ps.Stmt.(type) {
	case *PrivilegeStmt:
		return execResult{}, errorf(adbc.StatusNotImplemented, "SQL GRANT/REVOKE requires isolated snapshot reader leases; management ACL changes are not enabled for snapshot catalogs")
	case *SelectStmt, *ShowStmt, *SetStmt, *TransactionStmt:
	case *InsertStmt, *UpdateStmt, *DeleteStmt, *MergeStmt, *CreateTableStmt, *DropTableStmt, *CreateViewStmt, *DropViewStmt, *CreateSchemaStmt, *DropSchemaStmt, *TruncateStmt, *CommentStmt, *AlterTableStmt, *SequenceStmt, *CreateIndexStmt, *DropIndexStmt, *RefreshMatViewStmt, *DropMatViewStmt, *RenameMatViewStmt:
		write = true
	default:
		return execResult{}, errorf(adbc.StatusNotImplemented, "statement is not supported in snapshot catalogs")
	}
	for _, cte := range statementWith(ps.Stmt) {
		write = write || cte.Statement != nil
	}
	var target *TableName
	switch stmt := ps.Stmt.(type) {
	case *InsertStmt:
		target = &stmt.Table
	case *UpdateStmt:
		target = &stmt.Table
	case *DeleteStmt:
		target = &stmt.Table
	case *MergeStmt:
		target = &stmt.Table
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	var sctx context.Context
	var pin *snapshotPin
	var err error
	if write {
		var resolve func(context.Context) (*tableMeta, error)
		if target != nil {
			resolve = func(c context.Context) (*tableMeta, error) { return e.snapshotWriteTarget(c, target) }
		}
		sctx, pin, err = e.store.acquireWritableSnapshot(ctx, resolve)
	} else {
		sctx, pin, err = e.store.acquireSnapshot(ctx)
	}
	if err != nil {
		cancel()
		return execResult{}, err
	}
	defer func() {
		if pin != nil {
			pin.release()
			cancel()
		}
	}()
	if err := e.store.refreshSnapshotTemp(sctx); err != nil {
		return execResult{}, err
	}
	baseCtx := sctx
	published := false
	defer func() {
		if write && !published {
			_ = e.store.refreshSnapshotTemp(baseCtx)
		}
	}()
	id := ""
	if write {
		sctx, id, err = e.store.beginSnapshotWrite(sctx, pin)
		if err != nil {
			return execResult{}, err
		}
		defer e.store.abortSnapshot(pin, id)

		if target != nil {
			schema, table, err := e.resolveTable(*target)
			if err != nil {
				return execResult{}, err
			}
			if err := e.store.prepareSnapshotTable(sctx, schema, table); err != nil {
				return execResult{}, err
			}
		}
	}
	res, err := e.execute(sctx, ps, params, types, so)
	if err != nil {
		return execResult{}, err
	}
	if write && !snapshotNoopDML(sctx, ps) {
		err = e.store.publishSnapshot(sctx, pin, id)
		published = err == nil
	} else {
		err = pin.valid(sctx)
	}
	if err != nil {
		if res.stream != nil {
			res.stream.Release()
		}
		return execResult{}, err
	}
	// Small Arrow-native results are detached, fully materialized readers and
	// no longer read Redis. Only live streams must retain the generation.
	if r, ok := res.stream.(*streamReader); ok {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			r.Release()
			return execResult{}, errorf(adbc.StatusCancelled, "snapshot stream closed during execution")
		}
		r.snapshot = pin
		r.snapshotCancel = cancel
		r.snapshotStop = context.AfterFunc(sctx, func() { r.closeWith(cancelled(sctx)) })
		pin = nil // stream owns both lease and deadline
		r.mu.Unlock()
	}
	return res, nil
}

func snapshotUnsupported() error {
	return errorf(adbc.StatusNotImplemented, "operation is not supported in snapshot catalogs")
}

// RESP3 Search carries partial-result warnings beside results; cursors wrap
// that map with a cursor ID. Such a reply is never a successful snapshot read.
func snapshotReplyWarning(reply any) bool {
	if fields, ok := redisReplyMap(reply); ok {
		for _, key := range []string{"warning", "warnings"} {
			if value, present := fields[key]; present && value != nil && !noWarning(value) {
				return true
			}
		}
	}
	if list, ok := reply.([]any); ok && len(list) == 2 {
		return snapshotReplyWarning(list[0])
	}
	return false
}

// openSnapshotCatalog runs before legacy migration/recovery or any driver write.
func (s *store) openSnapshotCatalog(ctx context.Context, enabled bool, importing ...bool) error {
	allowExternal := len(importing) > 0 && importing[0]
	n, err := s.client.Exists(ctx, snapshotCatalogKey).Result()
	if err != nil {
		return err
	}
	if !enabled {
		if n != 0 {
			return errorf(adbc.StatusInvalidState, "database uses the statement-snapshot storage format")
		}
		return nil
	}
	if n == 0 && !allowExternal {
		indexes, err := s.searchDo(ctx, "", "FT._LIST").Slice()
		if err != nil {
			return err
		}
		if len(indexes) != 0 {
			return errorf(adbc.StatusInvalidState, "snapshot initialization requires a database without existing search indexes")
		}
	}
	if err := s.checkSnapshotDeployment(ctx, n == 0 && !allowExternal); err != nil {
		return err
	}
	mode := ""
	if n == 0 && allowExternal {
		if err := s.checkSnapshotImportNamespace(ctx); err != nil {
			return err
		}
		mode = "import"
	}
	if err := s.snapshotControl(ctx, "init", "", snapshotID(), "", mode).Err(); err != nil {
		return wrapRedis(err, "failed to initialize snapshot catalog")
	}
	s.snapshots = true
	return addSnapshotHooks(s.client)(ctx)
}
