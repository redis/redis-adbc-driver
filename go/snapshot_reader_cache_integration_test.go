// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type readerCacheObserver struct {
	snapshotHook
	lookups, admissions, refusals atomic.Int64
	afterRefusal                  func(string)
}

func (h *readerCacheObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if len(a) > 1 && a[0] == "EVAL" && a[1] == snapshotNamespaceRead {
			h.lookups.Add(1)
		}
		admission := false
		n := 0
		if len(a) > 3 && (a[0] == "EVALSHA" && a[1] == snapshotControlDigest || a[0] == "EVAL" && a[1] == snapshotControl) {
			n, _ = strconv.Atoi(fmt.Sprint(a[2]))
			admission = len(a) > 3+n && a[3+n] == "acquire"
			if admission {
				h.admissions.Add(1)
			}
		}
		err := next(ctx, cmd)
		if admission && snapshotNativeRefusal(err, "reader namespace changed before mutation") {
			h.refusals.Add(1)
			if h.afterRefusal != nil {
				h.afterRefusal(fmt.Sprint(a[4+n]))
			}
		}
		return err
	}
}

func TestSnapshotReaderKeyCacheNativeMembership(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	s := snapshotTestStore(t, raw)
	observer := &readerCacheObserver{}
	s.client.(*goredis.Client).AddHook(observer)
	old := snapshotReaderNamespace("cache-old-domain")
	newName := snapshotReaderNamespace("cache-new-domain")
	if err := raw.SAdd(h.ctx, snapshotReaderDirectory, old).Err(); err != nil {
		t.Fatal(err)
	}
	acquire := func() {
		t.Helper()
		token := snapshotID()
		root, err := s.snapshotControl(h.ctx, "acquire", token, "", "").Text()
		if err != nil {
			t.Fatal(err)
		}
		if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
			t.Fatal("wrong root")
		}
		if err := s.snapshotControl(h.ctx, "release", token, "", root).Err(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		acquire()
	}
	if observer.lookups.Load() != 1 || observer.admissions.Load() != 4 {
		t.Fatal("stable membership did not reuse hint", observer.lookups.Load(), observer.admissions.Load())
	}
	observer.afterRefusal = func(token string) {
		if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
			t.Fatal("stale directory partially admitted a pin")
		}
	}
	// Keep SCARD unchanged: checking only count cannot establish membership.
	if err := raw.Eval(h.ctx, `redis.call('SREM',KEYS[1],ARGV[1]);redis.call('SADD',KEYS[1],ARGV[2]);return 1`, []string{snapshotReaderDirectory}, old, newName).Err(); err != nil {
		t.Fatal(err)
	}
	acquire()
	if observer.lookups.Load() != 2 || observer.admissions.Load() != 6 || observer.refusals.Load() != 1 {
		t.Fatal("same-cardinality replacement did not refresh exactly once", observer.lookups.Load(), observer.admissions.Load(), observer.refusals.Load())
	}
	// Removal also needs a coherent refresh, then the empty list remains reusable.
	if err := raw.SRem(h.ctx, snapshotReaderDirectory, newName).Err(); err != nil {
		t.Fatal(err)
	}
	acquire()
	acquire()
	if observer.lookups.Load() != 3 || observer.admissions.Load() != 9 || observer.refusals.Load() != 2 {
		t.Fatal("removed membership was not refreshed", observer.lookups.Load(), observer.admissions.Load(), observer.refusals.Load())
	}
}

func TestSnapshotReaderKeyCacheNativeWarmResourceGuards(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprint(directory), func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			s := snapshotTestStore(t, raw)
			observer := &readerCacheObserver{}
			s.client.(*goredis.Client).AddHook(observer)
			ns := snapshotReaderNamespace("warm-resource-domain")
			if err := raw.SAdd(h.ctx, snapshotReaderDirectory, ns).Err(); err != nil {
				t.Fatal(err)
			}
			if _, err := s.snapshotReaderKeys(h.ctx, false); err != nil {
				t.Fatal(err)
			}
			if directory {
				// One oversized member preserves cardinality but exceeds the byte bound.
				if err := raw.Eval(h.ctx, `redis.call('SREM',KEYS[1],ARGV[1]);redis.call('SADD',KEYS[1],ARGV[2]);return 1`, []string{snapshotReaderDirectory}, ns, strings.Repeat("x", 300_000)).Err(); err != nil {
					t.Fatal(err)
				}
			} else if err := raw.HSet(h.ctx, snapshotReaderPrefix+ns, snapshotID(), strings.Repeat("x", 300_000)).Err(); err != nil {
				t.Fatal(err)
			}
			token := snapshotID()
			err := s.snapshotControl(h.ctx, "acquire", token, "", "").Err()
			if err == nil || !strings.Contains(err.Error(), "resource limit") {
				t.Fatal("warm cache bypassed byte guard", err)
			}
			if observer.lookups.Load() != 1 || observer.admissions.Load() != 1 || observer.refusals.Load() != 0 {
				t.Fatal("resource failure was retried", observer.lookups.Load(), observer.admissions.Load(), observer.refusals.Load())
			}
			if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
				t.Fatal("resource failure admitted a pin")
			}
		})
	}
}

func TestSnapshotReaderKeyCacheNativeCountBeforeMemoryBudget(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	s := snapshotTestStore(t, raw)
	keys, err := s.snapshotReaderKeys(h.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []any
	for i := 0; i < 129; i++ {
		names = append(names, snapshotReaderNamespace(fmt.Sprint("oversized-", i)))
	}
	if err := raw.SAdd(h.ctx, snapshotReaderDirectory, names...).Err(); err != nil {
		t.Fatal(err)
	}
	// Fault the expensive primitive in the actual Lua program. An oversized
	// directory must take its cheap bound refusal without reaching MEMORY.
	const forbidMemory = `local backend=redis
local redis=setmetatable({call=function(cmd,...)
 if cmd=='MEMORY' then error('unbounded MEMORY before directory count refusal') end
 return backend.call(cmd,...)
end},{__index=backend})
`
	controlKeys := []any{snapshotCatalogKey, snapshotMetadataGraph, snapshotDataQueue, snapshotCompactionQueue, snapshotCompactionCandidates}
	n := len(controlKeys)
	controlKeys = append(controlKeys, keys...)
	token := snapshotID()
	args := []any{"EVAL", forbidMemory + snapshotControl, len(controlKeys)}
	args = append(args, controlKeys...)
	args = append(args, "acquire", token, "", "", snapshotLease.Milliseconds(), "", "[]", n, len(keys)-1)
	err = once(h.ctx, s.client, args...).Err()
	if !snapshotNativeRefusal(err, "reader namespace changed before mutation") {
		t.Fatal("oversized directory reached memory accounting", err)
	}
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
		t.Fatal("oversized directory admitted a pin")
	}
	// Fresh discovery must likewise reject by count before materializing or
	// measuring the untrusted set.
	err = once(h.ctx, s.client, "EVAL", forbidMemory+snapshotNamespaceRead, 1, snapshotReaderDirectory).Err()
	if !snapshotNativeRefusal(err, "snapshot reader namespace resource limit exceeded; administrator reconciliation required") {
		t.Fatal("discovery reached memory accounting", err)
	}
}
