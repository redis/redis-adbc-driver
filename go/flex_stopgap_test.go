// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func TestFlexPlainSchema(t *testing.T) {
	meta := &tableMeta{PlainIndex: true, Columns: []columnMeta{{Name: "n", Type: typeInt64, Indexed: true}, {Name: "s", Type: typeString, Indexed: true}}}
	args := fmt.Sprint(indexCreateArgs(meta))
	if strings.Contains(args, "SORTABLE") || strings.Contains(args, "UNF") || !strings.Contains(args, "__rowid NUMERIC n NUMERIC s TAG") {
		t.Fatal(args)
	}
	c := columnMeta{Indexed: true, PlainIndex: true, Type: typeInt64}
	if c.inPipeline() || c.sortsExactly() || loadsExactly(c) {
		t.Fatal("plain index qualified for sortable pushdown")
	}
	meta.PlainIndex = false
	if !strings.Contains(fmt.Sprint(indexCreateArgs(meta)), "SORTABLE") {
		t.Fatal("standard schema lost SORTABLE")
	}
}

func TestFlexBoundedReplies(t *testing.T) {
	for _, reply := range []any{
		[]any{int64(3), "a", "b"},
		map[any]any{"total_results": int64(3), "results": []any{map[any]any{"id": "a"}, map[any]any{"id": "b"}}, "warning": []any{}},
	} {
		n, keys, err := parseBoundedSearch(reply)
		if err != nil || n != 3 || !slices.Equal(keys, []string{"a", "b"}) {
			t.Fatalf("%d %v %v", n, keys, err)
		}
	}
	for _, reply := range []any{[]any{int64(-1)}, []any{int64(1), 12}, map[any]any{"total_results": int64(1), "results": []any{}, "warning": []any{"timeout"}}} {
		if _, _, err := parseBoundedSearch(reply); err == nil {
			t.Fatalf("accepted malformed/incomplete reply: %v", reply)
		}
	}
}

// Tests only use the disposable deployment selected by REDIS_FLEX_URI.
func TestFlexStopgap(t *testing.T) {
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Skip("set REDIS_FLEX_URI to a disposable Flex database")
	}
	for _, protocol := range []string{"3"} {
		t.Run("RESP"+protocol, func(t *testing.T) {
			sep := "?"
			if strings.Contains(uri, "?") {
				sep = "&"
			}
			ctx := context.Background()
			db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{adbc.OptionKeyURI: uri + sep + "protocol=" + protocol, OptionStringFlex: "true"})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(ctx)
			conn, err := db.Open(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(ctx)
			h := &sqlHarness{t: t, ctx: ctx, conn: conn}
			name := "it_flex_stopgap_" + protocol
			h.exec("DROP TABLE IF EXISTS " + name)
			h.exec("CREATE TABLE " + name + " (n BIGINT, d DECIMAL(20,2), s VARCHAR)")
			defer h.exec("DROP TABLE IF EXISTS " + name)
			h.exec("INSERT INTO " + name + " VALUES (10, 12.34, 'ten'), (2, NULL, 'two'), (NULL, 0.01, NULL), (9007199254740993, 9999999999999999.99, 'large')")
			h.expectRows("SELECT n FROM "+name+" WHERE n >= 2 ORDER BY n", "2", "10", "9007199254740993")
			h.expectRows("SELECT s FROM "+name+" ORDER BY n NULLS LAST LIMIT 2", "two", "ten")
			h.expectRows("SELECT count(*), count(n), min(n), max(n) FROM "+name, "4|3|2|9007199254740993")
			h.expectRows("SELECT s, count(*) FROM "+name+" WHERE n = 2 GROUP BY s", "two|1")
			h.expectRows("SELECT d FROM "+name+" WHERE n = 9007199254740993", "9999999999999999.99")
			h.exec("UPDATE " + name + " SET s = 'updated' WHERE n = 2")
			h.expectRows("SELECT s FROM "+name+" WHERE n = 2", "updated")
			h.expectRows("SELECT n FROM "+name+" WHERE s = 'updated'", "2")
			h.exec("DELETE FROM " + name + " WHERE n = 10")
			h.expectRows("SELECT count(*) FROM "+name, "3")
			for _, sql := range []string{"ALTER TABLE " + name + " ADD COLUMN x INT"} {
				st, _ := conn.NewStatement(ctx)
				_ = st.SetSqlQuery(ctx, sql)
				_, err := st.ExecuteUpdate(ctx)
				st.Close(ctx)
				var ae adbc.Error
				if !errors.As(err, &ae) || ae.Code != adbc.StatusNotImplemented {
					t.Fatalf("%s: %v", sql, err)
				}
			}
			h.exec("CREATE TABLE " + name + "_bulk (n BIGINT, f DOUBLE PRECISION)")
			defer h.exec("DROP TABLE IF EXISTS " + name + "_bulk")
			h.exec("INSERT INTO " + name + "_bulk SELECT i, NULL FROM generate_series(1,2500) AS g(i)")
			h.expectRows("SELECT count(*), sum(n), min(n), max(n) FROM "+name+"_bulk", "2500|3126250|1|2500")
			h.expectRows("SELECT n FROM "+name+"_bulk ORDER BY n DESC LIMIT 3", "2500", "2499", "2498")
			h.expectRows("SELECT n FROM "+name+"_bulk LIMIT 3 OFFSET 2", "3", "4", "5")
			h.exec("INSERT INTO " + name + "_bulk VALUES (2501, 'NaN'), (2502, 'Infinity'), (2503, '-Infinity')")
			h.expectRows("SELECT n FROM "+name+"_bulk WHERE f = 'NaN'::double precision", "2501")
			h.expectRows("SELECT n FROM "+name+"_bulk WHERE f IS NOT NULL ORDER BY f", "2503", "2502", "2501")
			opts, _ := goredis.ParseURL(uri)
			raw := goredis.NewClient(opts)
			defer raw.Close()
			s := &store{client: raw, flex: true}
			meta, err := s.getTable(ctx, defaultSchema, name)
			if err != nil {
				t.Fatal(err)
			}
			// A reader captures its high-water mark once. Later inserts are
			// excluded, while an update in an unread range is visible.
			req := &aggRequest{meta: meta, index: meta.index(), query: "*", loadAll: true}
			live, err := s.openRangeReader(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			live.bounded.page = 2
			first, err := live.next(ctx)
			if err != nil || len(first) != 1 || first[0][rowIDField] != "2" {
				t.Fatalf("first range: %v %v", first, err)
			}
			h.exec("INSERT INTO " + name + " VALUES (99, NULL, 'later')")
			h.exec("UPDATE " + name + " SET s = 'changed' WHERE n = 9007199254740993")
			var rest []string
			for {
				rows, err := live.next(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if row != nil {
						rest = append(rest, row[rowIDField])
						if row[rowIDField] == "4" && row["s"] != "changed" {
							t.Fatal("unread update was lost")
						}
					}
				}
			}
			if !slices.Equal(rest, []string{"3", "4"}) {
				t.Fatalf("high-water boundary changed: %v", rest)
			}
			h.exec("DELETE FROM " + name + " WHERE n = 99")
			// Force small-page splitting and a sparse, exact high-water boundary.
			_ = raw.Set(ctx, seqKey(defaultSchema, name), exactDoubleLimit, 0).Err()
			cur, err := s.openRangeReader(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			cur.bounded.page = 2
			var ids []string
			for {
				rows, err := cur.next(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if row != nil {
						ids = append(ids, row[rowIDField])
					}
				}
			}
			if !slices.Equal(ids, []string{"2", "3", "4"}) {
				t.Fatalf("split scan missing/duplicate/out-of-order: %v", ids)
			}
			cur, err = s.openRangeReader(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := cur.next(canceled); err == nil {
				t.Fatal("ignored cancellation")
			}
			cur.close(ctx)
			if _, err := cur.next(ctx); !errors.Is(err, io.EOF) {
				t.Fatalf("read after close: %v", err)
			}
			_ = raw.Set(ctx, seqKey(defaultSchema, name), exactDoubleLimit+1, 0).Err()
			if _, err := s.openRangeReader(ctx, req); err == nil {
				t.Fatal("accepted imprecise row IDs")
			}
			// Unrelated keys, including nested keys, are not driver rows.
			unrelated := []string{meta.prefix() + "preferences", meta.prefix() + "2:preferences", "issue322-unrelated:" + protocol}
			for _, key := range unrelated {
				if err := raw.HSet(ctx, key, "value", "keep").Err(); err != nil {
					t.Fatal(err)
				}
			}
			defer raw.Del(ctx, unrelated...)
			h.exec("DROP TABLE " + name)
			for _, key := range unrelated {
				if n, err := raw.Exists(ctx, key).Result(); err != nil || n != 1 {
					t.Fatalf("DROP removed unrelated key: %s (%v)", key, err)
				}
			}
			if err := raw.Del(ctx, unrelated...).Err(); err != nil {
				t.Fatal(err)
			}

			ks, err := (&hashConn{client: raw}).scanKeys(ctx, meta.prefix(), "hash")
			if err != nil {
				t.Fatal(err)
			}
			keys, _, err := ks.next(ctx)
			if err != nil || len(keys) > 0 {
				t.Fatalf("DROP left documents: %v %v", keys, err)
			}
		})
	}
}

// Simulate a server whose result cap is lower than the requested page. The
// count describes the whole range, so the reader must split before emitting.
type flexReadHook struct {
	calls   int
	fail    bool
	warning bool
}

func (h *flexReadHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *flexReadHook) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() != "ft.search" {
			return fmt.Errorf("unexpected command %s", cmd.Name())
		}
		h.calls++
		if h.fail {
			return errors.New("injected read failure")
		}
		var lo, hi int64
		if _, err := fmt.Sscanf(fmt.Sprint(cmd.Args()[2]), "@__rowid:[%d %d]", &lo, &hi); err != nil {
			return err
		}
		ids := []int64{1, 2, 4, 10, 20}
		reply := []any{int64(0)}
		var count int64
		for _, id := range ids {
			if id >= lo && id <= hi {
				count++
				if len(reply) <= 2 {
					reply = append(reply, fmt.Sprint("rows:", id))
				}
			}
		}
		reply[0] = count
		if h.warning {
			cmd.(interface{ SetVal(any) }).SetVal(map[any]any{"total_results": count, "results": []any{}, "warning": []any{"timeout"}})
		} else {
			cmd.(interface{ SetVal(any) }).SetVal(reply)
		}
		return nil
	}
}
func (h *flexReadHook) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if cmd.Name() != "hmget" {
				return fmt.Errorf("unexpected pipeline command %s", cmd.Name())
			}
			id := strings.TrimPrefix(fmt.Sprint(cmd.Args()[1]), "rows:")
			vals := make([]any, len(cmd.Args())-2)
			for i := range vals {
				if fmt.Sprint(cmd.Args()[i+2]) == rowIDField {
					vals[i] = id
				}
			}
			cmd.(*goredis.SliceCmd).SetVal(vals)
		}
		return nil
	}
}
func TestFlexSplitsServerCapsAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"cap", "error", "warning"} {
		t.Run(mode, func(t *testing.T) {
			client := goredis.NewClient(&goredis.Options{})
			defer client.Close()
			hook := &flexReadHook{fail: mode == "error", warning: mode == "warning"}
			client.AddHook(hook)
			req := &aggRequest{meta: &tableMeta{KeyPrefix: "rows:"}, index: "index", query: "*"}
			r := &rangeReader{store: &store{client: client, flex: true}, req: req, ranges: []idRange{{1, 20}}, page: 10}
			var ids []string
			for {
				rows, err := r.next(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if mode != "cap" {
					if err == nil {
						t.Fatal("returned partial success")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					ids = append(ids, row[rowIDField])
				}
			}
			if !slices.Equal(ids, []string{"1", "2", "4", "10", "20"}) || hook.calls <= 1 {
				t.Fatalf("incomplete split read: %v, calls %d", ids, hook.calls)
			}
		})
	}
}

type flexRejectProbeHook struct {
	reason string
	calls  int
}

func (h *flexRejectProbeHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *flexRejectProbeHook) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(context.Context, []goredis.Cmder) error { return errors.New("unexpected metadata pipeline") }
}
func (h *flexRejectProbeHook) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		h.calls++
		if cmd.Name() != "ft.create" {
			return fmt.Errorf("unexpected command %s", cmd.Name())
		}
		return errors.New(h.reason)
	}
}
func TestFlexRejectsUnsupportedCapabilities(t *testing.T) {
	for _, reason := range []string{"NUMERIC is not supported in Redis Flex", "INDEXEMPTY is unsupported", "NOPERM index creation denied", "SEARCH_DISK_CREATION write-buffer budget exceeds Search disk maximum memory"} {
		client := goredis.NewClient(&goredis.Options{})
		hook := &flexRejectProbeHook{reason: reason}
		client.AddHook(hook)
		err := (&store{client: client}).configureSearch(context.Background(), true)
		client.Close()
		var ae adbc.Error
		expected := adbc.StatusNotImplemented
		if strings.HasPrefix(reason, "NOPERM") || strings.HasPrefix(reason, "SEARCH_DISK_CREATION") {
			expected = adbc.StatusIO
		}
		if !errors.As(err, &ae) || ae.Code != expected || hook.calls != 1 {
			t.Fatalf("%s: %v, calls %d", reason, err, hook.calls)
		}
	}
}

// Retained execution uses an independently budgeted transport but must retain
// the producer's schema capabilities; otherwise getTable rejects PlainIndex.
func TestFlexRetainedExecutorMode(t *testing.T) {
	for _, flex := range []bool{false, true} {
		client := goredis.NewClient(&goredis.Options{Addr: "localhost:1"})
		base := &executor{store: &store{client: client, flex: flex}}
		cfg := &clientConfig{single: &goredis.Options{Addr: "localhost:1"}}
		_, exec, capture, close, err := newRetainedExecutor(context.Background(), base, cfg, timeouts{}, defaultRetainedExecutionLimits())
		if err != nil {
			t.Fatal(err)
		}
		if exec.store.flex != flex || capture.capture.store.flex != flex {
			t.Fatal("retained coordinator lost producer capabilities")
		}
		close()
		client.Close()
	}
}

func TestFlexTruncateOwnership(t *testing.T) {
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Skip("set REDIS_FLEX_URI to a disposable Flex database")
	}
	t.Setenv("REDIS_URI", uri)
	h, c := flexBenchConn(t, true)
	name := "it_flex_truncate"
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT)")
	defer h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("INSERT INTO " + name + " VALUES (1),(2),(3)")
	meta, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := meta.prefix() + "application"
	if err := c.store.client.HSet(h.ctx, unrelated, "n", "keep").Err(); err != nil {
		t.Fatal(err)
	}
	defer c.store.client.Del(h.ctx, unrelated)
	h.exec("TRUNCATE " + name + " RESTART IDENTITY")
	h.expectRows("SELECT count(*) FROM "+name, "0")
	h.exec("INSERT INTO " + name + " VALUES (4)")
	h.expectRows("SELECT n FROM "+name, "4")
	current, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	if current.prefix() == meta.prefix() {
		t.Fatal("TRUNCATE reused source prefix")
	}
	if n, err := c.store.client.Exists(h.ctx, meta.prefix()+"1", meta.prefix()+"2", meta.prefix()+"3").Result(); err != nil || n != 0 {
		t.Fatal("old rows leaked", n, err)
	}
	if got, err := c.store.client.HGet(h.ctx, unrelated, "n").Result(); err != nil || got != "keep" {
		t.Fatal("unrelated HASH changed", got, err)
	}
	if last, err := c.store.rowSequence(h.ctx, current); err != nil || last != 1 {
		t.Fatal("identity restart failed", last, err)
	}
}

func TestFlexRekeyRollbackRows(t *testing.T) {
	uri := os.Getenv("REDIS_FLEX_URI")
	if uri == "" {
		t.Skip("set REDIS_FLEX_URI to a disposable Flex database")
	}
	t.Setenv("REDIS_URI", uri)
	h, c := flexBenchConn(t, true)
	name := "it_flex_rollback_rows"
	h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("CREATE TABLE " + name + " (n BIGINT)")
	defer h.exec("DROP TABLE IF EXISTS " + name)
	h.exec("INSERT INTO " + name + " VALUES (1),(2),(3)")
	before, err := c.store.getTable(h.ctx, defaultSchema, name)
	if err != nil {
		t.Fatal(err)
	}
	job, start, err := c.store.beginRekey(h.ctx, defaultSchema, name, name+"2")
	if err != nil {
		t.Fatal(err)
	}
	defer job.lease.release()
	if err := c.store.moveRows(h.ctx, job, start, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := c.store.rollbackRekey(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 3; id++ {
		key := fmt.Sprint(before.prefix(), id)
		row, err := c.store.client.HGetAll(h.ctx, key).Result()
		if err != nil || row[rowIDField] != fmt.Sprint(id) || row["n"] != fmt.Sprint(id) {
			t.Fatalf("source HASH changed after rollback: row %d: %v, %v", id, row, err)
		}
	}
	h.expectRows("SELECT n FROM "+name+" ORDER BY n", "1", "2", "3")
}
