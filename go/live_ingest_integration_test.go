// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

func liveArrowFixture(t *testing.T, name string) (*hashFixture, *LiveHashRegistration) {
	t.Helper()
	f := newHashFixture(t, "it:live:arrow:"+name+":")
	f.hset(f.prefix+"seed", "n", "10", "payload", "seed", "extra", "keep")
	r, err := RegisterLiveHashes(f.h.ctx, f.opts, HashAdoptOptions{Prefix: f.prefix, Table: "it_live_arrow_" + name, Apply: true, Types: map[string]string{"n": "BIGINT"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterLiveHashes(f.h.ctx, f.opts, "it_live_arrow_"+name); err != nil {
			t.Error(err)
		}
	})
	if err := f.h.conn.(adbc.GetSetOptionsWithContext).SetOption(f.h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	return f, r
}

func liveArrowStatement(t *testing.T, f *hashFixture, table, mode string) adbc.StatementWithContext {
	t.Helper()
	st, err := f.h.conn.NewStatement(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close(f.h.ctx) })
	for key, value := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionValueIngestTargetDBSchema: liveSchema, adbc.OptionKeyIngestMode: mode} {
		if err := st.SetOption(f.h.ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func liveArrowRecord(t *testing.T, fields []arrow.Field, rows ...[]any) arrow.RecordBatch {
	t.Helper()
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	for _, row := range rows {
		if len(row) != len(fields) {
			t.Fatal("bad test input row width")
		}
		for i, value := range row {
			if value == nil {
				b.Field(i).AppendNull()
				continue
			}
			switch col := b.Field(i).(type) {
			case *array.StringBuilder:
				col.Append(value.(string))
			case *array.LargeStringBuilder:
				col.Append(value.(string))
			case *array.Int64Builder:
				col.Append(value.(int64))
			case *array.BooleanBuilder:
				col.Append(value.(bool))
			default:
				t.Fatalf("unsupported test builder %T", col)
			}
		}
	}
	r := b.NewRecordBatch()
	t.Cleanup(r.Release)
	return r
}

func TestLiveArrowGeneratedKeysAndStreams(t *testing.T) {
	f, _ := liveArrowFixture(t, "generated")
	fields := []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "payload", Type: arrow.BinaryTypes.String, Nullable: true}}
	first := liveArrowRecord(t, fields, []any{int64(9007199254740993), "first"}, []any{nil, "nullable"})
	second := liveArrowRecord(t, fields, []any{int64(-3), "two\x00tail\x1f"})
	st := liveArrowStatement(t, f, "it_live_arrow_generated", adbc.OptionValueIngestModeCreateAppend)
	input := bindLiveArrow(t, f, st, first, second)
	n, err := st.ExecuteUpdate(f.h.ctx)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if input.batches.Load() != 2 || input.calls.Load() != 3 || input.releases.Load() != 1 {
		t.Fatal("stream consumption/release changed")
	}
	rows, _ := f.h.query("SELECT __key,n FROM redis_live.it_live_arrow_generated WHERE __key <> '" + f.prefix + "seed' ORDER BY n")
	if len(rows) != 3 {
		t.Fatalf("generated rows: %q", rows)
	}
	seen := map[string]bool{}
	for i, row := range rows {
		key, value, ok := strings.Cut(row, "|")
		id, e := uuid.Parse(strings.TrimPrefix(key, f.prefix))
		if !ok || e != nil || id.Version() != 4 || key != f.prefix+id.String() || seen[key] {
			t.Fatalf("invalid/repeated key %q", key)
		}
		seen[key] = true
		if value != []string{"-3", "9007199254740993", "NULL"}[i] {
			t.Fatalf("value changed: %q", row)
		}
		if f.raw.HExists(f.h.ctx, key, keyField).Val() || f.raw.HExists(f.h.ctx, key, rowIDField).Val() {
			t.Fatal("generated key stored as internal field")
		}
	}
	schema := liveSchema
	public, err := f.h.conn.GetTableSchema(f.h.ctx, nil, &schema, "it_live_arrow_generated")
	if err != nil || public.NumFields() != 3 {
		t.Fatalf("registration schema changed: %v %v", public, err)
	}
}

func TestLiveArrowRefusesBeforeInput(t *testing.T) {
	f, _ := liveArrowFixture(t, "refuse")
	root := f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
	fields := []arrow.Field{{Name: keyField, Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}
	rec := liveArrowRecord(t, fields, []any{f.prefix + "new", int64(1)})
	for _, tc := range []struct {
		name, mode, table string
		option, value     string
		shared            bool
	}{
		{name: "create", mode: adbc.OptionValueIngestModeCreate},
		{name: "replace", mode: adbc.OptionValueIngestModeReplace},
		{name: "missing", mode: adbc.OptionValueIngestModeCreateAppend, table: "it_live_arrow_missing"},
		{name: "retained", option: OptionStringOperationAction, value: "prepare"},
		{name: "temporary", option: adbc.OptionValueIngestTemporary, value: "true"},
		{name: "catalog", option: adbc.OptionValueIngestTargetCatalog, value: "other"},
		{name: "index_policy", option: OptionStringIngestIndexColumns, value: "n"},
		{name: "shared", shared: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode := tc.mode
			if mode == "" {
				mode = adbc.OptionValueIngestModeAppend
			}
			table := tc.table
			if table == "" {
				table = "it_live_arrow_refuse"
			}
			st := liveArrowStatement(t, f, table, mode)
			if tc.option != "" {
				if err := st.SetOption(f.h.ctx, tc.option, tc.value); err != nil {
					t.Fatal(err)
				}
			}
			if tc.shared {
				opts := f.h.conn.(adbc.GetSetOptionsWithContext)
				if err := opts.SetOption(f.h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
					t.Fatal(err)
				}
				defer opts.SetOption(f.h.ctx, OptionStringReadSnapshotAction, "release")
			}
			input := bindLiveArrow(t, f, st, rec)
			n, err := st.ExecuteUpdate(f.h.ctx)
			if err == nil {
				t.Fatal("unsupported live ingestion succeeded")
			}
			if input.calls.Load() != 0 {
				t.Fatal("preflight refusal advanced input")
			}
			if tc.shared || tc.name == "retained" || tc.name == "temporary" { // Existing option guards or pg_temp routing may refuse before live execution.
				if input.releases.Load() > 1 {
					t.Fatal("input released twice")
				}
			} else if n != 0 || input.releases.Load() != 1 {
				t.Fatalf("n=%d releases=%d err=%v", n, input.releases.Load(), err)
			}
		})
	}
	if f.raw.Exists(f.h.ctx, f.prefix+"new").Val() != 0 || root != f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("refusal mutated source or published catalog")
	}
}

func TestLiveArrowInputChecks(t *testing.T) {
	f, _ := liveArrowFixture(t, "input")
	key := arrow.Field{Name: keyField, Type: arrow.BinaryTypes.String, Nullable: true}
	number := arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}
	for _, tc := range []struct {
		name     string
		fields   []arrow.Field
		rows     [][]any
		consumes bool
		message  string
	}{
		{name: "numeric_key", fields: []arrow.Field{number, {Name: keyField, Type: arrow.PrimitiveTypes.Int64}}, rows: [][]any{{int64(1), int64(2)}}, message: "must be a string"},
		{name: "duplicate_column", fields: []arrow.Field{key, number, number}, rows: [][]any{{f.prefix + "one", int64(1), int64(2)}}, message: "duplicate Arrow"},
		{name: "unknown_column", fields: []arrow.Field{{Name: "unknown", Type: arrow.PrimitiveTypes.Int64}}, rows: [][]any{{int64(1)}}, message: "does not exist"},
		{name: "null_key", fields: []arrow.Field{key, number}, rows: [][]any{{nil, int64(1)}}, consumes: true, message: "__key"},
		{name: "existing", fields: []arrow.Field{key, number}, rows: [][]any{{f.prefix + "seed", int64(1)}}, consumes: true, message: "already exists"},
		{name: "outside", fields: []arrow.Field{key, number}, rows: [][]any{{"other:key", int64(1)}}, consumes: true, message: "prefix"},
		{name: "nested", fields: []arrow.Field{key, number}, rows: [][]any{{f.prefix + "one:nested", int64(1)}}, consumes: true, message: "nested"},
		{name: "driver_owned", fields: []arrow.Field{key, number}, rows: [][]any{{snapshotRows(snapshotID()) + "1", int64(1)}}, consumes: true, message: "prefix"},
		{name: "duplicate_key", fields: []arrow.Field{key, number}, rows: [][]any{{f.prefix + "one", int64(1)}, {f.prefix + "one", int64(2)}}, consumes: true, message: "duplicate __key"},
		{name: "empty_hash", fields: []arrow.Field{key, number}, rows: [][]any{{f.prefix + "one", nil}}, consumes: true, message: "without fields"},
		{name: "conversion", fields: []arrow.Field{key, {Name: "n", Type: arrow.BinaryTypes.String}}, rows: [][]any{{f.prefix + "one", "not an integer"}}, consumes: true, message: "n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := liveArrowStatement(t, f, "it_live_arrow_input", adbc.OptionValueIngestModeAppend)
			rec := liveArrowRecord(t, tc.fields, tc.rows...)
			input := bindLiveArrow(t, f, st, rec)
			n, err := st.ExecuteUpdate(f.h.ctx)
			if n != 0 || err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("n=%d err=%v", n, err)
			}
			assertUnmanagedAcknowledged(t, err, 0)
			if (input.calls.Load() > 0) != tc.consumes || input.releases.Load() != 1 {
				t.Fatal("incorrect input consumption/release")
			}
		})
	}
	if f.raw.Exists(f.h.ctx, f.prefix+"one").Val() != 0 || f.raw.HGet(f.h.ctx, f.prefix+"seed", "n").Val() != "10" {
		t.Fatal("invalid input changed source")
	}
}

// Observe the actual execution-owned source transport, including no-replay and
// byte/row bounds. Faults run only after a complete submitted pipeline reply.
type liveArrowPipelineHook struct {
	snapshotHook
	pipelines, rows int
	before          func()
	after           func(context.Context, []goredis.Cmder) error
}

func (h *liveArrowPipelineHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, commands []goredis.Cmder) error {
		var source []goredis.Cmder
		bytes := 0
		for _, cmd := range commands {
			a := snapshotObservedControlArgs(cmd)
			if len(a) < 2 || a[0] != "EVAL" || a[1] != keyRowInsertLua {
				continue
			}
			if snapshotOf(ctx) != nil {
				return fmt.Errorf("live writes retained managed snapshot scope")
			}
			marker, ok := cmd.(interface{ NoRetry() bool })
			if !ok || !marker.NoRetry() {
				return fmt.Errorf("live source pipeline permits replay")
			}
			source = append(source, cmd)
			bytes += 32
			for _, arg := range a {
				bytes += len(fmt.Sprint(arg)) + 32
			}
		}
		if len(source) > unmanagedInsertPipelineRows || len(source) > 1 && bytes > unmanagedInsertPipelineBytes {
			return fmt.Errorf("unbounded live pipeline")
		}
		if len(source) > 0 {
			h.pipelines++
			h.rows += len(source)
			if h.before != nil {
				h.before()
			}
		}
		err := next(ctx, commands)
		if len(source) > 0 && err == nil && h.after != nil {
			return h.after(ctx, source)
		}
		return err
	}
}

func liveArrowExecution(t *testing.T, f *hashFixture, st adbc.StatementWithContext, hook goredis.Hook) context.Context {
	t.Helper()
	ctx, execution := st.(*statementImpl).conn.executionContext(f.h.ctx)
	if execution == nil {
		t.Fatal("missing execution owner")
	}
	t.Cleanup(execution.finish)
	execution.client.AddHook(hook)
	return ctx
}

func TestLiveArrowBoundsAndPartialFailure(t *testing.T) {
	f, _ := liveArrowFixture(t, "partial")
	fields := []arrow.Field{{Name: keyField, Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}
	rows := make([][]any, snapshotIngestConvertRows+1)
	for i := range rows {
		rows[i] = []any{fmt.Sprintf("%sbatch%d", f.prefix, i), int64(i)}
	}
	rows[len(rows)-1][0] = rows[0][0]
	st := liveArrowStatement(t, f, "it_live_arrow_partial", adbc.OptionValueIngestModeAppend)
	input := bindLiveArrow(t, f, st, liveArrowRecord(t, fields, rows...))
	hook := &liveArrowPipelineHook{}
	ctx := liveArrowExecution(t, f, st, hook)
	n, err := st.ExecuteUpdate(ctx)
	if n != snapshotIngestConvertRows || err == nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	assertUnmanagedAcknowledged(t, err, n)
	var ae adbc.Error
	if !errors.As(err, &ae) || string(ae.SqlState[:]) != stateUnique {
		t.Fatalf("lost collision SQLSTATE: %v", err)
	}
	if input.batches.Load() != 1 || input.releases.Load() != 1 || hook.rows != int(n) || hook.pipelines < 2 {
		t.Fatal("input replayed, writes unbounded, or input reference retained")
	}
	if f.raw.HGet(f.h.ctx, rows[0][0].(string), "n").Val() != "0" {
		t.Fatal("collision replaced an acknowledged HASH")
	}
}

func TestLiveArrowUnknownReplyDoesNotReplay(t *testing.T) {
	f, _ := liveArrowFixture(t, "unknown")
	fields := []arrow.Field{{Name: keyField, Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}
	rows := make([][]any, unmanagedInsertPipelineRows+1)
	for i := range rows {
		rows[i] = []any{fmt.Sprintf("%srow%d", f.prefix, i), int64(i)}
	}
	st := liveArrowStatement(t, f, "it_live_arrow_unknown", adbc.OptionValueIngestModeAppend)
	input := bindLiveArrow(t, f, st, liveArrowRecord(t, fields, rows...))
	hook := &liveArrowPipelineHook{after: func(_ context.Context, cmds []goredis.Cmder) error {
		cmds[1].SetErr(io.ErrUnexpectedEOF)
		return io.ErrUnexpectedEOF
	}}
	ctx := liveArrowExecution(t, f, st, hook)
	n, err := st.ExecuteUpdate(ctx)
	if n != unmanagedInsertPipelineRows-1 || err == nil || hook.pipelines != 1 {
		t.Fatalf("n=%d pipelines=%d err=%v", n, hook.pipelines, err)
	}
	assertUnmanagedAcknowledged(t, err, n)
	if input.batches.Load() != 1 || f.raw.HGet(f.h.ctx, rows[1][0].(string), "n").Val() != "1" || f.raw.Exists(f.h.ctx, rows[len(rows)-1][0].(string)).Val() != 0 {
		t.Fatal("unknown mutation/input replayed or submitted a later pipeline")
	}
}

func TestLiveArrowSubmittedCollisionAndCancellation(t *testing.T) {
	for _, collision := range []bool{true, false} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			name := fmt.Sprintf("interrupt_%t", collision)
			f, _ := liveArrowFixture(t, name)
			fields := []arrow.Field{{Name: keyField, Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}
			rows := make([][]any, unmanagedInsertPipelineRows+1)
			for i := range rows {
				rows[i] = []any{fmt.Sprintf("%srow%d", f.prefix, i), int64(i)}
			}
			st := liveArrowStatement(t, f, "it_live_arrow_"+name, adbc.OptionValueIngestModeAppend)
			input := bindLiveArrow(t, f, st, liveArrowRecord(t, fields, rows...))
			hook := &liveArrowPipelineHook{}
			ctx, cancel := context.WithCancel(liveArrowExecution(t, f, st, hook))
			defer cancel()
			want := int64(unmanagedInsertPipelineRows)
			if collision {
				want--
				hook.before = func() { f.hset(rows[1][0].(string), "n", "999", "extra", "preserve") }
			} else {
				hook.after = func(context.Context, []goredis.Cmder) error { cancel(); return nil }
			}
			n, err := st.ExecuteUpdate(ctx)
			if n != want || err == nil || hook.pipelines != 1 {
				t.Fatalf("n=%d pipelines=%d err=%v", n, hook.pipelines, err)
			}
			assertUnmanagedAcknowledged(t, err, want)
			if input.batches.Load() != 1 || input.releases.Load() != 1 || f.raw.Exists(f.h.ctx, rows[len(rows)-1][0].(string)).Val() != 0 {
				t.Fatal("continued input/writes after interruption or retained input")
			}
			if collision && (f.raw.HGet(f.h.ctx, rows[1][0].(string), "n").Val() != "999" || f.raw.HGet(f.h.ctx, rows[1][0].(string), "extra").Val() != "preserve") {
				t.Fatal("collision changed the concurrent writer's HASH")
			}
		})
	}
}

func TestLiveArrowReaderFailure(t *testing.T) {
	f, _ := liveArrowFixture(t, "reader")
	st := liveArrowStatement(t, f, "it_live_arrow_reader", adbc.OptionValueIngestModeAppend)
	rec := liveArrowRecord(t, []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []any{int64(42)})
	input := bindLiveArrow(t, f, st, rec)
	if err := st.BindStream(f.h.ctx, &unmanagedArrowReaderFailure{RecordReader: input}); err != nil {
		t.Fatal(err)
	}
	n, err := st.ExecuteUpdate(f.h.ctx)
	if n != 1 || err == nil || !strings.Contains(err.Error(), "failed to read live Arrow input") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	assertUnmanagedAcknowledged(t, err, 1)
	// Rebinding released the original binding, and execution released the replacement.
	if input.batches.Load() != 1 || input.releases.Load() != 2 {
		t.Fatal("reader failure replayed or retained Arrow input")
	}
}

func TestLiveArrowSchemaDrift(t *testing.T) {
	f, _ := liveArrowFixture(t, "drift")
	st := liveArrowStatement(t, f, "it_live_arrow_drift", adbc.OptionValueIngestModeAppend)
	rec := liveArrowRecord(t, []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []any{int64(1)})
	input := bindLiveArrow(t, f, st, rec)
	drift := liveArrowRecord(t, []arrow.Field{{Name: "payload", Type: arrow.BinaryTypes.String}}, []any{"changed"})
	if err := st.BindStream(f.h.ctx, &unmanagedArrowChangedSchema{RecordReader: input, batch: drift}); err != nil {
		t.Fatal(err)
	}
	n, err := st.ExecuteUpdate(f.h.ctx)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "schema changed") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	assertUnmanagedAcknowledged(t, err, 0)
}

func TestLiveArrowByteBudget(t *testing.T) {
	f, _ := liveArrowFixture(t, "bytes")
	st := liveArrowStatement(t, f, "it_live_arrow_bytes", adbc.OptionValueIngestModeAppend)
	payload := strings.Repeat("x", unmanagedInsertPipelineBytes/2)
	rec := liveArrowRecord(t, []arrow.Field{{Name: "payload", Type: arrow.BinaryTypes.String}}, []any{payload}, []any{payload}, []any{payload})
	bindLiveArrow(t, f, st, rec)
	hook := &liveArrowPipelineHook{}
	n, err := st.ExecuteUpdate(liveArrowExecution(t, f, st, hook))
	if n != 3 || err != nil || hook.pipelines != 3 {
		t.Fatalf("n=%d pipelines=%d err=%v", n, hook.pipelines, err)
	}
}

func TestLiveArrowConnectionClose(t *testing.T) {
	f, _ := liveArrowFixture(t, "close")
	st := liveArrowStatement(t, f, "it_live_arrow_close", adbc.OptionValueIngestModeAppend)
	rec := liveArrowRecord(t, []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []any{int64(1)})
	input := bindLiveArrow(t, f, st, rec)
	ctx, execution := st.(*statementImpl).conn.executionContext(f.h.ctx)
	if execution == nil {
		t.Fatal("missing execution")
	}
	defer execution.finish()
	closed := make(chan error, 1)
	execution.client.AddHook(&liveArrowPipelineHook{before: func() {
		go func() { closed <- f.h.conn.Close(f.h.ctx) }()
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Error("connection close did not cancel ingestion")
		}
	}})
	n, err := st.ExecuteUpdate(ctx)
	execution.finish() // This test owns the transport that Close waits to drain.
	if n != 0 || err == nil || input.releases.Load() != 1 {
		t.Fatalf("n=%d releases=%d err=%v", n, input.releases.Load(), err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connection close did not drain live execution")
	}
}

// Exercise the shared pipeline through the pre-existing unmanaged Arrow path
// without the destructive snapshot fixture used by its broader test suite.
func TestLiveArrowSharedUnmanagedWriter(t *testing.T) {
	f := newHashFixture(t, "it:live:arrow:unmanaged:")
	const table = "it_live_arrow_unmanaged_regression"
	f.dropAdopted(table)
	f.hset(f.prefix+"seed", "n", "1")
	const index = "it_live_arrow_unmanaged_regression_idx"
	_ = f.raw.Do(f.h.ctx, "FT.DROPINDEX", index).Err()
	if err := f.raw.Do(f.h.ctx, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, f.prefix, "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.raw.Do(f.h.ctx, "FT.DROPINDEX", index).Err() })
	if err := (&store{client: f.raw}).awaitIndexReady(f.h.ctx, index); err != nil {
		t.Fatal(err)
	}
	f.adopt(HashAdoptOptions{Table: table, Mode: "unmanaged", Apply: true})
	t.Cleanup(func() { f.dropAdopted(table) })
	st := liveArrowStatement(t, f, table, adbc.OptionValueIngestModeAppend)
	if err := st.SetOption(f.h.ctx, adbc.OptionValueIngestTargetDBSchema, "public"); err != nil {
		t.Fatal(err)
	}
	rec := liveArrowRecord(t, []arrow.Field{{Name: keyField, Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []any{f.prefix + "new", int64(42)})
	bindLiveArrow(t, f, st, rec)
	n, err := st.ExecuteUpdate(f.h.ctx)
	if n != 1 || err != nil || f.raw.HGet(f.h.ctx, f.prefix+"new", "n").Val() != "42" {
		t.Fatalf("unmanaged append regression: n=%d err=%v", n, err)
	}
	f.h.expectRows("SELECT n FROM "+table+" WHERE __key='"+f.prefix+"new'", "42")
}

type liveArrowInput struct {
	array.RecordReader
	calls, batches, releases atomic.Int64
}

func (r *liveArrowInput) Next() bool {
	r.calls.Add(1)
	ok := r.RecordReader.Next()
	if ok {
		r.batches.Add(1)
	}
	return ok
}
func (r *liveArrowInput) Release() { r.releases.Add(1); r.RecordReader.Release() }

func bindLiveArrow(t *testing.T, f *hashFixture, st adbc.StatementWithContext, records ...arrow.RecordBatch) *liveArrowInput {
	t.Helper()
	r, err := array.NewRecordReader(records[0].Schema(), records)
	if err != nil {
		t.Fatal(err)
	}
	input := &liveArrowInput{RecordReader: r}
	t.Cleanup(input.Release)
	if err := st.BindStream(f.h.ctx, input); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestLiveArrowAppend(t *testing.T) {
	f, registration := liveArrowFixture(t, "append")
	h := f.h
	if err := f.raw.Expire(h.ctx, f.prefix+"seed", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	root := f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	meta := f.raw.HGet(h.ctx, liveRegistry, "it_live_arrow_append").Val()
	st := liveArrowStatement(t, f, "it_live_arrow_append", adbc.OptionValueIngestModeAppend)
	rec := liveArrowRecord(t, []arrow.Field{{Name: "payload", Type: arrow.BinaryTypes.String}, {Name: keyField, Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}},
		[]any{"one", f.prefix + "one", int64(9007199254740993)}, []any{"two\x00tail", f.prefix + "nul\x00two", int64(22)})
	input := bindLiveArrow(t, f, st, rec)
	n, err := st.ExecuteUpdate(h.ctx)
	if err != nil || n != 2 {
		t.Fatalf("append rows=%d err=%v", n, err)
	}
	if input.batches.Load() != 1 || input.releases.Load() != 1 || st.(*statementImpl).params != nil {
		t.Fatal("input replayed or driver reference retained")
	}
	h.expectRows("SELECT n,payload FROM redis_live.it_live_arrow_append WHERE __key='"+f.prefix+"one'", "9007199254740993|one")
	if got := f.raw.HGet(h.ctx, f.prefix+"nul\x00two", "payload").Val(); got != "two\x00tail" {
		t.Fatalf("binary row/value changed: %q", got)
	}
	if f.raw.HGet(h.ctx, f.prefix+"seed", "extra").Val() != "keep" || f.raw.TTL(h.ctx, f.prefix+"seed").Val() <= 0 {
		t.Fatal("append changed existing HASH/TTL")
	}
	if root != f.raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() || meta != f.raw.HGet(h.ctx, liveRegistry, "it_live_arrow_append").Val() {
		t.Fatal("append published or changed registration")
	}
	if err := f.raw.Do(h.ctx, "FT.INFO", registration.Index).Err(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{f.prefix + "one", f.prefix + "nul\x00two"} {
		if f.raw.HExists(h.ctx, key, keyField).Val() || f.raw.HExists(h.ctx, key, rowIDField).Val() {
			t.Fatal("append stored internal fields")
		}
	}
}
