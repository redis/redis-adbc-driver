// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestFlexUnmanagedSourceWrites(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
			if err := raw.Do(h.ctx, "FT.CREATE", "source_idx", "ON", "HASH", "PREFIX", 1, "app:", "SKIPINITIALSCAN", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 257; i++ {
				if err := raw.HSet(h.ctx, fmt.Sprintf("app:%d", i), "n", i, "extra", "keep").Err(); err != nil {
					t.Fatal(err)
				}
			}
			if err := (&store{client: raw}).awaitIndexReady(h.ctx, "source_idx"); err != nil {
				t.Fatal(err)
			}
			table := "live"
			opts := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: "true"}
			if foreign {
				setter := h.conn.(adbc.GetSetOptionsWithContext)
				if err := setter.SetOption(h.ctx, OptionStringForeignIndexesMode, "unmanaged"); err != nil {
					t.Fatal(err)
				}
				if err := setter.SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
					t.Fatal(err)
				}
				table = "redis_foreign.source_idx"
			} else {
				if _, err := AdoptHashes(h.ctx, opts, HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Index: "source_idx", Table: table, Apply: true}); err != nil {
					t.Fatal(err)
				}
			}
			h.expectRows("SELECT COUNT(*),SUM(n) FROM "+table, "257|32896")
			h.exec("CREATE TABLE managed(k INTEGER)")
			h.exec("INSERT INTO managed VALUES(0)")
			h.expectRows("SELECT COUNT(*) FROM "+table+" l JOIN managed m ON l.n=m.k", "1")

			h.expectRows("SELECT n FROM "+table+" WHERE n>=254 ORDER BY n DESC LIMIT 2", "256", "255")
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			h.exec("UPDATE " + table + " SET n=n+1000 WHERE n<2")
			h.exec("DELETE FROM " + table + " WHERE n=2")
			h.exec("INSERT INTO " + table + " (__key,n) VALUES ('app:new',9007199254740993)")
			h.expectRows("SELECT n FROM "+table+" WHERE __key='app:new'", "9007199254740993")
			if got := raw.HGet(h.ctx, "app:0", "extra").Val(); got != "keep" {
				t.Fatal("UPDATE discarded unrelated field", got)
			}
			if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
				t.Fatal("source DML published managed row generation")
			}
			h.exec("DROP TABLE " + table)
			s := snapshotTestStore(t, raw)
			s.flex = true
			snapshotReapTest(s, h.ctx)
			if got := raw.HGet(h.ctx, "app:0", "n").Val(); got != "1000" {
				t.Fatal("DROP/GC changed source", got)
			}
			if err := raw.Do(h.ctx, "FT.INFO", "source_idx").Err(); err != nil {
				t.Fatal("DROP/GC removed source index", err)
			}
		})
	}
}

func TestFlexUnmanagedForeignMembership(t *testing.T) {
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	if err := raw.Do(h.ctx, "FT.CREATE", "filtered", "ON", "HASH", "PREFIX", 3, "app:", "app:sub:", "other:", "FILTER", "@keep == 1", "SKIPINITIALSCAN", "SCHEMA", "n", "NUMERIC", "keep", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	for key, n := range map[string]int{"app:a": 1, "app:sub:nested": 2, "other:\xffbinary": 3, "app:excluded": 4} {
		keep := 1
		if n == 4 {
			keep = 0
		}
		if err := raw.HSet(h.ctx, key, "n", n, "keep", keep, "extra", "old").Err(); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "filtered source indexed", func() bool {
		n, err := (&store{client: raw}).countMatches(h.ctx, "filtered", "*")
		return err == nil && n == 3
	})
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: "true", OptionStringForeignIndexes: "true", OptionStringForeignIndexesMode: "unmanaged"}
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(h.ctx)
	conn, err := db.Open(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(h.ctx)
	foreign := &sqlHarness{t: t, ctx: h.ctx, conn: conn}
	foreign.expectRows("SELECT COUNT(*),SUM(n) FROM redis_foreign.filtered", "3|6")
	foreign.expectRows("SELECT __key FROM redis_foreign.filtered WHERE n=3", "other:\xffbinary")
	foreign.exec("UPDATE redis_foreign.filtered SET extra='changed'")
	foreign.exec("DELETE FROM redis_foreign.filtered WHERE __key='app:excluded'")
	if raw.HGet(h.ctx, "app:excluded", "extra").Val() != "old" || raw.Exists(h.ctx, "app:excluded").Val() != 1 {
		t.Fatal("DML escaped source FILTER membership")
	}
	foreign.expectError("INSERT INTO redis_foreign.filtered (__key,n) VALUES ('other:new',9)", "filtered foreign index")
	if err := foreign.setConnOption(OptionStringForeignIndexesMode, "managed"); err == nil {
		t.Fatal("enabled Flex discovery switched to managed mode")
	}
	if err := raw.PExpire(h.ctx, "app:a", time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "source expiration indexed", func() bool {
		n, err := (&store{client: raw}).countMatches(h.ctx, "filtered", "*")
		return err == nil && n == 2
	})
	foreign.expectRows("SELECT COUNT(*),SUM(n) FROM redis_foreign.filtered", "2|5")
	if err := raw.Do(h.ctx, "FT.DROPINDEX", "filtered").Err(); err != nil {
		t.Fatal(err)
	}
	foreign.expectError("SELECT n FROM redis_foreign.filtered", "unmanaged source index")
	if err := raw.Do(h.ctx, "FT.CREATE", "filtered", "ON", "HASH", "PREFIX", 3, "app:", "app:sub:", "other:", "FILTER", "@keep == 0", "SKIPINITIALSCAN", "SCHEMA", "n", "NUMERIC", "keep", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	foreign.expectError("SELECT n FROM redis_foreign.filtered", "definition changed")
	if err := raw.HSet(h.ctx, "app:excluded", "n", 4, "keep", 0).Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "refreshed source indexed", func() bool {
		n, err := (&store{client: raw}).countMatches(h.ctx, "filtered", "*")
		return err == nil && n == 1
	})
	if err := RefreshForeignIndexes(h.ctx, options); err != nil {
		t.Fatal(err)
	}
	foreign.expectRows("SELECT COUNT(*),SUM(n) FROM redis_foreign.filtered", "1|4")

	foreign.exec("DROP TABLE redis_foreign.filtered")
	if raw.Exists(h.ctx, "app:excluded", "app:sub:nested", "other:\xffbinary").Val() != 3 {
		t.Fatal("DROP changed application source")
	}
}

func TestFlexUnmanagedLookupChecksIndex(t *testing.T) {
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	args := []any{"FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SKIPINITIALSCAN", "SCHEMA", "n", "NUMERIC"}
	if err := raw.Do(h.ctx, args...).Err(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"app:a", "app:sub:nested"} {
		if err := raw.HSet(h.ctx, key, "n", 10).Err(); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "source indexed", func() bool {
		n, err := (&store{client: raw}).countMatches(h.ctx, "app_idx", "*")
		return err == nil && n == 2
	})
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: "true"}
	if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Index: "app_idx", Table: "live", Apply: true}); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT n FROM live", "10")
	h.expectRows("SELECT n FROM live WHERE __key='app:sub:nested'")
	if err := raw.Do(h.ctx, "FT.DROPINDEX", "app_idx").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, args...).Err(); err != nil {
		t.Fatal(err)
	}
	// Existing HASHes deliberately remain outside this SKIPINITIALSCAN index.
	h.expectRows("SELECT n FROM live WHERE __key='app:a'")
	h.expectRows("SELECT COUNT(*) FROM live", "0")
	if raw.HGet(h.ctx, "app:a", "n").Val() != "10" {
		t.Fatal("membership check altered HASH")
	}
}

func flexUnmanagedFixture(t *testing.T) (*sqlHarness, *store, context.Context, *tableMeta, *goredis.Client) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessOptions(t, false, false, map[string]string{OptionStringFlex: "true"})
	if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SKIPINITIALSCAN", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"app:1", "app:2"} {
		if err := raw.HSet(h.ctx, key, "n", "10", "enabled", "true").Err(); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "source indexed", func() bool {
		n, err := (&store{client: raw}).countMatches(h.ctx, "app_idx", "*")
		return err == nil && n == 2
	})
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI"), OptionStringFlex: "true"}
	o := HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Index: "app_idx", Table: "live", Apply: true, Types: map[string]string{"enabled": "BOOLEAN"}, Encodings: map[string]string{"enabled": "text"}}
	if _, err := AdoptHashes(h.ctx, options, o); err != nil {
		t.Fatal(err)
	}
	s := snapshotTestStore(t, raw)
	s.flex = true
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pin.release() })
	meta, err := s.getTable(ctx, defaultSchema, "live")
	if err != nil {
		t.Fatal(err)
	}
	return h, s, ctx, meta, raw
}

func TestFlexUnmanagedUnknownWriteAndEncoding(t *testing.T) {
	h, s, ctx, meta, raw := flexUnmanagedFixture(t)
	if err := raw.PExpire(h.ctx, "app:1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	h.exec("UPDATE live SET enabled=FALSE WHERE __key='app:1'")
	if raw.HGet(h.ctx, "app:1", "enabled").Val() != "false" || raw.PTTL(h.ctx, "app:1").Val() <= 0 {
		t.Fatal("UPDATE lost text encoding or TTL")
	}
	h.expectRows("SELECT enabled FROM live WHERE __key='app:1'", "false")
	h.expectError("TRUNCATE live", "unmanaged")
	h.expectError("ALTER TABLE live ADD COLUMN nope INTEGER", "unmanaged")
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	ctx = context.WithValue(ctx, unmanagedWriteKey{}, metaKey(defaultSchema, "live"))
	observer := &unmanagedLostReply{t: t}
	s.client.AddHook(observer)
	err := s.updateUnmanagedRows(ctx, meta, []rowChange{{key: "app:1", set: []any{"n", "20"}}, {key: "app:2", set: []any{"n", "30"}}})
	if err == nil || !strings.Contains(err.Error(), "partially applied") || observer.calls != 1 {
		t.Fatal("unknown source mutation was replayed or misreported", observer.calls, err)
	}
	if raw.HGet(h.ctx, "app:1", "n").Val() != "20" || raw.HGet(h.ctx, "app:2", "n").Val() != "10" {
		t.Fatal("unknown reply did not stop at first applied write")
	}
	if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
		t.Fatal("failed source write published managed generation")
	}
}

type flexUnmanagedReadFault struct {
	mode  string
	calls int
}

func (*flexUnmanagedReadFault) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *flexUnmanagedReadFault) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if h.mode == "scan_permission" && strings.EqualFold(cmd.Name(), "scan") {
			h.calls++
			err := readinessServerError("NOPERM source SCAN denied")
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}
func (h *flexUnmanagedReadFault) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		membership, hashes := false, false
		for _, cmd := range cmds {
			a := cmd.Args()
			membership = membership || len(a) > 3 && strings.EqualFold(fmt.Sprint(a[0]), "FT.SEARCH") && a[3] == "INKEYS"
			hashes = hashes || cmd.Name() == "hgetall"
		}
		if membership {
			h.calls++
			switch h.mode {
			case "permission":
				err := readinessServerError("NOPERM membership query denied")
				for _, cmd := range cmds {
					cmd.SetErr(err)
				}
				return err
			case "transport":
				if err := next(ctx, cmds); err != nil {
					return err
				}
				return io.ErrUnexpectedEOF
			case "warning", "duplicate", "malformed":
				for _, cmd := range cmds {
					n := any(int64(1))
					warning := []any{}
					if h.mode == "warning" {
						warning = []any{"partial"}
					}
					if h.mode == "duplicate" {
						n = int64(2)
					}
					if h.mode == "malformed" {
						n = "bad"
					}
					cmd.(interface{ SetVal(any) }).SetVal(map[any]any{"total_results": n, "warning": warning})
				}
				return nil
			}
		}
		err := next(ctx, cmds)
		if err == nil && hashes && (h.mode == "hash_transport" || h.mode == "hash_wrongtype_transport") {
			h.calls++
			if h.mode == "hash_wrongtype_transport" {
				return errors.New("WRONGTYPE injected lost pipeline reply")
			}
			return io.ErrUnexpectedEOF
		}
		return err
	}
}

func TestFlexUnmanagedReadFailures(t *testing.T) {
	for _, mode := range []string{"scan_permission", "permission", "transport", "warning", "duplicate", "malformed", "hash_transport", "hash_wrongtype_transport", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			_, s, ctx, meta, _ := flexUnmanagedFixture(t)
			hook := &flexUnmanagedReadFault{mode: mode}
			s.client.AddHook(hook)
			reader := &flexUnmanagedScan{store: s, meta: meta, seen: map[string]bool{}}
			if mode == "cancel" {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelCtx
			}
			if _, _, err := reader.next(ctx); err == nil {
				t.Fatal("failed or partial read returned success")
			}
			if _, _, err := reader.next(context.Background()); !errors.Is(err, io.EOF) {
				t.Fatal("terminal reader resumed", err)
			}
			if mode != "cancel" && hook.calls < 1 {
				t.Fatal("fault path was not exercised")
			}
		})
	}
}
