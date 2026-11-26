// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func setLiveSource(t *testing.T, h *sqlHarness, st adbc.StatementWithContext, cfg unmanagedIngestSource) {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, OptionStringIngestLiveSource, string(b)); err != nil {
		t.Fatal(err)
	}
}
func enableLiveIngest(t *testing.T, h *sqlHarness) {
	t.Helper()
	if err := h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterLiveHashes(h.ctx, unmanagedArrowDatabaseOptions(), "live") })
}

func TestSnapshotLiveArrowCreateAppendReplace(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	enableLiveIngest(t, h)
	st, input := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeCreate, []string{"liveapp:one"}, []int64{1})
	setLiveSource(t, h, st, unmanagedIngestSource{Prefix: "liveapp:"})
	if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("create %d %v", n, err)
	}
	if input.batches.Load() != 1 || raw.HGet(h.ctx, "liveapp:one", "n").Val() != "1" {
		t.Fatal("missing source row")
	}
	h.expectRows("SELECT n FROM redis_live.live", "1")
	impl := st.(*statementImpl)
	_, old, err := impl.conn.store.readLiveRecord(h.ctx, liveRef{schema: liveSchema}, "live")
	if err != nil {
		t.Fatal(err)
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	appendStmt, _ := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeCreateAppend, []string{"liveapp:two"}, []int64{2})
	if n, err := appendStmt.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("append %d %v", n, err)
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("live append published managed catalog")
	}
	raw.HSet(h.ctx, "liveapp:one", "unknown", "discard")
	raw.Expire(h.ctx, "liveapp:one", time.Hour)
	replace, _ := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeReplace, []string{"liveapp:one", "liveapp:three"}, []int64{11, 3})
	setLiveSource(t, h, replace, unmanagedIngestSource{Prefix: "liveapp:", Index: old.Meta.index(), Replace: &unmanagedReplaceConsent{Schema: liveSchema, Table: "live", ExclusiveQuiescent: true, DiscardTTLs: true, ResetIndex: true}})
	if n, err := replace.ExecuteUpdate(h.ctx); n != 2 || err != nil {
		t.Fatalf("replace %d %v", n, err)
	}
	h.expectRows("SELECT n FROM redis_live.live ORDER BY n", "3", "11")
	if err := impl.conn.store.checkLiveIdentity(h.ctx, old.Meta); err == nil {
		t.Fatal("old live writer survived replacement")
	}
	if raw.Exists(h.ctx, "liveapp:two").Val() != 0 || raw.HExists(h.ctx, "liveapp:one", "unknown").Val() || raw.PTTL(h.ctx, "liveapp:one").Val() != -1 {
		t.Fatal("replacement retained old rows, fields or TTL")
	}
	for _, key := range []string{"liveapp:one", "liveapp:three"} {
		if raw.HExists(h.ctx, key, rowIDField).Val() || raw.HExists(h.ctx, key, keyField).Val() {
			t.Fatal("stored internal field")
		}
	}
	if err := UnregisterLiveHashes(h.ctx, unmanagedArrowDatabaseOptions(), "live"); err != nil {
		t.Fatal(err)
	}
	if raw.Exists(h.ctx, "liveapp:one", "liveapp:three").Val() != 2 {
		t.Fatal("unregister removed application data")
	}
}

func TestSnapshotLiveArrowGeneratedKeys(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	enableLiveIngest(t, h)
	st, _ := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeCreate, nil, nil)
	fields := []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}
	b := array.NewInt64Builder(memory.DefaultAllocator)
	defer b.Release()
	b.AppendValues([]int64{7, 8}, nil)
	a := b.NewArray()
	defer a.Release()
	bindUnmanagedCustom(t, h, st, fields, []arrow.Array{a})
	setLiveSource(t, h, st, unmanagedIngestSource{Prefix: "generated:"})
	if n, err := st.ExecuteUpdate(h.ctx); n != 2 || err != nil {
		t.Fatalf("generated %d %v", n, err)
	}
	rows, _ := h.query("SELECT __key,n FROM redis_live.live ORDER BY n")
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		key, _, _ := strings.Cut(row, "|")
		if !strings.HasPrefix(key, "generated:") || raw.HLen(h.ctx, key).Val() != 1 {
			t.Fatal(row)
		}
	}
}

func TestSnapshotLiveArrowPreflight(t *testing.T) {
	for _, kind := range []string{"missing_config", "occupied", "nested", "no_consent", "disabled", "wrong_schema", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			enableLiveIngest(t, h)
			mode := adbc.OptionValueIngestModeCreate
			schema := liveSchema
			switch kind {
			case "occupied":
				raw.HSet(h.ctx, "pre:old", "n", "9")
			case "nested":
				raw.HSet(h.ctx, "pre:old:nested", "n", "9")
			case "no_consent":
				mode = adbc.OptionValueIngestModeReplace
			case "wrong_schema":
				schema = "public"
			case "disabled":
				_ = h.conn.(adbc.GetSetOptionsWithContext).SetOption(h.ctx, OptionStringLiveTables, "false")
			}
			if kind == "duplicate" {
				first, _ := unmanagedArrowInput(t, h, liveSchema, mode, nil, nil)
				setLiveSource(t, h, first, unmanagedIngestSource{Prefix: "pre:"})
				if _, err := first.ExecuteUpdate(h.ctx); err != nil {
					t.Fatal(err)
				}
			}
			st, input := unmanagedArrowInput(t, h, schema, mode, []string{"pre:new"}, []int64{1})
			if kind != "missing_config" {
				setLiveSource(t, h, st, unmanagedIngestSource{Prefix: "pre:"})
			}
			if _, err := st.ExecuteUpdate(h.ctx); err == nil {
				t.Fatal("accepted invalid preflight")
			}
			if input.calls.Load() != 0 || raw.Exists(h.ctx, "pre:new").Val() != 0 {
				t.Fatal("preflight consumed input or wrote rows")
			}
		})
	}
}

func newLiveReplaceInput(t *testing.T, h *sqlHarness, keys []string, values []int64) (adbc.StatementWithContext, *ingestAdmissionReader) {
	t.Helper()
	st, input := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeReplace, keys, values)
	_, r, err := st.(*statementImpl).conn.store.readLiveRecord(h.ctx, liveRef{schema: liveSchema}, "live")
	if err != nil {
		t.Fatal(err)
	}
	setLiveSource(t, h, st, unmanagedIngestSource{Prefix: r.Meta.prefix(), Index: r.Meta.index(), Replace: &unmanagedReplaceConsent{Schema: liveSchema, Table: "live", ExclusiveQuiescent: true, DiscardTTLs: true, ResetIndex: true}})
	return st, input
}
func createLiveSeed(t *testing.T, h *sqlHarness) {
	t.Helper()
	st, _ := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeCreate, []string{"liveapp:seed"}, []int64{9})
	setLiveSource(t, h, st, unmanagedIngestSource{Prefix: "liveapp:"})
	if _, err := st.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotLiveArrowReplacementSchemaAndEmpty(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	enableLiveIngest(t, h)
	createLiveSeed(t, h)
	st, _ := newLiveReplaceInput(t, h, nil, nil)
	key := "liveapp:" + string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(key, 0) {
		t.Fatal("no NUL in fixture")
	}
	fields := []arrow.Field{{Name: "__key", Type: arrow.BinaryTypes.String}, {Name: "label", Type: arrow.BinaryTypes.String}}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(key)
	b.Field(1).(*array.StringBuilder).Append("new")
	rec := b.NewRecordBatch()
	defer rec.Release()
	bindUnmanagedCustom(t, h, st, fields, rec.Columns())
	if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("schema replacement %d %v", n, err)
	}
	h.expectRows("SELECT label FROM redis_live.live", "new")
	if raw.HGet(h.ctx, key, "label").Val() != "new" {
		t.Fatal("binary key changed")
	}
	empty, _ := newLiveReplaceInput(t, h, nil, nil)
	if n, err := empty.ExecuteUpdate(h.ctx); n != 0 || err != nil {
		t.Fatalf("empty replacement %d %v", n, err)
	}
	h.expectRows("SELECT COUNT(*) FROM redis_live.live", "0")
	h.expectRows("SELECT column_name FROM information_schema.columns WHERE table_schema='redis_live' AND table_name='live'", "n")
}

func TestSnapshotLiveArrowReplaceScopeRefusals(t *testing.T) {
	for _, kind := range []string{"wrong_target", "wrong_index", "no_quiescence", "keep_ttls", "keep_index", "nested", "overlap", "other_registration"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			enableLiveIngest(t, h)
			createLiveSeed(t, h)
			st, input := newLiveReplaceInput(t, h, []string{"liveapp:new"}, []int64{1})
			cfg := st.(*statementImpl).liveSource
			switch kind {
			case "wrong_target":
				cfg.Replace.Table = "other"
			case "wrong_index":
				cfg.Index = "other"
			case "no_quiescence":
				cfg.Replace.ExclusiveQuiescent = false
			case "keep_ttls":
				cfg.Replace.DiscardTTLs = false
			case "keep_index":
				cfg.Replace.ResetIndex = false
			case "nested":
				raw.HSet(h.ctx, "liveapp:seed:nested", "n", "1")
			case "overlap":
				if err := raw.Do(h.ctx, "FT.CREATE", "overlap", "ON", "HASH", "PREFIX", 1, "liveapp:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
					t.Fatal(err)
				}
			case "other_registration":
				r := *st.(*statementImpl).conn.store
				_, record, err := r.readLiveRecord(h.ctx, liveRef{schema: liveSchema}, "live")
				if err != nil {
					t.Fatal(err)
				}
				other := *record.Meta
				other.Name = "other"
				b, _ := json.Marshal(liveRecord{State: "ready", Meta: &other})
				raw.HSet(h.ctx, liveRegistry, "other", string(b))
			}
			if _, err := st.ExecuteUpdate(h.ctx); err == nil {
				t.Fatal("unsafe replacement succeeded")
			}
			if input.calls.Load() != 0 || raw.HGet(h.ctx, "liveapp:seed", "n").Val() != "9" {
				t.Fatal("preflight changed data or consumed input")
			}
		})
	}
}

func TestSnapshotLiveArrowReplaceUnknownOutcome(t *testing.T) {
	for _, phase := range []string{"delete", "drop_index", "create_index"} {
		t.Run(phase, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			enableLiveIngest(t, h)
			createLiveSeed(t, h)
			st, input := newLiveReplaceInput(t, h, []string{"liveapp:new"}, []int64{1})
			calls := 0
			hook := &unmanagedReplaceHook{after: func(ctx context.Context, cmd goredis.Cmder) error {
				match := phase == "delete" && replaceScript(cmd, unmanagedReplaceDeleteLua) || phase == "drop_index" && cmd.Name() == "ft.dropindex" || phase == "create_index" && cmd.Name() == "ft.create"
				if match {
					calls++
					return io.ErrUnexpectedEOF
				}
				return nil
			}}
			impl := st.(*statementImpl)
			impl.conn.store.client.AddHook(hook)
			// Use the observed client for this execution, retaining one-shot commands.
			n, err := impl.executeSourceReplace(h.ctx, impl.liveSource, true, liveRef{schema: liveSchema})
			if n != 0 || err == nil || calls != 1 || input.calls.Load() != 0 {
				t.Fatalf("n=%d calls=%d input=%d err=%v", n, calls, input.calls.Load(), err)
			}
			assertReplaceDetail(t, err, "phase", phase)
			assertReplaceDetail(t, err, "current_outcome", "unknown")
			if raw.Exists(h.ctx, "liveapp:new").Val() != 0 {
				t.Fatal("input consumed after uncertain outcome")
			}
		})
	}
}

func TestSnapshotLiveArrowCreateAppendNewAndEncoding(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	enableLiveIngest(t, h)
	st, _ := unmanagedArrowInput(t, h, liveSchema, adbc.OptionValueIngestModeCreateAppend, nil, nil)
	fields := []arrow.Field{{Name: "active", Type: arrow.FixedWidthTypes.Boolean}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	b.Field(0).(*array.BooleanBuilder).Append(true)
	b.Field(1).(*array.Int64Builder).Append(9007199254740993)
	rec := b.NewRecordBatch()
	defer rec.Release()
	bindUnmanagedCustom(t, h, st, fields, rec.Columns())
	setLiveSource(t, h, st, unmanagedIngestSource{Prefix: "liveapp:", Encodings: map[string]string{"active": "text"}})
	if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("create_append %d %v", n, err)
	}
	h.expectRows("SELECT active,n FROM redis_live.live", "true|9007199254740993")
	rows, _ := h.query("SELECT __key FROM redis_live.live")
	if len(rows) != 1 || raw.HGet(h.ctx, rows[0], "active").Val() != "true" {
		t.Fatal("lost text encoding")
	}
}

func TestSnapshotLiveArrowReplacePartialWrite(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	enableLiveIngest(t, h)
	createLiveSeed(t, h)
	keys := make([]string, unmanagedInsertPipelineRows+1)
	values := make([]int64, len(keys))
	for i := range keys {
		keys[i] = fmt.Sprintf("liveapp:%d", i)
		values[i] = int64(i)
	}
	st, input := newLiveReplaceInput(t, h, keys, values)
	hook := &liveArrowPipelineHook{after: func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds[1:] {
			cmd.SetErr(io.ErrUnexpectedEOF)
		}
		return io.ErrUnexpectedEOF
	}}
	ctx, execution := st.(*statementImpl).conn.executionContext(h.ctx)
	if execution == nil {
		t.Fatal("missing execution owner")
	}
	defer execution.finish()
	execution.client.AddHook(hook)
	n, err := st.ExecuteUpdate(ctx)
	if err == nil || n != 1 || hook.pipelines != 1 || input.batches.Load() != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, hook.pipelines, err)
	}
	assertReplaceDetail(t, err, "acknowledged_deletes", "1")
	assertUnmanagedAcknowledged(t, err, 1)
	assertReplaceDetail(t, err, "phase", "write")
	assertReplaceDetail(t, err, "current_outcome", "unknown")
	if raw.Exists(h.ctx, "liveapp:seed", keys[len(keys)-1]).Val() != 0 || raw.HGet(h.ctx, keys[1], "n").Val() != "1" {
		t.Fatal("replayed input or lost applied rows")
	}
}
