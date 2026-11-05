// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sp "github.com/adbc-drivers/redis/go/internal/substrait/gen/substrait"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestSnapshotSubstraitSegmentVisibility(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL,v BIGINT NOT NULL)")
	h.exec("INSERT INTO t VALUES(1,9007199254740993),(2,9007199254740991),(3,9007199254740993)")
	h.exec("UPDATE t SET v=9007199254740991 WHERE id=1")
	h.exec("UPDATE t SET v=9007199254740993 WHERE id=2")
	h.exec("DELETE FROM t WHERE id=3")
	p := subTestPlan(t, "t", []string{"id", "v"}, subI64, subI64)
	condition := subFunctionPlan(p, "gte:any_any", subComparison, []*sp.Expression{subRef(1), subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_I64{I64: 9007199254740993}})}, subBoolType(false))
	root := p.Relations[0].GetRoot()
	root.Input = &sp.Rel{RelType: &sp.Rel_Filter{Filter: &sp.FilterRel{Input: root.Input, Condition: condition}}}
	for _, materialized := range []bool{false, true} {
		t.Run(fmt.Sprint(materialized), func(t *testing.T) {
			statement, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer statement.Close(h.ctx)
			if err := statement.SetOption(h.ctx, OptionStringStreamResults, fmt.Sprint(!materialized)); err != nil {
				t.Fatal(err)
			}
			if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
				t.Fatal(err)
			}
			reader, _, err := statement.ExecuteQuery(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := subCollect(t, reader)
			if len(rows) != 1 || rows[0][0].I != 2 || rows[0][1].I != 9007199254740993 {
				t.Fatal("stale predicate/version/tombstone or rounded BIGINT", rows)
			}
		})
	}
}

func TestSnapshotSubstraitRetainsGeneration(t *testing.T) {
	for _, change := range []string{"compact_drop", "alter", "truncate", "drop"} {
		t.Run(change, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT NOT NULL,v BIGINT NOT NULL,payload TEXT)")
			h.exec("INSERT INTO t SELECT i,9007199254740993,CASE WHEN i%5=0 THEN NULL ELSE 'row-'||i END FROM generate_series(1,1201) AS g(i)")
			h.exec("UPDATE t SET v=v+1 WHERE id=1")
			h.exec("DELETE FROM t WHERE id=2")
			h.exec("UPDATE t SET payload=NULL WHERE id=3")
			statement, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer statement.Close(h.ctx)
			if err := statement.SetOption(h.ctx, OptionIntStreamBatchRows, "7"); err != nil {
				t.Fatal(err)
			}
			if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, subTestPlan(t, "t", []string{"id", "v", "payload"}, subI64, subI64, subStr))); err != nil {
				t.Fatal(err)
			}
			reader, _, err := statement.ExecuteQuery(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Release()
			live, ok := reader.(*subReader)
			if !ok || live.snapshot == nil {
				t.Fatal("live Substrait reader does not own its generation")
			}
			token := live.snapshot.token
			base := live.source.req.meta.snapshotSegments()[0].Prefix
			seen := map[int64]bool{}
			check := func(recordRows int64) {
				record := reader.RecordBatch()
				if record.NumRows() != recordRows {
					t.Fatal("unexpected record size")
				}
				for i := 0; i < int(record.NumRows()); i++ {
					id := record.Column(0).(*array.Int64).Value(i)
					want := int64(9007199254740993)
					if id == 1 {
						want++
					}
					payload := record.Column(2).(*array.String)
					null := id == 3 || id%5 == 0
					if id < 1 || id > 1201 || id == 2 || seen[id] || record.Column(1).(*array.Int64).Value(i) != want || payload.IsNull(i) != null || (!null && payload.Value(i) != fmt.Sprintf("row-%d", id)) {
						t.Fatalf("mixed or incomplete generation at id %d", id)
					}
					seen[id] = true
				}
			}
			if !reader.Next() {
				t.Fatal(reader.Err())
			}
			check(7)
			maintenance := snapshotTestStore(t, raw)
			switch change {
			case "compact_drop":
				h.exec("UPDATE t SET v=v+100")
				snapshotDrainCompactionTest(t, maintenance)
				h.exec("DROP TABLE t")
			case "alter":
				h.exec("ALTER TABLE t ADD COLUMN extra INTEGER DEFAULT 7")
			case "truncate":
				h.exec("TRUNCATE t")
			case "drop":
				h.exec("DROP TABLE t")
			}
			snapshotReapTest(maintenance, h.ctx)
			if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() || raw.Exists(h.ctx, base+"1201").Val() != 1 {
				t.Fatal("live reader lost its lease or uncached source row")
			}
			for reader.Next() {
				check(reader.RecordBatch().NumRows())
			}
			if reader.Err() != nil || len(seen) != 1200 {
				t.Fatal("incomplete retained generation", len(seen), reader.Err())
			}
			if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
				t.Fatal("exhausted reader retained its snapshot lease")
			}
			snapshotReapTest(maintenance, h.ctx)
			if raw.Exists(h.ctx, base+"1201").Val() != 0 {
				t.Fatal("completed reader prevented source reclamation")
			}
		})
	}
}

func TestSnapshotSubstraitMetadataAndCancellationReleaseLeases(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	h.exec("INSERT INTO t SELECT i FROM generate_series(1,501) AS g(i)")
	statement, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	for range 8 {
		if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, subTestPlan(t, "t", []string{"id"}, subI64))); err != nil {
			t.Fatal(err)
		}
		if err := statement.Prepare(h.ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := statement.(*statementImpl).ExecuteSchema(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	fields, err := raw.HKeys(h.ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if strings.HasPrefix(field, "p:") {
			t.Fatal("metadata-only plan operations retained a reader pin")
		}
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	reader, _, err := statement.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	live := reader.(*subReader)
	token := live.snapshot.token
	cancel() // No Next/Release is needed to trigger lease cleanup.
	waitFor(t, "canceled Substrait snapshot released", func() bool { return !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() })
	if reader.Next() || reader.Err() == nil {
		t.Fatal("canceled reader exposed rows")
	}
	h.expectRows("SELECT COUNT(*) FROM t", "501")
	if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, subTestPlan(t, "t", []string{"id"}, subI64))); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotSubstraitRejectsExpiredReaderPin(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	h.exec("INSERT INTO t SELECT i FROM generate_series(1,501) AS g(i)")
	statement, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	if err := statement.SetOption(h.ctx, OptionIntStreamBatchRows, "1"); err != nil {
		t.Fatal(err)
	}
	if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, subTestPlan(t, "t", []string{"id"}, subI64))); err != nil {
		t.Fatal(err)
	}
	reader, _, err := statement.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	live := reader.(*subReader)
	if live.snapshot == nil || !reader.Next() || len(live.input) == 0 {
		t.Fatal("fixture did not retain a lease and buffered input")
	}
	if n, err := raw.HDel(h.ctx, snapshotCatalogKey, "p:"+live.snapshot.token).Result(); err != nil || n != 1 {
		t.Fatal("fixture did not revoke the reader lease", n, err)
	}
	if reader.Next() || reader.Err() == nil {
		t.Fatal("expired reader exposed buffered rows")
	}
	h.expectRows("SELECT COUNT(*) FROM t", "501")
}

func TestSnapshotSubstraitConnectionCloseReleasesLease(t *testing.T) {
	h, raw, db := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	h.exec("INSERT INTO t SELECT i FROM generate_series(1,501) AS g(i)")
	statement, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(h.ctx)
	if err := statement.SetOption(h.ctx, OptionIntStreamBatchRows, "1"); err != nil {
		t.Fatal(err)
	}
	if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, subTestPlan(t, "t", []string{"id"}, subI64))); err != nil {
		t.Fatal(err)
	}
	reader, _, err := statement.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	live := reader.(*subReader)
	if live.snapshot == nil || !reader.Next() {
		t.Fatal("fixture did not open a live snapshot reader", reader.Err())
	}
	token := live.snapshot.token
	record := reader.RecordBatch()
	record.Retain()
	defer record.Release()
	first := record.Column(0).(*array.Int64).Value(0)
	if err := h.conn.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
		t.Fatal("connection Close returned before reader lease cleanup")
	}
	if reader.Next() || reader.Err() == nil {
		t.Fatal("reader survived connection closure")
	}
	if record.NumRows() != 1 || record.Column(0).(*array.Int64).Value(0) != first {
		t.Fatal("connection closure invalidated caller-owned Arrow buffers")
	}
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	fresh := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	fresh.expectRows("SELECT COUNT(*) FROM t", "501")
}
