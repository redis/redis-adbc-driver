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

// Integration tests for which values GROUP BY, SELECT DISTINCT, grouping
// sets, DISTINCT aggregates, window partitions and set operations find to
// be the same: those that compare equal, as in Postgres ('1 mon' = '30
// days', '1 day' = '24 hours', 0.0 = -0.0), and only those, whatever bytes
// they hold. They run against the Redis server at REDIS_URI, standalone or
// a cluster, like sql_integration_test.go. The expected values are Postgres
// 16's for the same statements. A cluster returns rows in no fixed order,
// so which of two equal values represents a group is only checked where a
// UNION ALL fixes the order.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// setupGroupKeys creates the data set:
//
//	id g iv              d
//	1  1 1 mon           0
//	2  1 30 days         0
//	3  2 1 day 02:00:00  1
//	4  2 26:00:00        1
//	5  3 1 day           2
//	6  3 24:00:00        2
//	7  3 NULL            NULL
//	8  4 -1 mons +30 days 3
//	9  4 00:00:00        3
func (h *sqlHarness) setupGroupKeys() {
	h.t.Helper()
	h.exec("DROP TABLE IF EXISTS it_ivgrp")
	h.exec(`CREATE TABLE it_ivgrp (id INTEGER, g INTEGER, iv INTERVAL, d DOUBLE PRECISION)`)
	h.exec(`INSERT INTO it_ivgrp VALUES (1, 1, INTERVAL '1 mon', 0), (2, 1, INTERVAL '30 days', 0),
		(3, 2, INTERVAL '1 day 02:00:00', 1), (4, 2, INTERVAL '26 hours', 1), (5, 3, INTERVAL '1 day', 2),
		(6, 3, INTERVAL '24 hours', 2), (7, 3, NULL, NULL), (8, 4, INTERVAL '-1 mon 30 days', 3), (9, 4, INTERVAL '0', 3)`)
	h.t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_ivgrp") })
}

func TestSQLEqualIntervalsGroup(t *testing.T) {
	hs := pushdownHarnesses(t)
	h := hs["default"]
	h.setupGroupKeys()

	// Interval columns aren't indexed, so these run in the driver in every
	// pushdown mode; SELECT DISTINCT runs as GROUP BY, and the grouping
	// sets share one read of the rows.
	forModes(t, hs, func(mode string, h *sqlHarness) {
		h.expectRows(`SELECT COUNT(*), MIN(id), MAX(id) FROM it_ivgrp GROUP BY iv ORDER BY MIN(id)`,
			"2|1|2", "2|3|4", "2|5|6", "1|7|7", "2|8|9")
		h.expectRows(`SELECT COUNT(*) FROM (SELECT DISTINCT iv FROM it_ivgrp) s`, "5")
		h.expectRows(`SELECT g, COUNT(DISTINCT iv), COUNT(iv) FROM it_ivgrp GROUP BY g ORDER BY g`,
			"1|1|2", "2|1|2", "3|1|2", "4|1|2")
		h.expectRows(`SELECT g, GROUPING(g, iv), COUNT(*), MIN(id) FROM it_ivgrp
			GROUP BY GROUPING SETS ((g, iv), (iv)) ORDER BY 2, 4`,
			"1|0|2|1", "2|0|2|3", "3|0|2|5", "3|0|1|7", "4|0|2|8",
			"NULL|2|2|1", "NULL|2|2|3", "NULL|2|2|5", "NULL|2|1|7", "NULL|2|2|8")
	})

	// The first of equal values represents the group: a UNION ALL's rows
	// come in its branches' order.
	h.expectRows(`SELECT CAST(iv AS VARCHAR) AS s, COUNT(*) FROM (SELECT iv FROM it_ivgrp WHERE id = 2
		UNION ALL SELECT iv FROM it_ivgrp WHERE id = 1 UNION ALL SELECT iv FROM it_ivgrp WHERE id = 9
		UNION ALL SELECT iv FROM it_ivgrp WHERE id = 8) v GROUP BY iv ORDER BY iv`,
		"00:00:00|2", "30 days|2")
	h.expectRows(`SELECT MIN(id) FROM it_ivgrp GROUP BY iv HAVING COUNT(*) > 1 ORDER BY 1`, "1", "3", "5", "8")
	h.expectRows(`SELECT COUNT(*), BOOL_AND(iv = INTERVAL '1 mon') FROM (SELECT DISTINCT iv FROM it_ivgrp WHERE id IN (1, 2)) s`,
		"1|true")
	h.expectRows(`SELECT COUNT(*) FROM (SELECT DISTINCT g, iv FROM it_ivgrp) s`, "5")
	h.expectRows(`SELECT DISTINCT ON (iv) id FROM it_ivgrp ORDER BY iv, id`, "8", "5", "3", "1", "7")

	// DISTINCT aggregates count one of equal values, the first: '1 mon' and
	// not '30 days' too, and '26:00:00' and not '1 day 02:00:00' too.
	h.expectRows(`SELECT COUNT(DISTINCT iv), SUM(DISTINCT iv) = INTERVAL '1 mon 2 days 02:00:00',
		AVG(DISTINCT iv) = INTERVAL '8 days 00:30:00' FROM it_ivgrp`, "4|true|true")
	h.expectRows(`SELECT CAST(SUM(DISTINCT iv) AS VARCHAR), CAST(AVG(DISTINCT iv) AS VARCHAR), COUNT(DISTINCT iv)
		FROM (SELECT iv FROM it_ivgrp WHERE id = 1 UNION ALL SELECT iv FROM it_ivgrp WHERE id = 2
		UNION ALL SELECT iv FROM it_ivgrp WHERE id = 4 UNION ALL SELECT iv FROM it_ivgrp WHERE id = 3) v`,
		"1 mon 26:00:00|15 days 13:00:00|2")

	// Window partitions, ROLLUP and CUBE.
	h.expectRows(`SELECT id, COUNT(*) OVER (PARTITION BY iv), MIN(id) OVER (PARTITION BY g, iv) FROM it_ivgrp ORDER BY id`,
		"1|2|1", "2|2|1", "3|2|3", "4|2|3", "5|2|5", "6|2|5", "7|1|7", "8|2|8", "9|2|8")
	h.expectRows(`SELECT GROUPING(iv), COUNT(*), MIN(id) FROM it_ivgrp GROUP BY ROLLUP (iv) ORDER BY 1, 3`,
		"0|2|1", "0|2|3", "0|2|5", "0|1|7", "0|2|8", "1|9|1")
	h.expectRows(`SELECT GROUPING(g, iv), COUNT(*) FROM it_ivgrp GROUP BY CUBE (g, iv) ORDER BY 1, 2`,
		"0|1", "0|2", "0|2", "0|2", "0|2", "1|2", "1|2", "1|2", "1|3",
		"2|1", "2|2", "2|2", "2|2", "2|2", "3|9")
}

// -0.0 and 0.0 are one value, as in Postgres. A DOUBLE PRECISION column
// stores -0.0 as 0, so the -0.0 here are computed.
func TestSQLNegativeZeroGroups(t *testing.T) {
	h := newSQLHarness(t)
	h.setupGroupKeys()
	z := `CASE WHEN id = 1 THEN -d ELSE d END`
	h.expectRows(`SELECT COUNT(*) FROM (SELECT `+z+` AS z FROM it_ivgrp WHERE id IN (1, 2) GROUP BY 1) s`, "1")
	h.expectRows(`SELECT COUNT(DISTINCT `+z+`) FROM it_ivgrp WHERE id IN (1, 2)`, "1")
	h.expectRows(`SELECT COUNT(*) FROM (SELECT DISTINCT `+z+` FROM it_ivgrp WHERE id IN (1, 2)) s`, "1")
	h.expectRows(`SELECT CAST(z AS VARCHAR) AS s, COUNT(*) FROM (SELECT -d AS z FROM it_ivgrp WHERE id = 1
		UNION ALL SELECT d FROM it_ivgrp WHERE id = 2) v GROUP BY z`, "-0|2")
}

// The '1' of a COALESCE or CASE of an integer and '1' is the integer 1, as
// in Postgres (see unknown.go), so the two rows' 1s are one value to GROUP
// BY, DISTINCT and PARTITION BY.
func TestSQLMixedKindKeys(t *testing.T) {
	h := newSQLHarness(t)
	h.setupGroupKeys()
	c := `COALESCE(CASE WHEN id = 2 THEN g END, '1')`
	h.expectRows(`SELECT `+c+` AS c, COUNT(*) FROM it_ivgrp WHERE id IN (1, 2) GROUP BY 1`, "1|2")
	h.expectRows(`SELECT CASE WHEN id = 1 THEN '1' ELSE g END AS c, COUNT(*) FROM it_ivgrp WHERE id IN (1, 2) GROUP BY 1`, "1|2")
	h.expectRows(`SELECT COUNT(DISTINCT `+c+`) FROM it_ivgrp WHERE id IN (1, 2)`, "1")
	h.expectRows(`SELECT COUNT(*) FROM (SELECT DISTINCT `+c+` FROM it_ivgrp WHERE id IN (1, 2)) s`, "1")
	h.expectRows(`SELECT id, COUNT(*) OVER (PARTITION BY `+c+`) FROM it_ivgrp WHERE id IN (1, 2) ORDER BY id`, "1|2", "2|2")
}

// Rows whose values hold NUL bytes are told apart by every key, also where
// the values of one row read like another row's run together.
func TestSQLKeysWithNULs(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_ivgrp_nul")
	h.exec(`CREATE TABLE it_ivgrp_nul (id INTEGER, a BYTEA, b BYTEA)`)
	mem := memory.DefaultAllocator
	ib, ab, bb := array.NewInt32Builder(mem), array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary),
		array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	defer ib.Release()
	defer ab.Release()
	defer bb.Release()
	ib.AppendValues([]int32{1, 2, 3, 4}, nil)
	for _, r := range [][2]string{{"a", "b\x00Vc"}, {"a\x00Vb", "c"}, {"a", "b\x00Vsc"}, {"a\x00Vsb", "c"}} {
		ab.Append([]byte(r[0]))
		bb.Append([]byte(r[1]))
	}
	ic, ac, bc := ib.NewArray(), ab.NewArray(), bb.NewArray()
	defer ic.Release()
	defer ac.Release()
	defer bc.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32},
		{Name: "a", Type: arrow.BinaryTypes.Binary},
		{Name: "b", Type: arrow.BinaryTypes.Binary},
	}, nil), []arrow.Array{ic, ac, bc}, 4)
	defer rec.Release()
	if _, err := h.stmtWith(`INSERT INTO it_ivgrp_nul VALUES (?, ?, ?)`, rec).ExecuteUpdate(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT COUNT(*) FROM (SELECT a, b FROM it_ivgrp_nul GROUP BY a, b) s`, "4")
	h.expectRows(`SELECT COUNT(*) FROM (SELECT DISTINCT a, b FROM it_ivgrp_nul) s`, "4")
	h.expectRows(`SELECT COUNT(*) FROM (SELECT a, b FROM it_ivgrp_nul UNION SELECT a, b FROM it_ivgrp_nul) s`, "4")
	h.expectRows(`SELECT id, COUNT(*) OVER (PARTITION BY a, b) FROM it_ivgrp_nul ORDER BY id`, "1|1", "2|1", "3|1", "4|1")
	h.expectRows(`SELECT COUNT(DISTINCT a || b) FROM it_ivgrp_nul`, "4")
}
