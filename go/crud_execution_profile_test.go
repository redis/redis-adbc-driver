// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

type crudProfileKey struct{}
type crudProfileCall struct {
	Role            string   `json:"client_role"`
	Categories      []string `json:"categories"`
	StartNS         int64    `json:"start_ns"`
	ElapsedNS       int64    `json:"elapsed_ns"`
	Commands        int      `json:"commands"`
	ReplyValueBytes int      `json:"reply_value_bytes"`
	Error           bool     `json:"error"`
}
type crudProfileSample struct {
	Mode      string            `json:"mode"`
	Operation string            `json:"operation"`
	Sample    int               `json:"sample"`
	Warmup    bool              `json:"warmup"`
	StartNS   int64             `json:"start_ns"`
	ElapsedNS int64             `json:"elapsed_ns"`
	StageNS   map[string]int64  `json:"stage_ns"`
	Validated bool              `json:"validated"`
	Calls     []crudProfileCall `json:"calls"`
}
type crudProfileObserver struct {
	mu    sync.Mutex
	epoch time.Time
	calls []crudProfileCall
}
type crudProfileHook struct {
	observer *crudProfileObserver
	role     string
}

func (*crudProfileHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func crudProfileCategory(cmd goredis.Cmder) string {
	category := compactionProfileCategory(cmd)
	args := snapshotObservedControlArgs(cmd)
	if len(args) > 1 {
		key, _ := args[1].(string)
		switch {
		case strings.HasPrefix(key, snapshotPrefix+"metadata:root:"):
			return "metadata_root_" + category
		case strings.HasPrefix(key, snapshotPrefix+"metadata:bucket:"):
			return "metadata_bucket_" + category
		case strings.HasPrefix(key, snapshotPrefix+"metadata:fields:"):
			return "metadata_directory_" + category
		case strings.HasPrefix(key, snapshotPrefix+"workspace:") && (category == "get" || category == "strlen"):
			return "metadata_object_" + category
		}
	}
	// Categorize function-based candidate resolvers by their
	// declared root-map inputs, without recording script digests or arguments.
	if (category == "fcall" || category == "fcall_ro") && len(args) > 3 {
		n, _ := strconv.Atoi(fmt.Sprint(args[2]))
		for _, arg := range args[3:min(len(args), 3+n)] {
			if key, ok := arg.(string); ok && strings.HasPrefix(key, snapshotPrefix+"metadata:root:") {
				return "metadata_resolver"
			}
		}
	}
	return category
}

// Counts payload values only: not RESP framing, keys, Lua internal bytes, or
// server allocation. No response content is retained.
func crudProfileValueBytes(cmd goredis.Cmder) int {
	// This observer runs after the command. Strip only our known no-retry
	// wrappers; do not execute a command, inspect arguments or estimate RESP.
	for depth := 0; depth < 8; depth++ {
		if cmd == nil {
			return 0
		}
		switch c := cmd.(type) {
		case unmanagedProjectionCmd:
			cmd = c.Cmder
		case *unmanagedProjectionCmd:
			if c == nil {
				return 0
			}
			cmd = c.Cmder
		case onceCmd:
			if c.Cmd == nil {
				return 0
			}
			cmd = c.Cmd
		case *onceCmd:
			if c == nil || c.Cmd == nil {
				return 0
			}
			cmd = c.Cmd
		default:
			if cmd.Err() != nil {
				return 0
			}
			return crudProfileUnwrappedValueBytes(cmd)
		}
	}
	return 0 // Defensive bound for an unexpected cyclic/nested observer wrapper.
}
func crudProfileUnwrappedValueBytes(cmd goredis.Cmder) int {
	switch c := cmd.(type) {
	case *goredis.StringCmd:
		return len(c.Val())
	case *goredis.SliceCmd:
		n := 0
		for _, v := range c.Val() {
			if s, ok := v.(string); ok {
				n += len(s)
			} else if b, ok := v.([]byte); ok {
				n += len(b)
			}
		}
		return n
	case *goredis.MapStringStringCmd:
		n := 0
		for _, v := range c.Val() {
			n += len(v)
		}
		return n
	case *goredis.Cmd:
		// onceCmd carries the generic result type. Count only known flat
		// returned values; nested script responses remain deliberately partial.
		switch value := c.Val().(type) {
		case string:
			return len(value)
		case []byte:
			return len(value)
		case []any:
			n := 0
			for _, value := range value {
				switch value := value.(type) {
				case string:
					n += len(value)
				case []byte:
					n += len(value)
				}
			}
			return n
		}
	}
	return 0
}
func (h *crudProfileHook) observe(ctx context.Context, started time.Time, cmds []goredis.Cmder, err error) {
	row := crudProfileCall{Role: h.role, StartNS: started.Sub(h.observer.epoch).Nanoseconds(), ElapsedNS: time.Since(started).Nanoseconds(), Commands: len(cmds), Error: err != nil}
	for _, cmd := range cmds {
		row.Categories = append(row.Categories, crudProfileCategory(cmd))
		row.ReplyValueBytes += crudProfileValueBytes(cmd)
	}
	h.observer.mu.Lock()
	defer h.observer.mu.Unlock()
	if s, ok := ctx.Value(crudProfileKey{}).(*crudProfileSample); ok && h.role != "worker" {
		s.Calls = append(s.Calls, row)
	} else {
		h.observer.calls = append(h.observer.calls, row)
	}
}
func (h *crudProfileHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		started := time.Now()
		err := next(ctx, cmd)
		h.observe(ctx, started, []goredis.Cmder{cmd}, err)
		return err
	}
}
func (h *crudProfileHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		started := time.Now()
		err := next(ctx, cmds)
		h.observe(ctx, started, cmds, err)
		return err
	}
}
func crudProfileWant(id int) string { return fmt.Sprintf("%d|%d|%d|row-%08d", id, id%16, id*3, id) }
func crudProfileCheckRecord(t *testing.T, rec arrow.RecordBatch, want string) {
	t.Helper()
	if rec.NumRows() != 1 || rec.NumCols() != 4 || rec.Column(0).DataType().ID() != arrow.INT64 || rec.Column(1).DataType().ID() != arrow.INT64 || rec.Column(2).DataType().ID() != arrow.INT64 || rec.Column(3).DataType().ID() != arrow.STRING {
		t.Fatal("incorrect typed profile row")
	}
	values := make([]string, 4)
	for i, c := range rec.Columns() {
		if c.IsNull(0) {
			t.Fatal("unexpected null profile value")
		}
		values[i] = c.ValueStr(0)
	}
	if strings.Join(values, "|") != want {
		t.Fatal("incorrect profile row value")
	}
}

// Opt-in attribution on an exclusively owned verified Software 4x QPF fixture.
// Each case has one retained warmup plus 32 distinct operations. SQL is varied
// literal SQL, not a prepared-statement experiment. Native Python timings remain
// separate: this Go hook observer is NOT a packet RTT or server CPU profiler.
func TestCRUDExecutionProfile(t *testing.T) {
	if os.Getenv("REDIS_CRUD_EXECUTION_PROFILE") == "" {
		t.Skip("set REDIS_CRUD_EXECUTION_PROFILE on an owned verified Software fixture")
	}
	for _, mode := range []string{"managed", "unmanaged", "raw"} {
		for _, operation := range []string{"point_read", "insert", "update", "delete"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				h, raw, _ := newSnapshotHarnessMode(t, true, false)
				name := "crud_profile_target"
				prefix := "crud-profile-source:"
				index := "crud_profile_source_index"
				if mode == "managed" {
					h.exec("CREATE TABLE " + name + "(id BIGINT,grp BIGINT,amount BIGINT,label VARCHAR,bench_ordinal BIGINT)")
					h.exec("INSERT INTO " + name + " SELECT i,i%16,i*3,'row-'||lpad(CAST(i AS VARCHAR),8,'0'),i FROM generate_series(1,1000) AS g(i)")
				} else {
					_, err := raw.Pipelined(h.ctx, func(p goredis.Pipeliner) error {
						for i := 1; i <= 1000; i++ {
							p.HSet(h.ctx, prefix+strconv.Itoa(i), "id", i, "grp", i%16, "amount", i*3, "label", fmt.Sprintf("row-%08d", i), "bench_ordinal", i)
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := raw.Do(h.ctx, "FT.CREATE", index, "ON", "HASH", "PREFIX", 1, prefix, "SCHEMA", "id", "NUMERIC", "grp", "NUMERIC", "amount", "NUMERIC", "label", "TAG", "bench_ordinal", "NUMERIC").Err(); err != nil {
						t.Fatal(err)
					}
					if err := (&store{client: raw}).awaitIndexReady(h.ctx, index); err != nil {
						t.Fatal(err)
					}
					if mode == "unmanaged" {
						types := map[string]string{"id": "BIGINT", "grp": "BIGINT", "amount": "BIGINT", "label": "TEXT", "bench_ordinal": "BIGINT"}
						enc := map[string]string{}
						for k := range types {
							enc[k] = "native"
						}
						if _, err := AdoptHashes(h.ctx, map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}, HashAdoptOptions{Mode: "unmanaged", Prefix: prefix, Index: index, Table: name, Types: types, Encodings: enc, Apply: true}); err != nil {
							t.Fatal(err)
						}
					}
				}
				stmt, err := h.conn.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer stmt.Close(h.ctx)
				impl := stmt.(*statementImpl)
				c := impl.conn
				observer := &crudProfileObserver{epoch: time.Now()}
				// Install each hook once per distinct client, before it can serve a sample.
				c.store.client.AddHook(&crudProfileHook{observer: observer, role: "shared_store"})
				c.store.stopSnapshotCompactor()
				c.store.startSnapshotCompactor(c.clientCfg, c.timeouts, &crudProfileHook{observer: observer, role: "worker"})
				raw.AddHook(&crudProfileHook{observer: observer, role: "raw"})
				hooked := map[goredis.UniversalClient]bool{}
				var samples []*crudProfileSample
				finalStateValidated := false
				t.Cleanup(func() {
					_ = stmt.Close(h.ctx)
					_ = h.conn.Close(h.ctx)
					observer.mu.Lock()
					payload, err := json.Marshal(map[string]any{"mode": mode, "operation": operation, "samples": samples, "outside_operation_calls": observer.calls, "rows": 1000, "public_columns": 4, "control_columns": 1, "samples_per_case": 33, "measured_samples_per_case": 32, "scope": "Instrumented Go SQL/Arrow/native client attribution with explicit execution scope acquire/finish, one caller, changing literal SQL; not Python/ADBC latency, packet RTT or server CPU. Hook calls may include pool/transport/decode wait and background-context cleanup overlaps. Reply value bytes exclude RESP framing, keys and Lua-internal bytes. Outside-operation calls include setup, validation, worker and cleanup; never add them to per-operation latency.", "caller_replays": 0, "complete_fixture_validated": finalStateValidated, "test_failed": t.Failed()})
					observer.mu.Unlock()
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(payload), prefix) || strings.Contains(string(payload), "rediss://") || strings.Contains(string(payload), "snapshot:17:") {
						t.Fatal("profile leaked private runtime identifiers")
					}
					t.Logf("crud_execution_profile %s", payload)
				})

				schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "grp", Type: arrow.PrimitiveTypes.Int64}, {Name: "amount", Type: arrow.PrimitiveTypes.Int64}, {Name: "label", Type: arrow.BinaryTypes.String}}, nil)
				for sample := -1; sample < 32; sample++ {
					target := sample
					if target < 0 {
						target = 32
					}
					id := (target*61)%1000 + 1
					if operation == "insert" {
						id = 1001 + target
					}
					row := &crudProfileSample{Mode: mode, Operation: operation, Sample: sample, Warmup: sample < 0, StageNS: map[string]int64{}}
					samples = append(samples, row)
					ctx := context.WithValue(h.ctx, crudProfileKey{}, row)
					began := time.Now()
					row.StartNS = began.Sub(observer.epoch).Nanoseconds()
					stage := func(name string, fn func()) {
						started := time.Now()
						fn()
						row.StageNS[name] = time.Since(started).Nanoseconds()
					}
					if mode == "raw" {
						stage("raw_execute_and_convert", func() {
							key := prefix + strconv.Itoa(id)
							switch operation {
							case "point_read":
								values, err := raw.HMGet(ctx, key, "id", "grp", "amount", "label").Result()
								if err != nil {
									t.Fatal(err)
								}
								builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
								for n := 0; n < 3; n++ {
									v, err := strconv.ParseInt(fmt.Sprint(values[n]), 10, 64)
									if err != nil {
										t.Fatal(err)
									}
									builder.Field(n).(*array.Int64Builder).Append(v)
								}
								builder.Field(3).(*array.StringBuilder).Append(fmt.Sprint(values[3]))
								rec := builder.NewRecordBatch()
								builder.Release()
								crudProfileCheckRecord(t, rec, crudProfileWant(id))
								rec.Release()
							case "insert":
								if n, err := raw.HSet(ctx, key, "id", id, "grp", id%16, "amount", id*3, "label", fmt.Sprintf("row-%08d", id), "bench_ordinal", id).Result(); err != nil || n != 5 {
									t.Fatal("raw insert failed", n, err)
								}
							case "update":
								if n, err := raw.HSet(ctx, key, "amount", -id).Result(); err != nil || n != 0 {
									t.Fatal("raw update failed", n, err)
								}
							case "delete":
								if n, err := raw.Del(ctx, key).Result(); err != nil || n != 1 {
									t.Fatal("raw delete failed", n, err)
								}
							}
						})
					} else {
						var transport *executionTransport
						stage("transport_acquire", func() {
							ctx, transport = c.executionContext(ctx, &impl.streams, &c.streams)
							if transport == nil {
								t.Fatal("profile requires an ordinary fresh execution scope")
							}
							if !hooked[transport.client] {
								transport.client.AddHook(&crudProfileHook{observer: observer, role: "execution"})
								hooked[transport.client] = true
							}
						})
						defer transport.finish()
						predicate := fmt.Sprintf("__rowid=%d", id)
						if mode == "unmanaged" {
							predicate = fmt.Sprintf("__key='%s%d'", prefix, id)
						}
						query := ""
						switch operation {
						case "point_read":
							query = "SELECT id,grp,amount,label FROM " + name + " WHERE " + predicate
						case "insert":
							fields := "id,grp,amount,label,bench_ordinal"
							values := fmt.Sprintf("%d,%d,%d,'row-%08d',%d", id, id%16, id*3, id, id)
							if mode == "unmanaged" {
								fields = "__key," + fields
								values = fmt.Sprintf("'%s%d',", prefix, id) + values
							}
							query = "INSERT INTO " + name + "(" + fields + ") VALUES(" + values + ")"
						case "update":
							query = fmt.Sprintf("UPDATE %s SET amount=%d WHERE %s", name, -id, predicate)
						case "delete":
							query = "DELETE FROM " + name + " WHERE " + predicate
						}
						stage("set_query", func() {
							if err := stmt.SetSqlQuery(ctx, query); err != nil {
								t.Fatal(err)
							}
						})
						if operation == "point_read" {
							var reader array.RecordReader
							stage("execute", func() {
								reader, _, err = stmt.ExecuteQuery(ctx)
								if err != nil {
									t.Fatal(err)
								}
							})
							stage("fetch_and_release", func() {
								defer reader.Release()
								rows := int64(0)
								for reader.Next() {
									rec := reader.RecordBatch()
									crudProfileCheckRecord(t, rec, crudProfileWant(id))
									rows += rec.NumRows()
								}
								readErr := reader.Err()
								if readErr != nil || rows != 1 {
									t.Fatal("incomplete profile fetch", rows, readErr)
								}
							})
						} else {
							stage("execute", func() {
								n, err := stmt.ExecuteUpdate(ctx)
								if err != nil || n != 1 {
									t.Fatal("profile mutation failed", n, err)
								}
							})
						}
						stage("transport_finish", transport.finish)
					}
					row.ElapsedNS = time.Since(began).Nanoseconds()
					row.Validated = true
				}
				// Independently fetch the entire fixture after timers, with no operation
				// context; background and validation traffic remain outside sample calls.
				expected := map[int]string{}
				for i := 1; i <= 1000; i++ {
					expected[i] = crudProfileWant(i)
				}
				for sample := -1; sample < 32; sample++ {
					target := sample
					if target < 0 {
						target = 32
					}
					id := (target*61)%1000 + 1
					if operation == "insert" {
						id = 1001 + target
					}
					switch operation {
					case "insert":
						expected[id] = crudProfileWant(id)
					case "update":
						expected[id] = fmt.Sprintf("%d|%d|%d|row-%08d", id, id%16, -id, id)
					case "delete":
						delete(expected, id)
					}
				}
				if mode != "raw" {
					rows, sch := h.query("SELECT id,grp,amount,label FROM " + name + " ORDER BY id")
					if sch.NumFields() != 4 || sch.Field(0).Type.ID() != arrow.INT64 || sch.Field(1).Type.ID() != arrow.INT64 || sch.Field(2).Type.ID() != arrow.INT64 || sch.Field(3).Type.ID() != arrow.STRING || len(rows) != len(expected) {
						t.Fatal("incomplete final fixture")
					}
					for _, row := range rows {
						parts := strings.Split(row, "|")
						id, err := strconv.Atoi(parts[0])
						if err != nil || expected[id] != row {
							t.Fatal("wrong final fixture value")
						}
						delete(expected, id)
					}
					if len(expected) != 0 {
						t.Fatal("missing final row")
					}
				} else {
					if n, err := raw.Do(h.ctx, "FT.SEARCH", index, "*", "LIMIT", 0, 0).Result(); err != nil || n == nil {
						t.Fatal("raw final index unavailable")
					}
					for id, want := range expected {
						values, err := raw.HMGet(h.ctx, prefix+strconv.Itoa(id), "id", "grp", "amount", "label").Result()
						if err != nil || strings.Join([]string{fmt.Sprint(values[0]), fmt.Sprint(values[1]), fmt.Sprint(values[2]), fmt.Sprint(values[3])}, "|") != want {
							t.Fatal("wrong raw final fixture")
						}
					}
					keys := map[string]bool{}
					iter := raw.Scan(h.ctx, 0, prefix+"*", 256).Iterator()
					for iter.Next(h.ctx) {
						keys[iter.Val()] = true
					}
					if err := iter.Err(); err != nil || len(keys) != len(expected) {
						t.Fatal("raw final count mismatch")
					}
				}
				finalStateValidated = true
			})
		}
	}
}

func TestCRUDProfileCategoriesAndValueBytes(t *testing.T) {
	ctx := context.Background()
	cmd := goredis.NewStringCmd(ctx, "GET", snapshotPrefix+"workspace:private:table")
	cmd.SetVal("abc")
	if crudProfileCategory(cmd) != "metadata_object_get" || crudProfileValueBytes(cmd) != 3 {
		t.Fatal("metadata classification/value bytes")
	}
	root := goredis.NewStringCmd(ctx, "HGET", snapshotPrefix+"metadata:root:private", "slot")
	if crudProfileCategory(root) != "metadata_root_hget" {
		t.Fatal("root classification")
	}
	values := goredis.NewSliceCmd(ctx, "HMGET", "application:private", "a", "b")
	values.SetVal([]any{"one", []byte("two"), nil})
	if crudProfileValueBytes(values) != 6 {
		t.Fatal("selected value-byte count")
	}
	// Distinct client roles and operation context keep worker calls separate.
	obs := &crudProfileObserver{epoch: time.Now()}
	sample := &crudProfileSample{}
	op := context.WithValue(ctx, crudProfileKey{}, sample)
	for _, role := range []string{"execution", "shared_store", "worker"} {
		hook := &crudProfileHook{observer: obs, role: role}
		if err := hook.ProcessHook(func(context.Context, goredis.Cmder) error { return nil })(op, cmd); err != nil {
			t.Fatal(err)
		}
	}
	if len(sample.Calls) != 2 || len(obs.calls) != 1 || !reflect.DeepEqual(obs.calls[0].Categories, []string{"metadata_object_get"}) || obs.calls[0].Role != "worker" {
		t.Fatal("operation/client-role separation")
	}
}

// Observer-only follow-up. Earlier profiles retain their original observer hash
// and counts; this does not retroactively fill unsupported historical counters.
func TestCRUDProfileWrappedValueBytes(t *testing.T) {
	ctx := context.Background()
	values := goredis.NewSliceCmd(ctx, "HMGET", "synthetic", "a", "b")
	values.SetVal([]any{"one", []byte{'a', 0, 'b'}, nil})
	generic := goredis.NewCmd(ctx, "FCALL", "synthetic", 0)
	generic.SetVal([]any{"one", []byte{'a', 0, 'b'}, nil, []any{"not counted"}})
	projected := unmanagedProjectionCmd{Cmder: values}
	wrappedOnce := onceCmd{Cmd: generic}
	for _, cmd := range []goredis.Cmder{projected, &projected, unmanagedProjectionCmd{Cmder: projected}, wrappedOnce, &wrappedOnce, unmanagedProjectionCmd{Cmder: wrappedOnce}} {
		if got := crudProfileValueBytes(cmd); got != 6 {
			t.Fatalf("wrapped flat value bytes=%d, want 6", got)
		}
	}
	values.SetErr(fmt.Errorf("synthetic incomplete reply"))
	if got := crudProfileValueBytes(projected); got != 0 {
		t.Fatal("failed reply counted", got)
	}
}
