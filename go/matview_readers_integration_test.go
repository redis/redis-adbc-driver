// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestMatviewReaderDuringRefresh(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_reader")
	h.dropTables("it_mv_reader_source")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_reader") })
	h.exec("CREATE TABLE it_mv_reader_source AS SELECT n::bigint AS id FROM generate_series(1,25000) AS s(n)")
	h.exec("CREATE MATERIALIZED VIEW it_mv_reader AS SELECT id FROM it_mv_reader_source")
	raw := h.rawClient()
	store := &store{client: raw}
	old, err := store.getTable(h.ctx, defaultSchema, "it_mv_reader")
	if err != nil {
		t.Fatal(err)
	}
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
	if live, err := raw.SIsMember(h.ctx, prefixesKey, old.prefix()).Result(); err != nil || !live {
		t.Fatalf("old generation retired before reader ended: %v %v", live, err)
	}
	if raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() != true {
		t.Fatal("missing retired generation registration")
	}
	for r.Next() {
		consume()
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader interrupted after %d rows: %v", count, err)
	}
	if count != 25000 {
		t.Fatalf("got %d old rows", count)
	}
	if live, err := raw.SIsMember(h.ctx, prefixesKey, old.prefix()).Result(); err != nil || live {
		t.Fatalf("old generation not collected: %v %v", live, err)
	}
	if raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
		t.Fatal("retirement record leaked")
	}
	if err := store.searchDo(h.ctx, old.index(), "FT.INFO", old.index()).Err(); !isUnknownIndex(err) {
		t.Fatalf("old index remains: %v", err)
	}
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
	st := &store{client: raw}
	old, err := st.getTable(h.ctx, defaultSchema, "it_mv_multi")
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := newSQLHarness(t), newSQLHarness(t), newSQLHarness(t)
	opts := map[string]string{OptionIntStreamBatchRows: "100"}
	ra, _ := h.openStream(a.ctx, a.conn, "SELECT id,v FROM it_mv_multi", nil, opts)
	rb, _ := h.openStream(b.ctx, b.conn, "SELECT id,v FROM it_mv_multi", nil, opts)
	defer rb.Release()
	h.exec("UPDATE it_mv_multi_source SET v=1")
	h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_multi")
	middle, err := st.getTable(h.ctx, defaultSchema, "it_mv_multi")
	if err != nil {
		t.Fatal(err)
	}
	rc, _ := h.openStream(c.ctx, c.conn, "SELECT id,v FROM it_mv_multi", nil, opts)
	h.exec("UPDATE it_mv_multi_source SET v=2")
	h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_multi")
	ra.Release()
	if !raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
		t.Fatal("one reader released another reader's generation")
	}
	rc.Release()
	if raw.HExists(h.ctx, matviewRetiredKey, middle.prefix()).Val() {
		t.Fatal("middle generation leaked")
	}
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
	if raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
		t.Fatal("old generation leaked")
	}
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
			released := false
			defer func() {
				if !released {
					r.Release()
				}
			}()
			raw := h.rawClient()
			old, err := (&store{client: raw}).getTable(h.ctx, defaultSchema, "it_mv_cleanup")
			if err != nil {
				t.Fatal(err)
			}
			h.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_cleanup")
			if !raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
				t.Fatal("expected leased old generation")
			}
			switch mode {
			case "release":
				r.Release()
				released = true
			case "cancel":
				if err := canceler(t, stmt).Cancel(h.ctx); err != nil {
					t.Fatal(err)
				}
			case "connection":
				if err := reader.conn.Close(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(3 * time.Second)
			for raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
				t.Fatal("old generation leaked after " + mode)
			}
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
	st := &store{client: raw}
	old, err := st.getTable(h.ctx, defaultSchema, "it_mv_recovery")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a terminated reader: membership survives its expired alive key.
	if err = raw.SAdd(h.ctx, matviewReadersKey(old.prefix()), "abandoned-reader").Err(); err != nil {
		t.Fatal(err)
	}
	h.exec("REFRESH MATERIALIZED VIEW it_mv_recovery")
	if raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
		t.Fatal("abandoned reader prevented recovery")
	}
	h.expectRows("SELECT id FROM it_mv_recovery", "1")
	// Every statement in a script resolves the newly published generation.
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
	old, err := (&store{client: raw}).getTable(h.ctx, defaultSchema, "it_mv_idle")
	if err != nil {
		t.Fatal(err)
	}
	owners, err := raw.SMembers(h.ctx, matviewReadersKey(old.prefix())).Result()
	if err != nil || len(owners) != 1 {
		t.Fatalf("reader owners: %v %v", owners, err)
	}
	// Drive one renewal directly, without a time-dependent sleep or TTL change.
	token := owners[0]
	if err := raw.PExpire(h.ctx, rekeyAliveKey(token), time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	stream, ok := r.(*streamReader)
	if !ok {
		t.Fatalf("expected stream reader, got %T", r)
	}
	l := stream.transport.matviews.lease
	if !l.renew() {
		t.Fatal("reader lease renewal failed")
	}
	if ttl := raw.PTTL(h.ctx, rekeyAliveKey(token)).Val(); ttl < rekeyTTL/2 {
		t.Fatal(fmt.Sprintf("lease was not extended: %v", ttl))
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
	h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_open_recovery")
	t.Cleanup(func() { h.exec("DROP MATERIALIZED VIEW IF EXISTS it_mv_open_recovery") })
	h.exec("CREATE MATERIALIZED VIEW it_mv_open_recovery AS SELECT n AS id FROM generate_series(1,12000) s(n)")
	r, _ := h.openStream(h.ctx, h.conn, "SELECT id FROM it_mv_open_recovery", nil, map[string]string{OptionIntStreamBatchRows: "100"})
	defer r.Release()
	raw := h.rawClient()
	old, err := (&store{client: raw}).getTable(h.ctx, defaultSchema, "it_mv_open_recovery")
	if err != nil {
		t.Fatal(err)
	}
	other := newSQLHarness(t)
	other.exec("REFRESH MATERIALIZED VIEW CONCURRENTLY it_mv_open_recovery")
	if !raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
		t.Fatal("expected pending retirement")
	}
	l := r.(*streamReader).transport.matviews.lease
	// Simulate process death and expiry without relying on wall-clock sleeps.
	l.release()
	if err := raw.Del(h.ctx, rekeyAliveKey(l.owner)).Err(); err != nil {
		t.Fatal(err)
	}
	_ = newSQLHarness(t) // Open retries abandoned-generation collection.
	if raw.HExists(h.ctx, matviewRetiredKey, old.prefix()).Val() {
		t.Fatal("Open did not recover abandoned retirement")
	}
	if l.renew() {
		t.Fatal("expired reader lease was resurrected")
	}
	for r.Next() {
	}
	checkState(t, "expired reader", r.Err(), adbc.StatusInvalidState, "55000", "reader lease expired")
	other.expectRows("SELECT count(*) FROM it_mv_open_recovery", "12000")
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
