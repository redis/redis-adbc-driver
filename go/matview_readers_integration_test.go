// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

// Managed view generations use the same immutable references and statement pins
// as tables. Assert actual directories/indexes rather than the legacy mutable
// layout's retirement HASH and rekey leases.
func matviewGenerationHeld(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, m *tableMeta, rows int64) {
	t.Helper()
	if n, err := raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(m.prefix()))).Result(); err != nil || n != rows {
		t.Fatalf("pinned materialized view rows: %d, want %d: %v", n, rows, err)
	}
	if err := raw.Do(h.ctx, "FT.INFO", m.index()).Err(); err != nil {
		t.Fatal("pinned materialized view index lost", err)
	}
}

func stopMatviewTestMaintenance(t *testing.T, h *sqlHarness) {
	t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := st.(*statementImpl).conn.store
	if err := st.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	owner.stopSnapshotCompactor()
}

// Poll only: this helper must not run a collector itself. The Open-recovery
// cases stop all preexisting workers before starting the recovering connection.
func awaitMatviewOpenReclamation(t *testing.T, h *sqlHarness, raw goredis.UniversalClient, m *tableMeta) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n, err := raw.Exists(h.ctx, snapshotMembers(snapshotDataID(m.prefix()))).Result()
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			if err := raw.Do(h.ctx, "FT.INFO", m.index()).Err(); !isUnknownIndex(err) {
				t.Fatal("recovered view index remains", err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("new connection did not reclaim abandoned materialized view")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMatviewReaderDuringRefresh(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_reader")
	h.dropTables("it_mv_reader_source")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_reader") })
	h.exec("CREATE TABLE it_mv_reader_source AS SELECT n::bigint AS id FROM generate_series(1,25000) AS s(n)")
	h.exec("CREATE MATERIALIZED VIEW it_mv_reader AS SELECT id FROM it_mv_reader_source")
	raw := h.rawClient()
	old := h.inspectTable(raw, defaultSchema, "it_mv_reader")
	r, n := h.openStream(h.ctx, h.conn, "SELECT id FROM it_mv_reader", nil, map[string]string{OptionIntStreamBatchRows: "100", OptionStringStreamRequired: "true"})
	defer r.Release()
	if n != -1 {
		t.Fatalf("expected incremental reader, got count %d", n)
	}
	var count int64
	consume := func() {
		values := r.RecordBatch().Column(0).(*array.Int64)
		for i := 0; i < values.Len(); i++ {
			count++
			if values.IsNull(i) || values.Value(i) != count {
				t.Fatalf("old generation row %d: %s", count, values.ValueStr(i))
			}
		}
	}
	if !r.Next() {
		t.Fatalf("first batch: %v", r.Err())
	}
	consume()
	other := newSQLHarness(t)
	other.exec("DELETE FROM it_mv_reader_source WHERE id>50")
	other.exec("UPDATE it_mv_reader_source SET id=id+1000")
	other.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_reader")
	other.expectRows("SELECT count(*),min(id),max(id) FROM it_mv_reader", "50|1001|1050")
	matviewGenerationHeld(t, h, raw, old, 25000)
	for r.Next() {
		consume()
	}
	if r.Err() != nil || count != 25000 {
		t.Fatalf("old reader: %d rows: %v", count, r.Err())
	}
	r.Release()
	h.expectSnapshotReleased(raw, old.prefix(), old.index())
	other.expectRows("SELECT count(*),min(id),max(id) FROM it_mv_reader", "50|1001|1050")
}

func TestMatviewMultipleReaderGenerations(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_multi")
	h.dropTables("it_mv_multi_source")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_multi") })
	h.exec("CREATE TABLE it_mv_multi_source AS SELECT n AS id,0 AS v FROM generate_series(1,12000) s(n)")
	h.exec("CREATE MATERIALIZED VIEW it_mv_multi AS SELECT id,v FROM it_mv_multi_source")
	raw := h.rawClient()
	old := h.inspectTable(raw, defaultSchema, "it_mv_multi")
	a, b, c := newSQLHarness(t), newSQLHarness(t), newSQLHarness(t)
	opts := map[string]string{OptionIntStreamBatchRows: "100"}
	ra, _ := h.openStream(a.ctx, a.conn, "SELECT id,v FROM it_mv_multi", nil, opts)
	defer ra.Release()
	rb, _ := h.openStream(b.ctx, b.conn, "SELECT id,v FROM it_mv_multi", nil, opts)
	defer rb.Release()
	h.exec("UPDATE it_mv_multi_source SET v=1")
	h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_multi")
	middle := h.inspectTable(raw, defaultSchema, "it_mv_multi")
	rc, _ := h.openStream(c.ctx, c.conn, "SELECT id,v FROM it_mv_multi", nil, opts)
	defer rc.Release()
	h.exec("UPDATE it_mv_multi_source SET v=2")
	h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_multi")
	ra.Release()
	matviewGenerationHeld(t, h, raw, old, 12000)
	rc.Release()
	h.expectSnapshotReleased(raw, middle.prefix(), middle.index())
	var count int64
	for rb.Next() {
		rec := rb.RecordBatch()
		for i := 0; i < int(rec.NumRows()); i++ {
			count++
			if rec.Column(0).ValueStr(i) != fmt.Sprint(count) || rec.Column(1).ValueStr(i) != "0" {
				t.Fatal("mixed reader generations")
			}
		}
	}
	if count != 12000 || rb.Err() != nil {
		t.Fatalf("old reader: %d %v", count, rb.Err())
	}
	rb.Release()
	h.expectSnapshotReleased(raw, old.prefix(), old.index())
	h.expectRows("SELECT count(*),min(v),max(v) FROM it_mv_multi", "12000|2|2")
}

func TestMatviewReaderCleanup(t *testing.T) {
	for _, mode := range []string{"release", "cancel", "connection"} {
		t.Run(mode, func(t *testing.T) {
			h := newSQLHarness(t)
			h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_cleanup")
			h.dropTables("it_mv_cleanup_source")
			t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_cleanup") })
			h.exec("CREATE TABLE it_mv_cleanup_source AS SELECT n AS id FROM generate_series(1,12000) AS s(n)")
			h.exec("CREATE MATERIALIZED VIEW it_mv_cleanup AS SELECT id FROM it_mv_cleanup_source")
			reader := newSQLHarness(t)
			stmt, r, _ := h.openStmtStream(reader.ctx, reader.conn, "SELECT id FROM it_mv_cleanup", nil, map[string]string{OptionIntStreamBatchRows: "100"})
			defer r.Release()
			raw := h.rawClient()
			old := h.inspectTable(raw, defaultSchema, "it_mv_cleanup")
			h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_cleanup")
			matviewGenerationHeld(t, h, raw, old, 12000)
			switch mode {
			case "release":
				r.Release()
			case "cancel":
				if err := canceler(t, stmt).Cancel(h.ctx); err != nil {
					t.Fatal(err)
				}
			case "connection":
				if err := reader.conn.Close(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			h.expectSnapshotReleased(raw, old.prefix(), old.index())
		})
	}
}

func TestMatviewGenerationRecovery(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_recovery")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_recovery") })
	h.exec("CREATE MATERIALIZED VIEW it_mv_recovery AS SELECT 1 AS id")
	checkState(t, "truncate materialized view", h.stmtErr("TRUNCATE it_mv_recovery", nil), adbc.StatusInvalidArgument, "42809", "cannot change materialized view")
	raw := h.rawClient()
	old := h.inspectTable(raw, defaultSchema, "it_mv_recovery")
	st := &store{client: raw, snapshots: true}
	token := snapshotID()
	root, err := st.snapshotControl(h.ctx, "acquire", token, "", "").Text()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := st.snapshotControl(h.ctx, "release", token, "", root).Err(); err != nil {
			t.Error("fixture reader release failed", err)
		}
	}()
	h.exec("REFRESH MATERIALIZED VIEW it_mv_recovery")
	matviewGenerationHeld(t, h, raw, old, 1)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+token, `{"root":"`+root+`","expires":0}`).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectSnapshotReleased(raw, old.prefix(), old.index())
	h.expectRows("SELECT id FROM it_mv_recovery", "1")
	h.expectRows("SELECT * FROM it_mv_recovery; REFRESH MATERIALIZED VIEW it_mv_recovery WITH NO DATA; REFRESH MATERIALIZED VIEW it_mv_recovery; SELECT id FROM it_mv_recovery", "1")
}

func TestMatviewReaderLeasesSurviveIdle(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_idle")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_idle") })
	h.exec("CREATE MATERIALIZED VIEW it_mv_idle AS SELECT n AS id FROM generate_series(1,12000) s(n)")
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	r, _ := h.openStream(ctx, h.conn, "SELECT id FROM it_mv_idle", nil, map[string]string{OptionIntStreamBatchRows: "100"})
	defer r.Release()
	raw := h.rawClient()
	stream, ok := r.(*streamReader)
	if !ok || stream.snapshot == nil {
		t.Fatal("expected pinned stream")
	}
	p := stream.snapshot
	var record map[string]any
	text, err := raw.HGet(h.ctx, snapshotCatalogKey, "p:"+p.token).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(text), &record); err != nil {
		t.Fatal(err)
	}
	now, err := raw.Time(h.ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	record["expires"] = now.UnixMilli() + 1000
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "p:"+p.token, data).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := p.store.snapshotControl(h.ctx, "renew", p.token, "", p.root).Int(); err != nil || n != 1 {
		t.Fatal("reader lease renewal failed", err)
	}
	text, err = raw.HGet(h.ctx, snapshotCatalogKey, "p:"+p.token).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(text), &record); err != nil {
		t.Fatal(err)
	}
	if record["expires"].(float64) < float64(now.UnixMilli()+snapshotLease.Milliseconds()/2) {
		t.Fatal("lease was not extended")
	}
	other := newSQLHarness(t)
	other.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_idle")
	var rows int64
	for r.Next() {
		rows += r.RecordBatch().NumRows()
	}
	if r.Err() != nil || rows != 12000 {
		t.Fatalf("idle reader: %d %v", rows, r.Err())
	}
}

func TestMatviewRetirementOpenRecovery(t *testing.T) {
	h := newSQLHarness(t)
	stopMatviewTestMaintenance(t, h)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_open_recovery")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_open_recovery") })
	h.exec("CREATE MATERIALIZED VIEW it_mv_open_recovery AS SELECT n AS id FROM generate_series(1,12000) s(n)")
	r, _ := h.openStream(h.ctx, h.conn, "SELECT id FROM it_mv_open_recovery", nil, map[string]string{OptionIntStreamBatchRows: "100"})
	defer r.Release()
	raw := h.rawClient()
	old := h.inspectTable(raw, defaultSchema, "it_mv_open_recovery")
	other := newSQLHarness(t)
	stopMatviewTestMaintenance(t, other)
	other.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_open_recovery")
	matviewGenerationHeld(t, h, raw, old, 12000)
	p := r.(*streamReader).snapshot
	p.release()
	_ = newSQLHarness(t) // Its automatic reference collector is the only running worker.
	awaitMatviewOpenReclamation(t, h, raw, old)
	if n, err := p.store.snapshotControl(h.ctx, "renew", p.token, "", p.root).Int(); err != nil || n != 0 {
		t.Fatal("expired reader lease was resurrected", err)
	}
	if r.Next() || r.Err() == nil {
		t.Fatal("expired reader exposed buffered data")
	}
	checkState(t, "expired reader", r.Err(), adbc.StatusInvalidState, "55000", "reader lease expired")
	other.expectRows("SELECT count(*) FROM it_mv_open_recovery", "12000")
}

func TestACLMatviewRetirementOpenRecovery(t *testing.T) {
	a := newACLHarness(t)
	stopMatviewTestMaintenance(t, a.sqlHarness)
	a.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_acl_recovery")
	t.Cleanup(func() { a.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_acl_recovery") })
	a.exec("CREATE MATERIALIZED VIEW it_mv_acl_recovery AS SELECT n AS id FROM generate_series(1,12000) s(n)")
	r, _ := a.openStream(a.ctx, a.conn, "SELECT id FROM it_mv_acl_recovery", nil, map[string]string{OptionIntStreamBatchRows: "100"})
	defer r.Release()
	old := a.inspectTable(a.raw, defaultSchema, "it_mv_acl_recovery")
	other := newSQLHarness(t)
	stopMatviewTestMaintenance(t, other)
	other.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_acl_recovery")
	matviewGenerationHeld(t, a.sqlHarness, a.raw, old, 12000)
	r.(*streamReader).snapshot.release()
	// Keep this authorization assertion: a restricted Open must leave unrelated
	// retirement work for an authorized collector. Production reader isolation is
	// independently gated by TestSQLRBACSelect and its real Software principals.
	a.user("it_acl_mv_recovery", append(a.readOnlyCommands(), "~adbc:*", "~public:*")...)
	reader := a.connect("it_acl_mv_recovery", nil)
	reader.expectRows("SELECT 42", "42")
	matviewGenerationHeld(t, a.sqlHarness, a.raw, old, 12000)
	_ = newSQLHarness(t)
	awaitMatviewOpenReclamation(t, a.sqlHarness, a.raw, old)
}

func TestMatviewReadersRacingPublication(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_race")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_race") })
	h.exec("CREATE MATERIALIZED VIEW it_mv_race AS SELECT n AS id FROM generate_series(1,2000) s(n)")
	const readers = 4
	connections := make([]*sqlHarness, readers)
	for i := range connections {
		connections[i] = newSQLHarness(t)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, conn := range connections {
		wg.Add(1)
		go func(h *sqlHarness) {
			defer wg.Done()
			<-start
			for range 12 {
				h.expectRows("SELECT sum(id),count(*) FROM it_mv_race", "2001000|2000")
			}
		}(conn)
	}
	close(start)
	for range 8 {
		h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_race")
	}
	wg.Wait()
	h.expectRows("SELECT sum(id),count(*) FROM it_mv_race", "2001000|2000")
}

func TestMatviewLeasePreservesGenerationFence(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_fence")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_fence") })
	h.exec("CREATE MATERIALIZED VIEW it_mv_fence AS SELECT n AS id FROM generate_series(1,12000) s(n)")
	r, _ := h.openStream(h.ctx, h.conn, "SELECT id FROM it_mv_fence", nil, map[string]string{OptionIntStreamBatchRows: "100"})
	defer r.Release()
	raw := h.rawClient()
	old := h.inspectTable(raw, defaultSchema, "it_mv_fence")
	h.exec("DROP MATERIALIZED VIEW it_mv_fence")
	h.exec("CREATE MATERIALIZED VIEW it_mv_fence AS SELECT 99 AS id")
	current := h.inspectTable(raw, defaultSchema, "it_mv_fence")
	if current.prefix() == old.prefix() {
		t.Fatal("recreated view reused a pinned generation")
	}
	matviewGenerationHeld(t, h, raw, old, 12000)
	// Normal protocol release removes protection; no published reference graph
	// or legacy ownership counter is corrupted to manufacture this condition.
	r.(*streamReader).snapshot.release()
	h.expectSnapshotReleased(raw, old.prefix(), old.index())
	if r.Next() || r.Err() == nil {
		t.Fatal("reader bypassed its retired generation fence")
	}
	h.expectRows("SELECT id FROM it_mv_fence", "99")
}

func TestMatviewLegacyCollectorRefusesSnapshotGeneration(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_gc_fence")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_gc_fence") })
	h.exec("CREATE MATERIALIZED VIEW it_mv_gc_fence AS SELECT 9007199254740993::bigint AS id")
	raw := h.rawClient()
	m := h.inspectTable(raw, defaultSchema, "it_mv_gc_fence")
	// Reproduce a legacy caller without the managed client's command hook. Keep
	// the same endpoint, RESP3 and verified TLS options, including Cluster API.
	var legacy goredis.UniversalClient
	switch c := raw.(type) {
	case *goredis.Client:
		opts := *c.Options()
		legacy = goredis.NewClient(&opts)
	case *goredis.ClusterClient:
		opts := *c.Options()
		legacy = goredis.NewClusterClient(&opts)
	default:
		t.Fatal("unexpected native client type")
	}
	defer legacy.Close()
	encoded, err := marshalMeta(m)
	if err != nil {
		t.Fatal(err)
	}
	// Even an erroneous legacy retirement record must not authorize its DD
	// collector to delete a managed generation outside the reference protocol.
	if err := legacy.HSet(h.ctx, matviewRetiredKey, m.prefix(), encoded).Err(); err != nil {
		t.Fatal(err)
	}
	defer legacy.HDel(h.ctx, matviewRetiredKey, m.prefix())
	if err := (&store{client: legacy}).collectMatviewGeneration(h.ctx, m.prefix()); !isStatus(err, adbc.StatusInvalidState) || !strings.Contains(err.Error(), "require snapshot reclamation") {
		t.Fatal("legacy collector admitted managed row storage", err)
	}
	matviewGenerationHeld(t, h, raw, m, 1)
	h.expectRows("SELECT id FROM it_mv_gc_fence", "9007199254740993")
}
