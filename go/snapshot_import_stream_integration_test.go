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
	goredis "github.com/redis/go-redis/v9"
)

type snapshotImportJournalInterruption struct {
	snapshotHook
	command string
	after   bool
	calls   int
}

func (h *snapshotImportJournalInterruption) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := cmd.Args()
		if len(a) < 3 || !strings.EqualFold(fmt.Sprint(a[0]), "eval") {
			return next(ctx, cmd)
		}
		n, _ := strconv.Atoi(fmt.Sprint(a[2]))
		original := 3 + n + 4
		if original+1 >= len(a) || fmt.Sprint(a[original]) != h.command || !strings.Contains(fmt.Sprint(a[original+1]), ":import:") {
			return next(ctx, cmd)
		}
		h.calls++
		if h.calls != 1 {
			return next(ctx, cmd)
		}
		if h.after {
			if err := next(ctx, cmd); err != nil {
				return err
			}
		}
		cmd.SetErr(errors.New("injected import journal interruption"))
		return cmd.Err()
	}
}

func TestSnapshotImportJournalInterruptedMutation(t *testing.T) {
	for _, command := range []string{"hset", "zadd", "hdel", "zrem"} {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/after_%t", command, after), func(t *testing.T) {
				h, raw, _ := newSnapshotHarnessMode(t, false, false)
				h.exec("CREATE TABLE preserved(n INTEGER)")
				h.exec("INSERT INTO preserved VALUES(42)")
				s := snapshotTestStore(t, raw)
				observer := &snapshotImportJournalInterruption{command: command, after: after}
				s.client.AddHook(observer)
				root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
				source := &tableMeta{Name: "source", KeyPrefix: "app:interrupted:", KeyRows: true, Columns: []columnMeta{{Name: "n", Type: typeInt32}}}
				var work string
				calls := 0
				next := func(ctx context.Context) ([]map[string]Value, bool, error) {
					calls++
					work = snapshotOf(ctx).id
					return []map[string]Value{{keyField: stringValue("app:interrupted:1"), "n": intValue(typeInt32, 1)}}, true, nil
				}
				if err := s.importSnapshotTable(h.ctx, source, "public", "interrupted", false, nil, next); err == nil {
					t.Fatal("uncertain journal mutation published")
				}
				if observer.calls != 1 || calls != 1 || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
					t.Fatalf("replayed input/mutation or changed root: mutation=%d input=%d", observer.calls, calls)
				}
				h.expectRows("SELECT n FROM preserved", "42")
				h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='interrupted'", "0")
				for range 32 {
					s.reapSnapshot(h.ctx)
				}
				if raw.Exists(h.ctx, snapshotWorkspace(work)+"import:rows:"+work, snapshotWorkspace(work)+"import:keys:"+work).Val() != 0 {
					t.Fatal("interrupted journal was not reclaimed")
				}
			})
		}
	}
}

func TestSnapshotForeignImportBeyondCaptureQuota(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const prefix = "app:large_foreign:"
	for start := 1; start <= 100001; start += 1000 {
		p := raw.Pipeline()
		for i := start; i < min(start+1000, 100002); i++ {
			p.HSet(h.ctx, fmt.Sprintf("%s%d", prefix, i), "n", i)
		}
		if _, err := p.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "large_foreign_source", "ON", "HASH", "PREFIX", 1, prefix, "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "source index ready", func() bool {
		r, err := raw.Do(h.ctx, "FT.INFO", "large_foreign_source").Result()
		return err == nil && indexInfoOf(r).numDocs == 100001 && !indexInfoOf(r).indexing
	})
	if err := h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT COUNT(*),MIN(n),MAX(n) FROM redis_foreign.large_foreign_source", "100001|1|100001")
	if raw.HGet(h.ctx, prefix+"100001", "n").Val() != "100001" {
		t.Fatal("import changed application source")
	}
}

func TestSnapshotHashImportBeyondCandidateQuota(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const prefix = "app:large_hash:"
	for start := 1; start <= 200001; start += 1000 {
		p := raw.Pipeline()
		for i := start; i < min(start+1000, 200002); i++ {
			p.HSet(h.ctx, fmt.Sprintf("%s%d", prefix, i), "n", i)
		}
		if _, err := p.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	result, err := AdoptHashes(h.ctx, retainedTestDatabaseOptions(map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}), HashAdoptOptions{Prefix: prefix, Table: "large_hash_copy", Apply: true, OwnIndex: true, IndexColumns: []string{}})
	if err != nil || result == nil || !result.Applied {
		t.Fatalf("large HASH capture: %v", err)
	}
	h.expectRows("SELECT COUNT(*),MIN(n),MAX(n) FROM large_hash_copy", "200001|1|200001")
	if raw.HGet(h.ctx, prefix+"200001", "n").Val() != "200001" {
		t.Fatal("HASH import changed source")
	}
}

func TestSnapshotForeignCaptureOverlappingPrefixes(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	if err := raw.HSet(h.ctx, "app:prefixes:a", "n", "1").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, "app:prefixes:sub:b", "n", "2").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "overlap_source", "ON", "HASH", "PREFIX", 2, "app:prefixes:", "app:prefixes:sub:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "overlap source ready", func() bool {
		r, err := raw.Do(h.ctx, "FT.INFO", "overlap_source").Result()
		return err == nil && indexInfoOf(r).numDocs == 2 && !indexInfoOf(r).indexing
	})
	if err := h.setConnOption(OptionStringForeignIndexes, "true"); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT __key,n FROM redis_foreign.overlap_source ORDER BY n", "app:prefixes:a|1", "app:prefixes:sub:b|2")
}

func TestSnapshotImportJournalCaptureAndCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_failure_%t", fail), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
			source := &tableMeta{KeyPrefix: "app:journal:", KeyRows: true, Columns: []columnMeta{{Name: "n", Type: typeInt32, Nullable: true}}}
			source.Name = "source"
			name := "journal_copy"
			seed := func(context.Context) ([]map[string]Value, bool, error) {
				return []map[string]Value{{keyField: stringValue("app:journal:seed"), "n": intValue(typeInt32, 42)}}, true, nil
			}
			if err := s.importSnapshotTable(h.ctx, source, "public", name, false, nil, seed); err != nil {
				t.Fatal(err)
			}
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			var work, prefix string
			calls := 0
			next := func(ctx context.Context) ([]map[string]Value, bool, error) {
				calls++
				work = snapshotOf(ctx).id
				meta, err := s.getTable(ctx, "public", name)
				if err != nil {
					return nil, false, err
				}
				prefix = meta.prefix()
				if raw.ZCard(ctx, snapshotMembers(snapshotDataID(prefix))).Val() != 0 {
					t.Fatal("output rows appeared before source capture finished")
				}
				if calls == 1 {
					rows := make([]map[string]Value, 260)
					for i := range rows {
						rows[i] = map[string]Value{keyField: stringValue(fmt.Sprintf("app:journal:%03d", i)), "n": intValue(typeInt32, int64(i))}
					}
					rows[259] = map[string]Value{keyField: stringValue("app:journal:000"), "n": intValue(typeInt32, 999)}
					return rows, false, nil
				}
				if fail {
					return nil, false, io.ErrUnexpectedEOF
				}
				return []map[string]Value{{keyField: stringValue("app:journal:000"), "n": intValue(typeInt32, 1000)}, {keyField: stringValue("app:journal:nul\x00雪\xff"), "n": intValue(typeInt32, 300)}}, true, nil
			}
			err := s.importSnapshotTable(h.ctx, source, "public", name, true, nil, next)
			if fail {
				if err == nil || raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
					t.Fatalf("failed capture published: %v", err)
				}
				h.expectRows("SELECT n FROM journal_copy", "42")
				for range 32 {
					s.reapSnapshot(h.ctx)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				h.expectRows("SELECT COUNT(*),MIN(n),MAX(n) FROM journal_copy", "260|0|300")
				h.expectRows("SELECT n FROM journal_copy WHERE __key='app:journal:000'", "0")
				ctx, pin, err := s.acquireSnapshot(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer pin.release()
				for _, kind := range []string{"rows", "keys"} {
					key, err := resolveSnapshotMetadata(ctx, "import:"+kind+":"+work, false)
					if err != nil || !strings.HasPrefix(key, snapshotPrefix+"absent:") {
						t.Fatalf("journal remained in committed metadata: %q %v", key, err)
					}
				}
			}
			if raw.Exists(h.ctx, snapshotWorkspace(work)+"import:rows:"+work, snapshotWorkspace(work)+"import:keys:"+work).Val() != 0 {
				t.Fatal("private journal was not reclaimed")
			}
		})
	}
}
