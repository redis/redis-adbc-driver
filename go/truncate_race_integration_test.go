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

// A table truncated while another statement is still writing to it (#104):
// TRUNCATE publishes fresh immutable storage. Stale private writers and DDL
// refuse publication; old readers retain their generation until release.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

func (h *sqlHarness) setupTruncRace() {
	h.t.Helper()
	h.dropTables("it_tr_src", "it_tr_t")
	h.exec(fmt.Sprintf(`CREATE TABLE it_tr_src AS SELECT x AS id, 'old' AS note FROM generate_series(1, %d) AS g(x)`, srRows))
}

// truncModes are the TRUNCATE variants the races run with.
var truncModes = []struct {
	name    string
	clause  string
	restart bool
}{
	{"continue", "", false},
	{"restart", " RESTART IDENTITY", true},
}

// The repro of #104 at a real acknowledged native batch. A private writer's
// reserved IDs do not advance the winning catalog's committed identity counter.
func TestSQLTruncateDuringInsertSelect(t *testing.T) {
	for _, m := range truncModes {
		t.Run(m.name, func(t *testing.T) {
			h := newSQLHarness(t)
			h.setupTruncRace()
			raw := h.rawClient()
			other := newSQLHarness(t)
			h.exec("CREATE TABLE it_tr_t (id BIGINT,note VARCHAR)")
			h.exec("INSERT INTO it_tr_t VALUES (-1,'base'),(0,NULL)")
			st, oldCtx, pin := h.inspectionSnapshot(raw)
			defer pin.release()
			old, err := st.getTable(oldCtx, defaultSchema, "it_tr_t")
			if err != nil {
				t.Fatal(err)
			}
			var winner *tableMeta
			r := h.raceNativeRows(raw, "it_tr_t", func(*tableMeta) {
				other.exec("TRUNCATE TABLE it_tr_t" + m.clause)
				winner = other.inspectTable(raw, defaultSchema, "it_tr_t")
				other.expectRows("SELECT COUNT(*) FROM it_tr_t", "0")
			})
			_, err = tryExec(h.ctx, h.conn, "INSERT INTO it_tr_t SELECT id,note FROM it_tr_src")
			h.expectNativeRowRefusal(raw, r, err)
			if winner == nil || winner.prefix() == old.prefix() || winner.prefix() == r.meta.prefix() {
				t.Fatal("TRUNCATE did not publish fresh independent storage")
			}
			if got := strings.Join(valueRows(snapshotExecuteTest(t, st, oldCtx,
				"SELECT __rowid,id,note FROM it_tr_t ORDER BY __rowid").rows), ";"); got != "1|-1|base;2|0|NULL" {
				t.Fatal("TRUNCATE changed pinned input", got)
			}
			first := 3
			if m.restart {
				first = 1
			}
			h.exec("INSERT INTO it_tr_t (id) SELECT id FROM it_tr_src")
			rows, _ := h.query("SELECT __rowid,id,note FROM it_tr_t ORDER BY __rowid")
			if len(rows) != srRows {
				t.Fatal("incomplete fresh rows", len(rows))
			}
			for i, row := range rows {
				if row != fmt.Sprintf("%d|%d|NULL", first+i, i+1) {
					t.Fatal("fresh write inherited refused values or identity", i, row)
				}
			}
			pin.release()
			for _, seg := range old.snapshotSegments() {
				h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
			}
		})
	}
}

// An unpublished CTAS is invisible. A competing create plus TRUNCATE wins;
// the private CTAS must conflict rather than append to that winning definition.
func TestSQLTruncateDuringCreateTableAs(t *testing.T) {
	for _, m := range truncModes {
		t.Run(m.name, func(t *testing.T) {
			h := newSQLHarness(t)
			h.setupTruncRace()
			raw := h.rawClient()
			other := newSQLHarness(t)
			r := h.raceNativeRows(raw, "it_tr_t", func(*tableMeta) {
				if _, err := other.conn.GetTableSchema(h.ctx, nil, nil, "it_tr_t"); !isStatus(err, adbc.StatusNotFound) {
					t.Fatal("unpublished CTAS became visible", err)
				}
				other.exec("CREATE TABLE it_tr_t (id BIGINT,note VARCHAR)")
				other.exec("INSERT INTO it_tr_t VALUES (-1,'winner'),(0,NULL)")
				other.exec("TRUNCATE it_tr_t" + m.clause)
			})
			_, err := tryExec(h.ctx, h.conn, "CREATE TABLE it_tr_t AS SELECT id,note FROM it_tr_src")
			h.expectNativeRowRefusal(raw, r, err)
			first := 3
			if m.restart {
				first = 1
			}
			h.expectNativeTruncated(raw, r, defaultSchema, "it_tr_t", first)
		})
	}
}

// Check all fresh rows, including NULLs and exact physical identity sequencing.
func (h *sqlHarness) expectNativeTruncated(raw goredis.UniversalClient, r *nativeRowRace, schema, table string, first int) {
	h.t.Helper()
	h.expectRows("SELECT COUNT(*) FROM "+table, "0")
	current := h.inspectTable(raw, schema, strings.TrimPrefix(table, "pg_temp."))
	if current.prefix() == r.meta.prefix() {
		h.t.Fatal("TRUNCATE inherited private output")
	}
	h.exec("INSERT INTO " + table + " (id) SELECT id FROM it_tr_src")
	rows, _ := h.query("SELECT __rowid,id,note FROM " + table + " ORDER BY __rowid")
	if len(rows) != srRows {
		h.t.Fatal("incomplete fresh rows", len(rows))
	}
	for i, row := range rows {
		if row != fmt.Sprintf("%d|%d|NULL", first+i, i+1) {
			h.t.Fatal("fresh write inherited refused values or identity", i, row)
		}
	}
}

// Both Arrow create and append conflict after a real acknowledged batch.
// Small records also exercise the record boundary before a full native batch.
func TestSQLTruncateDuringIngest(t *testing.T) {
	for _, mode := range []string{adbc.OptionValueIngestModeCreate, adbc.OptionValueIngestModeAppend} {
		for _, size := range []int{srRows, snapshotWriteBatchRows / 2} {
			for _, m := range truncModes {
				t.Run(fmt.Sprintf("%s/%d/%s", mode, size, m.name), func(t *testing.T) {
					h := newSQLHarness(t)
					h.setupTruncRace()
					raw := h.rawClient()
					other := newSQLHarness(t)
					if mode == adbc.OptionValueIngestModeAppend {
						h.exec("CREATE TABLE it_tr_t (id BIGINT,note VARCHAR)")
						h.exec("INSERT INTO it_tr_t VALUES (-1,'base'),(0,NULL)")
					}
					r := h.raceNativeRowsAt(raw, "it_tr_t", min(size, snapshotWriteBatchRows), func(*tableMeta) {
						if mode == adbc.OptionValueIngestModeCreate {
							if _, err := other.conn.GetTableSchema(h.ctx, nil, nil, "it_tr_t"); !isStatus(err, adbc.StatusNotFound) {
								t.Fatal("private Arrow create became visible", err)
							}
							other.exec("CREATE TABLE it_tr_t (id BIGINT,note VARCHAR)")
							other.exec("INSERT INTO it_tr_t VALUES (-1,'winner'),(0,NULL)")
						}
						other.exec("TRUNCATE it_tr_t" + m.clause)
					})
					err := h.ingestNotes("it_tr_t", mode, size, size)
					h.expectNativeRowRefusal(raw, r, err)
					first := 3
					if m.restart {
						first = 1
					}
					h.expectNativeTruncated(raw, r, defaultSchema, "it_tr_t", first)
				})
			}
		}
	}
}

// A temporary CTAS stays invisible to its own connection until publication.
// A nested create/TRUNCATE of the same name wins without inheriting private rows.
func TestSQLTruncateDuringTempCreateTableAs(t *testing.T) {
	for _, m := range truncModes {
		t.Run(m.name, func(t *testing.T) {
			h := newSQLHarness(t)
			h.setupTruncRace()
			raw := h.rawClient()
			r := h.raceNativeRows(raw, "it_tr_tmp", func(meta *tableMeta) {
				if !isTempSchema(meta.Schema) {
					t.Fatal("temporary CTAS has no owner-local schema")
				}
				if _, err := h.conn.GetTableSchema(h.ctx, nil, nil, "it_tr_tmp"); !isStatus(err, adbc.StatusNotFound) {
					t.Fatal("private temporary CTAS became visible", err)
				}
				h.exec("CREATE TEMP TABLE it_tr_tmp (id BIGINT,note VARCHAR)")
				h.exec("INSERT INTO pg_temp.it_tr_tmp VALUES (-1,'winner'),(0,NULL)")
				h.exec("TRUNCATE pg_temp.it_tr_tmp" + m.clause)
			})
			_, err := tryExec(h.ctx, h.conn, "CREATE TEMP TABLE it_tr_tmp AS SELECT id,note FROM it_tr_src")
			h.expectNativeRowRefusal(raw, r, err)
			first := 3
			if m.restart {
				first = 1
			}
			h.expectNativeTruncated(raw, r, r.meta.Schema, "pg_temp.it_tr_tmp", first)
		})
	}
}

// Even a single-row private write must validate publication after a competing
// RESTART IDENTITY. It cannot succeed into abandoned storage or consume new IDs.
func TestSQLTruncateRestartLateWrite(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_tr_late")
	raw := h.rawClient()
	other := newSQLHarness(t)
	h.exec("CREATE TABLE it_tr_late (id BIGINT,note VARCHAR)")
	h.exec("INSERT INTO it_tr_late VALUES (1,'a'),(2,'b')")
	st, oldCtx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	old, err := st.getTable(oldCtx, defaultSchema, "it_tr_late")
	if err != nil {
		t.Fatal(err)
	}
	r := h.raceNativeRowsAt(raw, "it_tr_late", 1, func(*tableMeta) {
		other.exec("TRUNCATE it_tr_late RESTART IDENTITY")
	})
	_, err = tryExec(h.ctx, h.conn, "INSERT INTO it_tr_late VALUES (3,'old')")
	h.expectNativeRowRefusal(raw, r, err)
	if got := strings.Join(valueRows(snapshotExecuteTest(t, st, oldCtx,
		"SELECT __rowid,id,note FROM it_tr_late ORDER BY __rowid").rows), ";"); got != "1|1|a;2|2|b" {
		t.Fatal("late writer changed pinned input", got)
	}
	h.exec("INSERT INTO it_tr_late (id) VALUES (10),(11),(12),(13)")
	h.expectRows("SELECT __rowid,id,note FROM it_tr_late ORDER BY __rowid",
		"1|10|NULL", "2|11|NULL", "3|12|NULL", "4|13|NULL")
	pin.release()
	for _, seg := range old.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
}

// A stale TRUNCATE cannot discard an intervening ADD COLUMN. A new explicit
// request truncates the winning schema and its new field remains indexed.
func TestSQLTruncateDuringAddColumn(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_tr_alt")
	raw := h.rawClient()
	h.exec("CREATE TABLE it_tr_alt (id BIGINT)")
	h.exec("INSERT INTO it_tr_alt VALUES (1),(2)")
	st := &store{client: raw, snapshots: true}
	ctx, pin, work := snapshotStageSQL(t, st, h.ctx, "", "TRUNCATE it_tr_alt")
	private, err := st.getTable(ctx, defaultSchema, "it_tr_alt")
	if err != nil {
		t.Fatal(err)
	}
	h.exec("ALTER TABLE it_tr_alt ADD COLUMN z INTEGER")
	h.expectStagedTruncateConflict(raw, st, ctx, pin, work)
	h.expectSnapshotReleased(raw, private.prefix(), private.index())
	h.expectRows("SELECT id,z FROM it_tr_alt ORDER BY id", "1|NULL", "2|NULL")
	h.exec("TRUNCATE it_tr_alt")
	if got := h.indexAttrs(raw, defaultSchema, "it_tr_alt"); got != "__rowid "+snapshotDeletedField+" id z" {
		t.Fatal("new column absent from fresh index", got)
	}
	h.exec("INSERT INTO it_tr_alt VALUES (3,5),(4,6)")
	h.expectRows("SELECT id FROM it_tr_alt WHERE z=5", "3")
}

// An ALTER built against pre-TRUNCATE rows conflicts; a fresh explicit ALTER
// builds on the committed empty generation without replaying the stale request.
func TestSQLAddColumnDuringTruncate(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_tr_alt2")
	raw := h.rawClient()
	h.exec("CREATE TABLE it_tr_alt2 (id BIGINT)")
	h.exec("INSERT INTO it_tr_alt2 VALUES (1),(2)")
	st := &store{client: raw, snapshots: true}
	ctx, pin, work := snapshotStageSQL(t, st, h.ctx, "", "ALTER TABLE it_tr_alt2 ADD COLUMN z INTEGER DEFAULT 7")
	private, err := st.getTable(ctx, defaultSchema, "it_tr_alt2")
	if err != nil {
		t.Fatal(err)
	}
	h.exec("TRUNCATE it_tr_alt2")
	h.expectStagedTruncateConflict(raw, st, ctx, pin, work)
	h.expectSnapshotReleased(raw, private.prefix(), private.index())
	h.expectRows("SELECT column_name FROM information_schema.columns WHERE table_name='it_tr_alt2'", "id")
	h.expectRows("SELECT COUNT(*) FROM it_tr_alt2", "0")
	h.exec("ALTER TABLE it_tr_alt2 ADD COLUMN z INTEGER DEFAULT 7")
	if got := h.indexAttrs(raw, defaultSchema, "it_tr_alt2"); got != "__rowid "+snapshotDeletedField+" id z" {
		t.Fatal("new column absent from fresh index", got)
	}
	h.exec("INSERT INTO it_tr_alt2 (id) VALUES (3),(4)")
	h.expectRows("SELECT id,z FROM it_tr_alt2 WHERE z=7 ORDER BY id", "3|7", "4|7")
}

// A multi-table TRUNCATE publishes all targets or none after a competing DROP.
func TestSQLTruncateAllOrNone(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_tr_a", "it_tr_b")
	raw := h.rawClient()
	h.exec("CREATE TABLE it_tr_a (id BIGINT)")
	h.exec("CREATE TABLE it_tr_b (id BIGINT)")
	h.exec("INSERT INTO it_tr_a VALUES (1),(2)")
	h.exec("INSERT INTO it_tr_b VALUES (3)")
	before := h.inspectTable(raw, defaultSchema, "it_tr_a")
	st := &store{client: raw, snapshots: true}
	ctx, pin, work := snapshotStageSQL(t, st, h.ctx, "", "TRUNCATE it_tr_a,it_tr_b")
	var private []*tableMeta
	for _, name := range []string{"it_tr_a", "it_tr_b"} {
		meta, err := st.getTable(ctx, defaultSchema, name)
		if err != nil {
			t.Fatal(err)
		}
		private = append(private, meta)
	}
	h.exec("DROP TABLE it_tr_b")
	h.expectStagedTruncateConflict(raw, st, ctx, pin, work)
	h.expectRows("SELECT id FROM it_tr_a ORDER BY id", "1", "2")
	h.expectError("SELECT * FROM it_tr_b", "does not exist")
	after := h.inspectTable(raw, defaultSchema, "it_tr_a")
	if after.prefix() != before.prefix() || after.index() != before.index() {
		t.Fatal("refused TRUNCATE changed first target storage")
	}
	for _, meta := range private {
		h.expectSnapshotReleased(raw, meta.prefix(), meta.index())
	}
}

// A DROP planned before TRUNCATE cannot remove its newer generation. The
// current binding survives until a fresh explicit DROP; old readers retain rows.
func TestSQLDropDuringTruncate(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_tr_drop")
	raw := h.rawClient()
	h.exec("CREATE TABLE it_tr_drop (id BIGINT)")
	h.exec("INSERT INTO it_tr_drop VALUES (1),(2)")
	reader, oldCtx, oldPin := h.inspectionSnapshot(raw)
	defer oldPin.release()
	old, err := reader.getTable(oldCtx, defaultSchema, "it_tr_drop")
	if err != nil {
		t.Fatal(err)
	}
	st := &store{client: raw, snapshots: true}
	ctx, pin, work := snapshotStageSQL(t, st, h.ctx, "", "DROP TABLE it_tr_drop")
	h.exec("TRUNCATE it_tr_drop")
	h.exec("INSERT INTO it_tr_drop VALUES (3)")
	current := h.inspectTable(raw, defaultSchema, "it_tr_drop")
	if current.prefix() == old.prefix() {
		t.Fatal("TRUNCATE reused old storage")
	}
	h.expectStagedTruncateConflict(raw, st, ctx, pin, work)
	h.expectRows("SELECT id FROM it_tr_drop", "3")
	if got := strings.Join(valueRows(snapshotExecuteTest(t, reader, oldCtx, "SELECT id FROM it_tr_drop ORDER BY id").rows), "|"); got != "1|2" {
		t.Fatal("stale DROP changed old reader", got)
	}
	h.exec("DROP TABLE it_tr_drop")
	h.expectError("SELECT * FROM it_tr_drop", "does not exist")
	for _, seg := range current.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
	oldPin.release()
	for _, seg := range old.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
}

func (h *sqlHarness) expectStagedTruncateConflict(raw goredis.UniversalClient, st *store, ctx context.Context, pin *snapshotPin, work string) {
	h.t.Helper()
	root, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result()
	if err != nil {
		h.t.Fatal(err)
	}
	if err := st.publishSnapshot(ctx, pin, work); !isStatus(err, adbc.StatusIO) || sqlState(err) != "40001" {
		h.t.Fatal("stale DDL did not refuse publication", err)
	}
	if got, err := raw.HGet(h.ctx, snapshotCatalogKey, "root").Result(); err != nil || got != root {
		h.t.Fatal("refused DDL changed winning root", err)
	}
	st.abortSnapshot(pin, work)
	pin.release()
}
