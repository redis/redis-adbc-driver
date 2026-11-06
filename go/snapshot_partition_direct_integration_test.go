// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotPartitionNoInfo struct {
	snapshotHook
	info atomic.Int64
}

func TestSnapshotDirectPartitionRejectsCorruptNativeState(t *testing.T) {
	for _, mode := range []string{"missing owner", "wrong prefix", "zero references", "directory count", "missing row", "unregistered row", "nonzero score", "rogue newer image"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
			h.exec("INSERT INTO t VALUES(1,9007199254740993),(2,20),(3,30),(4,40)")
			h.exec("UPDATE t SET v=22 WHERE id=2")
			h.exec("DELETE FROM t WHERE id=3")
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetSqlQuery(h.ctx, "SELECT id,v FROM t WHERE __rowid=1"); err != nil {
				t.Fatal(err)
			}
			_, parts, _, err := st.ExecutePartitions(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			inspection := snapshotTestStore(t, raw)
			inspectCtx, pin, err := inspection.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer pin.release()
			meta := snapshotTableTest(t, inspection, inspectCtx, "t")
			var base snapshotSegment
			for _, segment := range meta.snapshotSegments() {
				if raw.HGet(h.ctx, segment.Prefix+"1", rowIDField).Val() == "1" {
					base = segment
					break
				}
			}
			if base.Prefix == "" || base.Rows < 1 {
				t.Fatal("registered original row is absent")
			}
			data := snapshotDataID(base.Prefix)
			directory := snapshotMembers(data)
			owner, err := raw.HGet(h.ctx, snapshotMetadataGraph, "d:"+data).Result()
			if err != nil {
				t.Fatal(err)
			}
			refs, err := raw.HGet(h.ctx, snapshotMetadataGraph, "r:"+data).Result()
			if err != nil {
				t.Fatal(err)
			}
			members, err := raw.ZRangeWithScores(h.ctx, directory, 0, -1).Result()
			if err != nil {
				t.Fatal(err)
			}
			rows := map[string]map[string]string{}
			for _, key := range []string{base.Prefix + "1", meta.prefix() + "1"} {
				v, err := raw.HGetAll(h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				rows[key] = v
			}
			// Explicit fixture-owner repair precedes harness reclamation even
			// when an assertion fails; never leave poisoned graph/row state.
			defer func() {
				if err := raw.HSet(h.ctx, snapshotMetadataGraph, "d:"+data, owner, "r:"+data, refs).Err(); err != nil {
					t.Error(err)
				}
				if err := raw.Del(h.ctx, directory).Err(); err != nil {
					t.Error(err)
				}
				if len(members) > 0 {
					if err := raw.ZAdd(h.ctx, directory, members...).Err(); err != nil {
						t.Error(err)
					}
				}
				for key, row := range rows {
					if err := raw.Del(h.ctx, key).Err(); err != nil {
						t.Error(err)
					}
					if len(row) > 0 {
						if err := raw.HSet(h.ctx, key, row).Err(); err != nil {
							t.Error(err)
						}
					}
				}
			}()
			switch mode {
			case "missing owner":
				err = raw.HDel(h.ctx, snapshotMetadataGraph, "d:"+data).Err()
			case "wrong prefix":
				var record map[string]any
				if err = json.Unmarshal([]byte(owner), &record); err != nil {
					t.Fatal(err)
				}
				record["prefix"] = snapshotRows(data, "another_schema")
				value, e := json.Marshal(record)
				if e != nil {
					t.Fatal(e)
				}
				err = raw.HSet(h.ctx, snapshotMetadataGraph, "d:"+data, value).Err()
			case "zero references":
				err = raw.HSet(h.ctx, snapshotMetadataGraph, "r:"+data, 0).Err()
			case "directory count":
				err = raw.ZAdd(h.ctx, directory, goredis.Z{Score: 0, Member: fmt.Sprintf("%020d", 99)}).Err()
			case "missing row":
				err = raw.Del(h.ctx, base.Prefix+"1").Err()
			case "unregistered row":
				if err = raw.ZRem(h.ctx, directory, fmt.Sprintf("%020d", 1)).Err(); err != nil {
					t.Fatal(err)
				}
				err = raw.ZAdd(h.ctx, directory, goredis.Z{Score: 0, Member: fmt.Sprintf("%020d", 99)}).Err()
			case "nonzero score":
				err = raw.ZAdd(h.ctx, directory, goredis.Z{Score: 1, Member: fmt.Sprintf("%020d", 1)}).Err()
			case "rogue newer image":
				err = raw.HSet(h.ctx, meta.prefix()+"1", rowIDField, "1", meta.Columns[0].field(), "1", meta.Columns[1].field(), "999").Err()
			}
			if err != nil {
				t.Fatal(err)
			}
			guard := &snapshotPartitionNoInfo{}
			ctx, worker := statisticsHookedTransport(t, h, guard)
			r, readErr := worker.ReadPartition(ctx, parts.PartitionIDs[0])
			var emitted int64
			if r != nil {
				for r.Next() {
					emitted += r.RecordBatch().NumRows()
				}
				readErr = r.Err()
				r.Release()
			}
			if readErr == nil || emitted != 0 {
				t.Fatalf("corrupt direct read: rows=%d error=%v", emitted, readErr)
			}
			if guard.info.Load() != 0 {
				t.Fatal("native corruption proof fell back to Search")
			}
		})
	}
}

func TestSnapshotDirectProofDoesNotAuthorizeIndexScan(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER)")
	h.exec("INSERT INTO t VALUES(1),(2)")
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTableMetadata(ctx, "public", "t")
	if err != nil {
		t.Fatal(err)
	}
	guard := &snapshotPartitionNoInfo{}
	s.client.AddHook(guard)
	if err := s.validateSnapshotDirectTable(ctx, meta); err != nil {
		t.Fatal(err)
	}
	if err := s.validateSnapshotTable(ctx, meta); !isStatus(err, adbc.StatusUnauthorized) {
		t.Fatal("native proof authorized an index scan", err)
	}
	if guard.info.Load() == 0 {
		t.Fatal("full validation reused native-only proof")
	}
}

func (h *snapshotPartitionNoInfo) refuse(c goredis.Cmder) error {
	if c.Name() != "ft.info" {
		return nil
	}
	h.info.Add(1)
	err := controlScriptNativeError("NOPERM this user has no permissions to run the 'ft.info' command")
	c.SetErr(err)
	return err
}
func (h *snapshotPartitionNoInfo) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		if err := h.refuse(c); err != nil {
			return err
		}
		return next(ctx, c)
	}
}
func (h *snapshotPartitionNoInfo) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cs []goredis.Cmder) error {
		for _, c := range cs {
			if err := h.refuse(c); err != nil {
				return err
			}
		}
		return next(ctx, cs)
	}
}

func TestSnapshotFinitePartitionDoesNotRequireSearchInfo(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,3000) AS g(i)")
	h.exec("UPDATE t SET v=9007199254740995 WHERE id=2")
	h.exec("DELETE FROM t WHERE id=3")
	h.exec("UPDATE t SET v=NULL WHERE id=4")
	produce := func(query string) []byte {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		if err := st.SetSqlQuery(h.ctx, query); err != nil {
			t.Fatal(err)
		}
		if err := st.SetOption(h.ctx, OptionIntPartitionCount, "1"); err != nil {
			t.Fatal(err)
		}
		_, parts, _, err := st.ExecutePartitions(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(parts.PartitionIDs) != 1 {
			t.Fatal("partition count", len(parts.PartitionIDs))
		}
		return parts.PartitionIDs[0]
	}
	direct := produce("SELECT id,v FROM t WHERE __rowid IN (1,2,3,4)")
	broad := produce("SELECT id,v FROM t")
	inspection := snapshotTestStore(t, raw)
	inspectCtx, pin, err := inspection.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := inspection.getTableMetadata(inspectCtx, "public", "t")
	pin.release()
	if err != nil {
		t.Fatal(err)
	}
	h.exec("DROP TABLE t")
	snapshotReapTest(inspection, h.ctx)
	// Fault injection deliberately removes Search only. The retained native
	// images/directories still define the direct path's authoritative input.
	for _, segment := range meta.snapshotSegments() {
		if err := raw.Do(h.ctx, "FT.DROPINDEX", segment.Index).Err(); err != nil {
			t.Fatal(err)
		}
	}
	guard := &snapshotPartitionNoInfo{}
	ctx, worker := statisticsHookedTransport(t, h, guard)
	r, err := worker.ReadPartition(ctx, direct)
	if err != nil {
		t.Fatal("finite HASH partition requires Search metadata:", err)
	}
	var n int
	for r.Next() {
		rec := r.RecordBatch()
		ids := rec.Column(0).(*array.Int32)
		values := rec.Column(1).(*array.Int64)
		for i := 0; i < ids.Len(); i++ {
			n++
			wantID := []int32{1, 2, 4}[n-1]
			wantValue := int64(9007199254740993)
			if wantID == 2 {
				wantValue = 9007199254740995
			}
			if ids.Value(i) != wantID || values.IsNull(i) != (wantID == 4) || wantID != 4 && values.Value(i) != wantValue {
				t.Fatal("wrong exact row")
			}
		}
	}
	err = r.Err()
	r.Release()
	if err != nil || n != 3 {
		t.Fatal("finite fetch", n, err)
	}
	if guard.info.Load() != 0 {
		t.Fatal("finite partition attempted FT.INFO")
	}
	r, err = worker.ReadPartition(ctx, broad)
	if r != nil {
		r.Release()
	}
	if !isStatus(err, adbc.StatusUnauthorized) || !strings.Contains(err.Error(), "FT.INFO permission") {
		t.Fatal("broad partition must retain FT.INFO permission refusal:", err)
	}
}
