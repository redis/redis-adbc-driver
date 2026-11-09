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
	goredis "github.com/redis/go-redis/v9"
)

const snapshotOperationRegistry = snapshotPrefix + "operations"
const snapshotOperationMaxRecords = 128
const snapshotOperationMaxPlan = 16 << 20
const snapshotOperationRetention = time.Hour
const snapshotOperationMaxRetention = 24 * time.Hour

func snapshotOperationKey(id string) string { return snapshotPrefix + "operation:" + id }

// This checkpoint contains evaluated state, never a SQL program, external SCAN
// cursor, unread Arrow stream or connection credentials. Cached metadata owners
// are reloaded from the retained private manifest on takeover.
type snapshotOperationPlan struct {
	Version                      int               `json:"version"`
	Kind                         string            `json:"kind"`
	Phase                        string            `json:"phase"`
	Affected                     int64             `json:"affected"`
	Owned                        map[string]bool   `json:"owned"`
	Reads                        map[string]string `json:"reads"`
	Dirty, Prune, Changed        map[string]bool
	Deferred                     map[string]snapshotOperationIndex `json:"deferred"`
	Bytes, Writes, BatchedWrites int64
	CompactionUpdates, TempDead  string
	Import                       *tableMeta      `json:"import,omitempty"`
	Scanned                      map[string]bool `json:"scanned,omitempty"`
}

type snapshotOperationIndex struct{ Overlay, Deleted bool }

// All owner changes have one metadata-slot linearization point. A record's
// fixed retention deadline is independent of its renewable, exclusive lease.
// Catalog outcome fields survive workspace GC and are bounded by the registry.
const snapshotOperationLua = snapshotImportCaptureProof + snapshotMetadataPublicationProof + `
local catalog,registry,record,map,graph=unpack(KEYS)
local op,id,token,base,retention,plan,kind,affected=unpack(ARGV)
if catalog~='` + snapshotCatalogKey + `' or registry~='` + snapshotOperationRegistry + `' or
 type(id)~='string' or #id~=32 or string.find(id,'[^0-9a-f]') or
 record~='` + snapshotPrefix + `operation:'..id or map~='` + snapshotPrefix + `metadata:root:'..id or graph~='` + snapshotMetadataGraph + `' or redis.call('HGET',catalog,'format')~='17' then
 return redis.error_reply('invalid operation identity or catalog format')
end
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local raw=redis.call('HGET',catalog,'w:'..id)
local w=raw and cjson.decode(raw)
local function owner()
 return w and (w.state=='staging' or w.state=='sealed') and w.token==token and w.expires>now
end
local function inputs_valid()
 return not w.import_pending and (w.metadata_builds or 0)==0 and w.metadata_ready and
  (not w.import_capture or import_capture_reservation(w.import_capture,graph,id))
end
local function checkpoint_valid()
 return inputs_valid() and (not w.operation or not w.operation.pending)
end
if op=='create' or op=='noop' then
 local ttl=tonumber(retention)
 if not ttl or ttl<120000 or ttl>86400000 or ttl%1~=0 or #plan==0 or #plan>16777216 or
  (kind~='sql' and kind~='arrow' and kind~='import') or not tonumber(affected) then
  return redis.error_reply('invalid operation retention or checkpoint')
 end
 if op=='noop' and (kind~='sql' or affected~='0') then return redis.error_reply('invalid no-op preparation') end
 if not owner() or w.base~=base or w.operation then return redis.error_reply('operation writer lease lost') end
 if not checkpoint_valid() then return redis.error_reply('operation input capture is incomplete') end
 local incomplete=snapshot_metadata_publication_error(map,graph)
 if incomplete then return redis.error_reply(incomplete) end
 local pinraw=redis.call('HGET',catalog,'p:'..token)
 local baseraw=redis.call('HGET',catalog,'w:'..base)
 if not pinraw or not baseraw then return redis.error_reply('operation source pin or base is unavailable') end
 local pin=cjson.decode(pinraw)
 if pin.root~=base or pin.expires<=now or cjson.decode(baseraw).state~='committed' then
  return redis.error_reply('operation source pin or base is unavailable')
 end
 local decoded=cjson.decode(plan)
 if decoded.version~=1 or decoded.kind~=kind then return redis.error_reply('invalid operation plan') end
 if redis.call('EXISTS',record)~=0 or redis.call('ZSCORE',registry,id) then return redis.error_reply('operation already registered') end
 local expired=redis.call('ZRANGEBYSCORE',registry,'-inf',now,'LIMIT',0,128)
 for _,old in ipairs(expired) do
  if #old~=32 or string.find(old,'[^0-9a-f]') then return redis.error_reply('invalid operation registry') end
 end
 if redis.call('ZCARD',registry)-#expired>=128 then return redis.error_reply('retained operation limit (128) reached') end
 local deadline=now+ttl
 local initial=tonumber(decoded.Bytes)
 if not initial or initial<0 or initial%1~=0 or initial+#plan>67108864 then return redis.error_reply('operation control budget exceeded') end
 if (kind=='import' and decoded.phase~='output') or (kind~='import' and decoded.phase~='indexes') then
  return redis.error_reply('invalid initial operation phase')
 end
 local operation={deadline=deadline,kind=kind,affected=affected,bytes=initial+#plan,phase=decoded.phase,state='ready',plan_hash=redis.sha1hex(plan)}
 local nextwork=cjson.decode(raw);nextwork.operation=operation;nextwork.expires=0;nextwork.token=''
 if not redis.acl_check_cmd('SET',record,plan,'PXAT',deadline,'NX') or
  not redis.acl_check_cmd('DEL',record) or
  not redis.acl_check_cmd('ZADD',registry,deadline,id) or
  not redis.acl_check_cmd('ZREM',registry,id) or
  not redis.acl_check_cmd('HDEL',catalog,'o:'..id) or
  not redis.acl_check_cmd('HSET',catalog,'w:'..id,cjson.encode(nextwork)) then
  return redis.error_reply('NOPERM operation retention requires private control permissions')
 end
 -- Incomplete retention cannot resume. The plan always has an atomic TTL;
 -- ordinary deferred abort must preserve a successful detached checkpoint.
 for _,old in ipairs(expired) do redis.call('HDEL',catalog,'o:'..old);redis.call('ZREM',registry,old) end
 redis.call('ZADD',registry,deadline,id)
 -- The plan and its TTL are one command: no partial mutation can strand an
 -- unbounded, unregistered plan without expiration.
 redis.call('SET',record,plan,'PXAT',deadline,'NX')
 redis.call('HSET',catalog,'w:'..id,cjson.encode(nextwork))
 if op=='noop' then
  local terminal=cjson.encode({state='committed',deadline=deadline,kind=kind,affected=affected})
  redis.call('HSET',catalog,'w:'..id,cjson.encode({state='deleting'}),'o:'..id,terminal)
  redis.call('DEL',record)
 end
 return deadline
end
local terminal=redis.call('HGET',catalog,'o:'..id)
if terminal then
 local outcome=cjson.decode(terminal)
 if outcome.deadline>now then return {"terminal",terminal} end
 return redis.error_reply('operation retention expired')
end
if not w or not w.operation or w.operation.deadline<=now or redis.call('EXISTS',record)~=1 then
 return redis.error_reply('operation not found or retention expired')
end
local state=w.operation.state
if op=='take' then
 if state~='ready' and state~='running' then return redis.error_reply('operation is abort-only') end
 if not checkpoint_valid() then return redis.error_reply('operation checkpoint is incomplete; abort required') end
 local incomplete=snapshot_metadata_publication_error(map,graph)
 if incomplete then return redis.error_reply(incomplete) end
 if w.expires>now then return redis.error_reply('operation has a live owner') end
 if redis.call('TYPE',record).ok~='string' or redis.call('STRLEN',record)<1 or redis.call('STRLEN',record)>16777216 then return redis.error_reply('operation plan is unavailable') end
 local savedplan=redis.call('GET',record)
 if redis.sha1hex(savedplan)~=w.operation.plan_hash then return redis.error_reply('operation plan integrity proof changed') end
 local root=redis.call('HGET',catalog,'w:'..w.base)
 if not root or cjson.decode(root).state~='committed' then return redis.error_reply('operation base is unavailable') end
 if #token~=32 or string.find(token,'[^0-9a-f]') or redis.call('HEXISTS',catalog,'p:'..token)==1 then return redis.error_reply('invalid operation owner') end
 local entries=redis.call('HGETALL',catalog);local pins=0
 for i=1,#entries,2 do
  if string.sub(entries[i],1,2)=='p:' then
   local pin=cjson.decode(entries[i+1])
   if pin.expires>now then pins=pins+1 else redis.call('HDEL',catalog,entries[i]) end
  end
 end
 if pins>=128 then return redis.error_reply('snapshot pin limit reached') end
 local expiry=math.min(now+120000,w.operation.deadline)
 w.token=token;w.expires=expiry;w.operation.state='running'
 redis.call('HSET',catalog,'w:'..id,cjson.encode(w),'p:'..token,cjson.encode({root=w.base,expires=expiry,writing=true}))
 return {w.base,savedplan}
elseif op=='checkpoint' then
 if not owner() or state~='running' or #plan==0 or #plan>16777216 or not inputs_valid() then
  return redis.error_reply('operation checkpoint requires a valid owner and complete input')
 end
 local decoded=cjson.decode(plan)
 if decoded.version~=1 or decoded.kind~=w.operation.kind then return redis.error_reply('operation plan identity changed') end
 if w.operation.pending and decoded.phase~='sealed' then return redis.error_reply('unsafe operation phase has not completed') end
 if decoded.phase=='sealed' then
  if w.state~='sealed' then return redis.error_reply('operation index preparation is not sealed') end
 elseif decoded.phase=='indexes' then
  if w.operation.phase~='output' or (w.import_capture and w.import_capture.remaining~=0) then
   return redis.error_reply('operation output is not complete')
  end
 else return redis.error_reply('invalid operation checkpoint phase') end
 if w.operation.bytes+#plan>67108864 then return redis.error_reply('operation control budget exceeded') end
 w.operation.bytes=w.operation.bytes+#plan;w.operation.pending='checkpoint'
 redis.call('HSET',catalog,'w:'..id,cjson.encode(w))
 if not redis.call('SET',record,plan,'XX','KEEPTTL') then return redis.error_reply('operation plan expired before checkpoint') end
 w.operation.pending=nil;w.operation.phase=decoded.phase;w.operation.plan_hash=redis.sha1hex(plan)
 redis.call('HSET',catalog,'w:'..id,cjson.encode(w))
 return 1
elseif op=='unsafe' then
 if not owner() or state~='running' or not checkpoint_valid() then return redis.error_reply('operation phase requires a complete owned checkpoint') end
 local phase=cjson.decode(plan).phase
 if phase~='indexing' then return redis.error_reply('invalid unsafe operation phase') end
 w.operation.pending=phase
 redis.call('HSET',catalog,'w:'..id,cjson.encode(w))
 return 1
elseif op=='release' or op=='fail' then
 if not owner() then return 0 end
 local nextstate=(op~='fail' and checkpoint_valid()) and 'ready' or 'abort_only'
 w.token='';w.expires=0;w.operation.state=nextstate
 redis.call('HSET',catalog,'w:'..id,cjson.encode(w))
 return 1
elseif op=='abort' then
 if w.expires>now and w.token~=token then return redis.error_reply('operation has a live owner') end
 local outcome={state='aborted',deadline=w.operation.deadline,kind=w.operation.kind,affected=w.operation.affected}
 -- Outcome and reference retirement become visible in the same command.
 redis.call('HSET',catalog,'w:'..id,cjson.encode({state='deleting'}),'o:'..id,cjson.encode(outcome))
 redis.call('DEL',record)
 return cjson.encode(outcome)
end
return redis.error_reply('unknown operation control action')
`

func (s *store) operationControl(ctx context.Context, op, id, token, base string, retention time.Duration, plan *snapshotOperationPlan) *goredis.Cmd {
	var raw []byte
	var err error
	kind, affected := "", "0"
	if plan != nil {
		raw, err = json.Marshal(plan)
		kind, affected = plan.Kind, strconv.FormatInt(plan.Affected, 10)
	}
	if s.restrictedReader {
		cmd := goredis.NewCmd(ctx)
		cmd.SetErr(errorf(adbc.StatusUnauthorized, "restricted snapshot readers cannot mutate retained operations"))
		return cmd
	}
	if !snapshotMetadataID(id) || err != nil || len(raw) > snapshotOperationMaxPlan {
		cmd := goredis.NewCmd(ctx)
		cmd.SetErr(errorf(adbc.StatusInvalidArgument, "invalid operation identity, permissions or plan"))
		return cmd
	}
	return once(ctx, s.client, "EVAL", snapshotOperationLua, 5, snapshotCatalogKey, snapshotOperationRegistry, snapshotOperationKey(id), snapshotMetadataMap(id), snapshotMetadataGraph, op, id, token, base, retention.Milliseconds(), string(raw), kind, affected)
}

func captureSnapshotOperationPlan(ctx context.Context, kind string, affected int64, importMeta *tableMeta) (*snapshotOperationPlan, error) {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write || scope.pin == nil {
		return nil, errorf(adbc.StatusInvalidState, "operation preparation requires a private writer")
	}
	scope.metadataMu.Lock()
	defer scope.metadataMu.Unlock()
	scope.mu.Lock()
	defer scope.mu.Unlock()
	plan := &snapshotOperationPlan{Version: 1, Kind: kind, Phase: "indexes", Affected: affected, Owned: scope.owned, Reads: scope.metadata.reads, Dirty: scope.metadata.dirty, Prune: scope.metadata.prune, Changed: scope.metadata.changed, Deferred: map[string]snapshotOperationIndex{}, Bytes: scope.bytes, Writes: scope.writes, BatchedWrites: scope.batchedWrites, CompactionUpdates: scope.compactionUpdates, TempDead: scope.tempDead, Import: importMeta}
	plan.Scanned = scope.scannedFinalIndexes
	if importMeta != nil {
		plan.Phase = "output"
	}
	for id, index := range scope.deferredIndexes {
		plan.Deferred[id] = snapshotOperationIndex{index.overlay, index.deleted}
	}
	raw, err := json.Marshal(plan)
	if err != nil || len(raw) > snapshotOperationMaxPlan {
		return nil, errorf(adbc.StatusIO, "operation plan exceeds the 16 MiB retention limit")
	}
	// Detach the saved maps from subsequent in-process changes.
	var copy snapshotOperationPlan
	if err = json.Unmarshal(raw, &copy); err != nil {
		return nil, fmt.Errorf("invalid operation checkpoint: %w", err)
	}
	return &copy, nil
}

// SnapshotOperationStatus reports a retained operation without returning its
// captured values, metadata dependency plan or physical storage locations.
type SnapshotOperationStatus struct {
	Handle        string    `json:"handle"`
	State         string    `json:"state"`
	Kind          string    `json:"kind"`
	Rows          int64     `json:"rows"`
	AffectedRows  int64     `json:"affected_rows"`
	ExpiresAt     time.Time `json:"expires_at"`
	OwnerActive   bool      `json:"owner_active"`
	RemainingRows int64     `json:"remaining_rows"`
}

const snapshotOperationStatusLua = `
local catalog,record=unpack(KEYS)
local id=ARGV[1]
if catalog~='` + snapshotCatalogKey + `' or #id~=32 or string.find(id,'[^0-9a-f]') or
 record~='` + snapshotPrefix + `operation:'..id or redis.call('HGET',catalog,'format')~='17' then
 return redis.error_reply('invalid operation identity or catalog format')
end
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local terminal=redis.call('HGET',catalog,'o:'..id)
if terminal then
 local result=cjson.decode(terminal)
 if result.deadline<=now then return false end
 return terminal
end
local raw=redis.call('HGET',catalog,'w:'..id)
if not raw then return false end
local w=cjson.decode(raw)
if not w.operation or w.operation.deadline<=now or redis.call('EXISTS',record)~=1 then return false end
local state=w.operation.state
if redis.call('TYPE',record).ok~='string' or (w.operation.pending and w.expires<=now) or (w.state~='staging' and w.state~='sealed') or (state~='ready' and state~='running') or w.import_pending then state='abort_only'
elseif w.expires>now then state='running' else state='ready' end
return cjson.encode({state=state,kind=w.operation.kind,affected=w.operation.affected,
 deadline=w.operation.deadline,owner_active=w.expires>now,
 remaining_rows=w.import_capture and w.import_capture.remaining or 0})
`

func decodeSnapshotOperationStatus(id string, raw string) (*SnapshotOperationStatus, error) {
	var wire struct {
		State, Kind, Affected string
		Deadline              int64
		OwnerActive           bool  `json:"owner_active"`
		RemainingRows         int64 `json:"remaining_rows"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return nil, errorf(adbc.StatusIO, "invalid retained operation status")
	}
	rows, err := strconv.ParseInt(wire.Affected, 10, 64)
	if err != nil || wire.Deadline <= 0 {
		return nil, errorf(adbc.StatusIO, "invalid retained operation outcome")
	}
	switch wire.State {
	case "ready", "running", "abort_only", "committed", "aborted":
	default:
		return nil, errorf(adbc.StatusIO, "invalid retained operation state")
	}
	result := &SnapshotOperationStatus{Handle: id, State: wire.State, Kind: wire.Kind, Rows: rows, ExpiresAt: time.UnixMilli(wire.Deadline).UTC(), OwnerActive: wire.OwnerActive, RemainingRows: wire.RemainingRows}
	if wire.State == "committed" {
		result.AffectedRows = rows
	}
	return result, nil
}

func (s *store) snapshotOperationStatus(ctx context.Context, id string) (*SnapshotOperationStatus, error) {
	if !snapshotMetadataID(id) {
		return nil, errorf(adbc.StatusInvalidArgument, "invalid operation handle")
	}
	raw, err := once(ctx, s.client, "EVAL", snapshotOperationStatusLua, 2, snapshotCatalogKey, snapshotOperationKey(id), id).Text()
	if err == goredis.Nil {
		return nil, errorf(adbc.StatusNotFound, "operation not found or retention expired")
	}
	if err != nil {
		return nil, wrapRedis(err, "operation status unavailable")
	}
	return decodeSnapshotOperationStatus(id, raw)
}

func (s *store) retainSnapshotOperation(ctx context.Context, plan *snapshotOperationPlan, retention time.Duration) (string, error) {
	scope := snapshotOf(ctx)
	if scope == nil || scope.pin == nil || plan == nil {
		return "", errorf(adbc.StatusInvalidState, "operation has no private checkpoint")
	}
	// The caller learns the fixed identity even after an unknown retention reply.
	// Its status can be inspected explicitly; capture/SQL is never replayed here.
	id := scope.id
	_, err := s.operationControl(ctx, "create", id, scope.pin.token, scope.pin.root, retention, plan).Int64()
	if err != nil {
		return id, wrapRedis(err, "operation retention outcome unavailable; inspect the handle before further action")
	}
	return id, nil
}

type snapshotOperationOwner struct {
	Context context.Context
	Pin     *snapshotPin
	Plan    *snapshotOperationPlan
	ID      string
}

func (s *store) takeSnapshotOperation(ctx context.Context, id string) (*snapshotOperationOwner, *SnapshotOperationStatus, error) {
	token := snapshotID()
	values, err := s.operationControl(ctx, "take", id, token, "", 0, nil).StringSlice()
	if err != nil {
		return nil, nil, wrapRedis(err, "operation takeover failed; acquisition was not replayed")
	}
	if len(values) != 2 {
		return nil, nil, errorf(adbc.StatusIO, "invalid operation takeover outcome")
	}
	if values[0] == "terminal" {
		outcome, err := decodeSnapshotOperationStatus(id, values[1])
		return nil, outcome, err
	}
	pin := &snapshotPin{ctx: ctx, store: s, token: token, root: values[0], workspace: id, stop: make(chan struct{}), done: make(chan struct{})}
	go pin.heartbeat()
	fail := func(err error) (*snapshotOperationOwner, *SnapshotOperationStatus, error) {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.operationControl(cleanup, "fail", id, token, pin.root, 0, nil).Err()
		cancel()
		pin.release()
		return nil, nil, err
	}
	var plan snapshotOperationPlan
	if len(values[1]) > snapshotOperationMaxPlan || json.Unmarshal([]byte(values[1]), &plan) != nil || plan.Version != 1 || !snapshotMetadataID(pin.root) {
		return fail(errorf(adbc.StatusIO, "invalid retained operation plan"))
	}
	scope := &snapshotScope{id: id, pin: pin, store: s, write: false, operation: true, owned: plan.Owned, bytes: plan.Bytes, writes: plan.Writes, batchedWrites: plan.BatchedWrites, compactionUpdates: plan.CompactionUpdates, tempDead: plan.TempDead, deferredIndexes: map[string]*snapshotIndexState{}}
	scope.metadata.reads, scope.metadata.dirty, scope.metadata.prune, scope.metadata.changed = plan.Reads, plan.Dirty, plan.Prune, plan.Changed
	for data, index := range plan.Deferred {
		if !snapshotMetadataID(data) || !plan.Owned[data] {
			return fail(errorf(adbc.StatusIO, "invalid retained operation index ownership"))
		}
		scope.deferredIndexes[data] = &snapshotIndexState{overlay: index.Overlay, deleted: index.Deleted}
	}
	scope.scannedFinalIndexes = plan.Scanned
	work := context.WithValue(ctx, snapshotContextKey{}, scope)
	if plan.Kind == "import" {
		// Only the retained import plan may write the managed foreign schema.
		// This authorization belonged to the original capture context, not to
		// the connection that later resumes it.
		work = context.WithValue(work, snapshotImportKey{}, true)
	}
	// Reload all changed mappings. Rebase consumes these exact private owners,
	// while the original semantic read set must survive the resolver reload.
	for key := range plan.Changed {
		if _, err := resolveSnapshotMetadata(work, key, false); err != nil {
			return fail(err)
		}
	}
	for slot, owner := range scope.metadata.roots {
		if owner == id {
			scope.metadata.ready[slot] = true
		}
	}
	scope.write = true
	return &snapshotOperationOwner{Context: work, Pin: pin, Plan: &plan, ID: id}, nil, nil
}

// Reserve control-command admission before sending it. Lost quota replies may
// conservatively consume budget, but never cause the protected command to run
// twice. Row images remain charged by their separate bounded batch protocol.
const snapshotOperationChargeLua = `
local raw=redis.call('HGET',KEYS[1],'w:'..ARGV[1])
if not raw then return redis.error_reply('operation writer unavailable') end
local w=cjson.decode(raw)
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local amount=tonumber(ARGV[3])
if not w.operation or (w.state~='staging' and w.state~='sealed') or w.token~=ARGV[2] or w.expires<=now then
 return redis.error_reply('operation owner lease lost')
end
if not amount or amount<0 or amount%1~=0 or w.operation.bytes+amount>67108864 then
 return redis.error_reply('operation control budget exceeded (64 MiB)')
end
w.operation.bytes=w.operation.bytes+amount
redis.call('HSET',KEYS[1],'w:'..ARGV[1],cjson.encode(w))
return 1
`

func chargeSnapshotOperation(ctx context.Context, amount int64) error {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.operation {
		return nil
	}
	_, err := once(ctx, scope.store.client, "EVAL", snapshotOperationChargeLua, 1, snapshotCatalogKey, scope.id, scope.pin.token, amount).Int()
	return wrapRedis(err, "operation control admission failed; protected command was not sent")
}

func (s *store) checkpointSnapshotOperation(owner *snapshotOperationOwner, phase string) error {
	plan, err := captureSnapshotOperationPlan(owner.Context, owner.Plan.Kind, owner.Plan.Affected, nil)
	if err != nil {
		return err
	}
	plan.Phase = phase
	_, err = s.operationControl(owner.Context, "checkpoint", owner.ID, owner.Pin.token, owner.Pin.root, 0, plan).Int()
	if err != nil {
		return wrapRedis(err, "operation checkpoint outcome unavailable; transition was not replayed")
	}
	owner.Plan = plan
	return nil
}

// Enter the non-repeatable native-index phase explicitly. A process/runtime
// interruption before its complete checkpoint is abort-only; a resumer never
// repeats an uncertain FT.CREATE, FT.DROPINDEX or notification sequence.
func (s *store) prepareOperationPublication(owner *snapshotOperationOwner) error {
	ctx := owner.Context
	switch owner.Plan.Phase {
	case "output":
		if owner.Plan.Import == nil {
			return errorf(adbc.StatusIO, "retained import has no captured target")
		}
		meta, err := s.getTable(ctx, owner.Plan.Import.Schema, owner.Plan.Import.Name)
		if err != nil {
			return err
		}
		if meta.prefix() != owner.Plan.Import.prefix() || meta.index() != owner.Plan.Import.index() {
			return errorf(adbc.StatusIO, "captured import layout changed")
		}
		if err := newSnapshotImportSpool(ctx, s, meta).apply(ctx); err != nil {
			return err
		}
		if err := s.checkpointSnapshotOperation(owner, "indexes"); err != nil {
			return err
		}
	case "indexes", "sealed":
	default:
		return errorf(adbc.StatusIO, "invalid retained operation phase")
	}
	if owner.Plan.Phase == "indexes" {
		pending := &snapshotOperationPlan{Version: 1, Kind: owner.Plan.Kind, Phase: "indexing"}
		if _, err := s.operationControl(ctx, "unsafe", owner.ID, owner.Pin.token, owner.Pin.root, 0, pending).Int(); err != nil {
			return wrapRedis(err, "operation index admission failed; native work was not started")
		}
		if err := s.rebuildSnapshotIndexes(ctx); err != nil {
			return err
		}
		if err := s.validateSnapshot(ctx); err != nil {
			return err
		}
		if err := s.pruneSnapshotMetadata(ctx); err != nil {
			return err
		}
		if n, err := s.snapshotControl(ctx, "seal", owner.Pin.token, owner.ID, owner.Pin.root).Int(); err != nil || n != 1 {
			if err != nil {
				return wrapRedis(err, "operation seal outcome unavailable")
			}
			return errorf(adbc.StatusInvalidState, "operation lease lost before sealing")
		}
		if err := s.checkpointSnapshotOperation(owner, "sealed"); err != nil {
			return err
		}
	}
	return s.validatePreparedOperation(ctx)
}

// Read-only revalidation of an already prepared index. Never recreate indexes
// or rewrite descriptors here: a completed checkpoint must survive takeover as
// the same physical layout, including the write-only initial-scan proof.
func (s *store) validatePreparedOperation(ctx context.Context) error {
	tables, err := s.snapshotDirtyTables(ctx)
	if err != nil {
		return err
	}
	for _, meta := range tables {
		if !snapshotOwns(ctx, meta.prefix()) {
			continue
		}
		if meta.SnapshotRows == nil {
			return errorf(adbc.StatusIO, "prepared operation has no final row count")
		}
		count, err := s.client.ZCard(ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Result()
		if err != nil {
			return wrapRedis(err, "prepared operation directory unavailable")
		}
		if count != *meta.SnapshotRows {
			return errorf(adbc.StatusIO, "prepared operation directory count changed")
		}
		for {
			reply, err := s.searchDo(ctx, meta.index(), "FT.INFO", meta.index()).Result()
			if err != nil && !inconsistentIndexState(err) {
				return wrapRedis(err, "prepared operation index unavailable; it was not recreated")
			}
			if err == nil {
				info := indexInfoOf(reply)
				if info.keyType != "HASH" || info.filter != "" || info.failures != 0 || info.numDocs > count || len(info.prefixes) != 1 || info.prefixes[0] != meta.prefix() {
					return errorf(adbc.StatusIO, "prepared operation index proof changed")
				}
				if !info.indexing && info.percent >= 1 && info.numDocs == count {
					scope := snapshotOf(ctx)
					if count == 0 || scope.scannedFinalIndexes[meta.index()] {
						break
					}
					indexed, err := s.snapshotIndexedRowCount(ctx, meta.index())
					if err != nil {
						return wrapRedis(err, "prepared operation indexed-row proof unavailable")
					}
					if indexed > count {
						return errorf(adbc.StatusIO, "prepared operation indexed-row count changed")
					}
					if indexed == count {
						break
					}
				}
			}
			// Recovery may rebuild a persisted native index. Poll its proof read-only;
			// a missing or malformed index is never recreated by continuation.
			select {
			case <-ctx.Done():
				return cancelled(ctx)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	return nil
}
