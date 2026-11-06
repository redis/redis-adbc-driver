// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func TestSnapshotNewSQLSequenceResetAndStaleDefinition(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE SEQUENCE q CACHE 5")
	h.expectRows("SELECT nextval('q')", "1")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	peer := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	set, err := ParseScript("SELECT setval('q',100)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peer.execute(h.ctx, set[0], nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Another connection's SETVAL must invalidate h's reserved values 2..5.
	h.expectRows("SELECT nextval('q')", "101")
	h.exec("ALTER SEQUENCE q RESTART WITH 50")
	next, err := ParseScript("SELECT nextval('q')")
	if err != nil {
		t.Fatal(err)
	}
	_, err = peer.execute(ctx, next[0], nil, nil, nil)
	checkState(t, "stale sequence definition", err, adbc.StatusIO, "40001", "sequence definition changed concurrently")
	h.expectRows("SELECT nextval('q')", "50")
}

func TestSnapshotNewSQLCTELateFailureAtomic(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t VALUES(1,10),(2,20)")
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	h.expectError("WITH changed AS (UPDATE t SET v=99 WHERE id=1 RETURNING v) SELECT 1/(v-v) FROM changed", "division by zero")
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("late CTE error published")
	}
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|10", "2|20")
	h.expectRows("WITH changed AS (UPDATE t SET v=99 WHERE id=1 RETURNING v) SELECT changed.v,t.v FROM changed CROSS JOIN t WHERE t.id=1", "99|10")
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|99", "2|20")
}

func TestSnapshotNewSQLSequenceVolatilityAndGC(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE SEQUENCE q")
	h.exec("CREATE TABLE t(id BIGINT DEFAULT nextval('q'),v INTEGER CHECK(v>0))")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	m, err := s.getSequence(ctx, "public", "q")
	if err != nil {
		t.Fatal(err)
	}
	counter, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(snapshotSequenceKey(m.ID), metaPrefix), false)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolveSnapshotMetadata(ctx, strings.TrimPrefix(sequenceKey("public", "q"), metaPrefix), false)
	if err != nil {
		t.Fatal(err)
	}
	before := raw.Get(ctx, definition).Val()
	root := raw.HGet(ctx, snapshotCatalogKey, "root").Val()
	h.expectRows("SELECT nextval('q')", "1")
	if raw.HGet(ctx, snapshotCatalogKey, "root").Val() != root || raw.Get(ctx, definition).Val() != before {
		t.Fatal("volatile allocation mutated catalog definition")
	}
	h.expectError("INSERT INTO t(v) VALUES(1),(-1)", "check constraint")
	if raw.HGet(ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("failed insert published")
	}
	h.expectRows("SELECT COUNT(*) FROM t", "0")
	h.expectRows("SELECT nextval('q')", "4")
	h.exec("DROP TABLE t")
	h.exec("DROP SEQUENCE q")
	for range 32 {
		s.reapSnapshot(h.ctx)
	}
	if raw.Exists(h.ctx, counter).Val() != 1 {
		t.Fatal("old pinned catalog lost sequence counter")
	}
	pin.release()
	for range 64 {
		s.reapSnapshot(h.ctx)
	}
	if raw.Exists(h.ctx, counter).Val() != 0 {
		t.Fatal("unreferenced volatile counter leaked")
	}
}

type snapshotSequenceLostReply struct {
	snapshotHook
	calls int
}

func (h *snapshotSequenceLostReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if len(a) < 2 || !strings.EqualFold(fmt.Sprint(a[0]), "eval") || !strings.Contains(fmt.Sprint(a[1]), "volatile snapshot SQL sequence counter") {
			return next(ctx, cmd)
		}
		h.calls++
		if err := next(ctx, cmd); err != nil {
			return err
		}
		if h.calls == 1 {
			cmd.SetErr(errors.New("injected lost sequence reply"))
			return cmd.Err()
		}
		return nil
	}
}

func TestSnapshotNewSQLSequenceLostReply(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE SEQUENCE q")
	s := snapshotTestStore(t, raw)
	observer := &snapshotSequenceLostReply{}
	s.client.AddHook(observer)
	e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
	parsed, err := ParseScript("SELECT nextval('q')")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.execute(h.ctx, parsed[0], nil, nil, nil); err == nil || !strings.Contains(err.Error(), "allocation was not replayed") {
		t.Fatalf("unknown allocation not reported: %v", err)
	}
	if observer.calls != 1 {
		t.Fatalf("uncertain allocation replayed %d times", observer.calls)
	}
	res, err := e.execute(h.ctx, parsed[0], nil, nil, nil)
	if err != nil || len(res.rows) != 1 || res.rows[0][0].I != 2 || observer.calls != 2 {
		t.Fatalf("explicit next allocation: %+v %v calls=%d", res, err, observer.calls)
	}
}
