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

// A bare name in ORDER BY and DISTINCT ON is an output column's name before
// it is an input column's, also where the output column is named by its
// expression, as in Postgres: after SELECT CAST(iv AS VARCHAR), ORDER BY iv
// sorts the text. The results and errors are pg16's for the same tables.
func TestSQLOrderByOutputNames(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_obn_iv")
	h.exec("CREATE TABLE it_obn_iv (id INTEGER, iv INTERVAL)")
	h.exec("INSERT INTO it_obn_iv VALUES (1, '1 mon'), (2, '30 days'), (3, '1 day 02:00:00'), (4, '26 hours'), " +
		"(5, '1 day'), (6, '24 hours')")
	text := []string{"1 day", "1 day 02:00:00", "1 mon", "24:00:00", "26:00:00", "30 days"}
	h.expectRows("SELECT CAST(iv AS VARCHAR) FROM it_obn_iv ORDER BY iv", text...)
	h.expectRows("SELECT iv::text FROM it_obn_iv ORDER BY iv DESC LIMIT 2", "30 days", "26:00:00")
	h.expectRows("SELECT CAST(iv AS VARCHAR) FROM (SELECT INTERVAL '1 mon' AS iv UNION ALL SELECT INTERVAL '26 hours') v ORDER BY iv",
		"1 mon", "26:00:00")
	h.expectRows("SELECT CAST(iv AS VARCHAR) FROM (SELECT iv FROM it_obn_iv WHERE id IN (1, 4)) v ORDER BY iv", "1 mon", "26:00:00")
	// A qualified name is the input column.
	h.expectRows("SELECT CAST(iv AS VARCHAR) FROM it_obn_iv ORDER BY it_obn_iv.iv, id",
		"1 day", "24:00:00", "1 day 02:00:00", "26:00:00", "1 mon", "30 days")
	h.expectError("SELECT CAST(iv AS VARCHAR), iv FROM it_obn_iv ORDER BY iv", `ORDER BY "iv" is ambiguous`)
	h.expectError("SELECT id, iv AS id FROM it_obn_iv ORDER BY id", `ORDER BY "id" is ambiguous`)

	// SELECT DISTINCT can order by the text; DISTINCT ON (iv) keeps a row
	// per text, DISTINCT ON (it_obn_iv.iv) one per interval.
	h.expectRows("SELECT DISTINCT CAST(iv AS VARCHAR) FROM it_obn_iv ORDER BY iv", text...)
	h.expectError("SELECT DISTINCT CAST(iv AS VARCHAR) FROM it_obn_iv ORDER BY it_obn_iv.iv",
		"for SELECT DISTINCT, ORDER BY expressions must appear in select list")
	h.expectRows("SELECT DISTINCT ON (iv) CAST(iv AS VARCHAR), id FROM it_obn_iv ORDER BY iv, id",
		"1 day|5", "1 day 02:00:00|3", "1 mon|1", "24:00:00|6", "26:00:00|4", "30 days|2")
	h.expectRows("SELECT DISTINCT ON (it_obn_iv.iv) CAST(iv AS VARCHAR) FROM it_obn_iv ORDER BY it_obn_iv.iv, id",
		"1 day", "1 day 02:00:00", "1 mon")
	h.expectError("SELECT DISTINCT ON (id) id, iv AS id FROM it_obn_iv ORDER BY 1", `DISTINCT ON "id" is ambiguous`)

	// A set operation's columns are named by its first branch's.
	h.expectRows("SELECT CAST(iv AS VARCHAR) FROM it_obn_iv UNION ALL SELECT '0' ORDER BY iv", append([]string{"0"}, text...)...)
	h.expectRows("SELECT count(*) FROM it_obn_iv UNION SELECT 7 ORDER BY count", "6", "7")
}

// ORDER BY id after SELECT CAST(id AS VARCHAR) sorts the text, so the index
// can't sort it on id, nor apply the LIMIT. With an alias, or without the
// cast, ORDER BY id is still the index's sort. Both tables (the index's and
// the driver's sort) give pg16's rows.
func TestSQLOrderByOutputNameNotIndexSort(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_obn_n", "it_obn_x")
	rows := "(1, 'b'), (2, 'a'), (3, 'c'), (10, 'd'), (11, 'e'), (12, 'f'), (20, 'g')"
	h.exec("CREATE TABLE it_obn_n (id INTEGER, v VARCHAR)")
	h.exec("INSERT INTO it_obn_n VALUES " + rows)
	h.exec("CREATE TABLE it_obn_x (id INTEGER NOINDEX, v VARCHAR NOINDEX)")
	h.exec("INSERT INTO it_obn_x VALUES " + rows)
	for _, tbl := range []string{"it_obn_n", "it_obn_x"} {
		h.expectRows("SELECT CAST(id AS VARCHAR) FROM "+tbl+" ORDER BY id", "1", "10", "11", "12", "2", "20", "3")
		h.expectRows("SELECT CAST(id AS VARCHAR) FROM "+tbl+" ORDER BY id LIMIT 3", "1", "10", "11")
		h.expectRows("SELECT CAST(id AS VARCHAR) FROM "+tbl+" ORDER BY id DESC LIMIT 2 OFFSET 1", "20", "2")
		h.expectRows("SELECT id::text, v FROM "+tbl+" WHERE id > 2 ORDER BY id LIMIT 2", "10|d", "11|e")
		h.expectRows("SELECT CAST(id AS VARCHAR) AS s FROM "+tbl+" ORDER BY id LIMIT 3", "1", "2", "3")
		h.expectRows("SELECT id FROM "+tbl+" ORDER BY id LIMIT 3", "1", "2", "3")
		h.expectRows("SELECT DISTINCT CAST(id AS VARCHAR) FROM "+tbl+" ORDER BY id LIMIT 2", "1", "10")
		h.expectRows("SELECT CAST(id AS VARCHAR), count(*) FROM "+tbl+" GROUP BY id ORDER BY id LIMIT 3", "1|1", "10|1", "11|1")
		h.expectRows("SELECT upper(v) FROM "+tbl+" ORDER BY upper DESC LIMIT 2", "G", "F")
		h.expectRows("SELECT max(id) FROM "+tbl+" GROUP BY v ORDER BY max DESC LIMIT 2", "20", "12")
		h.expectRows("SELECT CAST(id AS VARCHAR) FROM "+tbl+" UNION SELECT '9' ORDER BY id", "1", "10", "11", "12", "2", "20", "3", "9")
	}
}
