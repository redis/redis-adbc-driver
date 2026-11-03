// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/apache/arrow-adbc/go/adbc"
)

type snapshotCandidateReads struct {
	snapshotHook
	search, rows, info atomic.Int64
}

func (h *snapshotCandidateReads) record(c goredis.Cmder) {
	a := c.Args()
	if len(a) < 2 {
		return
	}
	name, key := strings.ToLower(fmt.Sprint(a[0])), fmt.Sprint(a[1])
	if name == "ft.info" {
		h.info.Add(1)
	}
	if name == "ft.search" {
		h.search.Add(1)
	}
	if (name == "hgetall" || name == "hmget") && snapshotRowPrefix(key) != "" {
		h.rows.Add(1)
	}
}
func (h *snapshotCandidateReads) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error { h.record(c); return next(ctx, c) }
}
func (h *snapshotCandidateReads) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cs []goredis.Cmder) error {
		for _, c := range cs {
			h.record(c)
		}
		return next(ctx, cs)
	}
}
func TestSnapshotCandidatesFilterBeforeHashFetch(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER,exact BIGINT)")
	h.exec("INSERT INTO t SELECT i,i,9007199254740993 FROM generate_series(1,4097) AS g(i)")
	h.exec("UPDATE t SET v=999 WHERE id=100")
	h.exec("DELETE FROM t WHERE id=101")
	h.exec("INSERT INTO t VALUES (4098,100,9007199254740993)")
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	result := snapshotExecuteTest(t, s, h.ctx, "SELECT id,exact FROM t WHERE v=100")
	if len(result.rows) != 1 || result.rows[0][0].I != 4098 || result.rows[0][1].I != 9007199254740993 {
		t.Fatal("old matching version resurfaced", result.rows)
	}
	if guard.search.Load() == 0 || guard.rows.Load() > 8 {
		t.Fatalf("selective query did not use bounded index candidates: searches=%d fetched=%d", guard.search.Load(), guard.rows.Load())
	}
	// Match several keyset pages and reduce with exact scalar accumulators.
	sum := new(big.Int).Mul(big.NewInt(4097), big.NewInt(9007199254740993)).String()
	h.expectRows("SELECT SUM(exact::DECIMAL(38,0)) FROM t WHERE v>=1", sum)
	if err := db.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringQueryWorkers, "4"); err != nil {
		t.Fatal(err)
	}
	worker, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: worker}
	h2.expectRows("SELECT COUNT(*),SUM(v),MIN(exact),MAX(exact) FROM t WHERE v>=1", fmt.Sprintf("4097|%d|9007199254740993|9007199254740993", 4097*4098/2+898))
	grant, err := worker.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionIntQueryWorkersLastGrant)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(grant)
	if n < 2 {
		t.Fatal("segmented aggregate did not execute parallel workers", grant)
	}
	h.expectRows("SELECT id FROM t WHERE v<4 ORDER BY id", "1", "2", "3")
	// The initial private tail is empty, so selective DML can use the same
	// index candidates; populated private tails still use authoritative reads.
	guard.search.Store(0)
	guard.rows.Store(0)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE t SET exact=42 WHERE v=100")
	if guard.search.Load() == 0 || guard.rows.Load() > 16 {
		t.Fatalf("selective UPDATE scanned the table: searches=%d fetched=%d", guard.search.Load(), guard.rows.Load())
	}
	h.expectRows("SELECT id,exact FROM t WHERE v=100", "4098|42")
	h.expectRows("SELECT COUNT(*) FROM t WHERE v=101", "0")
}

func TestSnapshotCandidatesRejectMissingShadowingVersion(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t VALUES(1,10),(2,20)")
	h.exec("UPDATE t SET v=99 WHERE id=1")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta := snapshotTableTest(t, s, ctx, "t")
	e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	where := &Binary{Op: "=", L: &ColumnRef{Name: "v"}, R: &Literal{V: intValue(typeInt32, 10)}}
	wp, err := e.planWhere(ctx, where, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Del(h.ctx, meta.prefix()+"1").Err(); err != nil {
		t.Fatal(err)
	}
	it, err := e.openScan(ctx, scanRequest{meta: meta, where: wp, need: map[string]bool{"id": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(ctx)
	if it.candidates == nil {
		t.Fatal("test did not exercise candidate visibility")
	}
	if _, _, err := it.next(ctx); err == nil {
		t.Fatal("missing shadowing HASH was silently discarded")
	}
}

func TestSnapshotLookupJoinUsesVisibleCandidates(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE left_t(k INTEGER)")
	h.exec("INSERT INTO left_t VALUES(100)")
	h.exec("CREATE TABLE right_t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO right_t SELECT i,i FROM generate_series(1,4097) AS g(i)")
	h.exec("UPDATE right_t SET v=999 WHERE id=100")
	h.exec("INSERT INTO right_t VALUES(4098,100)")
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	result := snapshotExecuteTest(t, s, h.ctx, "SELECT b.id FROM left_t a JOIN right_t b ON a.k=b.v")
	if len(result.rows) != 1 || result.rows[0][0].I != 4098 {
		t.Fatal("lookup join used a superseded value", result.rows)
	}
	if guard.rows.Load() > 16 {
		t.Fatal("lookup join fetched unrelated row images", guard.rows.Load())
	}
	h.expectRows("SELECT id FROM right_t r WHERE EXISTS(SELECT 1 FROM left_t l WHERE l.k=r.v) ORDER BY id", "4098")
}
