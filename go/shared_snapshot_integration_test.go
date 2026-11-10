// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func sharedSnapshotSet(t *testing.T, h *sqlHarness, key, value string) {
	t.Helper()
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, key, value); err != nil {
		t.Fatal(err)
	}
}

func TestSharedReadSnapshotCatalog(t *testing.T) {
	h, raw, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE orders(id INTEGER, v BIGINT)")
	h.exec("INSERT INTO orders VALUES(1,9007199254740993),(1,9007199254740993),(2,NULL)")
	h.exec("CREATE TABLE items(id INTEGER, label VARCHAR)")
	h.exec("INSERT INTO items VALUES(1,'old'),(2,NULL)")
	h.exec("CREATE VIEW item_view AS SELECT id,label FROM items")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	w := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "acquire")
	root, err := h.conn.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringReadSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("wrong acquired generation")
	}
	before, err := h.conn.GetTableSchema(h.ctx, nil, ptr("public"), "items")
	if err != nil {
		t.Fatal(err)
	}
	expectedObjects, err := h.conn.GetObjects(h.ctx, adbc.ObjectDepthAll, nil, ptr("public"), ptr("orders"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !expectedObjects.Next() {
		t.Fatal(expectedObjects.Err())
	}
	expectedMetadata := expectedObjects.RecordBatch()
	expectedMetadata.Retain()
	defer expectedMetadata.Release()
	if expectedObjects.Next() || expectedObjects.Err() != nil {
		t.Fatal("unexpected metadata batches")
	}
	expectedObjects.Release()
	h.expectRows("SELECT id,v FROM orders ORDER BY id", "1|9007199254740993\n1|9007199254740993\n2|NULL")
	w.exec("INSERT INTO orders VALUES(3,7)")
	w.exec("UPDATE orders SET v=42 WHERE id=1")
	w.exec("DELETE FROM orders WHERE id=2")
	w.exec("ALTER TABLE items ADD COLUMN extra INTEGER DEFAULT 7")
	w.exec("DROP VIEW item_view")
	w.exec("CREATE VIEW item_view AS SELECT id,extra FROM items")
	w.exec("TRUNCATE items")
	w.exec("DROP TABLE orders")
	h.expectRows("SELECT id,v FROM orders ORDER BY id", "1|9007199254740993\n1|9007199254740993\n2|NULL")
	h.expectRows("SELECT id,label FROM item_view ORDER BY id", "1|old\n2|NULL")
	after, err := h.conn.GetTableSchema(h.ctx, nil, ptr("public"), "items")
	if err != nil {
		t.Fatal(err)
	}
	if !before.Equal(after) {
		t.Fatalf("schema changed: %s / %s", before, after)
	}
	vr, err := h.conn.GetTableSchema(h.ctx, nil, ptr("public"), "item_view")
	if err != nil {
		t.Fatal(err)
	}
	if vr.Field(1).Name != "label" {
		t.Fatal("view definition changed")
	}
	objects, err := h.conn.GetObjects(h.ctx, adbc.ObjectDepthAll, nil, ptr("public"), ptr("orders"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !objects.Next() {
		t.Fatal(objects.Err())
	}
	if !array.RecordEqual(expectedMetadata, objects.RecordBatch()) {
		t.Fatal("complete metadata changed after publication")
	}
	if objects.Next() || objects.Err() != nil {
		t.Fatal("unexpected metadata batches")
	}
	objects.Release()
	w.expectRows("SELECT COUNT(*) FROM items", "0")
	w.expectError("SELECT * FROM orders", "does not exist")
	same, err := h.conn.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringReadSnapshotID)
	if err != nil || same != root {
		t.Fatalf("view changed: %s %v", same, err)
	}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "release")
	h.expectError("SELECT * FROM orders", "does not exist")
}

func TestSharedReadSnapshotStreamOwnership(t *testing.T) {
	h, _, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,12001) AS g(i)")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	w := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "acquire")
	query := func() adbc.StatementWithContext {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = st.SetOption(h.ctx, OptionIntStreamBatchRows, "7"); err != nil {
			t.Fatal(err)
		}
		if err = st.SetSqlQuery(h.ctx, "SELECT id,v FROM t"); err != nil {
			t.Fatal(err)
		}
		return st
	}
	st := query()
	defer st.Close(h.ctx)
	r, n, err := st.ExecuteQuery(h.ctx)
	if err != nil || n != -1 {
		t.Fatalf("stream: %d %v", n, err)
	}
	defer r.Release()
	if !r.Next() {
		t.Fatal(r.Err())
	}
	count := 0
	check := func() {
		batch := r.RecordBatch()
		values := batch.Column(1).(*array.Int64)
		for i := 0; i < int(batch.NumRows()); i++ {
			if values.Value(i) != 9007199254740993 {
				t.Fatal("mixed streamed generation")
			}
			count++
		}
	}
	check()
	w.exec("UPDATE t SET v=42")
	w.exec("TRUNCATE t")
	w.exec("DROP TABLE t")
	for r.Next() {
		check()
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 12001 {
		t.Fatal(count)
	}
	// Exhaustion/early consumer close/cancellation do not release the parent.
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "12001|9007199254740993|9007199254740993")
	early := query()
	er, _, err := early.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	er.Release()
	early.Close(h.ctx)
	h.expectRows("SELECT COUNT(*) FROM t", "12001")
	ctx, cancel := context.WithCancel(h.ctx)
	canceled := query()
	cr, _, err := canceled.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for cr.Next() {
	}
	if cr.Err() == nil {
		t.Fatal("cancellation succeeded silently")
	}
	cr.Release()
	canceled.Close(h.ctx)
	h.expectRows("SELECT COUNT(*) FROM t", "12001")
	pending := query()
	defer pending.Close(h.ctx)
	pr, _, err := pending.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Release()
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "release")
	for pr.Next() {
	}
	if pr.Err() == nil {
		t.Fatal("released parent allowed successful stream")
	}
}

func TestSharedReadSnapshotFailures(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	for _, value := range []string{"0", "999", "86400001", "-1", "garbage"} {
		if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringReadSnapshotRetention, value); err == nil {
			t.Fatal(value)
		}
	}
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringReadSnapshotID, "other"); err == nil {
		t.Fatal("accepted attachment")
	}
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringReadSnapshot, "other"); err == nil {
		t.Fatal("accepted handle")
	}
	sharedSnapshotSet(t, h, OptionStringReadSnapshotRetention, "1000")
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "acquire")
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringReadSnapshot, "acquire"); err == nil {
		t.Fatal("reacquired live view")
	}
	h.expectError("INSERT INTO t VALUES(2)", "read-only")
	h.expectError("CREATE TEMP TABLE temp_t(id INTEGER)", "read-only")
	h.expectError("SELECT nextval('missing')", "read-only")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	st.SetSqlQuery(h.ctx, "SELECT * FROM t")
	if _, _, _, err := st.ExecutePartitions(h.ctx); err == nil {
		t.Fatal("exported partitions")
	}
	if _, err := h.conn.ReadPartition(h.ctx, nil); err == nil {
		t.Fatal("accepted worker")
	}
	time.Sleep(1100 * time.Millisecond)
	h.expectError("SELECT * FROM t", "snapshot")
	if _, err := h.conn.GetTableSchema(h.ctx, nil, ptr("public"), "t"); err == nil {
		t.Fatal("expired metadata fell back")
	}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "release")
	h.expectRows("SELECT id FROM t", "1")
	sharedSnapshotSet(t, h, OptionStringReadSnapshotRetention, "60000")
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "acquire")
	// Remove the real server pin to model lost/collected ownership. This must
	// never bind a replacement root even though the live table is readable.
	fields := raw.HKeys(h.ctx, snapshotCatalogKey).Val()
	for _, field := range fields {
		if strings.HasPrefix(field, "p:") {
			raw.HDel(h.ctx, snapshotCatalogKey, field)
		}
	}
	h.expectError("SELECT * FROM t", "snapshot")
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "release")
	h.exec("CREATE TEMP TABLE temp_t(id INTEGER)")
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringReadSnapshot, "acquire"); err == nil {
		t.Fatal("accepted temporary objects")
	}
}

func TestSharedReadSnapshotQuotaAndClose(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER)")
	// Populate the actual bounded catalog with live reader reservations using
	// server time. No SQL work or input may run after refused acquisition.
	script := `local t=redis.call('TIME');local expiry=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)+120000;local root=redis.call('HGET',KEYS[1],'root');for i=1,128 do redis.call('HSET',KEYS[1],'p:quota-'..i,cjson.encode({root=root,expires=expiry})) end;return 1`
	if err := raw.Eval(h.ctx, script, []string{snapshotCatalogKey}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringReadSnapshot, "acquire"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("quota: %v", err)
	}
	for i := 1; i <= 128; i++ {
		raw.HDel(h.ctx, snapshotCatalogKey, fmt.Sprintf("p:quota-%d", i))
	}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "acquire")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	impl := st.(*statementImpl).conn
	st.Close(h.ctx)
	token := impl.readSnapshot.pin.token
	if !raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
		t.Fatal("missing parent pin")
	}
	if err := h.conn.Close(h.ctx); err != nil {
		t.Fatal(err)
	}
	if raw.HExists(h.ctx, snapshotCatalogKey, "p:"+token).Val() {
		t.Fatal("Close retained parent pin")
	}
	if _, _, err := impl.acquireReadSnapshot(h.ctx); err == nil {
		t.Fatal("closed view acquired live state")
	}
}

func TestSharedReadSnapshotProcessDeath(t *testing.T) {
	if os.Getenv("REDIS_SHARED_SNAPSHOT_CHILD") == "1" {
		db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(context.Background(), testUsageOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}))
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.(adbc.GetSetOptionsWithContext).SetOption(context.Background(), OptionStringReadSnapshot, "acquire"); err != nil {
			t.Fatal(err)
		}
		fmt.Println("shared snapshot acquired")
		// The parent kills the process without calling Close or releasing the view.
		select {}
	}
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.getTable(ctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	pin.release()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestSharedReadSnapshotProcessDeath$")
	child.Env = append(os.Environ(), "REDIS_SHARED_SNAPSHOT_CHILD=1")
	pipe, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if scanner.Text() == "shared snapshot acquired" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatalf("child failed: %s", stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("child acquisition timed out")
	}
	h.exec("DROP TABLE t")
	if raw.Exists(h.ctx, meta.prefix()+"1").Val() != 1 {
		t.Fatal("child did not retain generation")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	deadline := time.Now().Add(snapshotLease + 10*time.Second)
	for raw.Exists(h.ctx, meta.prefix()+"1").Val() != 0 {
		snapshotReapTest(s, h.ctx)
		if time.Now().After(deadline) {
			t.Fatal("dead process generation was not collected after lease expiry")
		}
		time.Sleep(time.Second)
	}
}

func TestSharedReadSnapshotSubstrait(t *testing.T) {
	h, _, db := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id BIGINT NOT NULL)")
	h.exec("INSERT INTO t VALUES(9007199254740993)")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	w := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "acquire")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, subTestPlan(t, "t", []string{"id"}, subI64))); err != nil {
		t.Fatal(err)
	}
	w.exec("DROP TABLE t")
	for i := 0; i < 2; i++ {
		r, _, err := st.ExecuteQuery(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := subCollect(t, r)
		if len(rows) != 1 || rows[0][0].I != 9007199254740993 {
			t.Fatal(rows)
		}
	}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "release")
}

func TestSharedReadSnapshotMissingData(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE t(id INTEGER)")
	h.exec("INSERT INTO t VALUES(1)")
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "acquire")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	c := st.(*statementImpl).conn
	ctx, pin, err := c.acquireReadSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.store.getTable(ctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	pin.release()
	if err := raw.Del(h.ctx, m.prefix()+"1").Err(); err != nil {
		t.Fatal(err)
	}
	if err := h.execErr("SELECT id FROM t"); err == nil {
		t.Fatal("out-of-protocol row deletion produced successful partial result")
	}
	sharedSnapshotSet(t, h, OptionStringReadSnapshot, "release")
}
