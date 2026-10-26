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

// UPDATE, UPDATE … FROM, MERGE and DELETE racing a TRUNCATE or a DROP TABLE
// that lands after the first of their three pipelines (raceDrop, as for the
// inserts in stale_rows_integration_test.go and
// truncate_race_integration_test.go).
//
// An HSET after the TRUNCATE or DROP makes a partial HASH of the row it
// sets, under the old key prefix, which no table has any more: the check
// after the last pipeline finds out, deletes those keys, and the statement
// fails. MERGE's new rows are written as INSERT's are. A DEL makes nothing:
// a DELETE (or MERGE … DELETE) that doesn't check succeeds, as if it had run
// first, and one that checks fails.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// updateRaces are the writes that change rows (writeUpdates) or insert them
// (MERGE … INSERT, into an empty target). %[1]s is the target, %[2]s the
// source, which has srRows rows (id, 'old').
var updateRaces = []struct {
	name  string
	empty bool // the target starts empty
	sql   string
}{
	{"UPDATE", false, `UPDATE %[1]s SET note = 'new'`},
	{"UPDATE FROM", false, `UPDATE %[1]s t SET note = 'new' FROM %[2]s s WHERE t.id = s.id`},
	{"MERGE UPDATE", false, `MERGE INTO %[1]s t USING %[2]s s ON t.id = s.id WHEN MATCHED THEN UPDATE SET note = 'new'`},
	{"MERGE INSERT", true, `MERGE INTO %[1]s t USING %[2]s s ON t.id = s.id WHEN NOT MATCHED THEN INSERT (id, note) VALUES (s.id, 'new')`},
}

// deleteRaces delete every row of a full target.
var deleteRaces = []struct{ name, sql string }{
	{"DELETE", `DELETE FROM %[1]s`},
	{"MERGE DELETE", `MERGE INTO %[1]s t USING %[2]s s ON t.id = s.id WHEN MATCHED THEN DELETE`},
}

// fillTarget creates the target of a race: empty, or with the source's rows.
func (h *sqlHarness) fillTarget(table, src string, empty bool) {
	h.t.Helper()
	if empty {
		h.exec(fmt.Sprintf(`CREATE TABLE %s (id BIGINT, note VARCHAR)`, table))
	} else {
		h.exec(fmt.Sprintf(`CREATE TABLE %s AS SELECT id, note FROM %s`, table, src))
	}
}

// raceMode is the tables, the TRUNCATE or DROP of a race, and what to
// check afterwards.
type raceMode struct {
	name string
	// setup creates the source and drops the target; the target and the
	// source are named so.
	setup       func(h *sqlHarness)
	target, src string
	run         func(other *sqlHarness)
	// after checks the table once the writer is done.
	after func(h *sqlHarness, raw goredis.UniversalClient, r *dropRace)
}

func raceModes() []raceMode {
	var modes []raceMode
	for _, m := range truncModes {
		firstID := map[bool]int{true: 1, false: srRows + 1}[m.restart]
		modes = append(modes, raceMode{
			name:   "TRUNCATE " + m.name,
			setup:  (*sqlHarness).setupTruncRace,
			target: "it_tr_t", src: "it_tr_src",
			run: func(other *sqlHarness) { other.exec(`TRUNCATE it_tr_t` + m.clause) },
			after: func(h *sqlHarness, raw goredis.UniversalClient, r *dropRace) {
				h.expectTruncated(raw, r, "public", "it_tr_t", firstID)
			},
		})
	}
	return append(modes, raceMode{
		name:   "DROP",
		setup:  (*sqlHarness).setupStaleRows,
		target: "it_sr_t", src: "it_sr_src",
		run: func(other *sqlHarness) { other.exec(`DROP TABLE it_sr_t`) },
		after: func(h *sqlHarness, raw goredis.UniversalClient, r *dropRace) {
			h.expectError(`SELECT * FROM it_sr_t`, "does not exist")
			h.expectNewTable(raw, r, "public", `CREATE TABLE it_sr_t (id BIGINT, note VARCHAR)`, "it_sr_t")
		},
	})
}

// A write that changes or inserts rows fails, and leaves no key behind.
func TestSQLTruncateOrDropDuringUpdate(t *testing.T) {
	for _, w := range updateRaces {
		for _, m := range raceModes() {
			t.Run(w.name+"/"+m.name, func(t *testing.T) {
				h := newSQLHarness(t)
				m.setup(h)
				raw := h.rawClient()
				other := newSQLHarness(t)
				h.fillTarget(m.target, m.src, w.empty)
				r := h.raceDrop(raw, m.target, func(*tableMeta) { m.run(other) })
				_, err := tryExec(h.ctx, h.conn, fmt.Sprintf(w.sql, m.target, m.src))
				h.expectRaceLost(raw, r, err)
				m.after(h, raw, r)
			})
		}
	}
}

// A delete deletes nothing after the TRUNCATE or DROP, and makes no key.
// One that checks fails, as any write does whose table's rows moved away
// meanwhile (it can't tell from a re-keying rename, which copied the rows
// it hadn't deleted yet); one that doesn't succeeds. Either way the table
// is empty (or gone) and nothing is left under the old prefix.
func TestSQLTruncateOrDropDuringDelete(t *testing.T) {
	for _, w := range deleteRaces {
		for _, m := range raceModes() {
			for _, checks := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/checks=%v", w.name, m.name, checks), func(t *testing.T) {
					h := newSQLHarness(t)
					m.setup(h)
					raw := h.rawClient()
					other := newSQLHarness(t)
					h.fillTarget(m.target, m.src, false)
					r := h.raceDrop(raw, m.target, func(*tableMeta) { m.run(other) })
					if checks {
						// After raceDrop's hook: the metadata looks read long ago.
						hook := testHookRowsWritten
						testHookRowsWritten = func(meta *tableMeta, written int) {
							hook(meta, written)
							meta.readAt = time.Now().Add(-time.Second)
						}
					}
					n, err := tryExec(h.ctx, h.conn, fmt.Sprintf(w.sql, m.target, m.src))
					testHookRowsWritten = nil
					if r.prefix == "" {
						t.Fatal("the race did not run")
					}
					switch {
					case checks && (err == nil || !strings.Contains(err.Error(), srWriteErr)):
						t.Errorf("writer: got %v, want an error containing %q", err, srWriteErr)
					case !checks && err != nil:
						t.Errorf("writer: %v", err)
					case !checks && n != srRows:
						t.Errorf("writer: %d rows, want %d", n, srRows)
					}
					if r.left != 0 {
						t.Errorf("%d keys under %q after the last delete, want 0", r.left, r.prefix)
					}
					m.after(h, raw, r)
				})
			}
		}
	}
}
