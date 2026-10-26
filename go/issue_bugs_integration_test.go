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
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

// Exercise the repeated unique-key shape emitted by dbt delete+insert,
// including the real Search GROUPBY path and driver-side aggregation.
func TestSQLIssue232RepeatedGrouping(t *testing.T) {
	newSQLHarness(t) // Skip before opening mode-specific connections without Redis.
	for _, mode := range []string{PushdownExact, PushdownAll, PushdownNone} {
		t.Run(mode, func(t *testing.T) {
			h := newGSPushdownHarness(t, mode)
			h.dropTables("it_bug232")
			h.exec(`CREATE TABLE it_bug232 (state VARCHAR, county VARCHAR, n INTEGER)`)
			h.exec(`INSERT INTO it_bug232 VALUES ('CT', 'Hartford', 1), ('CT', 'Hartford', 2), ('NY', 'Kings', 3)`)
			for _, sql := range []string{
				`SELECT DISTINCT state, state FROM it_bug232 ORDER BY state`,
				`SELECT DISTINCT state, state AS s2 FROM it_bug232 ORDER BY state`,
				`SELECT state, state FROM it_bug232 GROUP BY state ORDER BY state`,
			} {
				h.expectRows(sql, "CT|CT", "NY|NY")
				if _, _, pushed := h.planIn(sql, mode); pushed != (mode != PushdownNone) {
					t.Errorf("%s: index aggregation = %v in mode %s", sql, pushed, mode)
				}
			}
			h.expectRows(`SELECT DISTINCT state FROM it_bug232 ORDER BY state`, "CT", "NY")
			h.expectRows(`SELECT DISTINCT state, county, state FROM it_bug232 ORDER BY state`, "CT|Hartford|CT", "NY|Kings|NY")
			h.expectRows(`SELECT state FROM it_bug232 GROUP BY state, state ORDER BY state`, "CT", "NY")
			h.expectRows(`SELECT COUNT(*) FROM (SELECT DISTINCT state, state FROM it_bug232) x`, "2")
			if n := h.exec(`DELETE FROM it_bug232 WHERE (state, state) IN
				(SELECT DISTINCT state, state FROM it_bug232 WHERE county = 'Hartford')`); n != 2 {
				t.Errorf("deleted %d rows, want 2", n)
			}
			h.expectRows(`SELECT state, county, n FROM it_bug232`, "NY|Kings|3")
		})
	}
}

// The Arrow schema must reflect the resolved type even for empty results.
func TestSQLIssue187NullSetOperationSchema(t *testing.T) {
	h := newSQLHarness(t)
	for _, sql := range []string{
		`SELECT NULL UNION SELECT NULL`,
		`SELECT NULL UNION ALL SELECT NULL`,
		`SELECT NULL INTERSECT SELECT NULL`,
	} {
		want := []string{"NULL"}
		if sql == `SELECT NULL UNION ALL SELECT NULL` {
			want = append(want, "NULL")
		}
		schema := h.expectRows(sql, want...)
		h.expectType(schema, sql, 0, arrow.BinaryTypes.String)
	}
	for _, sql := range []string{
		`SELECT NULL EXCEPT SELECT NULL`,
		`SELECT NULL WHERE FALSE UNION SELECT NULL WHERE FALSE`,
	} {
		schema := h.expectRows(sql)
		h.expectType(schema, sql, 0, arrow.BinaryTypes.String)
	}
	h.expectError(`SELECT NULL UNION SELECT NULL UNION SELECT 1`, "types VARCHAR and INTEGER cannot be matched")
}
