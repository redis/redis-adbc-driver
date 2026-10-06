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

import "testing"

// A bare name in GROUP BY that no input column has is the output column of
// that name, also where the output column is named by its expression, as in
// Postgres: GROUP BY upper after SELECT upper(v) groups by upper(v). An
// input column of the name comes first. Both tables (the index's grouping
// and the driver's) give pg16's rows and errors for the same tables.
func TestSQLGroupByOutputNames(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_gbn_iv", "it_gbn_n", "it_gbn_x")
	h.exec("CREATE TABLE it_gbn_iv (id INTEGER, iv INTERVAL)")
	h.exec("INSERT INTO it_gbn_iv VALUES (1, '1 mon'), (2, '30 days'), (3, '1 day 02:00:00'), (4, '26 hours'), " +
		"(5, '1 day'), (6, '24 hours')")
	h.expectRows("SELECT upper(CAST(iv AS VARCHAR)) FROM it_gbn_iv GROUP BY upper ORDER BY upper",
		"1 DAY", "1 DAY 02:00:00", "1 MON", "24:00:00", "26:00:00", "30 DAYS")
	h.expectRows("SELECT upper(CAST(iv AS VARCHAR)), count(*) FROM it_gbn_iv GROUP BY upper ORDER BY upper LIMIT 2",
		"1 DAY|1", "1 DAY 02:00:00|1")
	// GROUP BY iv is the interval column, not the text: 3 groups, not 6.
	h.expectRows("SELECT count(*) FROM (SELECT CAST(iv AS VARCHAR) FROM it_gbn_iv GROUP BY iv) g", "3")

	rows := "(1, 'a', 1.2), (2, 'A', 1.4), (3, 'b', 2.0), (4, NULL, 2.6)"
	h.exec("CREATE TABLE it_gbn_n (id INTEGER, v VARCHAR, x DOUBLE PRECISION)")
	h.exec("INSERT INTO it_gbn_n VALUES " + rows)
	h.exec("CREATE TABLE it_gbn_x (id INTEGER NOINDEX, v VARCHAR NOINDEX, x DOUBLE PRECISION NOINDEX)")
	h.exec("INSERT INTO it_gbn_x VALUES " + rows)
	for _, tbl := range []string{"it_gbn_n", "it_gbn_x"} {
		h.expectRows("SELECT upper(v), count(*) FROM "+tbl+" GROUP BY upper ORDER BY upper", "A|2", "B|1", "NULL|1")
		h.expectRows("SELECT upper(v) FROM "+tbl+" GROUP BY upper ORDER BY upper LIMIT 1", "A")
		h.expectRows("SELECT upper(v), count(*) FROM "+tbl+" GROUP BY upper HAVING count(*) > 1", "A|2")
		h.expectRows("SELECT CAST(x + 0 AS INTEGER), count(*) FROM "+tbl+" GROUP BY int4 ORDER BY 1", "1|2", "2|1", "3|1")
		h.expectRows("SELECT id % 2, sum(id) FROM "+tbl+` GROUP BY "?column?" ORDER BY 1`, "0|6", "1|4")
		h.expectRows("SELECT upper(a.v), count(*) FROM "+tbl+" a JOIN "+tbl+" b ON a.id = b.id GROUP BY upper ORDER BY 1",
			"A|2", "B|1", "NULL|1")
		h.expectRows("SELECT upper(v), count(*) FROM "+tbl+" GROUP BY ROLLUP (upper) ORDER BY 1, 2",
			"A|2", "B|1", "NULL|1", "NULL|4")
		// The input column x, not the output column named x.
		h.expectRows("SELECT CAST(x AS INTEGER), count(*) FROM "+tbl+" GROUP BY x ORDER BY 1, 2", "1|1", "1|1", "2|1", "3|1")
		h.expectRows("SELECT CAST(x AS INTEGER) AS x, count(*) FROM "+tbl+" GROUP BY x ORDER BY 1, 2", "1|1", "1|1", "2|1", "3|1")
		h.expectRows("SELECT CAST(x AS INTEGER) AS y, count(*) FROM "+tbl+" GROUP BY y ORDER BY 1", "1|2", "2|1", "3|1")
		h.expectError("SELECT upper(v), lower(v) AS upper FROM "+tbl+" GROUP BY upper", `GROUP BY "upper" is ambiguous`)
		h.expectError("SELECT count(*) FROM "+tbl+" GROUP BY count", "aggregate functions are not allowed in GROUP BY")
		h.expectError("SELECT row_number() OVER () FROM "+tbl+" GROUP BY row_number", "window functions are not allowed in GROUP BY")
		// pg16: column it_gbn_n.upper does not exist (the driver quotes it).
		h.expectError("SELECT upper(v) FROM "+tbl+" GROUP BY "+tbl+".upper", `column "`+tbl+`.upper" does not exist`)
	}
}
