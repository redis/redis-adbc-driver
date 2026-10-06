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
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// In GROUP BY, HAVING and QUALIFY expressions, an input column of the
// query comes before an alias of its name, also in a join, and a qualified
// name is never an alias. A query's GROUP BY is resolved from its items as
// written each time it is planned. Both tables (the index's grouping and
// the driver's) give pg16's rows for the same tables.
func TestSQLAliasesInExpressions(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_als_n", "it_als_x", "it_als_u", "it_als_u2")
	rows := "(1, 'a', 1.2), (2, 'A', 1.4), (3, 'b', 2.0), (4, NULL, 2.6)"
	h.exec("CREATE TABLE it_als_n (id INTEGER, v VARCHAR, x DOUBLE PRECISION)")
	h.exec("INSERT INTO it_als_n VALUES " + rows)
	h.exec("CREATE TABLE it_als_x (id INTEGER NOINDEX, v VARCHAR NOINDEX, x DOUBLE PRECISION NOINDEX)")
	h.exec("INSERT INTO it_als_x VALUES " + rows)
	h.exec("CREATE TABLE it_als_u (k INTEGER, w VARCHAR)")
	h.exec("INSERT INTO it_als_u VALUES (1, 'p'), (2, 'q')")
	h.exec("CREATE TABLE it_als_u2 (k INTEGER, v VARCHAR)")
	h.exec("INSERT INTO it_als_u2 VALUES (1, 'p'), (2, 'q')")
	oneEach := []string{"1|1", "2|1", "3|1", "4|1"}
	for _, tbl := range []string{"it_als_n", "it_als_x"} {
		// HAVING x is the input column x, with or without the join.
		h.expectRows("SELECT CAST(x AS INTEGER) AS x, count(*) FROM "+tbl+" GROUP BY "+tbl+".x HAVING x > 1.3 ORDER BY 1, 2",
			"1|1", "2|1", "3|1")
		h.expectRows("SELECT CAST(x AS INTEGER) AS x, count(*) FROM "+tbl+" JOIN it_als_u ON true GROUP BY "+tbl+".x "+
			"HAVING x > 1.3 ORDER BY 1, 2", "1|2", "2|2", "3|2")
		h.expectRows("SELECT k, upper(w) AS w, count(*) FROM it_als_u u JOIN "+tbl+" n ON n.id = u.k GROUP BY k, u.w HAVING w = 'p'",
			"1|P|1")
		// A qualified name is the input column, not the alias x.
		h.expectRows("SELECT CAST(x AS INTEGER) AS x, count(*) FROM "+tbl+" n JOIN it_als_u ON true GROUP BY n.x HAVING n.x > 1.3 "+
			"ORDER BY 1, 2", "1|2", "2|2", "3|2")
		h.expectRows("SELECT CAST(x AS INTEGER) AS x, count(*) FROM "+tbl+" n JOIN it_als_u ON true GROUP BY n.x, 1 ORDER BY 1, 2",
			"1|2", "1|2", "2|2", "3|2")

		// GROUP BY w after id AS w (the enclosing query's id) groups by
		// that id, also where the item binds the subquery again (GROUP BY 1,
		// 2), and for a statement executed again.
		q := "SELECT id, (SELECT count(*) FROM (SELECT id AS w, max(v) AS id FROM it_als_u2 GROUP BY w) q) FROM " + tbl
		h.expectRows(q+" ORDER BY 1", oneEach...)
		h.expectRows(q+" GROUP BY 1, 2 ORDER BY 1", oneEach...)
		for _, q := range []string{
			q + " ORDER BY 1",
			"SELECT id, (SELECT count(*) FROM (SELECT CAST(id AS VARCHAR) AS w, max(v) AS id FROM it_als_u2 GROUP BY w) q) AS c " +
				"FROM " + tbl + " GROUP BY id, c ORDER BY 1",
		} {
			st := h.gsPrepare(q)
			for i := range 2 {
				if got := h.gsRows(st); !slices.Equal(got, oneEach) {
					t.Errorf("%s (execution %d): got %q, want %q", q, i+1, got, oneEach)
				}
			}
		}
		// A query with parameters is planned to describe them, then again
		// for each execution.
		st := h.gsPrepare("SELECT id, (SELECT max(w) FROM (SELECT id + k AS w, max(v) AS id FROM it_als_u2 GROUP BY w) q) " +
			"FROM " + tbl + " WHERE id > ? ORDER BY 1")
		for _, c := range []struct {
			param int64
			want  []string
		}{
			{2, []string{"3|5", "4|6"}},
			{0, []string{"1|3", "2|4", "3|5", "4|6"}},
		} {
			ib := array.NewInt64Builder(memory.DefaultAllocator)
			ib.Append(c.param)
			col := ib.NewArray()
			ib.Release()
			schema := arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.PrimitiveTypes.Int64}}, nil)
			if err := st.Bind(h.ctx, array.NewRecordBatch(schema, []arrow.Array{col}, 1)); err != nil {
				t.Fatal(err)
			}
			if got := h.gsRows(st); !slices.Equal(got, c.want) {
				t.Errorf("bound parameter %d: got %q, want %q", c.param, got, c.want)
			}
		}
	}
}
