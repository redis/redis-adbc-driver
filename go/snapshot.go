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
	id            string
	pin           *snapshotPin
	maintenance   bool
	tempDead      string
	write         bool
	owned         map[string]bool
	mu            sync.Mutex
	bytes, writes int64
}

func snapshotOf(ctx context.Context) *snapshotScope {
	s, _ := ctx.Value(snapshotContextKey{}).(*snapshotScope)
	return s
}

func snapshotWorkspace(id string) string    { return snapshotPrefix + "workspace:" + id + ":" }
func snapshotMetadataKeys(id string) string { return snapshotWorkspace(id) + "__keys" }
func snapshotRows(id string) string         { return snapshotPrefix + "rows:" + id + ":" }
func snapshotIndexPrefix(id string) string  { return "adbc_snapshot_{meta}_" + id + "_" }
func snapshotID() string                    { return strings.ReplaceAll(uuid.NewString(), "-", "") }

// The bounded catalog contains at most 16 generations and 128 live pins. Redis
// TIME governs leases. Root acquisition and pin creation, publication/outcome,
// and reclamation claims each have one metadata-slot linearization point.
const snapshotControl = `
local k = KEYS[1]
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
    redis.call('HSET', k, 'format', '6', 'root', id, 'w:'..id, cjson.encode({state='committed',metadata_keys=0}), 'r:'..id, '[]')
  end
  if redis.call('HGET', k, 'format') ~= '6' then return redis.error_reply('unsupported snapshot catalog format') end
  return 1
end
if redis.call('HGET', k, 'format') ~= '6' then return redis.error_reply('missing or unsupported snapshot catalog') end
local entries = redis.call('HGETALL', k)
local pins, works, protected = 0, 0, {}
for n=1,#entries,2 do
  local field = entries[n]
  if string.sub(field,1,2) == 'p:' then
    local pin = cjson.decode(entries[n+1])
    if pin.expires > now then
      pins = pins + 1
      protected[pin.root] = true
    else redis.call('HDEL', k, field) end
  elseif string.sub(field,1,2) == 'w:' then works = works + 1 end
end
if op == 'compact_start' then
  if redis.call('HEXISTS', k, 'compaction') == 1 or works >= 14 or not owns_pin() then return 0 end
  local job = cjson.decode(ARGV[6])
  for _,data in ipairs(job.references) do
    local raw = redis.call('HGET', k, 'd:'..data)
    if not raw or cjson.decode(raw).state ~= 'live' then return 0 end
  end
  if redis.call('HEXISTS', k, 'd:'..job.data) == 1 then return 0 end
  local refs = job.references
  table.insert(refs, job.data)
  redis.call('HSET', k, 'compaction', ARGV[6], 'w:'..id,
    cjson.encode({state='compacting',token=job.token,expires=expiry}),
    'r:'..id,cjson.encode(refs), 'd:'..job.data,cjson.encode({owner=id,state='live'}))
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
elseif op == 'compact_checkpoint' or op == 'compact_release' or op == 'compact_finish' then
  local raw = redis.call('HGET', k, 'compaction')
  if not raw then return 0 end
  local job = cjson.decode(raw)
  local wr = redis.call('HGET', k, 'w:'..id)
  if job.id ~= id or not wr then return 0 end
  local w = cjson.decode(wr)
  if w.state ~= 'compacting' or w.token ~= token or w.expires <= now then return 0 end
  if op == 'compact_checkpoint' then
    w.expires = expiry
    if cjson.decode(ARGV[6]).phase == 'install' then w.publish_until = now + 5000 end
    redis.call('HSET', k, 'compaction',ARGV[6],'w:'..id,cjson.encode(w))
  elseif op == 'compact_release' then
    w.expires, w.publish_until = 0, 0
    redis.call('HSET', k, 'w:'..id,cjson.encode(w))
  else
    redis.call('HSET', k, 'w:'..id,cjson.encode({state='deleting'}))
    redis.call('HDEL', k, 'compaction')
  end
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
elseif op == 'renew' or op == 'valid' then
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
      if w.state ~= 'staging' or w.token ~= token or w.expires <= now then return 0 end
      w.expires = expiry
      redis.call('HSET', k, 'w:'..id, cjson.encode(w))
    end
  end
  return 1
elseif op == 'release' then
  redis.call('HDEL', k, 'p:'..token)
  return 1
elseif op == 'reserve' then
  local prior = redis.call('HGET', k, 'w:'..id)
  if prior then
    local w = cjson.decode(prior)
    if w.state == 'staging' and w.token == token then return 1 end
    return 0
  end
  if works >= 16 then return redis.error_reply('snapshot generation limit (16) reached; release old readers or retry cleanup') end
  if not owns_pin() or redis.call('HGET', k, 'root') ~= base then return 0 end
  redis.call('HSET', k, 'w:'..id, cjson.encode({state='staging',token=token,expires=expiry,base=base}))
  redis.call('HSET', k, 'r:'..id, redis.call('HGET', k, 'r:'..base) or '[]')
  return 1
elseif op == 'own_data' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if not raw or not owns_pin() then return redis.error_reply('snapshot writer lease lost') end
  local w = cjson.decode(raw)
  if w.state ~= 'staging' or w.token ~= token or w.expires <= now then return redis.error_reply('snapshot writer lease lost') end
  local data = ARGV[6]
  local prior = redis.call('HGET', k, 'd:'..data)
  if prior then
    local d = cjson.decode(prior)
    if d.owner == id and d.state == 'live' then return 1 end
    return redis.error_reply('snapshot data generation unavailable')
  end
  local refs = cjson.decode(redis.call('HGET', k, 'r:'..id) or '[]')
  table.insert(refs, data)
  redis.call('HSET', k, 'd:'..data, cjson.encode({owner=id,state='live'}), 'r:'..id, cjson.encode(refs))
  return 1
elseif op == 'publish' or op == 'publish_compaction' or op == 'publish_temp' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if not raw then return -1 end
  local w = cjson.decode(raw)
  if w.state == 'committed' then return 1 end
  if w.state ~= 'staging' or w.token ~= token or w.expires <= now or not owns_pin() then return -1 end
  if redis.call('HGET', k, 'root') ~= base then return 0 end
  if op == 'publish_temp' and redis.call('EXISTS',KEYS[3]) ~= 0 then return -4 end
  if op == 'publish_compaction' then
    for n=1,#entries,2 do
      if string.sub(entries[n],1,2) == 'p:' then
        local pin = cjson.decode(entries[n+1])
        if pin.writing and pin.expires > now then return 0 end
      end
      if string.sub(entries[n],1,2) == 'w:' and entries[n] ~= 'w:'..id then
        local other = cjson.decode(entries[n+1])
        if other.state == 'staging' and other.expires > now then return 0 end
      end
    end
  end
  local refs = cjson.decode(ARGV[6])
  for _,data in ipairs(refs) do
    local d = redis.call('HGET', k, 'd:'..data)
    if not d or cjson.decode(d).state ~= 'live' then return redis.error_reply('snapshot data generation unavailable') end
  end
  redis.call('HSET', k, 'root', id, 'w:'..id, cjson.encode({state='committed',metadata_keys=redis.call('ZCARD',KEYS[2])}), 'r:'..id, ARGV[6])
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
    if w.state == 'staging' and w.token == token then
      redis.call('HSET', k, 'w:'..id, cjson.encode({state='deleting'}))
    end
  end
  return 1
elseif op == 'claim' then
  local root = redis.call('HGET', k, 'root')
  for n=1,#entries,2 do
    local field = entries[n]
    if string.sub(field,1,2) == 'w:' then
      local candidate = string.sub(field,3)
      local w = cjson.decode(entries[n+1])
      if candidate ~= root and not protected[candidate] and w.state ~= 'compacting' and (w.state ~= 'staging' or w.expires <= now) then
        redis.call('HSET', k, field, cjson.encode({state='deleting'}))
        return candidate
      end
    end
  end
  return ''
elseif op == 'reclaimed' then
  local raw = redis.call('HGET', k, 'w:'..id)
  if raw and cjson.decode(raw).state == 'deleting' then redis.call('HDEL', k, 'w:'..id, 'r:'..id) end
  return 1
elseif op == 'claim_data' then
  local referenced = {}
  for n=1,#entries,2 do
    local field = entries[n]
    if string.sub(field,1,2) == 'w:' then
      local w = cjson.decode(entries[n+1])
      if w.state == 'committed' or w.state == 'compacting' or (w.state == 'staging' and w.expires > now) then
        local refs = cjson.decode(redis.call('HGET', k, 'r:'..string.sub(field,3)) or '[]')
        for _,data in ipairs(refs) do referenced[data] = true end
      end
    end
  end
  for n=1,#entries,2 do
    if string.sub(entries[n],1,2) == 'd:' then
      local data = string.sub(entries[n],3)
      if not referenced[data] then
        local d = cjson.decode(entries[n+1])
        d.state = 'deleting'
        redis.call('HSET', k, entries[n], cjson.encode(d))
        return data
      end
    end
  end
  return ''
elseif op == 'reclaimed_data' then
  local raw = redis.call('HGET', k, 'd:'..id)
  if raw and cjson.decode(raw).state == 'deleting' then redis.call('HDEL', k, 'd:'..id) end
  return 1
end
return redis.error_reply('unknown snapshot control operation')
`

func (s *store) snapshotControl(ctx context.Context, op, token, id, base string, extra ...string) *goredis.Cmd {
	// EVAL is sent once. Acquisition/reservation/renewal are idempotent for their
	// tokens; publication resolves lost replies through its durable outcome.
	arg := ""
	if len(extra) != 0 {
		arg = extra[0]
	}
	keys := []any{snapshotCatalogKey}
	if op == "publish" || op == "publish_compaction" || op == "publish_temp" {
		keys = append(keys, snapshotMetadataKeys(id))
	}
	if op == "publish_temp" && len(extra) > 1 {
		keys = append(keys, tempAliveKey(extra[1]))
	}
	args := []any{"EVAL", snapshotControl, len(keys)}
	args = append(args, keys...)
	args = append(args, op, token, id, base, snapshotLease.Milliseconds(), arg)
	return once(ctx, s.client, args...)
}

type snapshotPin struct {
	ctx         context.Context
	store       *store
	token, root string
	mu          sync.Mutex
	workspace   string
	err         error
	stop        chan struct{}
	done        chan struct{}
	once        sync.Once
}

func (s *store) acquireSnapshot(ctx context.Context, writing ...bool) (context.Context, *snapshotPin, error) {
	if s == nil || !s.snapshots || snapshotOf(ctx) != nil {
		return ctx, nil, nil
	}
	s.reapSnapshot(ctx)
	p := &snapshotPin{ctx: ctx, store: s, token: snapshotID(), stop: make(chan struct{}), done: make(chan struct{})}
	mode := ""
	if len(writing) > 0 && writing[0] {
		mode = "write"
	}
	root, err := s.snapshotControl(ctx, "acquire", p.token, "", "", mode).Text()
	if err != nil {
		if strings.Contains(err.Error(), "snapshot compaction publication pending") {
			s.wakeSnapshotCompactor()
			return ctx, nil, stateErrorf("40001", "background compaction is publishing; retry the statement")
		}
		return ctx, nil, wrapRedis(err, "failed to acquire statement snapshot")
	}
	p.root = root
	go p.heartbeat()
	sctx := context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: root, pin: p})
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
	p.mu.Unlock()
	if err != nil {
		return err
	}
	n, err := p.store.snapshotControl(ctx, "valid", p.token, "", p.root).Int()
	if err != nil {
		return wrapRedis(err, "failed to validate statement snapshot")
	}
	if n != 1 {
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
for data in string.gmatch(ARGV[3], '[^,]+') do
  local raw = redis.call('HGET', KEYS[1], 'd:'..data)
  if not raw then return redis.error_reply('snapshot data generation was reclaimed') end
  local d = cjson.decode(raw)
  if d.owner ~= ARGV[1] or d.state ~= 'live' then return redis.error_reply('shared snapshot data is immutable') end
end
local command = string.lower(ARGV[5])
if w.state == 'compacting' then
  local jr = redis.call('HGET', KEYS[1], 'compaction')
  if not jr then return redis.error_reply('compaction job is no longer active') end
  local job = cjson.decode(jr)
  if job.id ~= ARGV[1] or
     (command == 'hset' and job.phase ~= 'copy') or
     (command == 'ft.create' and job.phase ~= 'index') or
     (command ~= 'hset' and command ~= 'ft.create') then
    return redis.error_reply('compaction output is sealed for this operation')
  end
end
`

const snapshotMutation = snapshotMutationAdmission + `
local result = redis.call(unpack(ARGV,5))
local directory = KEYS[#KEYS]
if string.sub(directory,1,string.len('` + snapshotPrefix + `workspace:')) == '` + snapshotPrefix + `workspace:' then
  for i=2,#KEYS-1 do
    if redis.call('EXISTS',KEYS[i]) == 1 then redis.call('ZADD',directory,0,KEYS[i])
    else redis.call('ZREM',directory,KEYS[i]) end
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
	case "set", "setnx", "incr", "incrby", "hset", "hdel", "hincrby", "sadd", "srem", "del", "unlink", "restore", "expire", "pexpire", "persist", "ft.create", "ft.dropindex", "ft.alter":
		// Declare every key, including the original command's keys. All
		// share {meta}; no script discovers an undeclared data key.
		keys := []any{snapshotCatalogKey}
		original := cmd.Args()
		if strings.EqualFold(fmt.Sprint(original[0]), "del") || strings.EqualFold(fmt.Sprint(original[0]), "unlink") {
			keys = append(keys, original[1:]...)
		} else {
			keys = append(keys, original[1])
		}
		member := ""
		if strings.EqualFold(fmt.Sprint(original[0]), "hset") {
			member = snapshotDirectoryMember(fmt.Sprint(original[1]))
			if member != "" {
				keys = append(keys, snapshotMembers(snapshotDataID(fmt.Sprint(original[1]))))
			}
		}
		for _, key := range keys[1:] {
			if strings.HasPrefix(fmt.Sprint(key), snapshotWorkspace(scope.id)) {
				keys = append(keys, snapshotMetadataKeys(scope.id))
				break
			}
		}
		args := []any{"EVAL", snapshotMutation, len(keys)}
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
	case "zrange", "zcard", "get", "set", "setnx", "incr", "incrby", "hget", "hgetall", "hkeys", "hlen", "hexists", "hmget", "hset", "hdel", "hincrby", "sadd", "srem", "smembers", "sismember", "scard", "dump", "restore", "expire", "pexpire", "persist", "ttl", "pttl":
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
		if !ok || !strings.HasPrefix(key, metaPrefix) || strings.HasPrefix(key, snapshotPrefix) || snapshotTempControlKey(key) {
			continue
		}
		if scope == nil {
			return errorf(adbc.StatusInvalidState, "snapshot catalog access requires a statement snapshot")
		}
		args[n] = snapshotWorkspace(scope.id) + strings.TrimPrefix(key, metaPrefix)
	}
	if scope == nil {
		return nil
	}
	write := false
	switch name {
	case "set", "setnx", "incr", "incrby", "hset", "hdel", "hincrby", "sadd", "srem", "del", "unlink", "restore", "expire", "pexpire", "persist", "ft.create", "ft.dropindex", "ft.alter":
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
		if name == "hset" && len(args) > 1 && strings.HasPrefix(fmt.Sprint(args[1]), snapshotPrefix+"rows:") {
			scope.writes++
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
	return tableNames{base: rowPrefix(schema, table), prefix: snapshotRows(id), index: snapshotIndexPrefix(id) + "working", n: 1}, true
}

func (s *store) beginSnapshotWrite(ctx context.Context, pin *snapshotPin) (context.Context, string, error) {
	id := snapshotID()
	n, err := s.snapshotControl(ctx, "reserve", pin.token, id, pin.root).Int()
	if err != nil {
		return ctx, "", wrapRedis(err, "failed to reserve snapshot generation")
	}
	if n != 1 {
		return ctx, "", stateErrorf("40001", "snapshot catalog changed concurrently; retry the statement")
	}
	pin.mu.Lock()
	pin.workspace = id
	pin.mu.Unlock()
	wctx := context.WithValue(ctx, snapshotContextKey{}, &snapshotScope{id: id, pin: pin, write: true, owned: map[string]bool{}})
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
	// settled. Index the final, now immutable HASHes with a fresh initial scan.
	if err := s.rebuildSnapshotIndexes(ctx); err != nil {
		return err
	}
	if err := s.validateSnapshot(ctx); err != nil {
		return err
	}
	refs, err := s.snapshotReferences(ctx)
	if err != nil {
		return err
	}
	operation := "publish"
	if snapshotOf(ctx).maintenance {
		operation = "publish_compaction"
	}
	if snapshotOf(ctx).tempDead != "" {
		operation = "publish_temp"
	}
	n, err := s.snapshotControl(ctx, operation, pin.token, id, pin.root, refs, snapshotOf(ctx).tempDead).Int()
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
	if err := s.validateSnapshotMetadata(baseCtx, base); err != nil {
		return err
	}
	err := s.walkSnapshotDirectory(baseCtx, snapshotMetadataKeys(base), func(keys []string) error {
		for _, key := range keys {
			if !strings.HasPrefix(key, snapshotWorkspace(base)) || key == snapshotMetadataKeys(base) {
				return errorf(adbc.StatusIO, "invalid snapshot metadata directory member")
			}
			raw, err := s.client.Dump(baseCtx, key).Result()
			if err == goredis.Nil {
				return errorf(adbc.StatusIO, "snapshot metadata key is missing")
			}
			if err != nil {
				return wrapRedis(err, "failed to copy snapshot metadata")
			}
			target := snapshotWorkspace(id) + strings.TrimPrefix(key, snapshotWorkspace(base))
			if err := s.client.RestoreReplace(targetCtx, target, 0, raw).Err(); err != nil {
				return wrapRedis(err, "failed to stage snapshot metadata")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return snapshotOf(baseCtx).pin.valid(baseCtx)
}

// prepareSnapshotTable detaches a DML target before planning can capture row
// keys. Read-only source tables continue to use their shared descriptors.
func (s *store) prepareSnapshotTable(ctx context.Context, schema, table string) error {
	return s.prepareSnapshotDelta(ctx, schema, table)
}

func snapshotDataID(key string) string {
	for _, prefix := range []string{snapshotPrefix + "rows:", snapshotPrefix + "members:", "adbc_snapshot_{meta}_"} {
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

func (s *store) ownSnapshotData(ctx context.Context, data string) error {
	scope := snapshotOf(ctx)
	if scope == nil || !scope.write || data == "" {
		return snapshotUnsupported()
	}
	if err := s.snapshotControl(ctx, "own_data", scope.pin.token, scope.id, scope.pin.root, data).Err(); err != nil {
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

func (s *store) snapshotReferences(ctx context.Context) (string, error) {
	schemas, err := s.snapshotSchemas(ctx)
	if err != nil {
		return "", err
	}
	refs := []string{}
	seen := map[string]bool{}
	for _, schema := range schemas {
		tables, err := s.listTables(ctx, schema)
		if err != nil {
			return "", err
		}
		for _, table := range tables {
			meta, err := s.getTable(ctx, schema, table)
			if err != nil {
				return "", err
			}
			for _, seg := range meta.snapshotSegments() {
				id := snapshotDataID(seg.Prefix)
				if id == "" || snapshotDataID(seg.Index) != id {
					return "", errorf(adbc.StatusIO, "invalid table segment descriptor")
				}
				if !seen[id] {
					refs = append(refs, id)
					seen[id] = true
				}
			}
		}
	}
	raw, err := json.Marshal(refs)
	return string(raw), err
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
	schemas, err := s.snapshotSchemas(ctx)
	if err != nil {
		return err
	}
	for _, schema := range schemas {
		tables, err := s.listTables(ctx, schema)
		if err != nil {
			return err
		}
		for _, table := range tables {
			meta, err := s.getTable(ctx, schema, table)
			if err != nil {
				return err
			}
			if !snapshotOwns(ctx, meta.prefix()) {
				continue
			}
			old := meta.index()
			meta.IndexName = snapshotIndexPrefix(snapshotDataID(meta.prefix())) + snapshotID()
			if err := s.searchDo(ctx, meta.index(), snapshotScannedIndexArgs(meta)...).Err(); err != nil {
				return wrapRedis(err, "failed to index final snapshot rows")
			}
			raw, err := json.Marshal(meta)
			if err != nil {
				return err
			}
			pipe := s.client.Pipeline()
			pipe.Set(ctx, metaKey(schema, table), raw, 0)
			pipe.SAdd(ctx, indexesKey, meta.index())
			pipe.SRem(ctx, indexesKey, old)
			if _, err := pipe.Exec(ctx); err != nil {
				return err
			}
			if err := s.searchDo(ctx, old, "FT.DROPINDEX", old).Err(); err != nil && !isUnknownIndex(err) {
				return wrapRedis(err, "failed to retire private working index")
			}
		}
	}
	return nil
}

func (s *store) validateSnapshot(ctx context.Context) error {
	schemas, err := s.snapshotSchemas(ctx)
	if err != nil {
		return err
	}
	for _, schema := range schemas {
		tables, err := s.listTables(ctx, schema)
		if err != nil {
			return err
		}
		for _, table := range tables {
			meta, err := s.getTable(ctx, schema, table)
			if err != nil {
				return err
			}
			if !snapshotOwns(ctx, meta.prefix()) {
				continue
			}
			count, err := s.redis(ctx).ZCard(ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Result()
			if err != nil {
				return wrapRedis(err, "failed to validate snapshot directory")
			}

			for {
				reply, err := s.searchDo(ctx, meta.index(), "FT.INFO", meta.index()).Result()
				if err != nil {
					return wrapRedis(err, "failed to validate snapshot index")
				}
				info := indexInfoOf(reply)
				if info.failures != 0 {
					return errorf(adbc.StatusIO, "snapshot index contains indexing failures")
				}
				if !info.indexing && info.percent >= 1 && info.numDocs == count {
					if len(info.prefixes) != 1 || info.prefixes[0] != meta.prefix() {
						return errorf(adbc.StatusIO, "snapshot index has an unexpected row prefix")
					}
					break
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
			if err := s.client.Set(ctx, metaKey(schema, table), raw, 0).Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *store) validateSnapshotRead(ctx context.Context) error {
	if err := s.validateSnapshotMetadata(ctx, snapshotOf(ctx).id); err != nil {
		return err
	}
	schemas, err := s.snapshotSchemas(ctx)
	if err != nil {
		return err
	}
	for _, schema := range schemas {
		tables, err := s.listTables(ctx, schema)
		if err != nil {
			return err
		}
		for _, table := range tables {
			meta, err := s.getTable(ctx, schema, table)
			if err != nil {
				return err
			}
			if meta.SnapshotRows == nil || meta.Adopted || meta.KeyRows || meta.Foreign || meta.AppIndex || meta.RekeyTo != "" {
				return errorf(adbc.StatusIO, "invalid managed snapshot descriptor")
			}
			if len(meta.SnapshotSegments) >= snapshotMaxSegments || len(meta.SnapshotSegments) > 0 && !meta.SnapshotOverlay {
				return errorf(adbc.StatusIO, "invalid snapshot segment manifest")
			}
			for _, seg := range meta.snapshotSegments() {
				id := snapshotDataID(seg.Prefix)
				if id == "" || snapshotDataID(seg.Index) != id || seg.Rows < 0 {
					return errorf(adbc.StatusIO, "invalid snapshot segment descriptor")
				}
				reply, err := s.searchDo(ctx, seg.Index, "FT.INFO", seg.Index).Result()
				if err != nil {
					return wrapRedis(err, "published snapshot index is unavailable")
				}
				info := indexInfoOf(reply)
				count, err := s.redis(ctx).ZCard(ctx, snapshotMembers(id)).Result()
				if err != nil {
					return wrapRedis(err, "snapshot row directory is unavailable")
				}
				if count != seg.Rows || info.indexing || info.percent < 1 || info.failures != 0 || info.numDocs != seg.Rows || len(info.prefixes) != 1 || info.prefixes[0] != seg.Prefix {
					return errorf(adbc.StatusIO, "published snapshot segment is incomplete or rebuilding")
				}
			}
		}
	}
	return snapshotOf(ctx).pin.valid(ctx)
}

func (s *store) reapSnapshot(parent context.Context) {
	s.reapSnapshotBudget(parent, 5*time.Second)
}
func (s *store) reapSnapshotBudget(parent context.Context, budget time.Duration) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)
	defer cancel()
	id, err := s.snapshotControl(ctx, "claim", "", "", "").Text()
	if err != nil {
		return
	}
	if id != "" {
		if !s.deleteSnapshotKeys(ctx, snapshotWorkspace(id)) {
			return
		}
		if err := s.snapshotControl(ctx, "reclaimed", "", id, "").Err(); err != nil {
			return
		}
	}
	data, err := s.snapshotControl(ctx, "claim_data", "", "", "").Text()
	if err != nil || data == "" {
		return
	}
	indexes, err := s.searchDo(ctx, "", "FT._LIST").Slice()
	if err != nil {
		return
	}
	for _, index := range indexes {
		name := fmt.Sprint(index)
		if strings.HasPrefix(name, snapshotIndexPrefix(data)) {
			if err := s.searchDo(ctx, name, "FT.DROPINDEX", name).Err(); err != nil && !isUnknownIndex(err) {
				return
			}
		}
	}
	if !s.deleteSnapshotKeys(ctx, snapshotRows(data)) {
		return
	}
	if err := s.client.Del(ctx, snapshotMembers(data)).Err(); err != nil {
		return
	}
	_ = s.snapshotControl(ctx, "reclaimed_data", "", data, "").Err()
}

func (s *store) deleteSnapshotKeys(ctx context.Context, prefix string) bool {
	directory := ""
	metadata := strings.HasPrefix(prefix, snapshotPrefix+"workspace:")
	if metadata {
		directory = prefix + "__keys"
	} else {
		directory = snapshotMembers(snapshotDataID(prefix))
	}
	err := s.walkSnapshotDirectory(ctx, directory, func(members []string) error {
		keys := make([]string, 0, len(members))
		for _, member := range members {
			if metadata {
				if !strings.HasPrefix(member, prefix) || member == directory {
					return errorf(adbc.StatusIO, "invalid snapshot metadata directory member")
				}
				keys = append(keys, member)
			} else {
				n, err := strconv.ParseInt(member, 10, 64)
				if err != nil || n < 0 || len(member) != 20 {
					return errorf(adbc.StatusIO, "invalid snapshot row directory member")
				}
				keys = append(keys, prefix+strconv.FormatInt(n, 10))
			}
		}
		if len(keys) == 0 {
			return nil
		}
		return s.client.Del(ctx, keys...).Err()
	})
	if err != nil {
		return false
	}
	return !metadata || s.client.Del(ctx, directory).Err() == nil
}

func (e *executor) executeSnapshot(ctx context.Context, ps ParsedStmt, params []Value, types []ColType, so *streamOpts) (execResult, error) {
	if drop, ok := ps.Stmt.(*DropPartitionResultStmt); ok {
		controlCtx := context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil))
		store, err := newRetainedStore(e.store.client, e.partitionNamespace, defaultRetainedLimits())
		if err != nil {
			return execResult{}, err
		}
		return execResult{affected: 0}, store.revoke(controlCtx, drop.JobID, "")
	}
	write := false
	switch ps.Stmt.(type) {
	case *SelectStmt, *ShowStmt, *SetStmt, *TransactionStmt:
	case *InsertStmt, *UpdateStmt, *DeleteStmt, *MergeStmt, *CreateTableStmt, *DropTableStmt, *CreateViewStmt, *DropViewStmt, *CreateSchemaStmt, *DropSchemaStmt, *TruncateStmt, *CommentStmt, *AlterTableStmt:
		write = true
	default:
		return execResult{}, errorf(adbc.StatusNotImplemented, "statement is not supported in snapshot catalogs")
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	sctx, pin, err := e.store.acquireSnapshot(ctx, write)
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
	if write {
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
