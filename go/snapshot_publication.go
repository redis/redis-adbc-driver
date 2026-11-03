// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"sort"
)

// Merge logical entries, never entire old buckets. A bucket can contain keys
// changed by both publications. Native values are already immutable and retain
// their data edges; adding an alias changes only the bucket-to-value count.
const snapshotMetadataMergeEntry = `
local raw=redis.call('HGET',KEYS[1],'w:'..ARGV[1])
local original=redis.call('HGET',KEYS[1],'w:'..ARGV[6])
local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
if not raw or not original then return redis.error_reply('snapshot merge ownership missing') end
local w,source=cjson.decode(raw),cjson.decode(original)
if (w.metadata_builds or 0)>0 then return redis.error_reply('snapshot metadata construction is incomplete') end
if w.state~='staging' or w.token~=ARGV[2] or w.expires<=now or source.state~='sealed' or source.token~=ARGV[7] or source.expires<=now then return redis.error_reply('snapshot merge lease lost') end
if KEYS[9]~='` + snapshotPrefix + `metadata:fields:'..ARGV[1]..':'..ARGV[3] or KEYS[2]~='` + snapshotPrefix + `metadata:root:'..ARGV[1] or KEYS[3]~='` + snapshotPrefix + `metadata:bucket:'..ARGV[1]..':'..ARGV[3] or KEYS[6]~='` + snapshotPrefix + `metadata:bucket:'..ARGV[6]..':'..ARGV[3] or KEYS[7]~='` + snapshotPrefix + `metadata:root:'..ARGV[6] then return redis.error_reply('invalid snapshot merge keys') end
if redis.call('HGET',KEYS[2],ARGV[3])~=ARGV[1] or redis.call('HGET',KEYS[7],ARGV[3])~=ARGV[6] or (redis.call('HGET',KEYS[6],ARGV[4]) or '')~=ARGV[5] then return redis.error_reply('snapshot merge mapping changed') end
local prior=redis.call('HGET',KEYS[3],ARGV[4]) or ''
if prior==ARGV[5] then return 1 end
local prefix='` + snapshotPrefix + `workspace:'
if prior~='' and tonumber(redis.call('HGET',KEYS[4],'v:'..prefix..prior..':'..ARGV[4]) or '0')<=0 then return redis.error_reply('snapshot merge value missing') end
if ARGV[5]~='' and (ARGV[5]~=ARGV[6] or tonumber(redis.call('HGET',KEYS[4],'v:'..KEYS[8]) or '0')<=0 or KEYS[8]~=prefix..ARGV[5]..':'..ARGV[4]) then return redis.error_reply('snapshot merge source missing') end
if ARGV[5]~='' then
 redis.call('HINCRBY',KEYS[4],'v:'..KEYS[8],1)
 redis.call('HSET',KEYS[3],ARGV[4],ARGV[5]);redis.call('ZADD',KEYS[9],0,ARGV[4])
else redis.call('HDEL',KEYS[3],ARGV[4]);redis.call('ZREM',KEYS[9],ARGV[4]) end
if prior~='' then
 local key=prefix..prior..':'..ARGV[4]
 if redis.call('HINCRBY',KEYS[4],'v:'..key,-1)==0 then redis.call('ZADD',KEYS[5],0,key) end
end
redis.call('HSET',KEYS[4],'n:'..ARGV[1]..':'..ARGV[3],redis.call('HLEN',KEYS[3]))
return 1
`

func (s *store) mergeSnapshotEntry(ctx context.Context, original *snapshotScope, suffix, sourceOwner string) error {
	// Load/prove the latest bucket through the normal immutable resolver, without
	// copying its native value. Then obtain a private bucket of references.
	if _, err := resolveSnapshotMetadata(ctx, suffix, false); err != nil {
		return err
	}
	scope := snapshotOf(ctx)
	scope.metadataMu.Lock()
	defer scope.metadataMu.Unlock()
	m := &scope.metadata
	slot := snapshotMetadataSlot(suffix)
	owner := m.roots[slot]
	if owner != scope.id || !m.ready[slot] {
		if err := s.ownSnapshotMetadataBucket(ctx, slot, owner); err != nil {
			return err
		}
	}
	if err := once(ctx, s.redis(ctx), "EVAL", snapshotMetadataMergeEntry, 9, snapshotCatalogKey, snapshotMetadataMap(scope.id), snapshotMetadataBucket(scope.id, slot), snapshotMetadataGraph, snapshotMetadataValueQueue, snapshotMetadataBucket(original.id, slot), snapshotMetadataMap(original.id), snapshotWorkspace(original.id)+suffix, snapshotMetadataDirectory(scope.id, slot), scope.id, scope.pin.token, slot, suffix, sourceOwner, original.id, original.pin.token).Err(); err != nil {
		return err
	}
	if sourceOwner == "" {
		delete(m.buckets[slot], suffix)
	} else {
		m.buckets[slot][suffix] = sourceOwner
	}
	return nil
}

type snapshotMergeBase struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	Base  string `json:"base"`
}

func (s *store) rebaseSnapshotPublication(ctx context.Context, pin *snapshotPin, id, operation string) (int, error) {
	original := snapshotOf(ctx)
	original.metadataMu.Lock()
	reads := make(map[string]string, len(original.metadata.reads))
	for key, owner := range original.metadata.reads {
		reads[key] = owner
	}
	changed := make(map[string]string, len(original.metadata.changed))
	for key := range original.metadata.changed {
		changed[key] = original.metadata.buckets[snapshotMetadataSlot(key)][key]
	}
	original.metadataMu.Unlock()
	keys := make([]string, 0, len(reads))
	for key := range reads {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	dirty := make([]string, 0, len(changed))
	for key := range changed {
		dirty = append(dirty, key)
	}
	sort.Strings(dirty)
	// Repeat only a known root CAS refusal. SQL, Arrow, defaults, volatile values,
	// row staging and index sealing have already finished and are never replayed.
	for attempt := 0; attempt < 8; attempt++ {
		if err := pin.valid(ctx); err != nil {
			return -1, err
		}
		latest, mergePin, err := s.acquireSnapshot(context.WithValue(ctx, snapshotContextKey{}, (*snapshotScope)(nil)))
		if err != nil {
			return -1, err
		}
		n, err := func() (int, error) {
			defer mergePin.release()
			for _, key := range keys {
				if _, err := resolveSnapshotMetadata(latest, key, false); err != nil {
					return -1, err
				}
				m := snapshotOf(latest)
				m.metadataMu.Lock()
				owner := m.metadata.reads[key]
				m.metadataMu.Unlock()
				if owner != reads[key] {
					return -5, nil
				}
			}
			mergeCtx, mergeID, err := s.beginSnapshotWrite(latest, mergePin)
			if err != nil {
				return -1, err
			}
			defer s.abortSnapshot(mergePin, mergeID)
			for _, key := range dirty {
				if err := s.mergeSnapshotEntry(mergeCtx, original, key, changed[key]); err != nil {
					return -1, err
				}
			}
			base, err := json.Marshal(snapshotMergeBase{ID: mergeID, Token: mergePin.token, Base: mergePin.root})
			if err != nil {
				return -1, err
			}
			return s.snapshotControl(ctx, operation, pin.token, id, pin.root, string(base), original.tempDead, mergeID).Int()
		}()
		if n == -5 {
			return 0, nil
		}
		if err != nil || n != 0 {
			return n, err
		}
	}
	return 0, nil
}
