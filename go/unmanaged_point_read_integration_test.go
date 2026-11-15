// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	goredis "github.com/redis/go-redis/v9"
)

type unmanagedPointNativeReads struct {
	mu       sync.Mutex
	commands []string
	fields   [][]any
}

func (*unmanagedPointNativeReads) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*unmanagedPointNativeReads) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return next
}
func (h *unmanagedPointNativeReads) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		h.mu.Lock()
		for _, cmd := range cmds {
			args := cmd.Args()
			if len(args) > 1 && (cmd.Name() == "hmget" || cmd.Name() == "hgetall") && strings.HasPrefix(fmt.Sprint(args[1]), "app:") {
				h.commands = append(h.commands, cmd.Name())
				h.fields = append(h.fields, append([]any(nil), args[2:]...))
			}
		}
		h.mu.Unlock()
		return next(ctx, cmds)
	}
}
func (h *unmanagedPointNativeReads) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commands = nil
	h.fields = nil
}
func (h *unmanagedPointNativeReads) assert(t *testing.T, want ...string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.commands) != len(want) {
		t.Fatal("wrong source calls", h.commands, want)
	}
	for i, name := range want {
		if h.commands[i] != name {
			t.Fatal("wrong source command", h.commands, want)
		}
		if name == "hmget" && (len(h.fields[i]) != 1 || h.fields[i][0] != "n") {
			t.Fatal("point fetched unselected registered/application payload", h.fields)
		}
	}
}

func TestUnmanagedPointReadSelectedFieldsSoftware(t *testing.T) {
	h, raw, _ := unmanagedIngestFixture(t, false)
	// 'extra' was registered by adoption; 'unregistered' is added afterwards.
	// Both large fields must stay outside a selected-n point read.
	wide := strings.Repeat("x", 64<<10)
	if err := raw.HSet(h.ctx, "app:seed", "n", "9007199254740993", "extra", wide, "unregistered", wide).Err(); err != nil {
		t.Fatal(err)
	}
	observer := &unmanagedPointNativeReads{}
	ctx, _ := statisticsHookedTransport(t, h, observer)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			stmt, err := h.conn.NewStatement(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close(ctx)
			if err := stmt.SetOption(ctx, OptionStringStreamResults, fmt.Sprint(stream)); err != nil {
				t.Fatal(err)
			}
			read := func(key string, want *int64) {
				t.Helper()
				if err := stmt.SetSqlQuery(ctx, "SELECT n FROM live WHERE __key='"+strings.ReplaceAll(key, "'", "''")+"'"); err != nil {
					t.Fatal(err)
				}
				reader, _, err := stmt.ExecuteQuery(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Release()
				count := 0
				for reader.Next() {
					rec := reader.RecordBatch()
					col, ok := rec.Column(0).(*array.Int64)
					if !ok {
						t.Fatal("wrong physical type", rec.Schema())
					}
					for i := 0; i < int(rec.NumRows()); i++ {
						count++
						if want == nil {
							if !col.IsNull(i) {
								t.Fatal("all-NULL existing HASH became value")
							}
						} else if col.IsNull(i) || col.Value(i) != *want {
							t.Fatal("exact native value changed", col.Value(i), *want)
						}
					}
				}
				if err := reader.Err(); err != nil {
					t.Fatal(err)
				}
				expected := 1
				if key == "app:missing" || key == "app:wrong" {
					expected = 0
				}
				if count != expected {
					t.Fatal("missing/all-null/source type conflated", key, count, expected)
				}
			}
			want := int64(9007199254740993)
			observer.reset()
			read("app:seed", &want)
			observer.assert(t, "hmget")
			// Native binary application keys retain every byte and source values remain live.
			key := "app:" + string([]byte{'b', 0, 'k'})
			if !strings.ContainsRune(key, 0) {
				t.Fatal("fixture lacks NUL")
			}
			if err := raw.HSet(h.ctx, key, "n", "-9223372036854775808", "extra", wide, "unregistered", wide).Err(); err != nil {
				t.Fatal(err)
			}
			want = -9223372036854775808
			observer.reset()
			read(key, &want)
			observer.assert(t, "hmget")
			if err := raw.HSet(h.ctx, "app:all-null", "extra", "keeps HASH alive").Err(); err != nil {
				t.Fatal(err)
			}
			observer.reset()
			read("app:all-null", nil)
			observer.assert(t, "hmget", "hgetall")
			observer.reset()
			read("app:missing", nil)
			observer.assert(t, "hmget", "hgetall")
			if err := raw.Set(h.ctx, "app:wrong", "not a HASH", 0).Err(); err != nil {
				t.Fatal(err)
			}
			observer.reset()
			read("app:wrong", nil)
			observer.assert(t, "hmget")
		})
	}
	if got := raw.HGet(h.ctx, "app:seed", "extra").Val(); got != wide {
		t.Fatal("point read changed source payload")
	}
}

// DML shares matchRows/scan with SELECT. RETURNING and SET dependencies must
// stay in the projection even when unrelated application fields are omitted.
func TestUnmanagedPointDMLReturningDependenciesSoftware(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			h, raw, _ := unmanagedIngestFixture(t, false)
			text := string([]byte{'r', 0, 't'})
			if !strings.ContainsRune(text, 0) {
				t.Fatal("fixture lacks NUL")
			}
			if err := raw.HSet(h.ctx, "app:seed", "n", "9007199254740993", "extra", text, "unregistered", "preserve until DELETE").Err(); err != nil {
				t.Fatal(err)
			}
			if err := raw.HSet(h.ctx, "app:other", "n", "17", "extra", "other source", "unregistered", "never change").Err(); err != nil {
				t.Fatal(err)
			}
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			observer := &unmanagedPointNativeReads{}
			ctx, _ := statisticsHookedTransport(t, h, observer)
			returning := func(query string, want int64) {
				t.Helper()
				stmt, err := h.conn.NewStatement(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer stmt.Close(ctx)
				if err := stmt.SetOption(ctx, OptionStringStreamResults, fmt.Sprint(stream)); err != nil {
					t.Fatal(err)
				}
				if err := stmt.SetSqlQuery(ctx, query); err != nil {
					t.Fatal(err)
				}
				observer.reset()
				reader, _, err := stmt.ExecuteQuery(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Release()
				count := 0
				for reader.Next() {
					rec := reader.RecordBatch()
					if rec.NumCols() != 2 {
						t.Fatal("RETURNING schema changed", rec.Schema())
					}
					nums, nok := rec.Column(0).(*array.Int64)
					texts, tok := rec.Column(1).(*array.String)
					if !nok || !tok {
						t.Fatal("RETURNING physical type changed", rec.Schema())
					}
					for i := 0; i < int(rec.NumRows()); i++ {
						count++
						if nums.IsNull(i) || texts.IsNull(i) || nums.Value(i) != want || texts.Value(i) != text {
							t.Fatal("RETURNING lost exact old/new dependency", nums.Value(i), texts.Value(i))
						}
					}
				}
				if err := reader.Err(); err != nil || count != 1 {
					t.Fatal("incomplete RETURNING result", count, err)
				}
				observer.mu.Lock()
				defer observer.mu.Unlock()
				if len(observer.commands) != 1 || observer.commands[0] != "hmget" {
					t.Fatal("DML did not use exact projected known-key image", observer.commands)
				}
				fields := map[any]bool{}
				for _, field := range observer.fields[0] {
					fields[field] = true
				}
				if len(fields) != 2 || !fields["n"] || !fields["extra"] {
					t.Fatal("DML projection omitted SET/RETURNING dependency or fetched unmodeled data", observer.fields)
				}
			}
			returning("UPDATE live SET n=n+1 WHERE __key='app:seed' RETURNING n,extra", 9007199254740994)
			if got := raw.HGet(h.ctx, "app:seed", "n").Val(); got != "9007199254740994" {
				t.Fatal("incorrect exact source mutation", got)
			}
			if raw.HGet(h.ctx, "app:seed", "extra").Val() != text || raw.HGet(h.ctx, "app:seed", "unregistered").Val() != "preserve until DELETE" {
				t.Fatal("projected UPDATE lost unrelated source fields")
			}
			returning("DELETE FROM live WHERE __key='app:seed' RETURNING n,extra", 9007199254740994)
			if raw.Exists(h.ctx, "app:seed").Val() != 0 {
				t.Fatal("DELETE did not remove complete source HASH")
			}
			if raw.HGet(h.ctx, "app:other", "n").Val() != "17" || raw.HGet(h.ctx, "app:other", "unregistered").Val() != "never change" {
				t.Fatal("DML changed a different application HASH")
			}
			if got := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val(); got != root {
				t.Fatal("unmanaged DML unexpectedly published managed rows")
			}
		})
	}
}
