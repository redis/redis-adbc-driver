// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func setUnmanagedSource(t *testing.T, h *sqlHarness, st adbc.StatementWithContext, prefix, index string, enc map[string]string) {
	t.Helper()
	b, _ := json.Marshal(unmanagedIngestSource{Prefix: prefix, Index: index, Encodings: enc})
	if err := st.SetOption(h.ctx, OptionStringIngestUnmanagedSource, string(b)); err != nil {
		t.Fatal(err)
	}
}
func TestUnmanagedArrowCreateAndCreateAppend(t *testing.T) {
	for _, mode := range []string{adbc.OptionValueIngestModeCreate, adbc.OptionValueIngestModeCreateAppend} {
		t.Run(mode, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			st, input := unmanagedArrowInput(t, h, "public", mode, []string{"new:one"}, []int64{1})
			setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
			if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
				t.Fatalf("create n=%d err=%v", n, err)
			}
			if input.batches.Load() != 1 || raw.HGet(h.ctx, "new:one", "n").Val() != "1" || raw.HExists(h.ctx, "new:one", rowIDField).Val() {
				t.Fatal("incorrect source rows")
			}
			h.expectRows("SELECT n FROM live", "1")
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			next, _ := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreateAppend, []string{"new:two"}, []int64{2})
			if n, err := next.ExecuteUpdate(h.ctx); n != 1 || err != nil {
				t.Fatalf("existing create_append n=%d err=%v", n, err)
			}
			if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
				t.Fatal("existing create_append published source data")
			}
			h.expectRows("SELECT n FROM live ORDER BY n", "1", "2")
			duplicate, unread := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:three"}, []int64{3})
			setUnmanagedSource(t, h, duplicate, "new:", "new_idx", nil)
			if _, err := duplicate.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatal("duplicate create", err)
			}
			if unread.calls.Load() != 0 {
				t.Fatal("duplicate create consumed input")
			}
			s := st.(*statementImpl).conn.store
			ctx, pin, err := s.acquireSnapshot(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := s.getTableMetadata(ctx, "public", "live")
			pin.release()
			if err != nil {
				t.Fatal(err)
			}
			if !meta.Unmanaged || meta.SnapshotRows != nil || len(meta.SnapshotSegments) != 0 || len(meta.Columns) != 1 {
				t.Fatalf("wrong metadata %+v", meta)
			}
			h.exec("DROP TABLE live")
			if _, err := SweepReleased(h.ctx, unmanagedArrowDatabaseOptions(), SweepOptions{Apply: true}); err != nil {
				t.Fatal(err)
			}
			if raw.Exists(h.ctx, "new:one", "new:two").Val() != 2 || raw.Do(h.ctx, "FT.INFO", "new_idx").Err() != nil {
				t.Fatal("DROP/GC changed application resources")
			}
		})
	}
}

func TestUnmanagedArrowCreateMembershipAndEncoding(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	st, _ := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, nil, nil)
	fields := []arrow.Field{{Name: "__key", Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "enabled", Type: arrow.FixedWidthTypes.Boolean}}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"new:a", "new:b"}, nil)
	b.Field(1).(*array.Int64Builder).AppendValues([]int64{0, 2}, []bool{false, true})
	b.Field(2).(*array.BooleanBuilder).AppendValues([]bool{true, false}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()
	bindUnmanagedCustom(t, h, st, fields, rec.Columns())
	setUnmanagedSource(t, h, st, "new:", "new_idx", map[string]string{"enabled": "text"})
	if n, err := st.ExecuteUpdate(h.ctx); n != 2 || err != nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if raw.HExists(h.ctx, "new:a", "n").Val() || raw.HGet(h.ctx, "new:a", "enabled").Val() != "true" || raw.HGet(h.ctx, "new:b", "enabled").Val() != "false" {
		t.Fatal("wrong encoding/NULL handling")
	}
	h.expectRows("SELECT enabled FROM live ORDER BY __key", "true", "false")
	h.expectRows("SELECT COUNT(*) FROM live", "2")
}

func TestUnmanagedArrowCreatePreflight(t *testing.T) {
	for _, kind := range []string{"prefix", "nested", "index", "managed", "view", "append_missing", "replace", "retained", "shared", "bad_encoding", "index_columns"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			mode := adbc.OptionValueIngestModeCreate
			if kind == "managed" {
				h.exec("CREATE TABLE live (n BIGINT)")
				mode = adbc.OptionValueIngestModeCreateAppend
			}
			if kind == "view" {
				h.exec("CREATE VIEW live AS SELECT 1 AS n")
			}
			if kind == "prefix" {
				raw.Set(h.ctx, "new:old", "keep", 0)
			}
			if kind == "nested" {
				raw.HSet(h.ctx, "new:old:nested", "n", "7")
			}
			if kind == "index" {
				if err := raw.Do(h.ctx, "FT.CREATE", "new_idx", "ON", "HASH", "PREFIX", 1, "other:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "append_missing" {
				mode = adbc.OptionValueIngestModeAppend
			}
			if kind == "replace" {
				mode = adbc.OptionValueIngestModeReplace
			}
			st, input := unmanagedArrowInput(t, h, "public", mode, []string{"new:one"}, []int64{1})
			enc := map[string]string(nil)
			if kind == "bad_encoding" {
				enc = map[string]string{"n": "text"}
			}
			setUnmanagedSource(t, h, st, "new:", "new_idx", enc)
			if kind == "index_columns" {
				st.SetOption(h.ctx, OptionStringIngestIndexColumns, "n")
			}
			if kind == "retained" {
				st.SetOption(h.ctx, OptionStringOperationAction, "prepare")
			}
			if kind == "shared" {
				setter := h.conn.(adbc.GetSetOptionsWithContext)
				if err := setter.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
					t.Fatal(err)
				}
				defer setter.SetOption(h.ctx, OptionStringReadSnapshotAction, "release")
			}
			if _, err := st.ExecuteUpdate(h.ctx); err == nil {
				t.Fatal("invalid creation accepted")
			}
			if input.calls.Load() != 0 || raw.Exists(h.ctx, "new:one").Val() != 0 {
				t.Fatal("preflight consumed input or changed source")
			}
			if kind != "index" && raw.Do(h.ctx, "FT.INFO", "new_idx").Err() == nil {
				t.Fatal("preflight created an index")
			}
			if kind == "prefix" && raw.Get(h.ctx, "new:old").Val() != "keep" {
				t.Fatal("changed preexisting key")
			}
		})
	}
}

type unmanagedCreateLostReply struct {
	goredis.UniversalClient
	input *ingestAdmissionReader
	calls int
	t     *testing.T
}

func (h *unmanagedCreateLostReply) Process(ctx context.Context, cmd goredis.Cmder) error {
	return h.ProcessHook(h.UniversalClient.Process)(ctx, cmd)
}

func (h *unmanagedCreateLostReply) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if strings.EqualFold(cmd.Name(), "ft.create") && len(cmd.Args()) > 1 && cmd.Args()[1] == "new_idx" {
			h.calls++
			if snapshotOf(ctx) != nil || h.input.calls.Load() != 0 {
				h.t.Fatal("source creation fenced or input consumed before registration")
			}
			if once, ok := cmd.(interface{ NoRetry() bool }); !ok || !once.NoRetry() {
				h.t.Fatal("creation permits transport replay")
			}
			if err := next(ctx, cmd); err != nil {
				return err
			}
			cmd.SetErr(io.ErrUnexpectedEOF)
			return cmd.Err()
		}
		return next(ctx, cmd)
	}
}
func TestUnmanagedArrowCreateUnknownReply(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	st, input := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:one"}, []int64{1})
	setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
	s := st.(*statementImpl).conn.store
	hook := &unmanagedCreateLostReply{UniversalClient: s.client, input: input, t: t}
	s.client = hook
	// Inject at the source controller boundary; public execution uses a fresh
	// transport pool, while this delegate observes the actual once command.
	n, err := st.(*statementImpl).executeUnmanagedSourceIngest(h.ctx)
	if n != 0 || err == nil || hook.calls != 1 || !strings.Contains(err.Error(), "may have left") {
		t.Fatalf("n=%d calls=%d err=%v", n, hook.calls, err)
	}
	if input.calls.Load() != 0 || raw.Exists(h.ctx, "new:one").Val() != 0 || raw.Do(h.ctx, "FT.INFO", "new_idx").Err() != nil {
		t.Fatal("unknown creation replayed, consumed input or removed application index")
	}
}
func TestUnmanagedArrowCreateAppendConcurrent(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, unmanagedArrowDatabaseOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(h.ctx) })
	statements := make([]adbc.StatementWithContext, 4)
	for i := range statements {
		c, err := db.Open(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close(h.ctx) })
		worker := &sqlHarness{t: t, ctx: h.ctx, conn: c}
		statements[i], _ = unmanagedArrowInput(t, worker, "public", adbc.OptionValueIngestModeCreateAppend, []string{fmt.Sprintf("new:k%d", i)}, []int64{int64(i)})
		setUnmanagedSource(t, worker, statements[i], "new:", "new_idx", nil)
	}
	start := make(chan struct{})
	done := make(chan error, 4)
	for _, st := range statements {
		go func(st adbc.StatementWithContext) {
			<-start
			n, err := st.ExecuteUpdate(h.ctx)
			if err == nil && n != 1 {
				err = fmt.Errorf("rows %d", n)
			}
			done <- err
		}(st)
	}
	close(start)
	for range statements {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		return
	}
	for i := range statements {
		if raw.HGet(h.ctx, fmt.Sprintf("new:k%d", i), "n").Val() != fmt.Sprint(i) {
			t.Fatal("missing concurrent source row")
		}
	}
	h.expectRows("SELECT COUNT(*) FROM live", "4")
}

func TestUnmanagedArrowCreateAppendIdentity(t *testing.T) {
	for _, kind := range []string{"prefix", "index", "encoding"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			first, _ := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:one"}, []int64{1})
			setUnmanagedSource(t, h, first, "new:", "new_idx", nil)
			if _, err := first.ExecuteUpdate(h.ctx); err != nil {
				t.Fatal(err)
			}
			next, input := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreateAppend, []string{"new:two"}, []int64{2})
			prefix, index := "new:", "new_idx"
			enc := map[string]string(nil)
			if kind == "prefix" {
				prefix = "other:"
			}
			if kind == "index" {
				index = "other_idx"
			}
			if kind == "encoding" {
				enc = map[string]string{"n": "text"}
			}
			setUnmanagedSource(t, h, next, prefix, index, enc)
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			if _, err := next.ExecuteUpdate(h.ctx); err == nil {
				t.Fatal("source identity/encoding change accepted")
			}
			if input.calls.Load() != 0 || raw.Exists(h.ctx, "new:two").Val() != 0 || root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
				t.Fatal("mismatch changed source/catalog or consumed input")
			}
		})
	}
}

func TestUnmanagedArrowCreatePartialInput(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	keys := make([]string, snapshotIngestConvertRows+1)
	values := make([]int64, len(keys))
	for i := range keys {
		keys[i] = fmt.Sprintf("new:k%d", i)
		values[i] = int64(i)
	}
	keys[len(keys)-1] = keys[0]
	st, _ := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, keys, values)
	setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
	n, err := st.ExecuteUpdate(h.ctx)
	if n != snapshotIngestConvertRows || err == nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	assertUnmanagedAcknowledged(t, err, n)
	if raw.HGet(h.ctx, "new:k0", "n").Val() != "0" || raw.HGet(h.ctx, fmt.Sprintf("new:k%d", snapshotIngestConvertRows-1), "n").Val() != fmt.Sprint(snapshotIngestConvertRows-1) {
		t.Fatal("partial source writes lost")
	}
	h.expectRows("SELECT COUNT(*) FROM live", fmt.Sprint(snapshotIngestConvertRows))
}

type unmanagedCreateAfter struct {
	goredis.UniversalClient
	after func()
	calls int
}

func (h *unmanagedCreateAfter) Process(ctx context.Context, cmd goredis.Cmder) error {
	err := h.UniversalClient.Process(ctx, cmd)
	if err == nil && strings.EqualFold(cmd.Name(), "ft.create") && len(cmd.Args()) > 1 && cmd.Args()[1] == "new_idx" {
		h.calls++
		h.after()
	}
	return err
}
func TestUnmanagedArrowCreateCancellation(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	st, input := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:one"}, []int64{1})
	setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	s := st.(*statementImpl)
	hook := &unmanagedCreateAfter{UniversalClient: s.conn.store.client, after: cancel}
	s.conn.store.client = hook
	n, err := s.executeUnmanagedSourceIngest(ctx)
	if n != 0 || err == nil || hook.calls != 1 {
		t.Fatalf("n=%d err=%v calls=%d", n, err, hook.calls)
	}
	if input.calls.Load() != 0 || raw.Exists(h.ctx, "new:one").Val() != 0 || raw.Do(h.ctx, "FT.INFO", "new_idx").Err() != nil {
		t.Fatal("cancellation consumed/wrote or deleted an application resource")
	}
}
func TestUnmanagedArrowCreateDoesNotRefreshRacingRegistration(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	st, input := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:one"}, []int64{1})
	setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
	s := st.(*statementImpl)
	other := snapshotTestStore(t, raw)
	hook := &unmanagedCreateAfter{UniversalClient: s.conn.store.client, after: func() {
		meta := &tableMeta{Schema: "public", Name: "live", KeyPrefix: "new:", IndexName: "new_idx", Adopted: true, KeyRows: true, AppIndex: true, Unmanaged: true, UnmanagedPrefixes: []string{"new:"}, Columns: []columnMeta{{Name: "other", Type: typeString, Nullable: true}}}
		if err := other.registerUnmanaged(h.ctx, meta); err != nil {
			t.Fatal(err)
		}
	}}
	s.conn.store.client = hook
	n, err := s.executeUnmanagedSourceIngest(h.ctx)
	if n != 0 || err == nil {
		t.Fatalf("racing registration overwritten: n=%d err=%v", n, err)
	}
	if input.calls.Load() != 0 {
		t.Fatal("race consumed input")
	}
	ctx, pin, err := other.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := other.getTableMetadata(ctx, "public", "live")
	if err != nil || len(meta.Columns) != 1 || meta.Columns[0].Name != "other" {
		t.Fatalf("race schema changed: %+v %v", meta, err)
	}
}

func TestUnmanagedSourceOptionClearManagedDefault(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	st, _ := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:one"}, []int64{1})
	setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
	original, _ := st.(*statementImpl).GetOption(h.ctx, OptionStringIngestUnmanagedSource)
	if err := st.SetOption(h.ctx, OptionStringIngestUnmanagedSource, `{"prefix":"bad:"}`); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	if got, _ := st.(*statementImpl).GetOption(h.ctx, OptionStringIngestUnmanagedSource); got != original {
		t.Fatal("invalid option changed prior configuration")
	}
	if _, err := st.ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, OptionStringIngestUnmanagedSource, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.(*statementImpl).GetOption(h.ctx, OptionStringIngestUnmanagedSource); got != "" {
		t.Fatal("option did not clear")
	}
	if err := st.SetOption(h.ctx, adbc.OptionKeyIngestTargetTable, "managed"); err != nil {
		t.Fatal(err)
	}
	b := array.NewInt64Builder(memory.DefaultAllocator)
	b.Append(2)
	a := b.NewArray()
	b.Release()
	defer a.Release()
	bindUnmanagedCustom(t, h, st, []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []arrow.Array{a})
	if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("managed default n=%d err=%v", n, err)
	}
	s := st.(*statementImpl).conn.store
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTableMetadata(ctx, "public", "managed")
	if err != nil || meta.Unmanaged {
		t.Fatalf("default did not remain managed: %+v %v", meta, err)
	}
}

type unmanagedCreateRefresh struct {
	goredis.UniversalClient
	after func()
	done  bool
}

func (h *unmanagedCreateRefresh) Process(ctx context.Context, cmd goredis.Cmder) error {
	err := h.UniversalClient.Process(ctx, cmd)
	a := snapshotObservedControlArgs(cmd)
	if err == nil && !h.done && len(a) > 3 && strings.EqualFold(snapshotObservedControlName(cmd), "eval") {
		n, _ := strconv.Atoi(fmt.Sprint(a[2]))
		if len(a) > 3+n && a[3+n] == "publish" {
			h.done = true
			h.after()
		}
	}
	return err
}
func TestUnmanagedArrowCreatePinsCreatedSchema(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	st, input := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:one"}, []int64{1})
	setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
	s := st.(*statementImpl)
	other := snapshotTestStore(t, raw)
	hook := &unmanagedCreateRefresh{UniversalClient: s.conn.store.client, after: func() {
		ctx, pin, err := other.acquireSnapshot(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := other.getTableMetadata(ctx, "public", "live")
		pin.release()
		if err != nil {
			t.Fatal(err)
		}
		meta.Columns[0].Name = "changed"
		if err := other.registerUnmanaged(h.ctx, meta); err != nil {
			t.Fatal(err)
		}
	}}
	s.conn.store.client = hook
	n, err := s.executeUnmanagedSourceIngest(h.ctx)
	if !hook.done {
		t.Fatal("publication observer did not run")
	}
	if n != 0 || err == nil || !strings.Contains(err.Error(), "changed during creation") {
		t.Fatalf("schema race redirected input: n=%d err=%v", n, err)
	}
	if input.calls.Load() != 0 || raw.Exists(h.ctx, "new:one").Val() != 0 {
		t.Fatal("schema race consumed/wrote input")
	}
}

type unmanagedCreateBefore struct {
	goredis.UniversalClient
	before func()
}

func (h *unmanagedCreateBefore) Process(ctx context.Context, cmd goredis.Cmder) error {
	if strings.EqualFold(cmd.Name(), "ft.create") && len(cmd.Args()) > 1 && cmd.Args()[1] == "new_idx" {
		h.before()
	}
	return h.UniversalClient.Process(ctx, cmd)
}
func TestUnmanagedArrowCreateRefusesMissedInitialRow(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	st, input := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeCreate, []string{"new:one"}, []int64{1})
	setUnmanagedSource(t, h, st, "new:", "new_idx", nil)
	s := st.(*statementImpl)
	s.conn.store.client = &unmanagedCreateBefore{UniversalClient: s.conn.store.client, before: func() {
		if err := raw.HSet(h.ctx, "new:external", "n", "99").Err(); err != nil {
			t.Fatal(err)
		}
	}}
	n, err := s.executeUnmanagedSourceIngest(h.ctx)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "empty application prefix") {
		t.Fatalf("missed row advertised: n=%d err=%v", n, err)
	}
	if input.calls.Load() != 0 || raw.HGet(h.ctx, "new:external", "n").Val() != "99" || raw.Exists(h.ctx, "new:one").Val() != 0 {
		t.Fatal("creation race consumed input or changed source")
	}
}
