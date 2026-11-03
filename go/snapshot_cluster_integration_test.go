// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/adbc-drivers/redis/go/internal/tlstrust"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// The owned orchestrator verifies Software/QPF/durability before setting this
// URI. Every test clears the selected disposable database on all primaries.
func snapshotClusterHarness(t *testing.T) (*sqlHarness, *goredis.ClusterClient, adbc.DatabaseWithContext) {
	t.Helper()
	uri := os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI")
	if uri == "" {
		t.Skip("set REDIS_SNAPSHOT_CLUSTER_URI to the verified disposable Software cluster")
	}
	opts, extra, _, err := parseURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	opts.Protocol, opts.OnConnect, opts.DisableIdentity = 3, requireRESP3, true
	if opts.TLSConfig != nil {
		opts.TLSConfig.RootCAs, err = tlstrust.Roots()
		if err != nil {
			t.Fatal(err)
		}
	}
	clusterOpts := clusterOptions(opts)
	if extra != nil {
		clusterOpts.Addrs = append(clusterOpts.Addrs, extra.Addrs[1:]...)
	}
	raw := goredis.NewClusterClient(clusterOpts)
	ctx := context.Background()
	reset := func() {
		indexes, err := raw.Do(ctx, "FT._LIST").StringSlice()
		if err != nil {
			t.Fatal(err)
		}
		for _, index := range indexes {
			if err := raw.Do(ctx, "FT.DROPINDEX", index).Err(); err != nil {
				t.Fatal(err)
			}
		}
		if err := raw.ForEachMaster(ctx, func(ctx context.Context, node *goredis.Client) error { return node.FlushDB(ctx).Err() }); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	t.Cleanup(func() { reset(); _ = raw.Close() })
	impl, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(ctx, map[string]string{adbc.OptionKeyURI: uri, OptionStringCluster: "true"})
	if err != nil {
		t.Fatal(err)
	}
	impl.snapshotCompactionDisabled = true
	db := driverbase.NewDatabase(impl)
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx); _ = db.Close(ctx) })
	return &sqlHarness{t: t, ctx: ctx, conn: conn}, raw, db
}

func TestSnapshotClusterManagedStatements(t *testing.T) {
	h, raw, db := snapshotClusterHarness(t)
	slots, err := raw.ClusterSlots(h.ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]bool{}
	for _, slot := range slots {
		if len(slot.Nodes) == 0 {
			t.Fatal("slot owner missing")
		}
		owners[slot.Nodes[0].Addr] = true
	}
	// A promoted replica can share an endpoint with another primary. The
	// orchestrator verifies three physical primaries; require multiple routed
	// endpoints here, without assuming one primary per node after failover.
	if os.Getenv("REDIS_SNAPSHOT_CLUSTER_PRIMARIES") != "3" || len(owners) < 2 {
		t.Fatal("requires three verified primaries and multiple routed endpoints")
	}
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t VALUES (1,9007199254740993),(2,20),(3,30)")
	h.exec("UPDATE t SET v=42 WHERE id=2")
	h.exec("DELETE FROM t WHERE id=3")
	h.expectRows("SELECT id,v FROM t ORDER BY id", "1|9007199254740993", "2|42")
	h.exec("ALTER TABLE t ADD COLUMN n INTEGER DEFAULT 7")
	h.expectRows("SELECT id,v,n FROM t ORDER BY id", "1|9007199254740993|7", "2|42|7")
	h.exec("CREATE TEMP TABLE temp_t(v INTEGER)")
	h.exec("INSERT INTO temp_t VALUES (99)")
	other, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: other}
	h2.exec("COMMENT ON TABLE t IS 'other writer'")
	h.expectRows("SELECT * FROM temp_t", "99")
	h2.expectRows("SELECT COUNT(*) FROM t", "2")
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	key := snapshotWorkspace(root) + strings.TrimPrefix(metaKey("public", "t"), metaPrefix)
	if raw.Exists(h.ctx, key).Val() != 1 {
		t.Fatal("catalog copied without its table metadata")
	}
}

func TestSnapshotClusterDirectNodeFences(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE t(v INTEGER)")
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	s := stmt.(*statementImpl).conn.store
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(pin, id)
	names, err := s.claimNames(wctx, "public", "stale")
	if err != nil {
		t.Fatal(err)
	}
	var work map[string]any
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "w:"+id).Val()), &work); err != nil {
		t.Fatal(err)
	}
	work["expires"] = 0
	encoded, _ := json.Marshal(work)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+id, encoded).Err(); err != nil {
		t.Fatal(err)
	}
	meta := &tableMeta{Schema: "public", Name: "stale", KeyPrefix: names.prefix, IndexName: names.index, Columns: []columnMeta{{Name: "v", Type: typeInt32, Indexed: true}}}
	if err := s.searchDo(wctx, names.index, snapshotScannedIndexArgs(meta)...).Err(); err == nil {
		t.Fatal("direct node Search call bypassed expired writer fence")
	}
	node, err := s.client.(*goredis.ClusterClient).MasterForKey(wctx, names.prefix+"1")
	if err != nil {
		t.Fatal(err)
	}
	if err := node.HSet(wctx, names.prefix+"1", rowIDField, "1", "v", "9").Err(); err == nil {
		t.Fatal("direct node HASH mutation bypassed writer fence")
	}
	if raw.Exists(h.ctx, names.prefix+"1").Val() != 0 {
		t.Fatal("expired writer created a row")
	}
}

// The client runs outside the database containers. The orchestrator changes the
// deployment only after ready, and releases it after all shards/QPF are active.
func TestSnapshotClusterRecovery(t *testing.T) {
	if os.Getenv("REDIS_SNAPSHOT_CHAOS_DIR") == "" {
		t.Skip("requires owned recovery orchestrator")
	}
	h, raw, db := snapshotClusterHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,1001) AS g(i)")
	for i := 0; i < 3; i++ {
		h.exec(fmt.Sprintf("UPDATE t SET v=%d WHERE __rowid=1", 100+i))
	}
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	s := stmt.(*statementImpl).conn.store
	token := snapshotID()
	for i := 0; i < 2; i++ {
		if _, err := s.snapshotCompactionStep(h.ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	job, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || job == nil || job.After == "" {
		t.Fatal("missing durable compaction checkpoint", err)
	}
	// Expire ownership before the interruption, modeling a dead process without
	// adding two minutes to each fixture run. The output/checkpoint stays durable.
	var owner map[string]any
	if err := json.Unmarshal([]byte(raw.HGet(h.ctx, snapshotCatalogKey, "w:"+job.ID).Val()), &owner); err != nil {
		t.Fatal(err)
	}
	owner["expires"] = 0
	encoded, _ := json.Marshal(owner)
	if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+job.ID, encoded).Err(); err != nil {
		t.Fatal(err)
	}
	// Keep a published snapshot pinned and a separate private workspace alive.
	oldCtx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(oldCtx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	wctx, wpin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer wpin.release()
	wctx, wid, err := s.beginSnapshotWrite(wctx, wpin)
	if err != nil {
		t.Fatal(err)
	}
	defer s.abortSnapshot(wpin, wid)
	if err := s.client.Set(wctx, metaPrefix+"unpublished", "private", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetOption(h.ctx, OptionIntStreamBatchRows, "7"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(h.ctx, "SELECT id,v FROM t"); err != nil {
		t.Fatal(err)
	}
	reader, n, err := stmt.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if reader != nil {
			reader.Release()
		}
	}()
	if n != -1 || !reader.Next() {
		t.Fatal("stream did not start", reader.Err())
	}
	seen := map[int32]bool{}
	checkBatch := func() {
		rec := reader.RecordBatch()
		ids := rec.Column(0).(*array.Int32)
		vs := rec.Column(1).(*array.Int64)
		for i := 0; i < int(rec.NumRows()); i++ {
			id := ids.Value(i)
			want := int64(9007199254740993)
			if id == 1 {
				want = 102
			}
			if id < 1 || id > 1001 || seen[id] || vs.Value(i) != want {
				t.Fatal("interrupted stream mixed, duplicated or changed rows")
			}
			seen[id] = true
		}
	}
	checkBatch()
	dir := os.Getenv("REDIS_SNAPSHOT_CHAOS_DIR")
	if err := os.WriteFile(dir+"/ready", []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if _, err := os.Stat(dir + "/continue"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovery orchestrator timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for reader.Next() {
		checkBatch()
	}
	if reader.Err() == nil {
		if len(seen) != 1001 {
			t.Fatal("interrupted stream silently truncated")
		}
		t.Log("stream completed with all exact pinned values")
	} else {
		t.Log("interrupted stream returned an explicit error; cursor was not replayed")
	}
	reader.Release()
	reader = nil
	snapshotClusterAwaitRecovery(t, h)
	// Established pools must recover routing without replaying an uncertain SQL
	// operation. These statements are issued only after deployment readiness.
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "1001|102|9007199254740993")
	rows, err := s.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1", old.prefix() + "1001"}, nil)
	if err != nil {
		t.Log("pinned reader explicitly invalidated during interruption")
	} else if len(rows) != 2 || rows[0]["v"] != "102" || rows[1]["v"] != "9007199254740993" {
		t.Fatal("pinned exact values changed")
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if raw.Exists(h.ctx, snapshotWorkspace(root)+"unpublished").Val() != 0 {
		t.Fatal("unpublished metadata became visible")
	}
	resumed, err := s.readSnapshotCompaction(h.ctx)
	if err != nil || resumed == nil || resumed.Data != job.Data || resumed.After != job.After {
		t.Fatal("durable checkpoint changed", err)
	}
	if raw.ZCard(h.ctx, snapshotMembers(job.Data)).Val() != snapshotCompactionBatchRows {
		t.Fatal("durable output missing")
	}
	// Release the write-intent pin before the compactor's publication barrier.
	s.abortSnapshot(wpin, wid)
	wpin.release()
	snapshotCompleteJobTest(t, s, snapshotID())
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "1001|102|9007199254740993")
	fresh, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: fresh}
	h2.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "1001|102|9007199254740993")
	h2.exec("INSERT INTO t VALUES (1002,77)")
	h.expectRows("SELECT id,v FROM t WHERE id=1002", "1002|77")
	t.Log("established and new clients, pinned values, private manifest and durable compaction verified")
}

func TestSnapshotClusterMetadataDirectoryAndRekey(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t VALUES (1,9007199254740993),(2,20)")
	h.exec("DELETE FROM t WHERE id=2")
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	s := stmt.(*statementImpl).conn.store
	oldCtx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	old, err := s.getTable(oldCtx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringRenameRekey, "true"); err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE t RENAME TO renamed")
	h.expectRows("SELECT id,v FROM renamed", "1|9007199254740993")
	nextCtx, nextPin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.getTable(nextCtx, "public", "renamed")
	if err != nil {
		t.Fatal(err)
	}
	if next.prefix() == old.prefix() || next.index() == old.index() {
		t.Fatal("rekey retained old physical identity")
	}
	nextPin.release()
	rows, err := s.fetchSnapshotVisible(oldCtx, old, []string{old.prefix() + "1"}, nil)
	if err != nil || len(rows) != 1 || rows[0]["v"] != "9007199254740993" {
		t.Fatal("rekey changed pinned rows", err)
	}
	h.exec("INSERT INTO renamed VALUES (3,30)")
	h.expectRows("SELECT __rowid,id FROM renamed ORDER BY id", "1|1", "3|3")
	h.exec("CREATE TABLE occupied(v INTEGER)")
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	h.expectError("ALTER TABLE renamed RENAME TO occupied", "already exists")
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("failed rekey published")
	}
	h.expectRows("SELECT id,v FROM renamed ORDER BY id", "1|9007199254740993", "3|30")
	root = raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if raw.ZCard(h.ctx, snapshotMetadataKeys(root)).Val() == 0 {
		t.Fatal("metadata directory missing")
	}
	// Destroying the directory must fail closed, before copying/publishing a
	// replacement that could otherwise silently forget the existing tables.
	if err := raw.Del(h.ctx, snapshotMetadataKeys(root)).Err(); err != nil {
		t.Fatal(err)
	}
	h.expectError("CREATE TABLE lost(v INTEGER)", "metadata directory is incomplete")
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("incomplete metadata published")
	}
}

type snapshotPauseSearchCreate struct {
	snapshotHook
	index           string
	entered, resume chan struct{}
	once            sync.Once
}

func (h *snapshotPauseSearchCreate) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if a := cmd.Args(); len(a) > 1 && strings.EqualFold(fmt.Sprint(a[0]), "FT.CREATE") && fmt.Sprint(a[1]) == h.index {
			h.once.Do(func() { close(h.entered); <-h.resume })
		}
		return next(ctx, cmd)
	}
}

func TestSnapshotClusterReclaimsLateSearchCreate(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	stmt, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(h.ctx)
	s := stmt.(*statementImpl).conn.store
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	wctx, wid, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	names, err := s.claimNames(wctx, "public", "late")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.client.(*goredis.ClusterClient).MasterForKey(wctx, names.index)
	if err != nil {
		t.Fatal(err)
	}
	pause := &snapshotPauseSearchCreate{index: names.index, entered: make(chan struct{}), resume: make(chan struct{})}
	node.AddHook(pause)
	var resumed sync.Once
	resume := func() { resumed.Do(func() { close(pause.resume) }) }
	defer resume()
	meta := &tableMeta{Schema: "public", Name: "late", KeyPrefix: names.prefix, IndexName: names.index, Columns: []columnMeta{{Name: "v", Type: typeInt32, Indexed: true}}}
	done := make(chan error, 1)
	go func() { done <- s.searchDo(wctx, names.index, snapshotScannedIndexArgs(meta)...).Err() }()
	select {
	case <-pause.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("Search CREATE was not intercepted")
	}
	s.abortSnapshot(pin, wid)
	snapshotReapTest(s, h.ctx)
	if raw.HExists(h.ctx, snapshotMetadataGraph, "d:"+snapshotDataID(names.prefix)).Val() {
		t.Fatal("private data reservation was not reclaimed")
	}
	resume()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("late Search CREATE passed post-validation")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Search CREATE did not finish")
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != pin.root {
		t.Fatal("late Search CREATE published")
	}
	if err := raw.Do(h.ctx, "FT.INFO", names.index).Err(); err != nil {
		t.Fatal("test did not exercise a late global index")
	}
	if err := s.reapSnapshotOrphanIndex(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.INFO", names.index).Err(); err == nil || !isUnknownIndex(err) {
		t.Fatal("late global index was not reclaimed", err)
	}
}

func TestSnapshotClusterImportsAcrossPrimaries(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	slots, err := raw.ClusterSlots(h.ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]bool{}
	for i := 1; i <= 90; i++ {
		key := fmt.Sprintf("application:%d", i)
		if err := raw.HSet(h.ctx, key, "id", i, "v", "9007199254740993").Err(); err != nil {
			t.Fatal(err)
		}
		slot, err := raw.ClusterKeySlot(h.ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range slots {
			if int64(entry.Start) <= slot && slot <= int64(entry.End) {
				owners[entry.Nodes[0].Addr] = true
			}
		}
	}
	if len(owners) < 2 {
		t.Fatal("external capture did not span multiple primary endpoints")
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	if _, err := CopyHashes(h.ctx, options, HashCopyOptions{Prefix: "application:", Table: "imported"}); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT COUNT(*),MIN(id),MAX(id),MIN(v),MAX(v) FROM imported", "90|1|90|9007199254740993|9007199254740993")
	if err := raw.HSet(h.ctx, "application:1", "v", "42").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM imported WHERE id=1", "9007199254740993")
	h.exec("DROP TABLE imported")
	if raw.HGet(h.ctx, "application:1", "v").Val() != "42" {
		t.Fatal("managed-copy cleanup changed application data")
	}
}

func TestSnapshotClusterAutoAndPartitionProduction(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE t(id BIGINT,v INTEGER)")
	h.exec("INSERT INTO t VALUES (1,10),(2,20),(3,30)")
	h.exec("UPDATE t SET v=25 WHERE id=2")
	h.exec("DELETE FROM t WHERE id=1")
	storage := retainedFixture(t, raw, defaultRetainedLimits())
	impl, err := newDriverImpl(memory.DefaultAllocator).newDatabaseImpl(h.ctx, map[string]string{
		adbc.OptionKeyURI:                    os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"),
		OptionStringCluster:                  "auto",
		OptionStringPartitionResultNamespace: storage.namespace,
		// The owned orchestrator verifies noeviction before invoking this test.
		OptionStringPartitionControlPersistence: "guaranteed",
	})
	if err != nil {
		t.Fatal(err)
	}
	impl.snapshotCompactionDisabled = true
	db := driverbase.NewDatabase(impl)
	defer db.Close(h.ctx)
	conn, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(h.ctx)
	h2 := &sqlHarness{t: t, ctx: h.ctx, conn: conn}
	h2.expectRows("SELECT id,v FROM t ORDER BY id", "2|25", "3|30")
	_, live := planPartitions(t, h2, "SELECT id FROM t WHERE v>=25", 2)
	var ids []int64
	for _, part := range live.PartitionIDs {
		reader, err := conn.ReadPartition(h.ctx, part)
		if err != nil {
			t.Fatal(err)
		}
		for reader.Next() {
			col := reader.RecordBatch().Column(0).(*array.Int64)
			for i := 0; i < col.Len(); i++ {
				ids = append(ids, col.Value(i))
			}
		}
		if err := reader.Err(); err != nil {
			t.Fatal(err)
		}
		reader.Release()
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int64{2, 3}) {
		t.Fatal("wrong live partition values", ids)
	}
	schema, parts, rows := retainedAPIPlan(t, conn, "SELECT id FROM t WHERE v>=25 ORDER BY id")
	if rows != 2 {
		t.Fatal("wrong retained count", rows)
	}
	h2.exec("TRUNCATE t")
	if got := retainedAPIReadIDs(t, conn, schema, parts); !slices.Equal(got, []int64{2, 3}) {
		t.Fatal("retained values changed", got)
	}
	descriptor, err := decodeRetainedPartition(parts.PartitionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	h2.exec("DROP PARTITION RESULT '" + descriptor.Reference.JobID + "'")
	if reader, err := conn.ReadPartition(h.ctx, parts.PartitionIDs[0]); err == nil {
		reader.Release()
		t.Fatal("revoked retained result was readable")
	}
}

// This is an application's explicit readiness loop, not driver replay. A failed
// read is discarded; every successful result must be complete and exact.
func snapshotClusterAwaitRecovery(t *testing.T, h *sqlHarness) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	failures := 0
	for {
		stmt, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := stmt.SetSqlQuery(h.ctx, "SELECT COUNT(*),MIN(v),MAX(v) FROM t"); err != nil {
			t.Fatal(err)
		}
		reader, _, err := stmt.ExecuteQuery(h.ctx)
		if err == nil {
			count := 0
			for reader.Next() {
				rec := reader.RecordBatch()
				for i := 0; i < int(rec.NumRows()); i++ {
					if rec.Column(0).(*array.Int64).Value(i) != 1001 || rec.Column(1).(*array.Int64).Value(i) != 102 || rec.Column(2).(*array.Int64).Value(i) != 9007199254740993 {
						t.Fatal("recovery returned incorrect aggregate values")
					}
					count++
				}
			}
			err = reader.Err()
			reader.Release()
			if err == nil && count != 1 {
				t.Fatal("recovery silently truncated its result")
			}
		}
		_ = stmt.Close(h.ctx)
		if err == nil {
			t.Logf("SQL recovered after %d explicit transient read failures", failures)
			return
		}
		var ae adbc.Error
		if !errors.As(err, &ae) || (ae.Code != adbc.StatusIO && ae.Code != adbc.StatusInvalidState) || time.Now().After(deadline) {
			t.Fatalf("SQL did not recover: %v", err)
		}
		failures++
		time.Sleep(100 * time.Millisecond)
	}
}
