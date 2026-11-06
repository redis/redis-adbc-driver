// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func snapshotInsertBatchRowKeys(cmd goredis.Cmder) []any {
	a := cmd.Args()
	if len(a) < 7 || !strings.EqualFold(fmt.Sprint(a[0]), "EVAL") || !strings.Contains(fmt.Sprint(a[1]), "snapshot insert row batch") {
		return nil
	}
	n, _ := strconv.Atoi(fmt.Sprint(a[2]))
	if n < 4 || len(a) < 3+n {
		return nil
	}
	var rows []any
	for _, key := range a[6 : 3+n] {
		if snapshotRowPrefix(fmt.Sprint(key)) != "" {
			rows = append(rows, key)
		}
	}
	return rows
}

type snapshotLargeInput struct {
	array.RecordReader
	next, records int
	fail          bool
}

func (r *snapshotLargeInput) Next() bool {
	r.next++
	if !r.RecordReader.Next() {
		return false
	}
	r.records++
	return true
}

func (r *snapshotLargeInput) Err() error {
	if r.fail {
		return io.ErrUnexpectedEOF
	}
	return r.RecordReader.Err()
}

func TestSnapshotLargeArrowRecordAtomicity(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_failure_%t", fail), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
			h.exec("INSERT INTO t VALUES(0,42)")
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
			builder := array.NewRecordBuilder(mem, schema)
			for i := 1; i <= 100001; i++ {
				builder.Field(0).(*array.Int64Builder).Append(int64(i))
				builder.Field(1).(*array.Int64Builder).Append(9007199254740993)
			}
			rec := builder.NewRecordBatch()
			builder.Release()
			reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
			rec.Release()
			if err != nil {
				t.Fatal(err)
			}
			input := &snapshotLargeInput{RecordReader: reader, fail: fail}
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				input.Release()
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: "t", adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeAppend} {
				if err := st.SetOption(h.ctx, k, v); err != nil {
					input.Release()
					t.Fatal(err)
				}
			}
			if err := st.BindStream(h.ctx, input); err != nil {
				t.Fatal(err)
			}
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			n, err := st.ExecuteUpdate(h.ctx)
			if input.next != 2 || input.records != 1 {
				t.Fatalf("input replayed: next=%d records=%d", input.next, input.records)
			}
			if fail {
				if err == nil || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
					t.Fatalf("late error published: %d %v", n, err)
				}
				h.expectRows("SELECT COUNT(*),MIN(id),MAX(v) FROM t", "1|0|42")
			} else {
				if err != nil || n != 100001 {
					t.Fatalf("large ingest: %d %v", n, err)
				}
				h.expectRows("SELECT COUNT(*),MIN(id),MAX(id),MAX(v) FROM t", "100002|0|100001|9007199254740993")
			}
		})
	}
}

type snapshotInsertBatchInterruption struct {
	snapshotHook
	after                bool
	calls, rows, maxRows int
}

func (h *snapshotInsertBatchInterruption) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		keys := snapshotInsertBatchRowKeys(cmd)
		if len(keys) == 0 {
			return next(ctx, cmd)
		}
		h.calls++
		h.rows += len(keys)
		h.maxRows = max(h.maxRows, len(keys))
		if h.calls == 2 && !h.after {
			cmd.SetErr(errors.New("injected batch admission interruption"))
			return cmd.Err()
		}
		if err := next(ctx, cmd); err != nil {
			return err
		}
		if h.calls == 2 && h.after {
			cmd.SetErr(errors.New("injected lost row batch reply"))
			return cmd.Err()
		}
		return nil
	}
}

func TestSnapshotInsertExceedsCumulativeRowBudget(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,v BIGINT)")
	h.exec("INSERT INTO t SELECT i,9007199254740993 FROM generate_series(1,100001) AS g(i)")
	h.expectRows("SELECT COUNT(*),MIN(id),MAX(id),MIN(v),MAX(v) FROM t", "100001|1|100001|9007199254740993|9007199254740993")
}

func TestSnapshotInsertExceedsCumulativeByteBudget(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id BIGINT,payload VARCHAR NOINDEX)")
	h.exec("INSERT INTO t SELECT i,REPEAT('x',2097152) FROM generate_series(1,33) AS g(i)")
	h.expectRows("SELECT COUNT(*),MIN(id),MAX(id),MIN(LENGTH(payload)),MAX(LENGTH(payload)) FROM t", "33|1|33|2097152|2097152")
}

func TestSnapshotInsertBatchOutcomesAndAtomicity(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprintf("after_execution_%t", after), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			h.exec("CREATE TABLE t(id BIGINT)")
			h.exec("INSERT INTO t VALUES(1)")
			s := snapshotTestStore(t, raw)
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
			if err := s.prepareSnapshotTable(wctx, "public", "t"); err != nil {
				t.Fatal(err)
			}
			meta := snapshotTableTest(t, s, wctx, "t")
			rows := make([][]Value, 600)
			for i := range rows {
				rows[i] = []Value{intValue(typeInt64, int64(i+2))}
			}
			observer := &snapshotInsertBatchInterruption{after: after}
			s.client.AddHook(observer)
			n, err := s.insertRows(wctx, meta, rows)
			h.expectRows("SELECT COUNT(*),SUM(id) FROM t", "1|1")
			if !after {
				if err == nil || n != 0 || observer.calls != 2 || raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Val() != 256 {
					t.Fatalf("unknown outcome admitted or replayed: n=%d err=%v calls=%d", n, err, observer.calls)
				}
				return
			}
			if err != nil || n != 600 || observer.calls != 3 || observer.rows != 600 || observer.maxRows > snapshotWriteBatchRows {
				t.Fatalf("lost reply replayed input or lost progress: n=%d err=%v observer=%+v", n, err, observer)
			}
			if err := s.publishSnapshot(wctx, pin, id); err != nil {
				t.Fatal(err)
			}
			h.expectRows("SELECT COUNT(*),SUM(id) FROM t", "601|180901")
		})
	}
}

func TestSnapshotInsertBatchPermissionPreflight(t *testing.T) {
	uri := os.Getenv("REDIS_SNAPSHOT_WRITE_ACL_URI")
	if uri == "" {
		t.Skip("requires owned Software principal with HSET allowed and ZADD denied")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
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
	meta := &tableMeta{Schema: "public", Name: "batch_acl", Columns: []columnMeta{{Name: "v", Type: typeInt64, Nullable: true}}}
	if _, err := s.createTable(wctx, meta, false); err != nil {
		t.Fatal(err)
	}
	credentials, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatal("invalid private principal configuration")
	}
	opts := *raw.Options()
	opts.Username, opts.Password = credentials.Username, credentials.Password
	limited := goredis.NewClient(&opts)
	defer limited.Close()
	probe := meta.prefix() + "acl-probe"
	if err := limited.HSet(h.ctx, probe, "v", "1").Err(); err != nil {
		t.Fatal("HSET positive control failed")
	}
	if err := raw.Del(h.ctx, probe).Err(); err != nil {
		t.Fatal(err)
	}
	directory := snapshotMembers(snapshotDataID(meta.prefix()))
	if err := limited.ZAdd(h.ctx, directory, goredis.Z{Member: "not-a-row"}).Err(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Fatal("ZADD negative control failed")
	}
	guarded := snapshotTestStore(t, limited)
	err = guarded.insertSnapshotBatch(wctx, meta.prefix(), []snapshotInsertRow{{key: meta.prefix() + "1", member: fmt.Sprintf("%020d", 1), fields: []any{rowIDField, "1", "v", "1"}}})
	if err == nil || !strings.Contains(err.Error(), "snapshot insert batch requires ZADD and control HSET") {
		t.Fatalf("permission preflight not exercised: %v", err)
	}
	if raw.Exists(h.ctx, meta.prefix()+"1").Val() != 0 || raw.ZCard(h.ctx, directory).Val() != 0 {
		t.Fatal("denied registration partially wrote rows")
	}
}

func TestSnapshotInsertBatchRejectsBeforeMutation(t *testing.T) {
	for _, mode := range []string{"key", "rowid", "duplicate", "bytes", "wrong_type"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
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
			meta := &tableMeta{Schema: "public", Name: "batch_validation", Columns: []columnMeta{{Name: "v", Type: typeString, Nullable: true}}}
			if _, err := s.createTable(wctx, meta, false); err != nil {
				t.Fatal(err)
			}
			rows := []snapshotInsertRow{
				{key: meta.prefix() + "1", member: fmt.Sprintf("%020d", 1), fields: []any{rowIDField, "1", "v", "first"}},
				{key: meta.prefix() + "2", member: fmt.Sprintf("%020d", 2), fields: []any{rowIDField, "2", "v", "second"}},
			}
			switch mode {
			case "key":
				rows[1].key = meta.prefix() + "3"
			case "rowid":
				rows[1].fields[1] = "3"
			case "duplicate":
				rows[1] = rows[0]
			case "bytes":
				rows[1].fields[3] = strings.Repeat("x", snapshotWriteBatchBytes)
			case "wrong_type":
				if err := raw.Set(h.ctx, rows[1].key, "unregistered", 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.insertSnapshotBatch(wctx, meta.prefix(), rows); err == nil {
				t.Fatal("invalid batch admitted")
			}
			if raw.Exists(h.ctx, meta.prefix()+"1").Val() != 0 || raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Val() != 0 {
				t.Fatal("batch validation partially mutated rows/directory")
			}
			if mode == "wrong_type" && raw.Get(h.ctx, meta.prefix()+"2").Val() != "unregistered" {
				t.Fatal("existing unregistered key modified")
			}
		})
	}
}
