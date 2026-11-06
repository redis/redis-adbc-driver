// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// SQL sequence counters are deliberately volatile/nontransactional. Definitions
// are immutable metadata; counters remain reference-owned native values so DROP,
// aborted creation and old reader pins use the ordinary metadata GC lifecycle.
type snapshotSequenceValue struct {
	ID                                  string
	Last                                int64
	Called                              bool
	ResetEpoch, ConfigEpoch, CacheEpoch int64
}

func snapshotSequenceKey(id string) string { return metaPrefix + "sequence-state:" + id }

func (s *store) readSnapshotSequenceValue(ctx context.Context, m *sequenceMeta) (*snapshotSequenceValue, string, error) {
	key, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(snapshotSequenceKey(m.ID), metaPrefix), false)
	if err != nil {
		return nil, "", err
	}
	raw, err := s.client.Get(ctx, key).Result()
	if err != nil {
		return nil, "", wrapRedis(err, "read sequence counter")
	}
	var state snapshotSequenceValue
	if err := json.Unmarshal([]byte(raw), &state); err != nil || state.ID != m.ID {
		return nil, "", errorf(adbc.StatusIO, "invalid sequence counter")
	}
	return &state, raw, nil
}

func overlaySnapshotSequenceValue(m *sequenceMeta, state *snapshotSequenceValue) {
	if state.ResetEpoch == m.ResetEpoch {
		m.Last, m.Called = state.Last, state.Called
	} else {
		m.Last, m.Called = m.ResetLast, m.ResetCalled
	}
	m.snapshotToken = fmt.Sprintf("%d/%d/%d", m.Epoch, m.ResetEpoch, state.CacheEpoch)
}

const snapshotSequenceValueCAS = `
-- volatile snapshot SQL sequence counter
local catalog,graph,counter,rootmap,bucket,definition=unpack(KEYS)
local workspace,token,base,owner,slot,suffix=unpack(ARGV)
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if catalog~='` + snapshotCatalogKey + `' or graph~='` + snapshotMetadataGraph + `' or
 definition~='` + snapshotPrefix + `workspace:'..owner..':'..suffix or
 not string.match(counter,'^` + snapshotPrefix + `workspace:[0-9a-f]+:sequence%-state:') then
 return redis.error_reply('invalid sequence counter keys')
end
if redis.call('HGET',catalog,'format')~='16' then return redis.error_reply('unsupported snapshot catalog format') end
local pinkey=KEYS[7];local pinfield='p:'..token
if pinkey~=catalog then
 local ns=string.sub(pinkey,#'` + snapshotReaderPrefix + `'+1)
 if #ns~=64 or string.find(ns,'[^0-9a-f]') or pinkey~='` + snapshotReaderPrefix + `'..ns or KEYS[8]~='` + snapshotReaderDirectory + `' or redis.call('SISMEMBER',KEYS[8],ns)~=1 then return redis.error_reply('invalid sequence reader namespace') end
 pinfield=token
end
local pinraw=redis.call('HGET',pinkey,pinfield)
if not pinraw then return redis.error_reply('sequence snapshot pin expired') end
local pin=cjson.decode(pinraw)
if pin.root~=base or pin.expires<=now then return redis.error_reply('sequence snapshot pin expired') end
if ARGV[11]=='private' then
 if owner~=workspace then return redis.error_reply('invalid private sequence definition') end
 local wr=redis.call('HGET',catalog,'w:'..workspace)
 if not wr then return redis.error_reply('sequence writer expired') end
 local w=cjson.decode(wr)
 if w.state~='staging' or w.token~=token or w.expires<=now then return redis.error_reply('sequence writer expired') end
else
 if redis.call('HGET',catalog,'root')~=ARGV[9] then return 0 end
 if rootmap~='` + snapshotPrefix + `metadata:root:'..ARGV[9] or
    bucket~='` + snapshotPrefix + `metadata:bucket:'..ARGV[10]..':'..slot or
    redis.call('HGET',rootmap,slot)~=ARGV[10] then return 0 end
 if redis.call('HGET',bucket,suffix)~=owner then return -1 end
end
if tonumber(redis.call('HGET',graph,'v:'..counter) or '0')<=0 or redis.call('HGET',graph,'e:'..counter)~='1' then
 return redis.error_reply('sequence counter reference unavailable')
end
if redis.call('GET',counter)~=ARGV[7] then return 0 end
redis.call('SET',counter,ARGV[8])
return 1
`

// Retry only a proven CAS refusal. The already evaluated SQL arguments are not
// evaluated again, and an uncertain allocation is never resent or guessed.
func (e *executor) mutateSnapshotSequenceValue(ctx context.Context, schema, name string, fn func(*sequenceMeta) error, drop, ifExists bool) error {
	if drop || ifExists {
		return errorf(adbc.StatusInternal, "invalid volatile sequence mutation")
	}
	scope := snapshotOf(ctx)
	if scope == nil || scope.pin == nil {
		return errorf(adbc.StatusInvalidState, "sequence allocation requires a snapshot pin")
	}
	suffix := strings.TrimPrefix(sequenceKey(schema, name), metaPrefix)
	definition, err := resolveSnapshotMetadata(ctx, suffix, false)
	if err != nil {
		return err
	}
	owner, _, ok := strings.Cut(strings.TrimPrefix(definition, snapshotPrefix+"workspace:"), ":")
	if !ok || !snapshotMetadataID(owner) {
		return errorf(adbc.StatusIO, "invalid sequence definition owner")
	}
	rawDefinition, err := e.store.client.Get(ctx, definition).Bytes()
	if err != nil {
		return err
	}
	privateDefinition := scope.write && owner == scope.id
	for attempt := 0; attempt < 100; attempt++ {
		var m sequenceMeta
		if err := json.Unmarshal(rawDefinition, &m); err != nil {
			return err
		}
		state, before, err := e.store.readSnapshotSequenceValue(ctx, &m)
		if err != nil {
			return err
		}
		if state.ConfigEpoch > m.Epoch {
			return stateErrorf("40001", "sequence definition changed concurrently")
		}
		overlaySnapshotSequenceValue(&m, state)
		oldEpoch := m.Epoch
		if err := fn(&m); err != nil {
			return err
		}
		if m.Epoch != oldEpoch {
			state.CacheEpoch++
		}
		state.Last, state.Called, state.ResetEpoch, state.ConfigEpoch = m.Last, m.Called, m.ResetEpoch, oldEpoch
		after, err := json.Marshal(state)
		if err != nil {
			return err
		}
		counter, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(snapshotSequenceKey(m.ID), metaPrefix), false)
		if err != nil {
			return err
		}
		if privateDefinition && !strings.HasPrefix(counter, snapshotWorkspace(scope.id)) {
			return stateErrorf("40001", "cannot allocate from an unpublished sequence definition change")
		}
		root, bucketOwner := scope.id, scope.id
		slot := snapshotMetadataSlot(suffix)
		mode := ""
		if privateDefinition {
			mode = "private"
		}
		if !privateDefinition {
			root, err = e.store.client.HGet(ctx, snapshotCatalogKey, "root").Result()
			if err != nil {
				return err
			}
			bucketOwner, err = e.store.client.HGet(ctx, snapshotMetadataMap(root), slot).Result()
			if err != nil {
				return wrapRedis(err, "read current sequence definition bucket")
			}
		}
		n, err := once(ctx, e.store.client, "EVAL", snapshotSequenceValueCAS, 8, snapshotCatalogKey, snapshotMetadataGraph, counter, snapshotMetadataMap(root), snapshotMetadataBucket(bucketOwner, slot), definition, scope.pin.leaseKey(), snapshotReaderDirectory, scope.id, scope.pin.token, scope.pin.root, owner, slot, suffix, before, string(after), root, bucketOwner, mode).Int()
		if err != nil {
			return wrapRedis(err, "sequence allocation outcome is unknown; allocation was not replayed")
		}
		if n == 1 {
			return nil
		}
		if n != 0 {
			return stateErrorf("40001", "sequence definition changed concurrently")
		}
	}
	return stateErrorf("40001", "sequence counter is being changed concurrently")
}
