// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"errors"
	"github.com/apache/arrow-adbc/go/adbc"
	"reflect"
	"testing"
)

func TestSnapshotMetadataOnlyAlterSharesRowsAndIndexes(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v VARCHAR(4) NOT NULL DEFAULT 'ab',n INTEGER DEFAULT 5 CONSTRAINT n_pos CHECK(n>=0))")
	h.exec("INSERT INTO t SELECT i,'ab',5 FROM generate_series(1,1001) AS g(i)")
	s := snapshotTestStore(t, raw)
	counter := &snapshotDeltaWrites{prefixes: map[string]bool{}, indexes: map[string]bool{}}
	s.client.AddHook(counter)
	oldCtx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(oldCtx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"ALTER TABLE t ALTER COLUMN v TYPE VARCHAR(8)",
		"ALTER TABLE t ALTER COLUMN v TYPE TEXT",
		"ALTER TABLE t ALTER COLUMN id TYPE INTEGER",
		"ALTER TABLE t ALTER COLUMN n SET DEFAULT 7",
		"ALTER TABLE t ALTER COLUMN v DROP DEFAULT",
		"ALTER TABLE t ALTER COLUMN v DROP NOT NULL",
		"ALTER TABLE t DROP CONSTRAINT n_pos",
	} {
		snapshotExecuteTest(t, s, h.ctx, sql)
		if counter.rows != 0 || len(counter.indexes) != 0 || len(counter.prefixes) != 0 {
			t.Fatalf("metadata-only ALTER rewrote rows/indexes: %s rows=%d indexes=%d", sql, counter.rows, len(counter.indexes))
		}
		ctx, currentPin, err := s.acquireSnapshotMode(h.ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := s.getTable(ctx, "public", "t")
		currentPin.release()
		if err != nil {
			t.Fatal(err)
		}
		if meta.prefix() != old.prefix() || meta.index() != old.index() || meta.SnapshotLineage != old.SnapshotLineage || !reflect.DeepEqual(meta.snapshotSegments(), old.snapshotSegments()) {
			t.Fatalf("metadata-only ALTER replaced shared layout: %s", sql)
		}
	}
	h.expectRows("SELECT COUNT(*),SUM(n) FROM t", "1001|5005")
	h.exec("INSERT INTO t(id) VALUES(1002)")
	h.expectRows("SELECT id,v,n FROM t WHERE id=1002", "1002|NULL|7")
	h.exec("UPDATE t SET n=-1 WHERE id=1002")
	h.expectRows("SELECT n FROM t WHERE id=1002", "-1")
	rows, err := s.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1"}, nil)
	if err != nil || len(rows) != 1 || rows[0]["v"] != "ab" || rows[0]["n"] != "5" {
		t.Fatalf("metadata ALTER changed pinned source: %v %v", rows, err)
	}
	if old.Columns[1].Type.Length != 4 || old.Columns[1].Nullable || len(old.Checks) != 1 {
		t.Fatal("metadata ALTER changed pinned schema")
	}
	// Conversion and validation still use private replacement rows and one final
	// index. The optimization must not bypass either class of work.
	counter.rows = 0
	clear(counter.indexes)
	clear(counter.prefixes)
	snapshotExecuteTest(t, s, h.ctx, "ALTER TABLE t ALTER COLUMN n TYPE BIGINT USING n*2")
	if counter.rows != 1002 || len(counter.indexes) != 1 {
		t.Fatalf("conversion avoided required rewrite: rows=%d indexes=%d", counter.rows, len(counter.indexes))
	}
	h.expectRows("SELECT COUNT(*),SUM(n) FROM t", "1002|10008")
	h.expectErrorText("ALTER TABLE t ALTER COLUMN v SET NOT NULL", `column "v" of relation "t" contains null values`)
	h.expectRows("SELECT v,n FROM t WHERE id=1002", "NULL|-2")
}

func TestSnapshotMetadataOnlyAlterStaysPrivateAndConflicts(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(v VARCHAR(4))")
	h.exec("INSERT INTO t VALUES('ab')")
	s := snapshotTestStore(t, raw)
	ctx, pin, work := snapshotStageSQL(t, s, h.ctx, "", "ALTER TABLE t ALTER COLUMN v TYPE VARCHAR(8)")
	staged, err := s.getTable(ctx, "public", "t")
	if err != nil || staged.Columns[0].Type.Length != 8 {
		t.Fatal("private ALTER missing", err)
	}
	h.expectErrorText("INSERT INTO t VALUES('abcde')", `value too long for type character varying(4)`)
	h.exec("UPDATE t SET v='cd'")
	err = s.publishSnapshot(ctx, pin, work)
	var ae adbc.Error
	if !errors.As(err, &ae) || string(ae.SqlState[:]) != "40001" {
		t.Fatalf("definition-only ALTER bypassed dependency CAS: %v", err)
	}
	s.abortSnapshot(pin, work)
	pin.release()
	h.expectRows("SELECT v FROM t", "cd")
	h.expectErrorText("INSERT INTO t VALUES('abcde')", `value too long for type character varying(4)`)
	h.exec("ALTER TABLE t ALTER COLUMN v TYPE VARCHAR(8)")
	h.exec("INSERT INTO t VALUES('abcde')")
	h.expectRows("SELECT v FROM t ORDER BY v", "abcde", "cd")
	snapshotReapTest(s, h.ctx)
	assertSnapshotDataReferences(t, h, s)
}
