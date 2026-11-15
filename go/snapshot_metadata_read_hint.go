// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const snapshotMetadataHintBytes = 64 << 10
const snapshotMetadataHintValueBytes = 1 << 20

type snapshotMetadataReadHint struct{ bucket, value string }

// Hints contain only physical owner IDs, never descriptors, permissions or proof
// results. Every use freshly verifies the complete address and native proofs.
// Entry count and retained suffix/ID bytes are bounded independently.
type snapshotMetadataReadHints struct {
	mu      sync.Mutex
	entries map[string]snapshotMetadataReadHint
	order   []string
	bytes   int
}

func (h *snapshotMetadataReadHints) get(suffix string) (snapshotMetadataReadHint, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	value, ok := h.entries[suffix]
	return value, ok
}
func (h *snapshotMetadataReadHints) put(suffix string, value snapshotMetadataReadHint) {
	if len(suffix) > 1024 || !snapshotMetadataID(value.bucket) || !snapshotMetadataID(value.value) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = map[string]snapshotMetadataReadHint{}
	}
	if _, ok := h.entries[suffix]; ok {
		h.entries[suffix] = value
		return
	}
	size := len(suffix) + len(value.bucket) + len(value.value) + 64
	for len(h.order) >= 64 || h.bytes+size > snapshotMetadataHintBytes {
		oldest := h.order[0]
		entry := h.entries[oldest]
		h.bytes -= len(oldest) + len(entry.bucket) + len(entry.value) + 64
		delete(h.entries, oldest)
		h.order[0] = "" // do not retain evicted suffix bytes in the backing array
		h.order = h.order[1:]
	}
	h.entries[suffix] = value
	h.order = append(h.order, suffix)
	h.bytes += size
}

func snapshotMetadataHintEligible(s *store, scope *snapshotScope, key string) bool {
	if s == nil || !s.snapshots || s.restrictedReader || scope == nil || scope.store != s ||
		scope.write || scope.operation || scope.maintenance || scope.pin == nil || scope.pin.store != s ||
		!scope.pin.descriptorHint || scope.pin.reader || scope.pin.shared != nil || scope.pin.root != scope.id ||
		!snapshotMetadataID(scope.id) || !strings.HasPrefix(key, metaPrefix+"table:") || len(key)-len(metaPrefix) > 1024 {
		return false
	}
	scope.pin.mu.Lock()
	materialized := scope.pin.materialized
	scope.pin.mu.Unlock()
	return !materialized
}
func (s *store) rememberSnapshotMetadataHint(ctx context.Context, key string, bytes int) {
	scope := snapshotOf(ctx)
	if bytes > snapshotMetadataHintValueBytes || !snapshotMetadataHintEligible(s, scope, key) {
		return
	}
	suffix := strings.TrimPrefix(key, metaPrefix)
	slot := snapshotMetadataSlot(suffix)
	scope.metadataMu.Lock()
	hint := snapshotMetadataReadHint{scope.metadata.roots[slot], scope.metadata.buckets[slot][suffix]}
	scope.metadataMu.Unlock()
	s.metadataReadHints.put(suffix, hint)
}

// All physical keys are declared. An untrusted prior address is useful only if
// the current pinned root and bucket still name it. A mismatch performs no GET.
// Native command errors are returned as data so an inner NOPERM cannot be
// mistaken for a definite top-level EVAL key/command refusal.
const snapshotMetadataReadHintLua = `
if #KEYS~=5 or #ARGV~=6 then return redis.error_reply('invalid snapshot metadata read') end
local root,slot,suffix,bucket,value,limit=ARGV[1],ARGV[2],ARGV[3],ARGV[4],ARGV[5],tonumber(ARGV[6])
local function id(v) return #v==32 and string.match(v,'^%x+$') end
if not id(root) or not id(bucket) or not id(value) or #slot~=2 or not string.match(slot,'^%x+$') or
 string.sub(suffix,1,6)~='table:' or #suffix>1024 or limit~=1048576 or
 KEYS[1]~='` + snapshotPrefix + `metadata:root:'..root or KEYS[2]~='` + snapshotMetadataGraph + `' or
 KEYS[3]~='` + snapshotPrefix + `metadata:bucket:'..bucket..':'..slot or
 KEYS[4]~='` + snapshotPrefix + `metadata:fields:'..bucket..':'..slot or
 KEYS[5]~='` + snapshotPrefix + `workspace:'..value..':'..suffix then
 return redis.error_reply('invalid snapshot metadata read keys')
end
local function call(...) local r=redis.pcall(...);if type(r)=='table' and r.err then return false,r.err end;return r,false end
local n,e=call('HLEN',KEYS[1]);if e then
 if string.sub(e,1,7)=='NOPERM ' then return {'decline'} end
 return {'native',e}
end
if n>256 then return {'root'} end
local entries;entries,e=call('HGETALL',KEYS[1]);if e then return {'native',e} end
local current=false
for i=1,#entries,2 do if entries[i]==slot then current=entries[i+1] end end
if current~=bucket then return {'miss'} end
local bp;bp,e=call('HMGET',KEYS[2],'n:'..bucket..':'..slot,'b:'..bucket..':'..slot);if e then return {'native',e} end
local count;count,e=call('HLEN',KEYS[3]);if e then return {'native',e} end
local fields;fields,e=call('ZCARD',KEYS[4]);if e then return {'native',e} end
local owner;owner,e=call('HGET',KEYS[3],suffix);if e then return {'native',e} end
if owner~=value then return {'miss'} end
local vp;vp,e=call('HMGET',KEYS[2],'e:'..KEYS[5],'v:'..KEYS[5]);if e then return {'native',e} end
local present;present,e=call('EXISTS',KEYS[5]);if e then return {'native',e} end
local kind,payload='value',false
local length;length,e=call('STRLEN',KEYS[5])
if e then
 if string.sub(e,1,7)=='NOPERM ' then kind='decline' else kind='error';payload=e end
elseif length>limit then kind='decline'
else
 payload,e=call('GET',KEYS[5]);if e then kind='error';payload=e elseif payload==false then kind='absent' end
end
return {'ready',entries,bp,count,fields,vp,present,kind,payload}
`

var snapshotMetadataReadHintDigest = goredis.NewScript(snapshotMetadataReadHintLua).Hash()

type snapshotMetadataNativeError string

func (e snapshotMetadataNativeError) Error() string { return string(e) }
func (snapshotMetadataNativeError) RedisError()     {}

func (s *store) readSnapshotMetadataHint(ctx context.Context, key string) (string, bool, error) {
	scope := snapshotOf(ctx)
	if !snapshotMetadataHintEligible(s, scope, key) {
		return "", false, nil
	}
	suffix := strings.TrimPrefix(key, metaPrefix)
	scope.metadataMu.Lock()
	defer scope.metadataMu.Unlock()
	// The native resolver already has a per-statement routing/proof cache. Do not
	// turn its subsequent single native GET into another full proof operation.
	if scope.metadata.loaded[suffix] {
		return "", false, nil
	}
	hint, ok := s.metadataReadHints.get(suffix)
	if !ok {
		return "", false, nil
	}
	if err := scope.pin.validLocal(ctx); err != nil {
		return "", true, err
	}
	slot := snapshotMetadataSlot(suffix)
	args := []any{"EVAL", snapshotMetadataReadHintLua, 5, snapshotMetadataMap(scope.id), snapshotMetadataGraph,
		snapshotMetadataBucket(hint.bucket, slot), snapshotMetadataDirectory(hint.bucket, slot), snapshotWorkspace(hint.value) + suffix,
		scope.id, slot, suffix, hint.bucket, hint.value, snapshotMetadataHintValueBytes}
	reply, err := s.metadataReadScript.runScript(ctx, s.client, snapshotMetadataReadHintDigest, args).Slice()
	if err != nil {
		if native, ok := err.(interface {
			error
			RedisError()
		}); ok && strings.HasPrefix(native.Error(), "NOPERM ") {
			return "", false, nil
		}
		return "", true, wrapRedis(transportContextError(ctx, err), "snapshot metadata read unavailable; read was not replayed")
	}
	return s.resolveSnapshotMetadataHintReply(ctx, scope, suffix, hint, reply)
}
func (s *store) resolveSnapshotMetadataHintReply(ctx context.Context, scope *snapshotScope, suffix string, hint snapshotMetadataReadHint, reply []any) (string, bool, error) {
	bad := func() (string, bool, error) {
		return "", true, errorf(adbc.StatusIO, "invalid snapshot metadata read reply")
	}
	if err := scope.pin.validLocal(ctx); err != nil {
		return "", true, err
	}
	if len(reply) == 0 {
		return bad()
	}
	status, ok := reply[0].(string)
	if !ok {
		return bad()
	}
	switch status {
	case "miss", "decline":
		if len(reply) != 1 {
			return bad()
		}
		return "", false, nil
	case "root":
		if len(reply) != 1 {
			return bad()
		}
		return "", true, errorf(adbc.StatusIO, "invalid snapshot metadata root")
	case "native":
		if len(reply) != 2 {
			return bad()
		}
		message, ok := reply[1].(string)
		if !ok || message == "" {
			return bad()
		}
		return "", true, snapshotMetadataNativeError(message)
	case "ready":
	default:
		return bad()
	}
	if len(reply) != 9 {
		return bad()
	}
	entries, a := reply[1].([]any)
	bp, b := reply[2].([]any)
	count, c := reply[3].(int64)
	fields, d := reply[4].(int64)
	vp, e := reply[5].([]any)
	present, f := reply[6].(int64)
	kind, g := reply[7].(string)
	if !a || !b || !c || !d || !e || !f || !g || len(entries)%2 != 0 || len(entries) > 512 || len(bp) != 2 || len(vp) != 2 || count < 0 || fields < 0 || present < 0 || present > 1 {
		return bad()
	}
	roots := make(map[string]string, len(entries)/2)
	for i := 0; i < len(entries); i += 2 {
		k, ok := entries[i].(string)
		v, vok := entries[i+1].(string)
		if !ok || !vok {
			return bad()
		}
		if _, dup := roots[k]; dup {
			return bad()
		}
		roots[k] = v
	}
	slot := snapshotMetadataSlot(suffix)
	if roots[slot] != hint.bucket {
		return bad()
	}
	expected, ep := strconv.ParseInt(fmt.Sprint(bp[0]), 10, 64)
	refs, rp := strconv.ParseInt(fmt.Sprint(bp[1]), 10, 64)
	if ep != nil || rp != nil || refs <= 0 || expected != count || expected != fields {
		return "", true, errorf(adbc.StatusIO, "snapshot metadata bucket is incomplete")
	}
	exists, xp := strconv.ParseInt(fmt.Sprint(vp[0]), 10, 64)
	vrefs, vrp := strconv.ParseInt(fmt.Sprint(vp[1]), 10, 64)
	if xp != nil || vrp != nil || vrefs <= 0 || exists != present {
		return "", true, errorf(adbc.StatusIO, "snapshot metadata object is missing or corrupt")
	}
	if kind == "decline" {
		if reply[8] != nil {
			return bad()
		}
		return "", false, nil
	}
	if kind == "error" {
		message, ok := reply[8].(string)
		if !ok || message == "" {
			return bad()
		}
		return "", true, snapshotMetadataNativeError(message)
	}
	if kind == "absent" {
		if reply[8] != nil {
			return bad()
		}
		if exists != 0 {
			return "", true, errorf(adbc.StatusIO, "snapshot metadata object is missing or corrupt")
		}
		return "", true, goredis.Nil
	}
	if kind != "value" {
		return bad()
	}
	value, ok := reply[8].(string)
	if !ok || len(value) > snapshotMetadataHintValueBytes || exists != 1 {
		return bad()
	}
	if scope.metadata.roots == nil {
		initializeSnapshotMetadataState(&scope.metadata, roots)
	} else {
		if len(scope.metadata.roots) != len(roots) {
			return "", true, errorf(adbc.StatusIO, "snapshot metadata root changed within statement")
		}
		for key, value := range roots {
			if scope.metadata.roots[key] != value {
				return "", true, errorf(adbc.StatusIO, "snapshot metadata root changed within statement")
			}
		}
	}
	m := &scope.metadata
	if previous, recorded := m.reads[suffix]; recorded && previous != hint.value {
		return "", true, errorf(adbc.StatusIO, "snapshot metadata dependency changed within statement")
	}
	if m.buckets[slot] == nil {
		m.buckets[slot] = map[string]string{}
	}
	m.buckets[slot][suffix] = hint.value
	m.loaded[suffix] = true
	m.bucketChecked[slot] = true
	m.checked[snapshotWorkspace(hint.value)+suffix] = true
	m.reads[suffix] = hint.value
	return value, true, nil
}
