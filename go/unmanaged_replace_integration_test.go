// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func consentUnmanagedReplace(t *testing.T, h *sqlHarness, st adbc.StatementWithContext) {
	t.Helper()
	cfg := unmanagedIngestSource{Prefix: "app:", Index: "app_idx", Replace: &unmanagedReplaceConsent{Schema: "public", Table: "live", ExclusiveQuiescent: true, DiscardTTLs: true, ResetIndex: true}}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetOption(h.ctx, OptionStringIngestUnmanagedSource, string(b)); err != nil {
		t.Fatal(err)
	}
}

func assertReplaceDetail(t *testing.T, err error, key, want string) {
	t.Helper()
	var ae adbc.Error
	if !errors.As(err, &ae) {
		t.Fatalf("not ADBC: %v", err)
	}
	for _, d := range ae.Details {
		if d.Key() == "adbc.redis.ingest."+key {
			v, e := d.Serialize()
			if e != nil || string(v) != want {
				t.Fatalf("%s=%q want %q (%v)", key, v, want, e)
			}
			return
		}
	}
	t.Fatalf("missing %s: %v", key, err)
}

func TestUnmanagedArrowReplaceSchemaDataTTLAndOwnership(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	if err := raw.HSet(h.ctx, "app:keep", "n", "30", "unmodeled", "remove").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Expire(h.ctx, "app:keep", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Set(h.ctx, "outside:keep", "untouched", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, nil, nil)
	consentUnmanagedReplace(t, h, st)
	// Change schema as well as data, retaining one original key and adding NUL.
	key := "app:" + string([]byte{'a', 0, 'b'})
	if !strings.ContainsRune(key, 0) {
		t.Fatal("missing NUL fixture")
	}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "__key", Type: arrow.BinaryTypes.String}, {Name: "label", Type: arrow.BinaryTypes.String}}, nil))
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"app:keep", key}, nil)
	b.Field(1).(*array.StringBuilder).AppendValues([]string{"new", "binary"}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()
	bindUnmanagedCustom(t, h, st, rec.Schema().Fields(), rec.Columns())
	n, err := st.ExecuteUpdate(h.ctx)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if input.calls.Load() != 0 {
		t.Fatal("rebound reader was consumed")
	}
	if raw.Exists(h.ctx, "app:seed").Val() != 0 || raw.HLen(h.ctx, "app:keep").Val() != 1 || raw.HGet(h.ctx, "app:keep", "label").Val() != "new" || raw.PTTL(h.ctx, "app:keep").Val() != -1 {
		t.Fatal("replacement retained old HASH fields, keys or TTL")
	}
	if raw.Get(h.ctx, "outside:keep").Val() != "untouched" || raw.PTTL(h.ctx, "outside:keep").Val() <= 0 || raw.HGet(h.ctx, key, "label").Val() != "binary" {
		t.Fatal("scope or binary key corruption")
	}
	h.expectRows("SELECT label FROM live ORDER BY label", "binary", "new")
	info, err := raw.Do(h.ctx, "FT.INFO", "app_idx").Result()
	if err != nil {
		t.Fatal(err)
	}
	attrs := indexInfoOf(info).attrs
	if len(attrs) != 1 || attrs[0].identifier != "label" || !slicesContainStringFold(attrs[0].options, "NOINDEX") {
		t.Fatalf("index not reset: %+v", attrs)
	}
	h.exec("DROP TABLE live")
	if _, err := SweepReleased(h.ctx, unmanagedArrowDatabaseOptions(), SweepOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	if raw.Exists(h.ctx, "app:keep", key).Val() != 2 || raw.Do(h.ctx, "FT.INFO", "app_idx").Err() != nil {
		t.Fatal("DROP/GC reclaimed application resources")
	}
}

func slicesContainStringFold(v []string, target string) bool {
	for _, s := range v {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}

func TestUnmanagedArrowReplacePreflight(t *testing.T) {
	for _, kind := range []string{"no_consent", "wrong_target", "no_quiescence", "keep_ttl", "keep_index", "wrong_prefix", "wrong_index", "nested", "non_hash", "overlap", "filtered", "multi_prefix", "unmodeled_index", "other_registration", "missing_key", "foreign", "managed", "missing"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, schema := unmanagedIngestFixture(t, kind == "foreign")
			st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, []string{"app:new"}, []int64{1})
			consentUnmanagedReplace(t, h, st)
			cfg := st.(*statementImpl).unmanagedSource
			switch kind {
			case "no_consent":
				cfg.Replace = nil
			case "wrong_target":
				cfg.Replace.Table = "other"
			case "no_quiescence":
				cfg.Replace.ExclusiveQuiescent = false
			case "keep_ttl":
				cfg.Replace.DiscardTTLs = false
			case "keep_index":
				cfg.Replace.ResetIndex = false
			case "wrong_prefix":
				cfg.Prefix = "else:"
			case "wrong_index":
				cfg.Index = "else"
			case "nested":
				if err := raw.HSet(h.ctx, "app:child:one", "n", "2").Err(); err != nil {
					t.Fatal(err)
				}
			case "non_hash":
				if err := raw.Set(h.ctx, "app:string", "keep", 0).Err(); err != nil {
					t.Fatal(err)
				}
			case "overlap":
				if err := raw.Do(h.ctx, "FT.CREATE", "overlap", "ON", "HASH", "PREFIX", 1, "app:child:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
					t.Fatal(err)
				}
			case "filtered", "multi_prefix", "unmodeled_index":
				if err := raw.Do(h.ctx, "FT.DROPINDEX", "app_idx").Err(); err != nil {
					t.Fatal(err)
				}
				args := []any{"FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:"}
				if kind == "multi_prefix" {
					args = []any{"FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 2, "app:", "other:"}
				}
				if kind == "filtered" {
					args = append(args, "FILTER", "@n > 0")
				}
				args = append(args, "SCHEMA", "n", "NUMERIC")
				if kind == "unmodeled_index" {
					args = append(args, "absent", "TEXT")
				}
				if err := raw.Do(h.ctx, args...).Err(); err != nil {
					t.Fatal(err)
				}
				if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
					t.Fatal(err)
				}
			case "other_registration":
				if _, err := AdoptHashes(h.ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Table: "other", Types: map[string]string{"n": "BIGINT"}, Apply: true}); err != nil {
					t.Fatal(err)
				}
			case "missing_key":
				nb := array.NewInt64Builder(memory.DefaultAllocator)
				nb.Append(1)
				a := nb.NewArray()
				nb.Release()
				defer a.Release()
				input = bindUnmanagedCustom(t, h, st, []arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, []arrow.Array{a})
			case "foreign":
				cfg.Replace.Schema = "redis_foreign"
				cfg.Replace.Table = "app_idx"
			case "managed":
				h.exec("DROP TABLE live")
				h.exec("CREATE TABLE live (n BIGINT)")
			case "missing":
				h.exec("DROP TABLE live")
			}
			n, err := st.ExecuteUpdate(h.ctx)
			if err == nil {
				t.Fatalf("accepted unsafe configuration: n=%d", n)
			}
			if input.calls.Load() != 0 || raw.HGet(h.ctx, "app:seed", "n").Val() != "10" || raw.Exists(h.ctx, "app:new").Val() != 0 {
				t.Fatal("preflight consumed input or modified source")
			}
		})
	}
}

// Inject after the native command applies, making reply loss distinguishable
// from a refused mutation. Hooks never rewrite the published source index.
type unmanagedReplaceHook struct {
	snapshotHook
	before, after func(context.Context, goredis.Cmder) error
}

func (h *unmanagedReplaceHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if h.before != nil {
			if err := h.before(ctx, cmd); err != nil {
				cmd.SetErr(err)
				return err
			}
		}
		err := next(ctx, cmd)
		if err == nil && h.after != nil {
			if e := h.after(ctx, cmd); e != nil {
				cmd.SetErr(e)
				return e
			}
		}
		return err
	}
}
func replaceScript(cmd goredis.Cmder, script string) bool {
	return cmd.Name() == "eval" && len(cmd.Args()) > 1 && cmd.Args()[1] == script
}

func TestUnmanagedArrowReplaceDeleteFailure(t *testing.T) {
	for _, kind := range []string{"lost_reply", "changed_hash", "changed_ttl", "changed_type", "expired", "cancel", "new_key", "definition", "registration"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, schema := unmanagedIngestFixture(t, false)
			st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, []string{"app:new"}, []int64{1})
			consentUnmanagedReplace(t, h, st)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			calls := 0
			hook := &unmanagedReplaceHook{}
			hook.before = func(c context.Context, cmd goredis.Cmder) error {
				if !replaceScript(cmd, unmanagedReplaceDeleteLua) {
					return nil
				}
				calls++
				if calls > 1 {
					t.Error("delete replayed")
				}
				switch kind {
				case "changed_hash":
					return raw.HSet(c, "app:seed", "n", "99").Err()
				case "changed_ttl":
					return raw.Expire(c, "app:seed", time.Hour).Err()
				case "changed_type":
					return raw.Set(c, "app:seed", "other", 0).Err()
				case "expired":
					return raw.Del(c, "app:seed").Err()
				case "new_key":
					return raw.HSet(c, "app:interloper", "n", "88").Err()
				}
				return nil
			}
			hook.after = func(c context.Context, cmd goredis.Cmder) error {
				if replaceScript(cmd, unmanagedReplaceInspectLua) {
					switch kind {
					case "definition":
						return raw.Do(c, "FT.ALTER", "app_idx", "SCHEMA", "ADD", "extra_index", "TAG").Err()
					case "registration":
						h.exec("DROP TABLE live")
					}
				}
				if replaceScript(cmd, unmanagedReplaceDeleteLua) {
					if kind == "lost_reply" {
						return io.ErrUnexpectedEOF
					}
					if kind == "cancel" {
						cancel()
					}
				}
				return nil
			}
			st.(*statementImpl).conn.store.client.AddHook(hook)
			n, err := st.ExecuteUpdate(ctx)
			if err == nil || n != 0 || input.calls.Load() != 0 {
				t.Fatalf("n=%d calls=%d err=%v", n, input.calls.Load(), err)
			}
			deletes := "0"
			if kind == "cancel" || kind == "new_key" {
				deletes = "1"
			}
			assertReplaceDetail(t, err, "acknowledged_deletes", deletes)
			if kind == "lost_reply" {
				assertReplaceDetail(t, err, "current_outcome", "unknown")
				if raw.Exists(h.ctx, "app:seed").Val() != 0 {
					t.Fatal("lost reply did not hide applied delete")
				}
			}
			if kind == "changed_hash" && raw.HGet(h.ctx, "app:seed", "n").Val() != "99" {
				t.Fatal("overwrote concurrent image")
			}
			if kind == "changed_type" && raw.Get(h.ctx, "app:seed").Val() != "other" {
				t.Fatal("deleted changed type")
			}
			if kind == "new_key" && raw.HGet(h.ctx, "app:interloper", "n").Val() != "88" {
				t.Fatal("broadened deletion inventory")
			}
			if raw.Exists(h.ctx, "app:new").Val() != 0 {
				t.Fatal("failure consumed input")
			}
		})
	}
}

func TestUnmanagedArrowReplaceInputFailures(t *testing.T) {
	for _, kind := range []string{"duplicate", "outside", "nested", "null_key", "reader", "conversion", "lost_write_reply"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, schema := unmanagedIngestFixture(t, false)
			keys := make([]string, unmanagedInsertPipelineRows+1)
			if kind == "duplicate" {
				keys = make([]string, snapshotIngestConvertRows+1)
			}
			values := make([]int64, len(keys))
			for i := range keys {
				keys[i] = fmt.Sprintf("app:%d", i)
				values[i] = int64(i)
			}
			if kind == "duplicate" {
				keys[len(keys)-1] = keys[0]
			}
			if kind == "outside" {
				keys[0] = "outside:key"
			}
			if kind == "nested" {
				keys[0] = "app:child:one"
			}
			st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, keys, values)
			consentUnmanagedReplace(t, h, st)
			if kind == "null_key" {
				fields := []arrow.Field{{Name: "__key", Type: arrow.BinaryTypes.String, Nullable: true}, {Name: "n", Type: arrow.PrimitiveTypes.Int64}}
				b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
				defer b.Release()
				b.Field(0).(*array.StringBuilder).AppendNull()
				b.Field(1).(*array.Int64Builder).Append(1)
				r := b.NewRecordBatch()
				defer r.Release()
				bindUnmanagedCustom(t, h, st, fields, r.Columns())
			}
			if kind == "reader" {
				if err := st.BindStream(h.ctx, &unmanagedArrowReaderFailure{RecordReader: input}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "conversion" {
				// Invalid UTF-8 is rejected by Arrow string conversion, after deletion.
				b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "__key", Type: arrow.BinaryTypes.String}, {Name: "label", Type: arrow.BinaryTypes.String}}, nil))
				defer b.Release()
				b.Field(0).(*array.StringBuilder).Append("app:new")
				b.Field(1).(*array.StringBuilder).Append(string([]byte{0xff}))
				r := b.NewRecordBatch()
				defer r.Release()
				bindUnmanagedCustom(t, h, st, r.Schema().Fields(), r.Columns())
			}
			var lost *unmanagedLostReply
			if kind == "lost_write_reply" {
				lost = &unmanagedLostReply{t: t}
				st.(*statementImpl).conn.store.client.AddHook(lost)
			}
			n, err := st.ExecuteUpdate(h.ctx)
			want := int64(0)
			switch kind {
			case "duplicate":
				want = snapshotIngestConvertRows
			case "reader":
				want = int64(len(keys))
			case "lost_write_reply":
				want = 1
			}
			if err == nil || n != want {
				t.Fatalf("n=%d want=%d err=%v", n, want, err)
			}
			assertReplaceDetail(t, err, "acknowledged_deletes", "1")
			assertUnmanagedAcknowledged(t, err, want)
			if raw.Exists(h.ctx, "app:seed").Val() != 0 {
				t.Fatal("old data was restored or delete not applied")
			}
			if kind == "lost_write_reply" && (lost.calls != 1 || raw.Exists(h.ctx, keys[len(keys)-1]).Val() != 0 || raw.HGet(h.ctx, keys[1], "n").Val() != "1") {
				t.Fatal("lost write replay or lost applied row")
			}
			if kind == "duplicate" && raw.HGet(h.ctx, keys[0], "n").Val() != "0" {
				t.Fatal("duplicate overwrote first input")
			}
			if kind == "outside" && raw.Exists(h.ctx, "outside:key").Val() != 0 {
				t.Fatal("escaped prefix")
			}
		})
	}
}

func TestUnmanagedArrowReplaceIndexFailure(t *testing.T) {
	for _, phase := range []string{"drop_index", "create_index", "registration", "changed_index"} {
		t.Run(phase, func(t *testing.T) {
			h, raw, schema := unmanagedIngestFixture(t, false)
			st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, []string{"app:new"}, []int64{1})
			consentUnmanagedReplace(t, h, st)
			calls := 0
			hook := &unmanagedReplaceHook{after: func(ctx context.Context, cmd goredis.Cmder) error {
				if len(cmd.Args()) < 2 || cmd.Args()[1] != "app_idx" {
					return nil
				}
				if phase == "drop_index" && cmd.Name() == "ft.dropindex" {
					calls++
					return io.ErrUnexpectedEOF
				}
				if cmd.Name() == "ft.create" {
					if phase == "changed_index" {
						calls++
						return raw.Do(ctx, "FT.ALTER", "app_idx", "SCHEMA", "ADD", "interloper", "TAG").Err()
					}
					if phase == "create_index" {
						calls++
						return io.ErrUnexpectedEOF
					}
					if phase == "registration" {
						calls++
						h.exec("DROP TABLE live")
					}
				}
				return nil
			}}
			st.(*statementImpl).conn.store.client.AddHook(hook)
			// Observe DDL on this client's hooks without the public execution
			// pool's separate Search connection. Source execution is unchanged.
			n, err := st.(*statementImpl).executeUnmanagedReplace(h.ctx)
			if n != 0 || err == nil || calls != 1 || input.calls.Load() != 0 {
				t.Fatalf("n=%d calls=%d input=%d err=%v", n, calls, input.calls.Load(), err)
			}
			wantPhase := phase
			if phase == "changed_index" {
				wantPhase = "create_index"
			}
			assertReplaceDetail(t, err, "phase", wantPhase)
			assertReplaceDetail(t, err, "acknowledged_deletes", "1")
			if phase == "drop_index" || phase == "create_index" {
				assertReplaceDetail(t, err, "current_outcome", "unknown")
			}
			if raw.Exists(h.ctx, "app:seed", "app:new").Val() != 0 {
				t.Fatal("unexpected input write or rollback")
			}
			indexErr := raw.Do(h.ctx, "FT.INFO", "app_idx").Err()
			if phase == "drop_index" && !isUnknownIndex(indexErr) || phase != "drop_index" && indexErr != nil {
				t.Fatalf("wrong surviving index state: %v", indexErr)
			}
		})
	}
}

// scripts/unmanaged_replace_acl.py creates actual limited Software principals;
// the ordinary suite reports this prerequisite explicitly instead of simulating ACLs.
func TestUnmanagedArrowReplacePermissionPreflight(t *testing.T) {
	for _, denied := range []string{"del", "hset", "ft.dropindex", "ft.create"} {
		t.Run(denied, func(t *testing.T) {
			uri := os.Getenv("REDIS_UNMANAGED_REPLACE_" + strings.ToUpper(strings.ReplaceAll(denied, ".", "_")) + "_ACL_URI")
			if uri == "" {
				t.Skip("requires owned Software API principal with " + denied + " denied")
			}
			h, raw, schema := unmanagedIngestFixture(t, false)
			st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, []string{"app:new"}, []int64{1})
			consentUnmanagedReplace(t, h, st)
			credentials, err := goredis.ParseURL(uri)
			if err != nil {
				t.Fatal("invalid private principal configuration")
			}
			opts := *raw.Options()
			opts.Username, opts.Password = credentials.Username, credentials.Password
			limited := goredis.NewClient(&opts)
			defer limited.Close()
			if err := limited.Ping(h.ctx).Err(); err != nil {
				t.Fatal("authentication positive control failed")
			}
			if limited.HGet(h.ctx, "app:seed", "n").Val() != "10" || limited.Do(h.ctx, "FT.INFO", "app_idx").Err() != nil {
				t.Fatal("read positive control failed")
			}
			s := st.(*statementImpl).conn.store
			original := s.client
			s.client = limited
			defer func() { s.client = original }()
			n, err := st.(*statementImpl).executeUnmanagedReplace(h.ctx)
			if n != 0 || err == nil || !strings.Contains(err.Error(), "NOPERM") {
				t.Fatalf("n=%d err=%v", n, err)
			}
			assertReplaceDetail(t, err, "acknowledged_deletes", "0")
			if input.calls.Load() != 0 || raw.HGet(h.ctx, "app:seed", "n").Val() != "10" || raw.Exists(h.ctx, "app:new").Val() != 0 || raw.Do(h.ctx, "FT.INFO", "app_idx").Err() != nil {
				t.Fatal("denied preflight changed source or consumed input")
			}
		})
	}
}

func TestUnmanagedArrowReplaceEmptyInput(t *testing.T) {
	h, raw, schema := unmanagedIngestFixture(t, false)
	for range 2 { // Both a populated source and a subsequently empty source.
		st, _ := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, nil, nil)
		consentUnmanagedReplace(t, h, st)
		if n, err := st.ExecuteUpdate(h.ctx); n != 0 || err != nil {
			t.Fatalf("n=%d err=%v", n, err)
		}
		if raw.Exists(h.ctx, "app:seed").Val() != 0 {
			t.Fatal("empty replacement retained old row")
		}
		h.expectRows("SELECT COUNT(*) FROM live", "0")
	}
}

func TestUnmanagedReplaceInventoryReplyBudget(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	err := raw.Do(h.ctx, "EVAL", unmanagedReplaceInspectLua, 1, "app:seed", "n", 0).Err()
	if err == nil || !strings.Contains(err.Error(), "inventory exceeds 64 MiB") {
		t.Fatalf("ignored remaining reply budget: %v", err)
	}
	if raw.HGet(h.ctx, "app:seed", "n").Val() != "10" {
		t.Fatal("inventory refusal modified source")
	}
}

func TestUnmanagedArrowReplaceCancelAndClose(t *testing.T) {
	for _, mode := range []string{"statement_cancel", "connection_close"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, schema := unmanagedIngestFixture(t, false)
			st, input := unmanagedArrowInput(t, h, schema, adbc.OptionValueIngestModeReplace, []string{"app:new"}, []int64{1})
			consentUnmanagedReplace(t, h, st)
			entered := make(chan struct{})
			hook := &unmanagedReplaceHook{after: func(ctx context.Context, cmd goredis.Cmder) error {
				if replaceScript(cmd, unmanagedReplaceDeleteLua) {
					close(entered)
					select {
					case <-ctx.Done():
					case <-time.After(5 * time.Second):
						return errors.New("test cancellation barrier timed out")
					}
				}
				return nil
			}}
			st.(*statementImpl).conn.store.client.AddHook(hook)
			type result struct {
				n   int64
				err error
			}
			done := make(chan result, 1)
			go func() { n, err := st.ExecuteUpdate(h.ctx); done <- result{n, err} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("replacement never reached delete")
			}
			closed := make(chan error, 1)
			if mode == "statement_cancel" {
				if err := st.(*statementImpl).Cancel(h.ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				go func() { closed <- h.conn.Close(h.ctx) }()
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Close did not join replacement")
				}
			}
			select {
			case r := <-done:
				if r.n != 0 || r.err == nil {
					t.Fatalf("n=%d err=%v", r.n, r.err)
				}
				want := adbc.StatusCancelled
				if mode == "connection_close" {
					want = adbc.StatusInvalidState
				}
				if !isStatus(r.err, want) {
					t.Fatalf("lost cancellation/close status: %v", r.err)
				}
				assertReplaceDetail(t, r.err, "acknowledged_deletes", "1")
			case <-time.After(5 * time.Second):
				t.Fatal("replacement did not stop")
			}
			if input.calls.Load() != 0 || raw.Exists(h.ctx, "app:seed", "app:new").Val() != 0 || raw.Do(h.ctx, "FT.INFO", "app_idx").Err() != nil {
				t.Fatal("interruption consumed input or changed later phases")
			}
		})
	}
}
