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

// Native UPDATE/MERGE replacement images and DELETE tombstones stay private.
// An intervening TRUNCATE or DROP/recreation must win over their stale writer.

import (
	"fmt"
	"strings"
	"testing"
)

var updateRaces = []struct {
	name  string
	empty bool
	sql   string
}{
	{"UPDATE", false, "UPDATE %[1]s SET note='new'"},
	{"UPDATE FROM", false, "UPDATE %[1]s t SET note='new' FROM %[2]s s WHERE t.id=s.id"},
	{"MERGE UPDATE", false, "MERGE INTO %[1]s t USING %[2]s s ON t.id=s.id WHEN MATCHED THEN UPDATE SET note='new'"},
	{"MERGE INSERT", true, "MERGE INTO %[1]s t USING %[2]s s ON t.id=s.id WHEN NOT MATCHED THEN INSERT (id,note) VALUES (s.id,'new')"},
}

var deleteRaces = []struct{ name, sql string }{
	{"DELETE", "DELETE FROM %[1]s"},
	{"MERGE DELETE", "MERGE INTO %[1]s t USING %[2]s s ON t.id=s.id WHEN MATCHED THEN DELETE"},
}

func TestSQLTruncateOrDropDuringUpdate(t *testing.T) {
	for _, w := range updateRaces {
		for _, mode := range []string{"continue", "restart", "drop"} {
			t.Run(w.name+"/"+mode, func(t *testing.T) { testNativeDMLRace(t, w.sql, w.empty, false, srRows, mode) })
		}
	}
}

func TestSQLTruncateOrDropDuringDelete(t *testing.T) {
	for _, w := range deleteRaces {
		for _, mode := range []string{"continue", "restart", "drop"} {
			for _, rows := range []int{snapshotWriteBatchRows / 2, srRows} {
				t.Run(fmt.Sprintf("%s/%s/%d", w.name, mode, rows), func(t *testing.T) { testNativeDMLRace(t, w.sql, false, true, rows, mode) })
			}
		}
	}
}

func testNativeDMLRace(t *testing.T, sql string, empty, deleted bool, rows int, mode string) {
	t.Helper()
	h := newSQLHarness(t)
	h.setupTruncRace()
	raw := h.rawClient()
	other := newSQLHarness(t)
	if empty {
		h.exec("CREATE TABLE it_tr_t (id BIGINT,note VARCHAR)")
	} else {
		h.exec(fmt.Sprintf("CREATE TABLE it_tr_t AS SELECT id,note FROM it_tr_src WHERE id<=%d", rows))
	}
	st, oldCtx, pin := h.inspectionSnapshot(raw)
	defer pin.release()
	old, err := st.getTable(oldCtx, defaultSchema, "it_tr_t")
	if err != nil {
		t.Fatal(err)
	}
	field, want, firstBatch := "note", "new", min(rows, pipelineChunk)
	if empty {
		firstBatch = min(rows, snapshotWriteBatchRows)
	}
	if deleted {
		field, want = snapshotDeletedField, "1"
	}
	r := h.raceNativeImagesAt(raw, "it_tr_t", firstBatch, field, want, func(*tableMeta) {
		switch mode {
		case "continue":
			other.exec("TRUNCATE it_tr_t")
		case "restart":
			other.exec("TRUNCATE it_tr_t RESTART IDENTITY")
		case "drop":
			other.exec("DROP TABLE it_tr_t")
			other.exec("CREATE TABLE it_tr_t (id BIGINT,note VARCHAR)")
			other.exec("INSERT INTO it_tr_t VALUES (999999,'replacement')")
		default:
			t.Fatal("unknown race mode", mode)
		}
	})
	_, err = tryExec(h.ctx, h.conn, fmt.Sprintf(sql, "it_tr_t", "it_tr_src"))
	h.expectNativeRowRefusal(raw, r, err)
	current := h.inspectTable(raw, defaultSchema, "it_tr_t")
	if current.prefix() == old.prefix() || current.prefix() == r.meta.prefix() {
		t.Fatal("competing binding reused storage")
	}
	if mode == "drop" {
		h.expectRows("SELECT id,note FROM it_tr_t", "999999|replacement")
		h.exec("TRUNCATE it_tr_t RESTART IDENTITY")
	}
	first := 1
	if mode == "continue" && !empty {
		first = rows + 1
	}
	h.expectNativeTruncated(raw, r, defaultSchema, "it_tr_t", first)
	retained := valueRows(snapshotExecuteTest(t, st, oldCtx, "SELECT __rowid,id,note FROM it_tr_t ORDER BY __rowid").rows)
	wantRows := rows
	if empty {
		wantRows = 0
	}
	if len(retained) != wantRows {
		t.Fatal("old reader row count changed", len(retained), wantRows)
	}
	for i, row := range retained {
		if row != fmt.Sprintf("%d|%d|old", i+1, i+1) {
			t.Fatal("old reader changed", i, row)
		}
	}
	if got := strings.Join(valueRows(snapshotExecuteTest(t, st, oldCtx, "SELECT COUNT(*) FROM it_tr_src").rows), "|"); got != fmt.Sprint(srRows) {
		t.Fatal("source changed", got)
	}
	pin.release()
	for _, seg := range old.snapshotSegments() {
		h.expectSnapshotReleased(raw, seg.Prefix, seg.Index)
	}
}
