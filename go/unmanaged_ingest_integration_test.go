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
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func unmanagedArrowDatabaseOptions() map[string]string {
	return map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: fmt.Sprint(os.Getenv("REDIS_SNAPSHOT_FLEX") == "true")}
}

func unmanagedIngestFixture(t *testing.T, foreign bool) (*sqlHarness, *goredis.Client, string) {
	t.Helper()
	h, raw, _ := newSnapshotHarness(t)
	if err := raw.HSet(h.ctx, "app:seed", "n", "10", "extra", "keep").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	if foreign {
		setter := h.conn.(adbc.GetSetOptionsWithContext)
		for _, option := range [][2]string{{OptionStringForeignIndexesMode, "unmanaged"}, {OptionStringForeignIndexes, "true"}} {
			if err := setter.SetOption(h.ctx, option[0], option[1]); err != nil {
				t.Fatal(err)
			}
		}
		h.expectRows("SELECT n FROM redis_foreign.app_idx", "10")
		return h, raw, "redis_foreign"
	}
	_, err := AdoptHashes(h.ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Table: "live", Types: map[string]string{"n": "BIGINT"}, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	return h, raw, "public"
}

func unmanagedArrowInput(t *testing.T, h *sqlHarness, schema, mode string, keys []string, values []int64) (adbc.StatementWithContext, *ingestAdmissionReader) {
	t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close(h.ctx) })
	table := "live"
	if schema == "redis_foreign" {
		table = "app_idx"
	}
	for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionValueIngestTargetDBSchema: schema, adbc.OptionKeyIngestMode: mode} {
		if err := st.SetOption(h.ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	kb := array.NewStringBuilder(memory.DefaultAllocator)
	kb.AppendValues(keys, nil)
	ka := kb.NewArray()
	kb.Release()
	defer ka.Release()
	nb := array.NewInt64Builder(memory.DefaultAllocator)
	nb.AppendValues(values, nil)
	na := nb.NewArray()
	nb.Release()
	defer na.Release()
	fields := []arrow.Field{{Name: "__key", Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}
	as := arrow.NewSchema(fields, nil)
	rec := array.NewRecordBatch(as, []arrow.Array{ka, na}, int64(len(keys)))
	defer rec.Release()
	reader, err := array.NewRecordReader(as, []arrow.RecordBatch{rec})
	if err != nil {
		t.Fatal(err)
	}
	counted := &ingestAdmissionReader{RecordReader: reader}
	t.Cleanup(counted.Release)
	counted.Retain() // BindStream takes ownership; the test retains its observer.
	if err := st.BindStream(h.ctx, counted); err != nil {
		t.Fatal(err)
	}
	return st, counted
}

func TestUnmanagedArrowAppend(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "adopt"
		if foreign {
			name = "foreign"
		}
		t.Run(name, func(t *testing.T) {
			h, raw, schema := unmanagedIngestFixture(t, foreign)
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:one", "app:\x00two"}, []int64{21, 22})
			n, err := st.ExecuteUpdate(h.ctx)
			if err != nil || n != 2 {
				t.Fatalf("append rows=%d err=%v", n, err)
			}
			for key, want := range map[string]string{"app:one": "21", "app:\x00two": "22"} {
				if got := raw.HGet(h.ctx, key, "n").Val(); got != want {
					t.Fatalf("%q: %q want %q", key, got, want)
				}
			}
			if got := raw.HGet(h.ctx, "app:seed", "extra").Val(); got != "keep" {
				t.Fatal("append changed unrelated source")
			}
			if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
				t.Fatal("unmanaged Arrow published a managed generation")
			}
		})
	}
}

func TestUnmanagedArrowRefusalsBeforeInput(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	for _, mode := range []string{adbc.OptionValueIngestModeReplace} {
		t.Run(mode, func(t *testing.T) {
			st, input := unmanagedArrowInput(t, h, schema, mode, []string{"app:new"}, []int64{1})
			if _, err := st.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), "unmanaged") {
				t.Fatalf("mode %s: %v", mode, err)
			}
			if input.calls.Load() != 0 || raw.Exists(h.ctx, "app:new").Val() != 0 {
				t.Fatal("refused mode consumed input or wrote source")
			}
		})
	}
	t.Run("retained", func(t *testing.T) {
		st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:new"}, []int64{1})
		if err := st.SetOption(h.ctx, OptionStringOperationAction, "prepare"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), "unmanaged") {
			t.Fatalf("retained ingest: %v", err)
		}
		if input.calls.Load() != 0 {
			t.Fatal("retained refusal consumed input")
		}
	})
	t.Run("shared_snapshot", func(t *testing.T) {
		setter := h.conn.(adbc.GetSetOptionsWithContext)
		if err := setter.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
			t.Fatal(err)
		}
		defer setter.SetOption(h.ctx, OptionStringReadSnapshotAction, "release")
		st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:new"}, []int64{1})
		if _, err := st.ExecuteUpdate(h.ctx); err == nil {
			t.Fatal("shared read snapshot accepted source writes")
		}
		if input.calls.Load() != 0 {
			t.Fatal("shared snapshot refusal consumed input")
		}
	})
}

func TestUnmanagedArrowKeyChecks(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	if err := raw.PExpire(h.ctx, "app:seed", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		keys    []string
		message string
	}{
		{"existing", []string{"app:seed"}, "already exists"},
		{"outside", []string{"other:one"}, "prefix"},
		{"nested", []string{"app:nested:one"}, "nested"},
		{"duplicate", []string{"app:one", "app:one"}, "duplicate"},
		{"driver_owned", []string{snapshotRows(snapshotID()) + "1"}, "prefix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, test.keys, make([]int64, len(test.keys)))
			n, err := st.ExecuteUpdate(h.ctx)
			if n != 0 || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("n=%d err=%v", n, err)
			}
			assertUnmanagedAcknowledged(t, err, 0)
		})
	}
	if raw.HGet(h.ctx, "app:seed", "n").Val() != "10" || raw.PTTL(h.ctx, "app:seed").Val() <= 0 {
		t.Fatal("refused input changed existing HASH or TTL")
	}
	if raw.Exists(h.ctx, "app:one", "other:one").Val() != 0 {
		t.Fatal("preflight failure wrote source")
	}
}

func assertUnmanagedAcknowledged(t *testing.T, err error, want int64) {
	t.Helper()
	var ae adbc.Error
	if !errors.As(err, &ae) {
		t.Fatalf("lost ADBC error: %v", err)
	}
	for _, detail := range ae.Details {
		if detail.Key() == "adbc.redis.ingest.acknowledged_rows" {
			b, e := detail.Serialize()
			if e != nil || string(b) != strconv.FormatInt(want, 10) {
				t.Fatalf("wrong count detail: %q %v", b, e)
			}
			return
		}
	}
	t.Fatal("missing acknowledged_rows detail")
}

func TestUnmanagedArrowBoundedPartialFailure(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	keys := make([]string, snapshotIngestConvertRows+1)
	values := make([]int64, len(keys))
	for i := range keys {
		keys[i] = fmt.Sprintf("app:batch%d", i)
		values[i] = int64(i)
	}
	keys[len(keys)-1] = keys[0]
	st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, keys, values)
	n, err := st.ExecuteUpdate(h.ctx)
	if n != snapshotIngestConvertRows || err == nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	var ae adbc.Error
	if !errors.As(err, &ae) || string(ae.SqlState[:]) != stateUnique {
		t.Fatalf("lost duplicate SQLSTATE: %v", err)
	}
	assertUnmanagedAcknowledged(t, err, n)
	if input.batches.Load() != 1 {
		t.Fatal("input was replayed")
	}
	for i := 0; i < snapshotIngestConvertRows; i++ {
		if got := raw.HGet(h.ctx, keys[i], "n").Val(); got != strconv.Itoa(i) {
			t.Fatalf("row %d: %q", i, got)
		}
	}
	if raw.HExists(h.ctx, keys[0], rowIDField).Val() {
		t.Fatal("unmanaged source received managed row IDs")
	}
}

func TestUnmanagedArrowUnknownReplyDoesNotReplay(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	keys := make([]string, unmanagedInsertPipelineRows+1)
	values := make([]int64, len(keys))
	for i := range keys {
		keys[i], values[i] = fmt.Sprintf("app:%d", i), int64(20+i)
	}
	st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, keys, values)
	hook := &unmanagedLostReply{t: t}
	st.(*statementImpl).conn.store.client.AddHook(hook)
	n, err := st.ExecuteUpdate(h.ctx)
	if err == nil || n != 1 || hook.calls != 1 {
		t.Fatalf("uncertain mutation replayed/count wrong: n=%d calls=%d err=%v", n, hook.calls, err)
	}
	assertUnmanagedAcknowledged(t, err, 1)
	if input.batches.Load() != 1 || raw.HGet(h.ctx, "app:0", "n").Val() != "20" || raw.HGet(h.ctx, "app:1", "n").Val() != "21" || raw.HGet(h.ctx, "app:2", "n").Val() != "22" || raw.Exists(h.ctx, keys[len(keys)-1]).Val() != 0 {
		t.Fatal("submitted pipeline lost applied rows or replayed input")
	}
}

type unmanagedArrowReaderFailure struct {
	array.RecordReader
	done bool
}

func (r *unmanagedArrowReaderFailure) Next() bool {
	ok := r.RecordReader.Next()
	r.done = !ok
	return ok
}
func (r *unmanagedArrowReaderFailure) Err() error {
	if r.done {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func TestUnmanagedArrowReaderFailure(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:one"}, []int64{9})
	input.Retain()
	if err := st.BindStream(h.ctx, &unmanagedArrowReaderFailure{RecordReader: input}); err != nil {
		t.Fatal(err)
	}
	n, err := st.ExecuteUpdate(h.ctx)
	if n != 1 || err == nil || !strings.Contains(err.Error(), "failed to read ingest data") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	assertUnmanagedAcknowledged(t, err, 1)
	if raw.HGet(h.ctx, "app:one", "n").Val() != "9" {
		t.Fatal("acknowledged row lost")
	}
}

type unmanagedArrowCancelHook struct {
	snapshotHook
	cancel context.CancelFunc
	calls  int
}

func (h *unmanagedArrowCancelHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && strings.EqualFold(cmd.Name(), "eval") && len(cmd.Args()) > 3 && cmd.Args()[3] == "app:first" {
			h.calls++
			h.cancel()
		}
		return err
	}
}
func (h *unmanagedArrowCancelHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		err := next(ctx, cmds)
		for _, cmd := range cmds {
			if err == nil && cmd.Name() == "eval" && len(cmd.Args()) > 3 && cmd.Args()[3] == "app:first" {
				h.calls++
				h.cancel()
				break
			}
		}
		return err
	}
}
func TestUnmanagedArrowCancellation(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	keys := make([]string, unmanagedInsertPipelineRows+1)
	values := make([]int64, len(keys))
	for i := range keys {
		keys[i], values[i] = fmt.Sprintf("app:cancel%d", i), int64(i)
	}
	keys[0] = "app:first"
	st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, keys, values)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	hook := &unmanagedArrowCancelHook{cancel: cancel}
	st.(*statementImpl).conn.store.client.AddHook(hook)
	n, err := st.ExecuteUpdate(ctx)
	if n != unmanagedInsertPipelineRows || err == nil || hook.calls != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, hook.calls, err)
	}
	assertUnmanagedAcknowledged(t, err, n)
	if input.batches.Load() != 1 || raw.Exists(h.ctx, keys[len(keys)-1]).Val() != 0 {
		t.Fatal("cancellation submitted another pipeline or replayed input")
	}
	for _, key := range keys[:len(keys)-1] {
		if raw.Exists(h.ctx, key).Val() != 1 {
			t.Fatal("lost acknowledged pipeline row")
		}
	}
}

func bindUnmanagedCustom(t *testing.T, h *sqlHarness, st adbc.StatementWithContext, fields []arrow.Field, cols []arrow.Array) *ingestAdmissionReader {
	t.Helper()
	as := arrow.NewSchema(fields, nil)
	rec := array.NewRecordBatch(as, cols, int64(cols[0].Len()))
	defer rec.Release()
	reader, err := array.NewRecordReader(as, []arrow.RecordBatch{rec})
	if err != nil {
		t.Fatal(err)
	}
	counted := &ingestAdmissionReader{RecordReader: reader}
	t.Cleanup(counted.Release)
	counted.Retain()
	if err := st.BindStream(h.ctx, counted); err != nil {
		t.Fatal(err)
	}
	return counted
}

func TestUnmanagedArrowInputSchemaChecks(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	sb := array.NewStringBuilder(memory.DefaultAllocator)
	defer sb.Release()
	sb.Append("app:one")
	key := sb.NewArray()
	defer key.Release()
	sb.Append("bad integer")
	bad := sb.NewArray()
	defer bad.Release()
	sb.AppendNull()
	nullKey := sb.NewArray()
	defer nullKey.Release()
	nb := array.NewInt64Builder(memory.DefaultAllocator)
	defer nb.Release()
	nb.Append(1)
	number := nb.NewArray()
	defer number.Release()
	nb.AppendNull()
	nullNumber := nb.NewArray()
	defer nullNumber.Release()
	keyField := arrow.Field{Name: "__key", Type: arrow.BinaryTypes.String, Nullable: true}
	nField := arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}
	for _, test := range []struct {
		name     string
		fields   []arrow.Field
		arrays   []arrow.Array
		message  string
		consumes bool
	}{
		{"missing_key", []arrow.Field{nField}, []arrow.Array{number}, "explicit __key", false},
		{"numeric_key", []arrow.Field{{Name: "__key", Type: arrow.PrimitiveTypes.Int64}, nField}, []arrow.Array{number, number}, "must be a string", false},
		{"duplicate_column", []arrow.Field{keyField, nField, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []arrow.Array{key, number, number}, "duplicate Arrow", false},
		{"null_key", []arrow.Field{keyField, nField}, []arrow.Array{nullKey, number}, "__key", true},
		{"conversion", []arrow.Field{keyField, {Name: "n", Type: arrow.BinaryTypes.String}}, []arrow.Array{key, bad}, "n", true},
		{"empty_hash", []arrow.Field{keyField, nField}, []arrow.Array{key, nullNumber}, "without fields", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, nil, nil)
			input := bindUnmanagedCustom(t, h, st, test.fields, test.arrays)
			n, err := st.ExecuteUpdate(h.ctx)
			if n != 0 || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("n=%d err=%v", n, err)
			}
			if (input.calls.Load() > 0) != test.consumes {
				t.Fatalf("unexpected input consumption %d", input.calls.Load())
			}
			assertUnmanagedAcknowledged(t, err, 0)
			if raw.Exists(h.ctx, "app:one").Val() != 0 {
				t.Fatal("invalid input wrote source")
			}
		})
	}
}

func TestUnmanagedArrowFilteredForeignRefusal(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	if err := raw.HSet(h.ctx, "app:seed", "n", "1").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "FILTER", "@n == 1", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	setter := h.conn.(adbc.GetSetOptionsWithContext)
	for _, option := range [][2]string{{OptionStringForeignIndexesMode, "unmanaged"}, {OptionStringForeignIndexes, "true"}} {
		if err := setter.SetOption(h.ctx, option[0], option[1]); err != nil {
			t.Fatal(err)
		}
	}
	h.expectRows("SELECT n FROM redis_foreign.app_idx", "1")
	st, input := unmanagedArrowInput(t, h, "redis_foreign", adbc.OptionValueIngestModeAppend, []string{"app:one"}, []int64{1})
	n, err := st.ExecuteUpdate(h.ctx)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "filtered foreign index") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if input.calls.Load() != 0 || raw.Exists(h.ctx, "app:one").Val() != 0 {
		t.Fatal("filtered refusal consumed input or wrote source")
	}
}

func TestUnmanagedArrowDoesNotClaimManagedAdmission(t *testing.T) {
	h, _, schema := unmanagedIngestFixture(t, false)
	st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:one"}, []int64{1})
	s := st.(*statementImpl).conn.store
	observer := &ingestAdmissionObserver{UniversalClient: s.client}
	s.client = observer
	if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if observer.claims.Load() != 0 {
		t.Fatal("application HASH writes entered the managed Arrow serialization gate")
	}
}

func TestUnmanagedArrowRegisteredEncodings(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	if err := raw.HSet(h.ctx, "app:seed", "n", "10", "enabled", "true", "born", "-1", "seen", "1234567").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	opts := HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Table: "live", Apply: true,
		Types:     map[string]string{"number": "BIGINT", "enabled": "BOOLEAN", "born": "DATE", "seen": "TIMESTAMP"},
		Encodings: map[string]string{"number": "native", "enabled": "text", "born": "native", "seen": "native"}, Renames: map[string]string{"n": "number"}}
	if _, err := AdoptHashes(h.ctx, unmanagedArrowDatabaseOptions(), opts); err != nil {
		t.Fatal(err)
	}
	st, _ := unmanagedArrowInput(t, h, "public", adbc.OptionValueIngestModeAppend, nil, nil)
	fields := []arrow.Field{{Name: "__key", Type: arrow.BinaryTypes.LargeString}, {Name: "number", Type: arrow.PrimitiveTypes.Int64}, {Name: "enabled", Type: arrow.FixedWidthTypes.Boolean}, {Name: "born", Type: arrow.FixedWidthTypes.Date32}, {Name: "seen", Type: &arrow.TimestampType{Unit: arrow.Microsecond}}}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer b.Release()
	b.Field(0).(*array.LargeStringBuilder).Append("app:typed")
	b.Field(1).(*array.Int64Builder).Append(9223372036854775807)
	b.Field(2).(*array.BooleanBuilder).Append(false)
	b.Field(3).(*array.Date32Builder).Append(-2)
	b.Field(4).(*array.TimestampBuilder).Append(123456789)
	rec := b.NewRecordBatch()
	defer rec.Release()
	bindUnmanagedCustom(t, h, st, fields, rec.Columns())
	if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for f, want := range map[string]string{"n": "9223372036854775807", "enabled": "false", "born": "-2", "seen": "123456789"} {
		if got := raw.HGet(h.ctx, "app:typed", f).Val(); got != want {
			t.Fatalf("%s encoding %q want %q", f, got, want)
		}
	}
	if raw.HExists(h.ctx, "app:typed", "number").Val() {
		t.Fatal("renamed logical name leaked into source HASH")
	}
	h.expectRows("SELECT number,enabled FROM live WHERE __key='app:typed'", "9223372036854775807|false")
	h.exec("DROP TABLE live")
	if _, err := SweepReleased(h.ctx, unmanagedArrowDatabaseOptions(), SweepOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	if raw.HGet(h.ctx, "app:typed", "enabled").Val() != "false" {
		t.Fatal("DROP/sweep deleted Arrow-written source")
	}
	if err := raw.Do(h.ctx, "FT.INFO", "app_idx").Err(); err != nil {
		t.Fatal("DROP/sweep deleted source index", err)
	}
}

func TestUnmanagedArrowStagedRefusal(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:one"}, []int64{1})
	s := st.(*statementImpl).conn.store
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
	wctx = context.WithValue(wctx, unmanagedWriteKey{}, metaKey("public", "live"))
	if _, err := st.ExecuteUpdate(wctx); err == nil || !strings.Contains(err.Error(), "staged") {
		t.Fatalf("staged source write accepted: %v", err)
	}
	if input.calls.Load() != 0 || raw.Exists(h.ctx, "app:one").Val() != 0 {
		t.Fatal("staged refusal consumed or wrote")
	}
}

type unmanagedArrowChangedSchema struct {
	array.RecordReader
	batch arrow.RecordBatch
}

func (r *unmanagedArrowChangedSchema) RecordBatch() arrow.RecordBatch { return r.batch }
func (r *unmanagedArrowChangedSchema) Record() arrow.Record           { return r.batch }
func TestUnmanagedArrowChangedStreamSchema(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:one"}, []int64{1})
	b := array.NewStringBuilder(memory.DefaultAllocator)
	b.Append("app:one")
	a := b.NewArray()
	b.Release()
	defer a.Release()
	as := arrow.NewSchema([]arrow.Field{{Name: "different", Type: arrow.BinaryTypes.String}}, nil)
	rec := array.NewRecordBatch(as, []arrow.Array{a}, 1)
	defer rec.Release()
	input.Retain()
	if err := st.BindStream(h.ctx, &unmanagedArrowChangedSchema{RecordReader: input, batch: rec}); err != nil {
		t.Fatal(err)
	}
	n, err := st.ExecuteUpdate(h.ctx)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "schema changed") {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if raw.Exists(h.ctx, "app:one").Val() != 0 {
		t.Fatal("changed schema wrote source")
	}
}

func TestUnmanagedArrowExecuteQueryFailureCount(t *testing.T) {
	h, _, schema := unmanagedIngestFixture(t, false)
	st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, []string{"app:seed"}, []int64{1})
	reader, n, err := st.ExecuteQuery(h.ctx)
	if reader != nil {
		reader.Release()
		t.Fatal("failed append returned a reader")
	}
	if n != 0 || err == nil {
		t.Fatalf("query entry point lost count: n=%d err=%v", n, err)
	}
	assertUnmanagedAcknowledged(t, err, 0)
}

func TestUnmanagedArrowConcurrentWriters(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
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
		statements[i], _ = unmanagedArrowInput(t, worker, schema, adbc.OptionValueIngestModeAppend, []string{fmt.Sprintf("app:concurrent%d", i)}, []int64{int64(i)})
	}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	start := make(chan struct{})
	done := make(chan error, 4)
	for _, st := range statements {
		go func(st adbc.StatementWithContext) {
			<-start
			n, err := st.ExecuteUpdate(h.ctx)
			if err == nil && n != 1 {
				err = fmt.Errorf("written %d rows", n)
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
		if got := raw.HGet(h.ctx, fmt.Sprintf("app:concurrent%d", i), "n").Val(); got != strconv.Itoa(i) {
			t.Fatalf("writer %d value %q", i, got)
		}
	}
	if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("source writers published managed generations")
	}
}

func TestUnmanagedArrowDictionaryKeys(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeAppend, nil, nil)
	typ := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.BinaryTypes.String}
	kb := array.NewDictionaryBuilder(memory.DefaultAllocator, typ)
	defer kb.Release()
	if err := kb.(*array.BinaryDictionaryBuilder).AppendString("app:dictionary"); err != nil {
		t.Fatal(err)
	}
	key := kb.NewDictionaryArray()
	defer key.Release()
	nb := array.NewInt64Builder(memory.DefaultAllocator)
	defer nb.Release()
	nb.Append(42)
	n := nb.NewArray()
	defer n.Release()
	bindUnmanagedCustom(t, h, st, []arrow.Field{{Name: "__key", Type: typ}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []arrow.Array{key, n})
	if n, err := st.ExecuteUpdate(h.ctx); n != 1 || err != nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if raw.HGet(h.ctx, "app:dictionary", "n").Val() != "42" {
		t.Fatal("dictionary key was not decoded exactly")
	}
}
