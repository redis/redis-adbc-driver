// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotGCPageObserver struct {
	snapshotHook
	prefix    string
	interrupt bool
	batches   atomic.Int64
	minimum   atomic.Int64
	maximum   atomic.Int64
}

func (h *snapshotGCPageObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if err != nil {
			return err
		}
		args := cmd.Args()
		var rows []any
		if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "DEL") && strings.HasPrefix(fmt.Sprint(args[1]), h.prefix) {
			rows = args[1:]
		}
		if len(args) > 3 && strings.EqualFold(fmt.Sprint(args[0]), "EVAL") && strings.Contains(fmt.Sprint(args[1]), "snapshot row reclamation batch") {
			n, _ := strconv.Atoi(fmt.Sprint(args[2]))
			if n > 3 && len(args) >= 3+n {
				rows = args[6 : 3+n]
			}
		}
		if len(rows) == 0 {
			return nil
		}
		h.batches.Add(1)
		h.maximum.Store(max(h.maximum.Load(), int64(len(rows))))
		for _, key := range rows {
			id, err := strconv.ParseInt(strings.TrimPrefix(fmt.Sprint(key), h.prefix), 10, 64)
			if err != nil {
				return err
			}
			if h.minimum.Load() == 0 || id < h.minimum.Load() {
				h.minimum.Store(id)
			}
		}
		if h.interrupt && h.batches.Load() == 1 {
			return errors.New("injected interruption after GC batch reply")
		}
		return nil
	}
}

func TestSnapshotGCBatchProgressSurvivesLostReply(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, _ := snapshotSweepGarbageRows(t, h, s, true, 600)
	observer := &snapshotGCPageObserver{prefix: meta.prefix(), interrupt: true}
	s.client.AddHook(observer)
	result, err := s.reapSnapshotStep(h.ctx)
	if err == nil || !strings.Contains(err.Error(), "injected interruption after GC batch reply") || observer.batches.Load() != 1 || result.deleted != 0 {
		t.Fatalf("lost reply not exercised once: %+v %v batches=%d", result, err, observer.batches.Load())
	}
	directory := snapshotMembers(snapshotDataID(meta.prefix()))
	if n := raw.ZCard(h.ctx, directory).Val(); n != 344 {
		t.Fatalf("completed GC batch did not durably advance its directory: remaining=%d want=344", n)
	}
	if raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"256").Val() != 0 || raw.Exists(h.ctx, meta.prefix()+"257", meta.prefix()+"600").Val() != 2 {
		t.Fatal("first batch deletion coverage incorrect")
	}
	// A fresh client/process sees the remaining work, without an in-memory cursor.
	restarted := snapshotTestStore(t, raw)
	resumedObserver := &snapshotGCPageObserver{prefix: meta.prefix()}
	restarted.client.AddHook(resumedObserver)
	result, err = restarted.reapSnapshotStep(h.ctx)
	if err != nil || result.deleted != 344 || resumedObserver.minimum.Load() != 257 || resumedObserver.maximum.Load() > 256 || resumedObserver.batches.Load() != 2 {
		t.Fatalf("resumed GC rescanned/replayed work: %+v %v min=%d max=%d batches=%d", result, err, resumedObserver.minimum.Load(), resumedObserver.maximum.Load(), resumedObserver.batches.Load())
	}
	if raw.Exists(h.ctx, directory, meta.prefix()+"257", meta.prefix()+"600").Val() != 0 || raw.HExists(h.ctx, snapshotMetadataGraph, "d:"+snapshotDataID(meta.prefix())).Val() {
		t.Fatal("resumed generation not fully reclaimed")
	}
}

func TestSnapshotGCBatchAdmission(t *testing.T) {
	for _, mode := range []string{"unclaimed", "referenced", "missing_queue", "wrong_prefix", "bad_score", "bad_member", "wrong_key"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
			meta, _ := snapshotSweepGarbage(t, h, s, true)
			prefix := meta.prefix()
			id := snapshotDataID(prefix)
			directory := snapshotMembers(id)
			if mode != "unclaimed" {
				if err := s.snapshotControl(h.ctx, "claim_data", "", "", "").Err(); err != nil {
					t.Fatal(err)
				}
			}
			members := []string{fmt.Sprintf("%020d", 1), fmt.Sprintf("%020d", 2)}
			switch mode {
			case "referenced":
				raw.HSet(h.ctx, snapshotMetadataGraph, "r:"+id, 1)
			case "missing_queue":
				raw.ZRem(h.ctx, snapshotDataQueue, id)
			case "wrong_prefix":
				prefix = snapshotRows(id, "another_schema")
			case "bad_score":
				raw.ZAdd(h.ctx, directory, goredis.Z{Score: 1, Member: members[1]})
			case "bad_member":
				members[1] = "09223372036854775808" // int64 overflow
			}
			var err error
			if mode == "wrong_key" {
				err = raw.Eval(h.ctx, snapshotGCRowBatchLua,
					[]string{snapshotMetadataGraph, snapshotDataQueue, directory, prefix + "1", prefix + "3"},
					id, prefix, members[0], members[1]).Err()
			} else {
				_, err = s.deleteSnapshotRowBatch(h.ctx, prefix, members)
			}
			if err == nil {
				t.Fatal("unsafe batch admitted")
			}
			if raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3").Val() != 3 || raw.ZCard(h.ctx, directory).Val() != 3 {
				t.Fatal("batch validation partially mutated rows/directory")
			}
		})
	}
}

func TestSnapshotGCBatchExactIDsAndStaleCollectors(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, _ := snapshotSweepGarbage(t, h, s, true)
	prefix := meta.prefix()
	id := snapshotDataID(prefix)
	directory := snapshotMembers(id)
	large := "9223372036854775807"
	member := "0" + large
	if err := raw.Rename(h.ctx, prefix+"1", prefix+large).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.ZRem(h.ctx, directory, fmt.Sprintf("%020d", 1)).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.ZAdd(h.ctx, directory, goredis.Z{Member: member}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.snapshotControl(h.ctx, "claim_data", "", "", "").Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.deleteSnapshotRowBatch(h.ctx, prefix, []string{member}); err != nil || n != 1 {
		t.Fatalf("exact int64 row deletion: %d %v", n, err)
	}
	// A stale collector must not delete an unregistered object at an old key.
	if err := raw.Set(h.ctx, prefix+large, "unregistered", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.deleteSnapshotRowBatch(h.ctx, prefix, []string{member}); err != nil || n != 0 {
		t.Fatalf("stale batch: %d %v", n, err)
	}
	result, err := s.reapSnapshotStep(h.ctx)
	if err != nil || result.deleted != 2 {
		t.Fatalf("remaining rows: %+v %v", result, err)
	}
	if n, err := s.deleteSnapshotRowBatch(h.ctx, prefix, []string{member}); err != nil || n != 0 {
		t.Fatalf("completed concurrent collector: %d %v", n, err)
	}
	if v, err := raw.Get(h.ctx, prefix+large).Result(); err != nil || v != "unregistered" {
		t.Fatalf("unregistered object deleted: %q %v", v, err)
	}
}

func TestSnapshotGCBatchRepeatedInterruptions(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, _ := snapshotSweepGarbageRows(t, h, s, true, 600)
	directory := snapshotMembers(snapshotDataID(meta.prefix()))
	for batch, remaining := range []int64{344, 88, 0} {
		restarted := snapshotTestStore(t, raw)
		observer := &snapshotGCPageObserver{prefix: meta.prefix(), interrupt: true}
		restarted.client.AddHook(observer)
		result, err := restarted.reapSnapshotStep(h.ctx)
		if err == nil || !strings.Contains(err.Error(), "injected interruption") || result.deleted != 0 || observer.batches.Load() != 1 || observer.minimum.Load() != int64(batch*256+1) {
			t.Fatalf("interruption %d: %+v %v min=%d batches=%d", batch, result, err, observer.minimum.Load(), observer.batches.Load())
		}
		if n := raw.ZCard(h.ctx, directory).Val(); n != remaining {
			t.Fatalf("interruption %d lost progress: %d want=%d", batch, n, remaining)
		}
	}
	result, err := s.reapSnapshotStep(h.ctx)
	if err != nil || result.deleted != 0 || raw.HExists(h.ctx, snapshotMetadataGraph, "d:"+snapshotDataID(meta.prefix())).Val() {
		t.Fatalf("finish empty generation: %+v %v", result, err)
	}
}

func TestSnapshotGCBatchPermissionPreflight(t *testing.T) {
	uri := os.Getenv("REDIS_SNAPSHOT_GC_ACL_URI")
	if uri == "" {
		t.Skip("requires owned Software API principal with DEL allowed and ZREM denied")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	meta, _ := snapshotSweepGarbage(t, h, s, true)
	if err := s.snapshotControl(h.ctx, "claim_data", "", "", "").Err(); err != nil {
		t.Fatal(err)
	}
	credentials, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatal("invalid private principal configuration")
	}
	opts := *raw.Options()
	opts.Username, opts.Password = credentials.Username, credentials.Password
	limited := goredis.NewClient(&opts)
	defer limited.Close()
	directory := snapshotMembers(snapshotDataID(meta.prefix()))
	member := fmt.Sprintf("%020d", 1)
	// Verify the tested ACL difference rather than accepting any authentication
	// or EVAL denial as evidence that the atomic preflight works.
	probe := meta.prefix() + "acl-probe"
	if err := raw.Set(h.ctx, probe, "probe", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := limited.Del(h.ctx, probe).Result(); err != nil || n != 1 {
		t.Fatal("DEL positive control failed")
	}
	if err := limited.ZRem(h.ctx, directory, "nonexistent").Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("ZREM negative control failed")
	}
	guarded := snapshotTestStore(t, limited)
	_, err = guarded.deleteSnapshotRowBatch(h.ctx, meta.prefix(), []string{member})
	if err == nil || !strings.Contains(err.Error(), "snapshot reclamation batch requires DEL and ZREM") {
		t.Fatalf("permission preflight not exercised: %v", err)
	}
	if raw.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3").Val() != 3 || raw.ZCard(h.ctx, directory).Val() != 3 {
		t.Fatal("permission denial partially mutated generation")
	}
}
