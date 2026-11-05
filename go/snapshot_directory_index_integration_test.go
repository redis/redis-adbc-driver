// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
	"strings"
	"sync/atomic"
	"testing"
)

type snapshotFullIndexScanCounter struct{ full atomic.Int64 }

func (*snapshotFullIndexScanCounter) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotFullIndexScanCounter) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *snapshotFullIndexScanCounter) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() == "ft.create" {
			skip := false
			for _, arg := range cmd.Args() {
				if strings.EqualFold(fmt.Sprint(arg), "SKIPINITIALSCAN") {
					skip = true
				}
			}
			if !skip {
				h.full.Add(1)
			}
		}
		return next(ctx, cmd)
	}
}
func TestSnapshotFinalIndexAvoidsUnrelatedDatabaseScan(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,i FROM generate_series(1,500) AS g(i)")
	h.exec("CREATE TABLE unrelated(id INTEGER)")
	h.exec("INSERT INTO unrelated SELECT i FROM generate_series(1,2000) AS g(i)")
	s := snapshotTestStore(t, raw)
	observer := &snapshotFullIndexScanCounter{}
	s.client.AddHook(observer)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE t SET v=999 WHERE __rowid=1")
	if n := observer.full.Load(); n != 0 {
		t.Fatalf("one-row output launched %d database-wide initial scans", n)
	}
	h.expectRows("SELECT id,v FROM t WHERE id<=2 ORDER BY id", "1|999", "2|2")
	h.expectRows("SELECT COUNT(*),SUM(v) FROM t", "500|126248")
	h.expectRows("SELECT COUNT(*) FROM unrelated", "2000")
}

type snapshotIndexNotifyObserver struct {
	maxBytes       atomic.Int64
	proofWarning   bool
	proofDenied    bool
	proofTransport bool
	proofCalls     atomic.Int64
	calls          atomic.Int64
	rows           atomic.Int64
	maxRows        atomic.Int64
	lose           bool
	cancel         context.CancelFunc
}

func (*snapshotIndexNotifyObserver) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotIndexNotifyObserver) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}
func (h *snapshotIndexNotifyObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		notify := cmd.Name() == "eval" && len(args) > 2 && args[1] == snapshotIndexNotifyLua
		if notify {
			if once, ok := cmd.(interface{ NoRetry() bool }); !ok || !once.NoRetry() {
				return fmt.Errorf("notification lost no-retry wrapper")
			}
			n := int64(args[2].(int) - 3)
			h.calls.Add(1)
			h.rows.Add(n)
			h.maxRows.Store(max(h.maxRows.Load(), n))
			var bytes int64
			for _, arg := range args[3:] {
				bytes += int64(len(fmt.Sprint(arg)))
			}
			h.maxBytes.Store(max(h.maxBytes.Load(), bytes))
		}
		if (h.proofDenied || h.proofTransport) && cmd.Name() == "ft.search" && len(args) > 2 && args[2] == "@"+rowIDField+":[1 +inf]" {
			h.proofCalls.Add(1)
			var err error = readinessServerError("NOPERM injected index-proof denial")
			if h.proofTransport {
				err = errors.New("NOPERM injected lost index-proof reply")
			}
			cmd.SetErr(err)
			return err
		}

		if h.proofWarning && cmd.Name() == "ft.search" && len(args) > 2 && args[2] == "@"+rowIDField+":[1 +inf]" {
			h.proofCalls.Add(1)
			cmd.(interface{ SetVal(any) }).SetVal(map[any]any{"total_results": int64(300), "warning": []any{"injected partial result"}})
			return nil
		}

		err := next(ctx, cmd)
		if notify && err == nil {
			if h.cancel != nil {
				h.cancel()
			}
			if h.lose {
				err = fmt.Errorf("lost successful index notification reply")
				cmd.SetErr(err)
			}
		}
		return err
	}
}
func TestSnapshotIndexNotificationBatchesPreserveValues(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT,payload VARCHAR)")
	h.exec("INSERT INTO t SELECT i,9007199254740993,'before' FROM generate_series(1,513) AS g(i)")
	s := snapshotTestStore(t, raw)
	observer := &snapshotIndexNotifyObserver{}
	s.client.AddHook(observer)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE t SET v=v+1,payload=CASE WHEN id%2=0 THEN NULL ELSE 'Redis → value' END")
	if observer.rows.Load() != 513 || observer.calls.Load() != 3 || observer.maxRows.Load() > 256 {
		t.Fatal("unbounded/incomplete notification batches", observer.calls.Load(), observer.rows.Load(), observer.maxRows.Load())
	}
	rows, _ := h.query("SELECT id,v,payload FROM t ORDER BY id")
	if len(rows) != 513 {
		t.Fatal("lost rows", len(rows))
	}
	for i, row := range rows {
		payload := "Redis → value"
		if (i+1)%2 == 0 {
			payload = "NULL"
		}
		if want := fmt.Sprintf("%d|9007199254740994|%s", i+1, payload); row != want {
			t.Fatal("changed native value", i+1, row)
		}
	}
	snapshotExecuteTest(t, s, h.ctx, "DELETE FROM t WHERE id%7=0")
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "440|9007199254740994|9007199254740994")
	h.expectRows("SELECT COUNT(*) FROM t WHERE id%7=0", "0")
}
func TestSnapshotIndexNotificationUnknownReplyAndCancellation(t *testing.T) {
	for _, mode := range []string{"unknown_reply", "cancel", "partial_index_proof"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
			h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,300) AS g(i)")
			s := snapshotTestStore(t, raw)
			work, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=2")
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			ctx, cancel := context.WithCancel(work)
			defer cancel()
			observer := &snapshotIndexNotifyObserver{lose: mode == "unknown_reply", proofWarning: mode == "partial_index_proof"}
			if mode == "cancel" {
				observer.cancel = cancel
			}
			s.client.AddHook(observer)
			publishErr := s.publishSnapshot(ctx, pin, id)
			if publishErr == nil {
				t.Fatal("incomplete/unknown index population published")
			}
			if mode == "cancel" && !errors.Is(publishErr, context.Canceled) && !isStatus(publishErr, adbc.StatusCancelled) {
				t.Fatal("wrong cancellation status", publishErr)
			}
			wantCalls, wantRows := int64(1), int64(256)
			if mode == "partial_index_proof" {
				wantCalls, wantRows = 2, 300
				if observer.proofCalls.Load() != 1 {
					t.Fatal("indexed-row proof not exercised")
				}
			}
			if observer.calls.Load() != wantCalls || observer.rows.Load() != wantRows {
				t.Fatal("notification replay or input re-execution", observer.calls.Load(), observer.rows.Load())
			}
			if got := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); got != root {
				t.Fatal("root changed after incomplete index population")
			}
			h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM t", "300|1|1")
		})
	}
}
func TestSnapshotIndexNotificationRejectsDamagedBatch(t *testing.T) {
	for _, mode := range []string{"missing", "wrong_type", "wrong_id", "score", "duplicate", "prefix_alias", "aborted", "expired", "stale_token"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
			h.exec("INSERT INTO t VALUES(1,1),(2,1)")
			s := snapshotTestStore(t, raw)
			work, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=2")
			meta := snapshotTableTest(t, s, work, "t")
			first, second := meta.prefix()+"1", meta.prefix()+"2"
			if err := s.searchDo(work, meta.index(), indexCreateArgs(meta)...).Err(); err != nil {
				t.Fatal(err)
			}
			directory := snapshotMembers(snapshotDataID(meta.prefix()))
			members := []string{fmt.Sprintf("%020d", 1), fmt.Sprintf("%020d", 2)}
			prefix := meta.prefix()
			switch mode {
			case "missing":
				if err := raw.Del(h.ctx, second).Err(); err != nil {
					t.Fatal(err)
				}
			case "wrong_type":
				if err := raw.Del(h.ctx, second).Err(); err != nil {
					t.Fatal(err)
				}
				if err := raw.Set(h.ctx, second, "sentinel", 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "wrong_id":
				if err := raw.HSet(h.ctx, second, rowIDField, "3").Err(); err != nil {
					t.Fatal(err)
				}
			case "score":
				if err := raw.ZAdd(h.ctx, directory, goredis.Z{Score: 1, Member: members[1]}).Err(); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				members[1] = members[0]
			case "prefix_alias":
				prefix = snapshotRows(snapshotDataID(prefix), "other")
			case "aborted":
				s.abortSnapshot(pin, id)
			case "expired":
				pin.release() // stop renewal before expiring this test's private work
				payload, err := raw.HGet(h.ctx, snapshotCatalogKey, "w:"+id).Bytes()
				if err != nil {
					t.Fatal(err)
				}
				var workRecord map[string]any
				if err := json.Unmarshal(payload, &workRecord); err != nil {
					t.Fatal(err)
				}
				workRecord["expires"] = 0
				payload, err = json.Marshal(workRecord)
				if err != nil {
					t.Fatal(err)
				}
				if err := raw.HSet(h.ctx, snapshotCatalogKey, "w:"+id, payload).Err(); err != nil {
					t.Fatal(err)
				}
			case "stale_token":
				work = context.WithValue(work, snapshotContextKey{}, &snapshotScope{id: id, pin: &snapshotPin{token: snapshotID()}, store: s, write: true, owned: map[string]bool{snapshotDataID(prefix): true}})
			}
			beforeCount, err := s.countMatches(work, meta.index(), "*")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.notifySnapshotIndexBatch(work, prefix, members); err == nil {
				t.Fatal("damaged/unauthorized batch was accepted")
			}
			if got := raw.HGet(h.ctx, first, "v").Val(); got != "2" {
				t.Fatal("first row changed", got)
			}
			if mode == "missing" && raw.Exists(h.ctx, second).Val() != 0 {
				t.Fatal("missing HASH was resurrected")
			}
			if mode == "wrong_type" && raw.Get(h.ctx, second).Val() != "sentinel" {
				t.Fatal("unregistered object changed")
			}
			n, err := s.countMatches(work, meta.index(), "*")
			if err != nil || n != beforeCount {
				t.Fatal("batch notified an earlier row before validating later rows", n, beforeCount, err)
			}
			h.expectRows("SELECT id,v FROM t ORDER BY id", "1|1", "2|1")
		})
	}
}

func TestSnapshotIndexProofPermissionFallback(t *testing.T) {
	for _, transport := range []bool{false, true} {
		t.Run(fmt.Sprint(transport), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(v INTEGER)")
			h.exec("INSERT INTO t VALUES(1)")
			s := snapshotTestStore(t, raw)
			work, pin, id := snapshotStageSQL(t, s, h.ctx, "t", "UPDATE t SET v=2")
			observer := &snapshotIndexNotifyObserver{proofDenied: !transport, proofTransport: transport}
			scans := &snapshotFullIndexScanCounter{}
			s.client.AddHook(observer)
			s.client.AddHook(scans)
			err := s.publishSnapshot(work, pin, id)
			if observer.proofCalls.Load() != 1 || observer.calls.Load() != 1 {
				t.Fatal("proof or notification replayed", observer.proofCalls.Load(), observer.calls.Load())
			}
			if transport {
				if err == nil || scans.full.Load() != 0 {
					t.Fatal("unknown query outcome selected compatibility replay", err, scans.full.Load())
				}
				h.expectRows("SELECT v FROM t", "1")
			} else {
				if err != nil || scans.full.Load() != 1 {
					t.Fatal("typed permission refusal did not retain original completion proof", err, scans.full.Load())
				}
				h.expectRows("SELECT v FROM t", "2")
			}
		})
	}
}

func TestSnapshotIndexNotificationsBoundLongKeys(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	schema := strings.Repeat("long_schema_", 700)
	h.exec("CREATE SCHEMA " + schema)
	h.exec("CREATE TABLE " + schema + ".t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO " + schema + ".t SELECT i,1 FROM generate_series(1,256) AS g(i)")
	s := snapshotTestStore(t, raw)
	observer := &snapshotIndexNotifyObserver{}
	s.client.AddHook(observer)
	snapshotExecuteTest(t, s, h.ctx, "UPDATE "+schema+".t SET v=2")
	if observer.rows.Load() != 256 || observer.calls.Load() < 2 || observer.maxBytes.Load() > snapshotWriteBatchBytes {
		t.Fatal("long row keys exceeded bounded notification arguments", observer.rows.Load(), observer.calls.Load(), observer.maxBytes.Load())
	}
	h.expectRows("SELECT COUNT(*),MIN(v),MAX(v) FROM "+schema+".t", "256|2|2")
}
