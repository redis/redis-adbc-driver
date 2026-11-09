// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"os"
	"reflect"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
	"strings"
	"testing"
	"time"
)

func retainOperationFixture(t *testing.T, count int) (*sqlHarness, *store, context.Context, *tableMeta, *snapshotImportSpool, string) {
	t.Helper()
	h, s, ctx, meta, spool := snapshotClusterCaptureFixture(t, count, func(h *sqlHarness) {
		h.exec("CREATE TABLE operation_anchor (v INTEGER)")
		h.exec("INSERT INTO operation_anchor VALUES (1)")
	})
	if err := spool.complete(ctx); err != nil {
		t.Fatal(err)
	}
	plan, err := captureSnapshotOperationPlan(ctx, "import", int64(count), meta)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.retainSnapshotOperation(ctx, plan, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// A successful detach outlives the ordinary owner's pin and deferred abort.
	original := snapshotOf(ctx)
	original.pin.release()
	s.abortSnapshot(original.pin, id)
	return h, s, ctx, meta, spool, id
}

func TestSnapshotClusterOperationRetentionTakeoverAndOutcomeGC(t *testing.T) {
	h, s, old, _, _, id := retainOperationFixture(t, 3)
	status, err := s.snapshotOperationStatus(h.ctx, id)
	if err != nil || status.State != "ready" || status.OwnerActive || status.RemainingRows != 3 {
		t.Fatal("detached operation status", status, err)
	}
	// Change an independent logical table, then try normal cleanup. The original
	// base must remain retained for dependency validation and continuation.
	h.exec("INSERT INTO operation_anchor VALUES (2)")
	for i := 0; i < 16; i++ {
		s.reapSnapshot(h.ctx)
	}
	if n := s.client.HExists(h.ctx, snapshotCatalogKey, "w:"+snapshotOf(old).pin.root).Val(); !n {
		t.Fatal("detached operation lost its base")
	}
	owner, terminal, err := s.takeSnapshotOperation(h.ctx, id)
	if err != nil || terminal != nil {
		t.Fatal("operation takeover", terminal, err)
	}
	defer owner.Pin.release()
	if err := s.prepareOperationPublication(owner); err != nil {
		t.Fatal(err)
	}
	if err := s.publishPreparedSnapshot(owner.Context, owner.Pin, id); err != nil {
		t.Fatal(err)
	}
	owner.Pin.release()
	h.expectRows("SELECT COUNT(*),MAX(v),COUNT(__key) FROM capture_barrier", "3|9007199254740993|3")
	h.expectRows("SELECT SUM(v) FROM operation_anchor", "3")
	h.exec("INSERT INTO operation_anchor VALUES (3)")
	for i := 0; i < 128 && s.client.HExists(h.ctx, snapshotCatalogKey, "w:"+id).Val(); i++ {
		s.reapSnapshot(h.ctx)
	}
	if s.client.HExists(h.ctx, snapshotCatalogKey, "w:"+id).Val() {
		t.Fatal("test did not retire the committed manifest")
	}
	status, err = s.snapshotOperationStatus(h.ctx, id)
	if err != nil || status.State != "committed" || status.AffectedRows != 3 {
		t.Fatal("terminal outcome did not survive manifest retirement", status, err)
	}
}

func TestSnapshotClusterOperationExclusiveOwnerAndStaleFence(t *testing.T) {
	h, s, old, meta, spool, id := retainOperationFixture(t, 3)
	first, _, err := s.takeSnapshotOperation(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Pin.release()
	if _, _, err := s.takeSnapshotOperation(h.ctx, id); err == nil || !strings.Contains(err.Error(), "live owner") {
		t.Fatal("competing owner admitted", err)
	}
	if err := spool.applyPage(old, []string{"\x00\xfforiginal-key"}); err == nil {
		t.Fatal("original owner emitted rows after detachment")
	}
	if _, err := s.operationControl(first.Context, "release", id, first.Pin.token, first.Pin.root, 0, nil).Int(); err != nil {
		t.Fatal(err)
	}
	second, _, err := s.takeSnapshotOperation(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Pin.release()
	if err := newSnapshotImportSpool(first.Context, s, meta).applyPage(first.Context, []string{"\x00\xfforiginal-key"}); err == nil {
		t.Fatal("stale takeover owner emitted rows")
	}
	if _, err := s.operationControl(second.Context, "abort", id, second.Pin.token, second.Pin.root, 0, nil).Text(); err != nil {
		t.Fatal(err)
	}
	status, err := s.snapshotOperationStatus(h.ctx, id)
	if err != nil || status.State != "aborted" {
		t.Fatal(status, err)
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='capture_barrier'", "0")
}

func TestSnapshotClusterOperationIncompleteCaptureAndRuntimeFailure(t *testing.T) {
	h, s, ctx, meta, spool := snapshotClusterCaptureFixture(t, 2)
	plan, err := captureSnapshotOperationPlan(ctx, "import", 2, meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.retainSnapshotOperation(ctx, plan, time.Hour); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatal("incomplete capture detached", err)
	}
	if err := spool.complete(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := s.retainSnapshotOperation(ctx, plan, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	snapshotOf(ctx).pin.release()
	// Model a runtime-interrupted complete-image mutation, which is deliberately
	// abort-only even though the input-complete checkpoint still exists.
	var w map[string]any
	raw, err := s.client.HGet(h.ctx, snapshotCatalogKey, "w:"+id).Bytes()
	if err != nil || json.Unmarshal(raw, &w) != nil {
		t.Fatal(err)
	}
	w["import_pending"] = snapshotID()
	raw, _ = json.Marshal(w)
	if err := s.client.HSet(h.ctx, snapshotCatalogKey, "w:"+id, raw).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.takeSnapshotOperation(h.ctx, id); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatal("interrupted output was resumable", err)
	}
	status, err := s.snapshotOperationStatus(h.ctx, id)
	if err != nil || status.State != "abort_only" {
		t.Fatal(status, err)
	}
	if _, err := s.operationControl(h.ctx, "abort", id, "", "", 0, nil).Text(); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotClusterOperationSQLAPI(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	h.exec("CREATE SEQUENCE operation_seq")
	h.exec("CREATE TABLE operation_sql (id BIGINT DEFAULT NEXTVAL('operation_seq'), v BIGINT)")
	h.exec("CREATE TABLE operation_source (v BIGINT)")
	h.exec("INSERT INTO operation_source VALUES (10),(20)")
	h.exec("CREATE TABLE operation_unrelated (v INTEGER)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionStringOperationAction, "prepare"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "INSERT INTO operation_sql (v) SELECT v FROM operation_source ORDER BY v"); err != nil {
		t.Fatal(err)
	}
	if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 0 {
		t.Fatal("prepare SQL", n, err)
	}
	handle, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringOperationHandle)
	if err != nil || !snapshotMetadataID(handle) {
		t.Fatal("queryable operation handle", handle, err)
	}
	h.expectRows("SELECT COUNT(*) FROM operation_sql", "0")
	h.exec("INSERT INTO operation_unrelated VALUES (1)")
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	status, err := GetOperationStatus(h.ctx, options, handle)
	if err != nil || status.State != "ready" {
		t.Fatal(status, err)
	}
	status, err = ResumeOperation(h.ctx, options, handle)
	if err != nil || status.State != "committed" || status.AffectedRows != 2 {
		t.Fatal(status, err)
	}
	h.expectRows("SELECT id,v FROM operation_sql ORDER BY id", "1|10", "2|20")
	h.expectRows("SELECT NEXTVAL('operation_seq')", "3")
	h.expectRows("SELECT COUNT(*) FROM operation_unrelated", "1")
	// Repeated explicit resume resolves the same outcome; defaults are not rerun.
	if status, err = ResumeOperation(h.ctx, options, handle); err != nil || status.State != "committed" {
		t.Fatal(status, err)
	}
	h.expectRows("SELECT COUNT(*) FROM operation_sql", "2")
	// No-op UPDATE remains a no-op even through the preparation interface.
	before := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	if err := st.SetSqlQuery(h.ctx, "UPDATE operation_sql SET v=99 WHERE FALSE"); err != nil {
		t.Fatal(err)
	}
	if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	noop, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringOperationStatus)
	if err != nil || !strings.Contains(noop, `"state":"committed"`) {
		t.Fatal(noop, err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != before {
		t.Fatal("prepared no-op published an empty generation")
	}
}

func TestSnapshotClusterOperationSQLDependencyConflict(t *testing.T) {
	h, _, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE operation_source (v BIGINT)")
	h.exec("INSERT INTO operation_source VALUES (10)")
	h.exec("CREATE TABLE operation_sql (v BIGINT)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionStringOperationAction, "prepare"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "INSERT INTO operation_sql SELECT v FROM operation_source"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	handle, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringOperationHandle)
	if err != nil {
		t.Fatal(err)
	}
	h.exec("UPDATE operation_source SET v=20")
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	if _, err := ResumeOperation(h.ctx, options, handle); sqlState(err) != "40001" {
		t.Fatal("changed read dependency did not conflict", err)
	}
	h.expectRows("SELECT COUNT(*) FROM operation_sql", "0")
	if status, err := AbortOperation(h.ctx, options, handle); err != nil || status.State != "aborted" {
		t.Fatal(status, err)
	}
}

func TestSnapshotClusterOperationNativeAdoption(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	key := "operation:source:\x00\xff"
	if err := raw.HSet(h.ctx, key, "v", "9007199254740993", "payload", "\x00\xff").Err(); err != nil {
		t.Fatal(err)
	}
	adopted, status, err := PrepareAdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "operation:source:", Table: "operation_adopt", Types: map[string]string{"v": "BIGINT", "payload": "TEXT"}}, time.Hour)
	if err != nil || adopted.Applied || status == nil || status.State != "ready" {
		t.Fatal("prepare managed adoption", status, err)
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='operation_adopt'", "0")
	if err := raw.Del(h.ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	resumed, err := ResumeOperation(h.ctx, options, status.Handle)
	if err != nil || resumed.State != "committed" {
		t.Fatal(resumed, err)
	}
	h.expectRows("SELECT COUNT(*),MAX(v),COUNT(__key) FROM operation_adopt", "1|9007199254740993|1")
	if raw.Exists(h.ctx, key).Val() != 0 {
		t.Fatal("resumption recreated an application HASH")
	}
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, "SELECT __key,payload FROM operation_adopt"); err != nil {
		t.Fatal(err)
	}
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	var count int
	for reader.Next() {
		batch := reader.RecordBatch()
		for i := 0; i < int(batch.NumRows()); i++ {
			source, ok := batch.Column(0).(*array.String)
			if !ok || source.Value(i) != key {
				t.Fatal("source-key bytes changed")
			}
			payload, ok := batch.Column(1).(*array.String)
			if !ok || payload.Value(i) != "\x00\xff" {
				t.Fatal("captured payload bytes changed")
			}
			count++
		}
	}
	if reader.Err() != nil || count != 1 {
		t.Fatal("complete fetch", count, reader.Err())
	}
}

func TestSnapshotClusterOperationNativeCopy(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	keys := []string{"operation:copy:\x00\xff", "operation:copy:empty"}
	if err := raw.HSet(h.ctx, keys[0], "v", "9007199254740993").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, keys[1], "other", "value").Err(); err != nil {
		t.Fatal(err)
	}
	copied, status, err := PrepareCopyHashes(h.ctx, options, HashCopyOptions{Prefix: "operation:copy:", Table: "operation_copy", Types: map[string]string{"v": "BIGINT"}}, time.Hour)
	if err != nil || copied == nil || copied.Rows != 2 || status == nil || status.State != "ready" {
		t.Fatal(copied, status, err)
	}
	for _, key := range keys {
		if err := raw.Del(h.ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if status, err = ResumeOperation(h.ctx, options, status.Handle); err != nil || status.State != "committed" {
		t.Fatal(status, err)
	}
	h.expectRows("SELECT COUNT(*),COUNT(v),MAX(v) FROM operation_copy", "2|1|9007199254740993")
}

func TestSnapshotClusterOperationForeignRefresh(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	index := "operation_foreign_source"
	source := &store{client: raw}
	if err := source.searchDo(h.ctx, index, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, "operation:foreign:", "SCHEMA", "v", "NUMERIC", "label", "TAG").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, "operation:foreign:1", "v", "7", "label", "a").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, "operation:foreign:2", "v", "8").Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "operation foreign source indexed", func() bool {
		reply, err := source.searchDo(h.ctx, index, "FT.INFO", index).Result()
		info := indexInfoOf(reply)
		return err == nil && !info.indexing && info.percent >= 1 && info.numDocs == 2
	})
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	prepared, err := PrepareForeignIndexRefresh(h.ctx, options, index, time.Hour)
	if err != nil || prepared.State != "ready" {
		t.Fatal(prepared, err)
	}
	if err := source.searchDo(h.ctx, index, "FT.DROPINDEX", index).Err(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"operation:foreign:1", "operation:foreign:2"} {
		if err := raw.Del(h.ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if status, err := ResumeOperation(h.ctx, options, prepared.Handle); err != nil || status.State != "committed" {
		t.Fatal(status, err)
	}
	// Enabling reads after source deletion cannot import it a second time.
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT COUNT(*),COUNT(label),MIN(v),MAX(v) FROM redis_foreign.operation_foreign_source", "2|1|7|8")
}

func TestSnapshotClusterOperationLostPublicationReply(t *testing.T) {
	h, s, _, _, _, id := retainOperationFixture(t, 3)
	owner, _, err := s.takeSnapshotOperation(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Pin.release()
	if err := s.prepareOperationPublication(owner); err != nil {
		t.Fatal(err)
	}
	hook := &snapshotLostReply{}
	s.client.AddHook(hook)
	if err := s.publishPreparedSnapshot(owner.Context, owner.Pin, id); err != nil {
		t.Fatal("lost publication reply did not resolve", err)
	}
	if !hook.lost.Load() {
		t.Fatal("publication reply was not interrupted")
	}
	status, err := s.resumeSnapshotOperation(h.ctx, id)
	if err != nil || status.State != "committed" || status.AffectedRows != 3 {
		t.Fatal(status, err)
	}
	h.expectRows("SELECT COUNT(*),MAX(v) FROM capture_barrier", "3|9007199254740993")
}

func TestSnapshotClusterOperationPlanIntegrityAndUnsafeIndexPhase(t *testing.T) {
	h, s, _, _, _, id := retainOperationFixture(t, 3)
	saved, err := s.client.Get(h.ctx, snapshotOperationKey(id)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.client.SetArgs(h.ctx, snapshotOperationKey(id), saved+" ", goredis.SetArgs{KeepTTL: true}).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.takeSnapshotOperation(h.ctx, id); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatal("changed plan admitted", err)
	}
	if err := s.client.SetArgs(h.ctx, snapshotOperationKey(id), saved, goredis.SetArgs{KeepTTL: true}).Err(); err != nil {
		t.Fatal(err)
	}
	owner, _, err := s.takeSnapshotOperation(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Pin.release()
	pending := &snapshotOperationPlan{Version: 1, Kind: "import", Phase: "indexing"}
	if err := s.operationControl(owner.Context, "unsafe", id, owner.Pin.token, owner.Pin.root, 0, pending).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.operationControl(owner.Context, "release", id, owner.Pin.token, owner.Pin.root, 0, nil).Err(); err != nil {
		t.Fatal(err)
	}
	status, err := s.snapshotOperationStatus(h.ctx, id)
	if err != nil || status.State != "abort_only" {
		t.Fatal(status, err)
	}
	if _, err := s.resumeSnapshotOperation(h.ctx, id); err == nil {
		t.Fatal("uncertain native index phase was replayable")
	}
	status, err = s.abortSnapshotOperation(h.ctx, id)
	if err != nil || status.State != "aborted" || status.AffectedRows != 0 {
		t.Fatal(status, err)
	}
	if _, err := s.resumeSnapshotOperation(h.ctx, id); err == nil {
		t.Fatal("aborted operation reported resume success")
	}
}

func TestSnapshotClusterOperationRecordLimitAndReadOnlyStatus(t *testing.T) {
	h, raw, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE operation_limit(v INTEGER)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionStringOperationAction, "prepare"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "UPDATE operation_limit SET v=1 WHERE FALSE"); err != nil {
		t.Fatal(err)
	}
	var handle string
	for i := 0; i < snapshotOperationMaxRecords; i++ {
		if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 0 {
			t.Fatal("record admission", i, n, err)
		}
		handle, err = st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringOperationHandle)
		if err != nil {
			t.Fatal(err)
		}
	}
	before, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	status, err := GetOperationStatus(h.ctx, options, handle)
	if err != nil || status.State != "committed" || status.AffectedRows != 0 {
		t.Fatal(status, err)
	}
	after, err := raw.HGetAll(h.ctx, snapshotCatalogKey).Result()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("status mutated catalog or performed GC", err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), "operation limit") {
		t.Fatal("retained record limit exceeded", err)
	}
	h.expectRows("SELECT COUNT(*) FROM operation_limit", "0")
}

func TestSnapshotClusterOperationAlterAndTemporaryTarget(t *testing.T) {
	h, _, _ := snapshotClusterHarness(t)
	h.exec("CREATE TABLE operation_alter(v BIGINT)")
	h.exec("INSERT INTO operation_alter VALUES(9007199254740993)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetOption(h.ctx, OptionStringOperationAction, "prepare"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSqlQuery(h.ctx, "ALTER TABLE operation_alter RENAME COLUMN v TO renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	handle, err := st.(adbc.GetSetOptionsWithContext).GetOption(h.ctx, OptionStringOperationHandle)
	if err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT v FROM operation_alter", "9007199254740993")
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_CLUSTER_URI"), OptionStringCluster: "true"}
	if _, err := ResumeOperation(h.ctx, options, handle); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT renamed FROM operation_alter", "9007199254740993")
	if err := st.SetSqlQuery(h.ctx, "CREATE TEMPORARY TABLE operation_temporary(v INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), "temporary") {
		t.Fatal("temporary target detached", err)
	}
	h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='operation_temporary'", "0")
}
