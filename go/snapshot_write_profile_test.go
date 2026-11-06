// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"maps"
	"os"
	"sync"
	"testing"
	"time"
)

type snapshotIndexWaitProfile struct {
	mu         sync.Mutex
	created    map[string]time.Time
	polls      int
	wait       time.Duration
	readyDelay time.Duration
	waiting    int
}

func (*snapshotIndexWaitProfile) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotIndexWaitProfile) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func (h *snapshotIndexWaitProfile) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		args := cmd.Args()
		if len(args) < 2 {
			return err
		}
		name := cmd.Name()
		key := fmt.Sprint(args[1])
		h.mu.Lock()
		defer h.mu.Unlock()
		if name == "ft.create" && err == nil {
			h.created[key] = start
		}
		if name == "ft.info" {
			if created, ok := h.created[key]; ok {
				h.polls++
				h.wait += time.Since(start)
				if c, ok := cmd.(interface{ Val() any }); ok && err == nil {
					info := indexInfoOf(c.Val())
					if info.indexing || info.percent < 1 || info.numDocs != 1 || info.failures != 0 {
						h.waiting++
					} else {
						if h.readyDelay == 0 {
							h.readyDelay = time.Since(created)
						}
					}
				}
			}
		}
		return err
	}
}
func TestSnapshotPointWriteIndexWaitProfile(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_WRITE_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_WRITE_PROFILE for owned-fixture profiling")
	}
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT,payload VARCHAR)")
	h.exec("INSERT INTO t SELECT i,9007199254740993,'row-'||i FROM generate_series(1,20000) AS g(i)")
	hook := &snapshotIndexWaitProfile{created: map[string]time.Time{}}
	ctx, _ := statisticsHookedTransport(t, h, hook)
	prior := h.ctx
	h.ctx = ctx
	defer func() { h.ctx = prior }()
	for _, extra := range []int{0, 20000} {
		if extra != 0 {
			h.exec("CREATE TABLE unrelated(id BIGINT)")
			h.exec("INSERT INTO unrelated SELECT i FROM generate_series(1,20000) AS g(i)")
		}
		hook.mu.Lock()
		hook.created = map[string]time.Time{}
		hook.polls = 0
		hook.wait = 0
		hook.readyDelay = 0
		hook.waiting = 0
		hook.mu.Unlock()
		started := time.Now()
		want := "1|9007199254740994"
		if extra != 0 {
			want = "1|9007199254740995"
		}
		h.expectRows("UPDATE t SET v=v+1 WHERE __rowid=1 RETURNING id,v", want)
		elapsed := time.Since(started)
		hook.mu.Lock()
		t.Logf("profile unrelated_rows=%d elapsed=%s new_indexes=%d readiness_polls=%d unsettled=%d poll_roundtrips=%s created_to_ready=%s", extra, elapsed, len(hook.created), hook.polls, hook.waiting, hook.wait, hook.readyDelay)
		complete := len(hook.created) == 1 && hook.polls > 0 && hook.readyDelay > 0
		hook.mu.Unlock()
		if !complete {
			t.Fatal("incomplete readiness observation")
		}
	}
}

// This is an opt-in experiment, not an alternate production publication path.
// Only a private workspace's rows/index are touched; it is always aborted.
func TestSnapshotPointWriteDirectoryIndexExperiment(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_WRITE_PROFILE") == "" {
		t.Skip("set REDIS_SNAPSHOT_WRITE_PROFILE for owned-fixture experiments")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT,payload VARCHAR)")
	h.exec("INSERT INTO t SELECT i,9007199254740993,'row-'||i FROM generate_series(1,20000) AS g(i)")
	h.exec("CREATE TABLE unrelated(id BIGINT)")
	h.exec("INSERT INTO unrelated SELECT i FROM generate_series(1,20000) AS g(i)")
	s := snapshotTestStore(t, raw)
	work, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=v+1 WHERE __rowid=1")
	defer s.abortSnapshot(pin, id)
	meta, err := s.getTable(work, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	key := meta.prefix() + "1"
	before, err := s.redis(work).HGetAll(work, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.redis(work).ZCard(work, snapshotMembers(snapshotDataID(meta.prefix()))).Result(); err != nil || n != 1 {
		t.Fatal("unexpected private directory", n, err)
	}
	started := time.Now()
	if err := s.searchDo(work, meta.index(), indexCreateArgs(meta)...).Err(); err != nil {
		t.Fatal(err)
	}
	// Redis Search receives one complete, already-final row image after the
	// initially empty index exists. The ownership/lease mutation fence remains.
	if err := s.redis(work).HSet(work, key, rowIDField, "1").Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "experimental private index readiness", func() bool {
		reply, err := s.searchDo(work, meta.index(), "FT.INFO", meta.index()).Result()
		if err != nil {
			t.Fatal(err)
		}
		info := indexInfoOf(reply)
		if info.failures != 0 {
			t.Fatal("experimental index failure")
		}
		return !info.indexing && info.percent >= 1 && info.numDocs == 1 && len(info.prefixes) == 1 && info.prefixes[0] == meta.prefix()
	})
	elapsed := time.Since(started)
	reply, err := s.searchDo(work, meta.index(), "FT.SEARCH", meta.index(), "@id:[1 1] @v:[9007199254740994 9007199254740994]", "NOCONTENT", "LIMIT", 0, 0, "DIALECT", 2).Result()
	if err != nil {
		t.Fatal(err)
	}
	if n, err := parseSearchCount(reply); err != nil || n != 1 {
		t.Fatal("experimental indexed fields", n, err)
	}
	after, err := s.redis(work).HGetAll(work, key).Result()
	if err != nil || !maps.Equal(before, after) {
		t.Fatal("index notification changed private row image", err)
	}
	h.expectRows("SELECT id,v FROM t WHERE __rowid=1", "1|9007199254740993")
	t.Logf("private directory-index experiment: unrelated_rows=20000 output_rows=1 create_touch_ready=%s; workspace aborted, no publication", elapsed)
}
