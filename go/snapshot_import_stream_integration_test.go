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
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func snapshotImportBatchFixture(t *testing.T) (*sqlHarness, *goredis.Client, *store, context.Context, *tableMeta, *snapshotImportOutput, []snapshotInsertRow) {
	t.Helper()
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	ctx, pin, err := s.acquireSnapshot(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pin.release)
	wctx, id, err := s.beginSnapshotWrite(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.abortSnapshot(pin, id) })
	meta := &tableMeta{Schema: "public", Name: "batch_import", SnapshotSourceKey: true, Columns: []columnMeta{{Name: "v", Type: typeInt64}}}
	if _, err := s.createTable(wctx, meta, false); err != nil {
		t.Fatal(err)
	}
	spool := newSnapshotImportSpool(wctx, s, meta)
	if err := spool.append(wctx, []map[string]Value{
		{keyField: stringValue("app:batch:1"), "v": intValue(typeInt64, 1)},
		{keyField: stringValue("app:batch:2"), "v": intValue(typeInt64, 2)},
	}); err != nil {
		t.Fatal(err)
	}
	journal := &snapshotImportOutput{rows: snapshotWorkspace(id) + "import:rows:" + id, keys: snapshotWorkspace(id) + "import:keys:" + id}
	wctx = context.WithValue(wctx, snapshotImportOutputKey{}, journal)
	rows := []snapshotInsertRow{
		{key: meta.prefix() + "1", member: fmt.Sprintf("%020d", 1), fields: []any{rowIDField, "1", snapshotSourceKeyField, "app:batch:1", "v", "1"}},
		{key: meta.prefix() + "2", member: fmt.Sprintf("%020d", 2), fields: []any{rowIDField, "2", snapshotSourceKeyField, "app:batch:2", "v", "2"}},
	}
	return h, raw, s, wctx, meta, journal, rows
}

func TestSnapshotImportBatchAdmission(t *testing.T) {
	for _, mode := range []string{"missing_input", "duplicate_input", "wrong_score", "wrong_namespace", "unregistered_journal", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			h, raw, s, ctx, meta, journal, rows := snapshotImportBatchFixture(t)
			switch mode {
			case "missing_input":
				rows[1].fields[3] = "app:batch:missing"
			case "duplicate_input":
				rows[1].fields[3] = "app:batch:1"
			case "wrong_score":
				if err := raw.ZAdd(ctx, journal.keys, goredis.Z{Score: 1, Member: "app:batch:2"}).Err(); err != nil {
					t.Fatal(err)
				}
			case "wrong_namespace":
				journal.rows = "app:batch:foreign"
			case "unregistered_journal":
				if err := raw.HDel(ctx, snapshotMetadataGraph, "v:"+journal.rows).Err(); err != nil {
					t.Fatal(err)
				}
			case "incomplete":
				var work map[string]any
				field := "w:" + snapshotOf(ctx).id
				if err := json.Unmarshal([]byte(raw.HGet(ctx, snapshotCatalogKey, field).Val()), &work); err != nil {
					t.Fatal(err)
				}
				work["import_pending"] = snapshotID()
				encoded, _ := json.Marshal(work)
				if err := raw.HSet(ctx, snapshotCatalogKey, field, encoded).Err(); err != nil {
					t.Fatal(err)
				}
				for _, op := range []string{"seal", "publish"} {
					if err := s.snapshotControl(ctx, op, snapshotOf(ctx).pin.token, snapshotOf(ctx).id, snapshotOf(ctx).pin.root).Err(); err == nil || !strings.Contains(err.Error(), "incomplete import batch") {
						t.Fatalf("%s accepted incomplete batch: %v", op, err)
					}
				}
			}
			if err := s.insertSnapshotBatch(ctx, meta.prefix(), rows); err == nil {
				t.Fatal("invalid capture admitted")
			}
			if raw.Exists(h.ctx, rows[0].key, rows[1].key).Val() != 0 || raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Val() != 0 {
				t.Fatal("invalid capture partially wrote output")
			}
			id := snapshotOf(ctx).id
			if raw.HLen(ctx, snapshotWorkspace(id)+"import:rows:"+id).Val() != 2 || raw.ZCard(ctx, journal.keys).Val() != 2 {
				t.Fatal("invalid batch consumed captured input")
			}
		})
	}
}

func TestSnapshotImportBatchPermissionPreflight(t *testing.T) {
	for _, denied := range []string{"hdel", "zrem"} {
		t.Run(denied, func(t *testing.T) {
			uri := os.Getenv("REDIS_SNAPSHOT_IMPORT_" + strings.ToUpper(denied) + "_ACL_URI")
			if uri == "" {
				t.Skip("requires owned Software principal with journal " + denied + " denied")
			}
			h, raw, _, ctx, meta, journal, rows := snapshotImportBatchFixture(t)
			credentials, err := goredis.ParseURL(uri)
			if err != nil {
				t.Fatal("invalid private principal configuration")
			}
			opts := *raw.Options()
			opts.Username, opts.Password = credentials.Username, credentials.Password
			limited := goredis.NewClient(&opts)
			defer limited.Close()
			probe := meta.prefix() + "acl-probe"
			if err := limited.HSet(ctx, probe, "v", "1").Err(); err != nil {
				t.Fatal("HSET positive control failed")
			}
			if err := raw.Del(ctx, probe).Err(); err != nil {
				t.Fatal(err)
			}
			if err := limited.ZAdd(ctx, snapshotMembers(snapshotDataID(meta.prefix())), goredis.Z{Member: "probe"}).Err(); err != nil {
				t.Fatal("ZADD positive control failed")
			}
			if err := raw.Del(ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Err(); err != nil {
				t.Fatal(err)
			}
			var negative error
			if denied == "hdel" {
				negative = limited.HDel(ctx, journal.rows, "missing").Err()
				if err := limited.ZRem(ctx, journal.keys, "missing").Err(); err != nil {
					t.Fatal("ZREM positive control failed")
				}
			} else {
				negative = limited.ZRem(ctx, journal.keys, "missing").Err()
				if err := limited.HDel(ctx, journal.rows, "missing").Err(); err != nil {
					t.Fatal("HDEL positive control failed")
				}
			}
			if negative == nil || !strings.Contains(negative.Error(), "NOPERM") {
				t.Fatal("journal consumption negative control failed")
			}
			field := "w:" + snapshotOf(ctx).id
			before := raw.HGet(ctx, snapshotCatalogKey, field).Val()
			guarded := snapshotTestStore(t, limited)
			if err := guarded.insertSnapshotBatch(ctx, meta.prefix(), rows); err == nil || !strings.Contains(err.Error(), "journal consumption") {
				t.Fatalf("journal permission preflight not exercised: %v", err)
			}
			if raw.Exists(h.ctx, rows[0].key, rows[1].key).Val() != 0 || raw.ZCard(ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Val() != 0 || raw.HGet(ctx, snapshotCatalogKey, field).Val() != before || raw.HLen(ctx, journal.rows).Val() != 2 || raw.ZCard(ctx, journal.keys).Val() != 2 {
				t.Fatal("denied journal consumption partially mutated work, rows or capture")
			}
		})
	}
}

type snapshotImportJournalInterruption struct {
	snapshotHook
	command string
	after   bool
	calls   int
}

type snapshotImportOutputObserver struct {
	snapshotHook
	raw       goredis.UniversalClient
	lostReply bool
	uncleared bool
	calls     int
}

func (h *snapshotImportOutputObserver) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if len(snapshotInsertBatchRowKeys(cmd)) == 0 {
			return next(ctx, cmd)
		}
		h.calls++
		if err := next(ctx, cmd); err != nil {
			return err
		}
		id := snapshotOf(ctx).id
		prefix := snapshotWorkspace(id) + "import:"
		if h.raw.HLen(ctx, prefix+"rows:"+id).Val() != 0 || h.raw.ZCard(ctx, prefix+"keys:"+id).Val() != 0 {
			h.uncleared = true
			cmd.SetErr(errors.New("output acknowledged before captured input was consumed"))
			return cmd.Err()
		}
		if h.lostReply {
			cmd.SetErr(errors.New("injected lost import output reply"))
			return cmd.Err()
		}
		return nil
	}
}

func TestSnapshotImportOutputConsumesCaptureAtomic(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost_reply_%t", lost), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			s := snapshotTestStore(t, raw)
			observer := &snapshotImportOutputObserver{raw: raw, lostReply: lost}
			s.client.AddHook(observer)
			source := &tableMeta{Name: "source", KeyPrefix: "app:atomic:", KeyRows: true, Columns: []columnMeta{{Name: "n", Type: typeInt64}}}
			calls := 0
			next := func(context.Context) ([]map[string]Value, bool, error) {
				calls++
				return []map[string]Value{{keyField: stringValue("app:atomic:\x00雪\xff"), "n": intValue(typeInt64, 9007199254740993)}}, true, nil
			}
			if err := s.importSnapshotTable(h.ctx, source, "public", "atomic_copy", false, nil, next); err != nil {
				t.Fatal(err)
			}
			if observer.uncleared {
				t.Fatal("row output and captured input consumption were not atomic")
			}
			if observer.calls != 1 || calls != 1 {
				t.Fatalf("replayed output/input: output=%d input=%d", observer.calls, calls)
			}
			h.expectRows("SELECT n FROM atomic_copy", "9007199254740993")
		})
	}
}

func TestSnapshotImportOutputSplitsWideCapturePage(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	s := snapshotTestStore(t, raw)
	source := &tableMeta{Name: "source", KeyPrefix: "app:wide:", KeyRows: true, Columns: []columnMeta{{Name: "v", Type: typeString}}}
	value := strings.Repeat("x", (1<<19)-100)
	key := "app:wide:" + strings.Repeat("k", 1024)
	calls := 0
	next := func(context.Context) ([]map[string]Value, bool, error) {
		calls++
		return []map[string]Value{
			{keyField: stringValue(key + "1"), "v": stringValue(value)},
			{keyField: stringValue(key + "2"), "v": stringValue(value)},
		}, true, nil
	}
	observer := &snapshotInsertBatchInterruption{after: true}
	s.client.AddHook(observer)
	if err := s.importSnapshotTable(h.ctx, source, "public", "wide_copy", false, nil, next); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || observer.calls != 2 || observer.rows != 2 || observer.maxRows != 1 {
		t.Fatalf("wide capture did not split safely: input=%d observer=%+v", calls, observer)
	}
	actual, _ := h.query("SELECT __key,v FROM wide_copy ORDER BY __key")
	if len(actual) != 2 || actual[0] != key+"1|"+value || actual[1] != key+"2|"+value {
		t.Fatal("wide capture changed exact source keys or payloads")
	}
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
	for _, command := range []string{"hset", "zadd"} {
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
