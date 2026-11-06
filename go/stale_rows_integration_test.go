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

// A table dropped while another statement is still writing to it (#82):
// the writer fails, deletes what it wrote, and a new table of the same name
// gets new keys, so it never reads the dropped table's rows.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

// srRows keeps racing writes large enough to cross multiple native batches.
const srRows = 3 * pipelineChunk

// namesN checks that a table's key prefix and index name are those of
// the name named with some N (namesFor), and returns N. N depends on the
// names earlier runs used, since they are never reused.
func (h *sqlHarness) namesN(raw goredis.UniversalClient, schema, table, named string) int64 {
	h.t.Helper()
	prefix, index := h.tableNames(raw, schema, table)
	base := rowPrefix(schema, named)
	n := int64(1)
	if prefix != base {
		digits, ok := strings.CutPrefix(prefix, strings.TrimSuffix(base, ":")+"~")
		digits, ok2 := strings.CutSuffix(digits, ":")
		var err error
		if n, err = strconv.ParseInt(digits, 10, 64); !ok || !ok2 || err != nil || n < 2 {
			h.t.Fatalf("table %s.%s: key prefix %q is not one of %q", schema, table, prefix, named)
		}
	}
	if want := namesFor(schema, named, n); prefix != want.prefix || index != want.index {
		h.t.Fatalf("table %s.%s: names %q, %q, want %q, %q", schema, table, prefix, index, want.prefix, want.index)
	}
	return n
}

func (h *sqlHarness) setupStaleRows() {
	h.t.Helper()
	h.dropTables("it_sr_src", "it_sr_t")
	h.exec(fmt.Sprintf(`CREATE TABLE it_sr_src AS SELECT x AS id, 'old' AS note FROM generate_series(1, %d) AS g(x)`, srRows))
}

// Native races run synchronously after real acknowledged row writes. The
// callback's statements do not inherit this hook or mutate the private layout.
type nativeRowRace struct {
	meta      *tableMeta
	root      string
	batches   int
	firstRows int
	rows      int64
}

func (h *sqlHarness) raceNativeRows(raw goredis.UniversalClient, table string, change func(*tableMeta)) *nativeRowRace {
	h.t.Helper()
	return h.raceNativeRowsAt(raw, table, snapshotWriteBatchRows, change)
}

func (h *sqlHarness) raceNativeRowsAt(raw goredis.UniversalClient, table string, firstRows int, change func(*tableMeta)) *nativeRowRace {
	h.t.Helper()
	return h.raceNativeImagesAt(raw, table, firstRows, "note", "old", change)
}

func (h *sqlHarness) raceNativeImagesAt(raw goredis.UniversalClient, table string, firstRows int, field, want string, change func(*tableMeta)) *nativeRowRace {
	h.t.Helper()
	r := &nativeRowRace{firstRows: firstRows}
	var hook func(*tableMeta, int)
	hook = func(meta *tableMeta, written int) {
		if meta.Name != table {
			return
		}
		count, err := raw.ZCard(h.ctx, snapshotMembers(snapshotDataID(meta.prefix()))).Result()
		if err != nil {
			h.t.Fatal(err)
		}
		r.batches++
		r.rows = count
		if r.meta != nil {
			return
		}
		r.meta = cloneMeta(meta)
		if written != firstRows || count != int64(firstRows) {
			h.t.Fatal("race missed first acknowledged native batch", written, count)
		}
		data := snapshotDataID(meta.prefix())
		if data == "" || meta.prefix() != snapshotRows(data, meta.Schema, meta.SnapshotAccessID) {
			h.t.Fatal("race output is not canonical private storage")
		}
		// These are exact private images, not partially written published HASHes.
		members, err := raw.ZRange(h.ctx, snapshotMembers(data), 0, -1).Result()
		if err != nil || len(members) != firstRows {
			h.t.Fatal("private directory differs", err)
		}
		for _, member := range []string{members[0], members[len(members)-1]} {
			id, err := strconv.ParseInt(member, 10, 64)
			if err != nil {
				h.t.Fatal(err)
			}
			if value, err := raw.HGet(h.ctx, meta.prefix()+strconv.FormatInt(id, 10), field).Result(); err != nil || value != want {
				h.t.Fatal("acknowledged private image missing", err)
			}
		}
		testHookRowsWritten = nil
		defer func() { testHookRowsWritten = hook }()
		change(meta)
		r.root, err = raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
		if err != nil {
			h.t.Fatal(err)
		}
	}
	testHookRowsWritten = hook
	h.t.Cleanup(func() { testHookRowsWritten = nil })
	return r
}

func (h *sqlHarness) expectNativeRowRefusal(raw goredis.UniversalClient, r *nativeRowRace, err error) {
	h.t.Helper()
	testHookRowsWritten = nil
	if r.meta == nil || r.batches == 0 || r.firstRows <= 0 || r.rows < int64(r.firstRows) {
		h.t.Fatal("native race never ran")
	}
	if !isStatus(err, adbc.StatusIO) || sqlState(err) != "40001" {
		h.t.Fatal("stale writer did not refuse publication", err)
	}
	if root, readErr := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); readErr != nil || root != r.root {
		h.t.Fatal("refused writer changed committed root", readErr)
	}
	h.expectSnapshotReleased(raw, r.meta.prefix(), r.meta.index())
	indexes, e := raw.Do(h.ctx, "FT._LIST").StringSlice()
	if e != nil {
		h.t.Fatal(e)
	}
	for _, index := range indexes {
		if snapshotDataID(index) == snapshotDataID(r.meta.prefix()) {
			h.t.Fatal("refused output retained a final index")
		}
	}
}

func (h *sqlHarness) expectNativeReplacement(raw goredis.UniversalClient, r *nativeRowRace, table string) {
	h.t.Helper()
	h.expectRows("SELECT id,note FROM "+table, "999999|replacement")
	schema := defaultSchema
	name := table
	if strings.HasPrefix(table, "pg_temp.") {
		schema = r.meta.Schema
		name = strings.TrimPrefix(table, "pg_temp.")
	}
	current := h.inspectTable(raw, schema, name)
	if current.prefix() == r.meta.prefix() {
		h.t.Fatal("replacement inherited refused storage")
	}
	h.exec("DROP TABLE " + table)
	h.exec("CREATE TABLE " + table + " (id BIGINT,note VARCHAR)")
	h.exec("INSERT INTO " + table + " (id) SELECT id FROM it_sr_src")
	rows, _ := h.query("SELECT id,note FROM " + table + " ORDER BY id")
	if len(rows) != srRows {
		h.t.Fatal("incomplete fresh replacement", len(rows))
	}
	for i, row := range rows {
		if row != fmt.Sprintf("%d|NULL", i+1) {
			h.t.Fatal("fresh binding read refused values", i, row)
		}
	}
	h.expectRows("SELECT COUNT(*) FROM "+table+" WHERE note='old'", "0")
}

// A private CTAS is invisible. A competing create/drop/recreate wins its name;
// the old CTAS must refuse instead of exposing any of its copied rows.
func TestSQLDropDuringCreateTableAs(t *testing.T) {
	h := newSQLHarness(t)
	h.setupStaleRows()
	raw := h.rawClient()
	other := newSQLHarness(t)
	r := h.raceNativeRows(raw, "it_sr_t", func(*tableMeta) {
		if _, err := other.conn.GetTableSchema(h.ctx, nil, nil, "it_sr_t"); !isStatus(err, adbc.StatusNotFound) {
			t.Fatal("private CTAS became visible", err)
		}
		other.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
		other.exec("DROP TABLE it_sr_t")
		other.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
		other.exec("INSERT INTO it_sr_t VALUES (999999,'replacement')")
	})
	_, err := tryExec(h.ctx, h.conn, "CREATE TABLE it_sr_t AS SELECT id,note FROM it_sr_src")
	h.expectNativeRowRefusal(raw, r, err)
	h.expectNativeReplacement(raw, r, "it_sr_t")
}

func TestSQLDropDuringInsertSelect(t *testing.T) {
	h := newSQLHarness(t)
	h.setupStaleRows()
	raw := h.rawClient()
	other := newSQLHarness(t)
	h.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
	h.exec("INSERT INTO it_sr_t VALUES (0,'base')")
	source, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	old, err := source.getTable(oldCtx, defaultSchema, "it_sr_t")
	if err != nil {
		t.Fatal(err)
	}
	r := h.raceNativeRows(raw, "it_sr_t", func(*tableMeta) {
		other.exec("DROP TABLE it_sr_t")
		other.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
		other.exec("INSERT INTO it_sr_t VALUES (999999,'replacement')")
	})
	_, err = tryExec(h.ctx, h.conn, "INSERT INTO it_sr_t SELECT id,note FROM it_sr_src")
	h.expectNativeRowRefusal(raw, r, err)
	rows := snapshotExecuteTest(t, source, oldCtx, "SELECT id,note FROM it_sr_t").rows
	if got := strings.Join(valueRows(rows), "|"); got != "0|base" {
		t.Fatal("DROP changed pinned input", got)
	}
	oldPin.release()
	for _, seg := range old.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
	h.expectNativeReplacement(raw, r, "it_sr_t")
}

// ingestNotes bulk-ingests rows (id, 'old') in batches of the given sizes.
func (h *sqlHarness) ingestNotes(table, mode string, batches ...int) error {
	h.t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "note", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	var recs []arrow.RecordBatch
	next := int64(1)
	for _, n := range batches {
		b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
		for i := 0; i < n; i++ {
			b.Field(0).(*array.Int64Builder).Append(next)
			b.Field(1).(*array.StringBuilder).Append("old")
			next++
		}
		recs = append(recs, b.NewRecordBatch())
		b.Release()
	}
	rdr, err := array.NewRecordReader(schema, recs)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rdr.Release()
	for _, r := range recs {
		r.Release()
	}
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	defer st.Close(h.ctx)
	for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
		if err := st.SetOption(h.ctx, k, v); err != nil {
			h.t.Fatal(err)
		}
	}
	input := &snapshotLargeInput{RecordReader: rdr}
	if err := st.BindStream(h.ctx, input); err != nil {
		h.t.Fatal(err)
	}
	_, err = st.ExecuteUpdate(h.ctx)
	if input.records > len(batches) || input.next > len(batches)+1 {
		h.t.Fatal("ingest replayed input", input.records, input.next)
	}
	return err
}

// Both Arrow create and append conflict after an acknowledged private batch.
// The input spans two records, and the helper rejects repeated consumption.
func TestSQLDropDuringIngest(t *testing.T) {
	for _, mode := range []string{adbc.OptionValueIngestModeCreate, adbc.OptionValueIngestModeAppend} {
		t.Run(mode, func(t *testing.T) {
			h := newSQLHarness(t)
			h.setupStaleRows()
			raw := h.rawClient()
			other := newSQLHarness(t)
			if mode == adbc.OptionValueIngestModeAppend {
				h.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
			}
			r := h.raceNativeRows(raw, "it_sr_t", func(*tableMeta) {
				if mode == adbc.OptionValueIngestModeCreate {
					if _, err := other.conn.GetTableSchema(h.ctx, nil, nil, "it_sr_t"); !isStatus(err, adbc.StatusNotFound) {
						t.Fatal("private Arrow create became visible", err)
					}
					other.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
				}
				other.exec("DROP TABLE it_sr_t")
				other.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
				other.exec("INSERT INTO it_sr_t VALUES (999999,'replacement')")
			})
			err := h.ingestNotes("it_sr_t", mode, srRows, srRows)
			h.expectNativeRowRefusal(raw, r, err)
			h.expectNativeReplacement(raw, r, "it_sr_t")
		})
	}
	// Records smaller than a native batch still cannot resurrect a dropped
	// binding. No out-of-protocol HASH writes are injected into published input.
	t.Run("small records", func(t *testing.T) {
		h := newSQLHarness(t)
		h.setupStaleRows()
		raw := h.rawClient()
		other := newSQLHarness(t)
		h.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
		var private *tableMeta
		root := ""
		testHookRowsWritten = func(meta *tableMeta, written int) {
			if meta.Name != "it_sr_t" || private != nil {
				return
			}
			private = cloneMeta(meta)
			if written != snapshotWriteBatchRows/2 {
				t.Fatal("small record boundary differs", written)
			}
			testHookRowsWritten = nil
			other.exec("DROP TABLE it_sr_t")
			other.exec("CREATE TABLE it_sr_t (id BIGINT,note VARCHAR)")
			other.exec("INSERT INTO it_sr_t VALUES (999999,'replacement')")
			root = raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
		}
		t.Cleanup(func() { testHookRowsWritten = nil })
		err := h.ingestNotes("it_sr_t", adbc.OptionValueIngestModeAppend, snapshotWriteBatchRows/2, snapshotWriteBatchRows/2)
		testHookRowsWritten = nil
		if private == nil || sqlState(err) != "40001" {
			t.Fatal("small record writer did not conflict", err)
		}
		if raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() != root {
			t.Fatal("small record refusal changed root")
		}
		h.expectSnapshotReleased(raw, private.prefix(), private.index())
		h.expectNativeReplacement(raw, &nativeRowRace{meta: private}, "it_sr_t")
	})
}

// A temporary CTAS is private too. The owning connection may create, drop and
// recreate a competing binding without exposing or mutating the outer workspace.
func TestSQLDropDuringTempCreateTableAs(t *testing.T) {
	h := newSQLHarness(t)
	h.setupStaleRows()
	raw := h.rawClient()
	r := h.raceNativeRows(raw, "it_sr_tmp", func(meta *tableMeta) {
		if !isTempSchema(meta.Schema) {
			t.Fatal("temporary output has no private owner")
		}
		if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, "it_sr_tmp"); !isStatus(err, adbc.StatusNotFound) {
			t.Fatal("private temporary CTAS became visible", err)
		}
		h.exec("CREATE TEMP TABLE it_sr_tmp (id BIGINT,note VARCHAR)")
		h.exec("DROP TABLE pg_temp.it_sr_tmp")
		h.exec("CREATE TEMP TABLE it_sr_tmp (id BIGINT,note VARCHAR)")
		h.exec("INSERT INTO pg_temp.it_sr_tmp VALUES (999999,'replacement')")
	})
	_, err := tryExec(h.ctx, h.conn, "CREATE TEMP TABLE it_sr_tmp AS SELECT id,note FROM it_sr_src")
	h.expectNativeRowRefusal(raw, r, err)
	h.expectNativeReplacement(raw, r, "pg_temp.it_sr_tmp")
	h.exec("DROP TABLE pg_temp.it_sr_tmp")
}

// sameKeys tells a table's current metadata from another table's of the
// same name (no Redis needed).
func TestSameKeys(t *testing.T) {
	enc := func(m tableMeta) string {
		raw, err := json.Marshal(&m)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	mine := tableMeta{Schema: "public", Name: "t", KeyPrefix: "public:t~2:", IndexName: "idx:public:t~2",
		Columns: []columnMeta{{Name: "c", Type: typeString}}}
	other := mine
	other.KeyPrefix, other.IndexName = "public:t~3:", "idx:public:t~3"
	// A comment that spells out another prefix doesn't fool it.
	sneaky := other
	sneaky.Comment = `"key_prefix":"public:t~2:"`
	legacy := tableMeta{Schema: "public", Name: "t"}
	reused := legacy
	reused.PrefixGen = 1
	for _, c := range []struct {
		meta tableMeta
		raw  string
		want bool
	}{
		{mine, enc(mine), true},
		{mine, enc(other), false},
		{mine, enc(sneaky), false},
		{mine, "", false},
		{legacy, enc(legacy), true},
		{legacy, enc(reused), false},
		{reused, enc(reused), true},
		{mine, enc(legacy), false},
	} {
		if got := sameKeys(&c.meta, c.raw); got != c.want {
			t.Errorf("sameKeys(%q, %s) = %v, want %v", c.meta.prefix(), c.raw, got, c.want)
		}
	}
}

// Canonical physical identities are never reused across DROP/recreation or
// logical rename. This is native storage ownership, without a legacy migration.
func TestSQLKeyPrefixesNotReused(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_sr_names", "it_sr_names2")
	raw := h.rawClient()
	seen := map[string]bool{}
	create := func() {
		t.Helper()
		h.exec("CREATE TABLE it_sr_names (id INTEGER)")
		prefix, index := h.rekeyNativeLayout(raw, defaultSchema, "it_sr_names")
		if seen[prefix] || seen[index] {
			t.Fatal("recreated table reused physical identity")
		}
		seen[prefix], seen[index] = true, true
	}
	for i := 0; i < 3; i++ {
		create()
		h.exec("INSERT INTO it_sr_names VALUES (1)")
		h.exec("DROP TABLE it_sr_names")
	}
	create()
	prefix, index := h.rekeyNativeLayout(raw, defaultSchema, "it_sr_names")
	h.exec("ALTER TABLE it_sr_names RENAME TO it_sr_names2")
	if p, i := h.rekeyNativeLayout(raw, defaultSchema, "it_sr_names2"); p != prefix || i != index {
		t.Fatal("metadata-only rename changed physical identity")
	}
	create()
	h.expectRows("SELECT COUNT(*) FROM it_sr_names", "0")
	h.expectRows("SELECT COUNT(*) FROM it_sr_names2", "0")
}
