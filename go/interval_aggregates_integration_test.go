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

// Integration tests for SUM, AVG, MIN and MAX of intervals, grouped and as
// window functions. They run against the Redis server at REDIS_URI,
// standalone or a cluster, like sql_integration_test.go. The expected
// values are Postgres 16's for the same statements.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

// setupIntervalAggregates creates the data set:
//
//	id g iv
//	1  1 1 mon 01:00:00
//	2  1 30 days
//	3  1 1 day 02:00:00
//	4  1 NULL
//	5  2 00:00:01
//	6  2 00:00:00
//	7  2 00:00:00
//	8  3 NULL
//	9  4 -1 years +3 days -00:00:01.5
//	10 4 25:00:00
func (h *sqlHarness) setupIntervalAggregates() {
	h.t.Helper()
	h.exec("DROP TABLE IF EXISTS it_ivagg")
	h.exec(`CREATE TABLE it_ivagg (id INTEGER, g INTEGER, iv INTERVAL)`)
	h.exec(`INSERT INTO it_ivagg VALUES (1, 1, INTERVAL '1 mon 1 hour'), (2, 1, INTERVAL '30 days'),
		(3, 1, INTERVAL '1 day 02:00:00'), (4, 1, NULL), (5, 2, INTERVAL '1 sec'), (6, 2, INTERVAL '0'),
		(7, 2, INTERVAL '0'), (8, 3, NULL), (9, 4, INTERVAL '-1 year 3 days -00:00:01.5'), (10, 4, INTERVAL '25 hours')`)
	h.t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_ivagg") })
}

func ivText(e string) string { return "CAST(" + e + " AS VARCHAR)" }

func TestSQLIntervalAggregates(t *testing.T) {
	hs := pushdownHarnesses(t)
	h := hs["default"]
	h.setupIntervalAggregates()
	h.exec("DROP TABLE IF EXISTS it_ivagg_ctas")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_ivagg_ctas") })

	// Months, days and time are summed on their own, as in Postgres: 30 days
	// stay days and 27 hours stay hours. AVG divides the sum by the count,
	// rounded to microseconds. In every pushdown mode: interval columns
	// aren't indexed, so these aggregates always run in the driver.
	grouped := `SELECT g, ` + ivText("SUM(iv)") + `, ` + ivText("AVG(iv)") + `, ` + ivText("MIN(iv)") + `, ` +
		ivText("MAX(iv)") + `, COUNT(iv) FROM it_ivagg GROUP BY g ORDER BY g`
	forModes(t, hs, func(mode string, h *sqlHarness) {
		h.expectRows(grouped,
			"1|1 mon 31 days 03:00:00|20 days 09:00:00|1 day 02:00:00|1 mon 01:00:00|3",
			"2|00:00:01|00:00:00.333333|00:00:00|00:00:01|3",
			"3|NULL|NULL|NULL|NULL|0",
			"4|-1 years +3 days 24:59:58.5|-6 mons +1 day 24:29:59.25|-1 years +3 days -00:00:01.5|25:00:00|2")
		if mode == "default" {
			return
		}
		for _, agg := range []string{"SUM(iv)", "AVG(iv)", "MIN(iv)", "MAX(iv)", "COUNT(iv)"} {
			if _, _, indexAgg := h.planIn(`SELECT g, `+agg+` FROM it_ivagg GROUP BY g`, mode); indexAgg {
				t.Errorf("%s: %s runs in FT.AGGREGATE", mode, agg)
			}
		}
		if _, _, indexAgg := h.planIn(`SELECT g, COUNT(*) FROM it_ivagg GROUP BY g`, mode); indexAgg != (mode != PushdownNone) {
			t.Errorf("%s: COUNT(*) GROUP BY g in FT.AGGREGATE: %v", mode, indexAgg)
		}
	})
	h.expectRows(`SELECT `+ivText("SUM(iv)")+`, `+ivText("AVG(iv)")+`, `+ivText("MIN(iv)")+`, `+ivText("MAX(iv)")+` FROM it_ivagg`,
		"-11 mons +34 days 27:59:59.5|-1 mons -7 days +03:29:59.9375|-1 years +3 days -00:00:01.5|1 mon 01:00:00")

	// The results are intervals, also NULL ones and window results.
	schema := h.expectRows(`SELECT SUM(iv), AVG(iv), MIN(iv) FROM it_ivagg WHERE id > 100`, "NULL|NULL|NULL")
	expectTypeIDs(t, schema, arrow.INTERVAL_MONTH_DAY_NANO, arrow.INTERVAL_MONTH_DAY_NANO, arrow.INTERVAL_MONTH_DAY_NANO)
	_, schema = h.query(`SELECT SUM(iv) OVER (ORDER BY id), AVG(iv) OVER (), SUM(SUM(iv)) OVER () FROM it_ivagg GROUP BY id, iv`)
	expectTypeIDs(t, schema, arrow.INTERVAL_MONTH_DAY_NANO, arrow.INTERVAL_MONTH_DAY_NANO, arrow.INTERVAL_MONTH_DAY_NANO)
	h.expectRows(`SELECT TO_CHAR(SUM(iv), 'DD HH24:MI:SS'), TO_CHAR(AVG(iv), 'DD HH24:MI:SS.US') FROM it_ivagg WHERE g = 1`,
		"31 03:00:00|20 09:00:00.000000")

	// FILTER, HAVING and ORDER BY on interval aggregates, aggregates of
	// interval expressions, and windows over the groups.
	h.expectRows(`SELECT g, `+ivText("SUM(iv) FILTER (WHERE id <> 2)")+`, `+ivText("AVG(iv) FILTER (WHERE id <> 2)")+`
		FROM it_ivagg GROUP BY g HAVING SUM(iv) > INTERVAL '1 day' ORDER BY SUM(iv) DESC`,
		"1|1 mon 1 day 03:00:00|15 days 13:30:00")
	h.expectRows(`SELECT `+ivText("SUM(iv * 2)")+`, `+ivText("SUM(-iv)")+`, `+ivText("AVG(iv + INTERVAL '1 hour')")+` FROM it_ivagg`,
		"-1 years -10 mons +68 days 55:59:59|11 mons -34 days -27:59:59.5|-1 mons -7 days +04:29:59.9375")
	h.expectRows(`SELECT g, `+ivText("SUM(SUM(iv)) OVER (ORDER BY g)")+`, `+ivText("AVG(AVG(iv)) OVER ()")+`
		FROM it_ivagg GROUP BY g ORDER BY g`,
		"1|1 mon 31 days 03:00:00|-2 mons +7 days 11:09:59.861111",
		"2|1 mon 31 days 03:00:01|-2 mons +7 days 11:09:59.861111",
		"3|1 mon 31 days 03:00:01|-2 mons +7 days 11:09:59.861111",
		"4|-11 mons +34 days 27:59:59.5|-2 mons +7 days 11:09:59.861111")
	h.expectRows(`SELECT `+ivText("(SELECT SUM(iv) FROM it_ivagg WHERE g = 1)"), "1 mon 31 days 03:00:00")
	h.expectRows(`SELECT g, `+ivText("SUM(iv)")+` FROM it_ivagg GROUP BY ROLLUP (g) ORDER BY g`,
		"1|1 mon 31 days 03:00:00", "2|00:00:01", "3|NULL", "4|-1 years +3 days 24:59:58.5", "NULL|-11 mons +34 days 27:59:59.5")

	// CREATE TABLE AS stores them as intervals.
	h.exec(`CREATE TABLE it_ivagg_ctas AS SELECT g, SUM(iv) AS s, AVG(iv) AS a FROM it_ivagg GROUP BY g`)
	h.expectRows(`SELECT g, `+ivText("s")+`, `+ivText("a")+`, s > a FROM it_ivagg_ctas ORDER BY g`,
		"1|1 mon 31 days 03:00:00|20 days 09:00:00|true", "2|00:00:01|00:00:00.333333|true", "3|NULL|NULL|NULL",
		"4|-1 years +3 days 24:59:58.5|-6 mons +1 day 24:29:59.25|false")
	h.expectRows(`SELECT column_name, driver_data_type FROM information_schema.columns WHERE table_name = 'it_ivagg_ctas'
		ORDER BY column_name`, "a|INTERVAL", "g|INTEGER", "s|INTERVAL")
}

func TestSQLIntervalWindowAggregates(t *testing.T) {
	h := newSQLHarness(t)
	h.setupIntervalAggregates()

	// Frames that grow, slide (rows leave by subtraction), partition, have a
	// hole (EXCLUDE) and are ranges of intervals.
	h.expectRows(`SELECT id, `+ivText("SUM(iv) OVER (ORDER BY id)")+`,
			`+ivText("AVG(iv) OVER (ORDER BY id ROWS BETWEEN 1 PRECEDING AND 1 FOLLOWING)")+`
		FROM it_ivagg ORDER BY id`,
		"1|1 mon 01:00:00|30 days 00:30:00",
		"2|1 mon 30 days 01:00:00|20 days 09:00:00",
		"3|1 mon 31 days 03:00:00|15 days 13:00:00",
		"4|1 mon 31 days 03:00:00|13:00:00.5",
		"5|1 mon 31 days 03:00:01|00:00:00.5",
		"6|1 mon 31 days 03:00:01|00:00:00.333333",
		"7|1 mon 31 days 03:00:01|00:00:00",
		"8|1 mon 31 days 03:00:01|-6 mons +1 day 11:59:59.25",
		"9|-11 mons +34 days 02:59:59.5|-6 mons +1 day 24:29:59.25",
		"10|-11 mons +34 days 27:59:59.5|-6 mons +1 day 24:29:59.25")
	h.expectRows(`SELECT id, `+ivText("SUM(iv) OVER (PARTITION BY g ORDER BY id ROWS BETWEEN 1 PRECEDING AND CURRENT ROW)")+`,
			`+ivText("AVG(iv) OVER (PARTITION BY g)")+`,
			`+ivText("SUM(iv) OVER (ORDER BY id ROWS BETWEEN 2 PRECEDING AND 2 FOLLOWING EXCLUDE CURRENT ROW)")+`,
			`+ivText("AVG(iv) OVER (ORDER BY iv RANGE BETWEEN INTERVAL '1 day' PRECEDING AND CURRENT ROW)")+`
		FROM it_ivagg ORDER BY id`,
		"1|1 mon 01:00:00|20 days 09:00:00|31 days 02:00:00|30 days 00:30:00",
		"2|1 mon 30 days 01:00:00|20 days 09:00:00|1 mon 1 day 03:00:00|30 days",
		"3|31 days 02:00:00|20 days 09:00:00|1 mon 30 days 01:00:01|25:30:00",
		"4|1 day 02:00:00|20 days 09:00:00|31 days 02:00:01|NULL",
		"5|00:00:01|00:00:00.333333|1 day 02:00:00|00:00:00.333333",
		"6|00:00:01|00:00:00.333333|00:00:01|00:00:00",
		"7|00:00:00|00:00:00.333333|-1 years +3 days -00:00:00.5|00:00:00",
		"8|NULL|NULL|-1 years +3 days 24:59:58.5|NULL",
		"9|-1 years +3 days -00:00:01.5|-6 mons +1 day 24:29:59.25|25:00:00|-1 years +3 days -00:00:01.5",
		"10|-1 years +3 days 24:59:58.5|-6 mons +1 day 24:29:59.25|-1 years +3 days -00:00:01.5|25:00:00")
}

// AVG divides as Postgres's interval / n does: a fraction of a month
// cascades into days, one of a day into time, and the time is rounded to
// microseconds, half to even.
func TestSQLIntervalAvgRounding(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_ivavg")
	h.exec(`CREATE TABLE it_ivavg (k INTEGER, iv INTERVAL)`)
	h.exec(`INSERT INTO it_ivavg VALUES (1, INTERVAL '1 sec'), (1, INTERVAL '0'), (1, INTERVAL '0'),
		(2, INTERVAL '3 microseconds'), (2, INTERVAL '0'), (3, INTERVAL '29 days 23:59:59.999999'), (3, INTERVAL '0'),
		(4, INTERVAL '1 mon'), (4, INTERVAL '0'), (4, INTERVAL '0'), (4, INTERVAL '0'), (4, INTERVAL '0'), (4, INTERVAL '0'),
		(4, INTERVAL '0'), (5, INTERVAL '-5 mons -1 day'), (5, INTERVAL '0'), (5, INTERVAL '0')`)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_ivavg") })
	want := []string{
		"1|00:00:00.333333",
		"2|00:00:00.000002",
		"3|14 days 24:00:00",
		"4|4 days 06:51:25.6896",
		"5|-1 mons -20 days -08:00:00",
	}
	h.expectRows(`SELECT k, `+ivText("AVG(iv)")+` FROM it_ivavg GROUP BY k ORDER BY k`, want...)
	h.expectRows(`SELECT DISTINCT k, `+ivText("AVG(iv) OVER (PARTITION BY k)")+` FROM it_ivavg ORDER BY k`, want...)
}

// Only a result out of range is an error: the sums are exact, so the order
// of the rows doesn't matter (Postgres fails when a running total in its
// order is out of range).
func TestSQLIntervalAggregateRange(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_ivovf")
	h.exec(`CREATE TABLE it_ivovf (id INTEGER, iv INTERVAL)`)
	h.exec(`INSERT INTO it_ivovf VALUES (1, INTERVAL '-1 mon'), (2, INTERVAL '2147483647 mons'), (3, INTERVAL '1 mon'),
		(4, INTERVAL '-2147483647 days'), (5, INTERVAL '-2 days'), (6, INTERVAL '2000000 hours'), (7, INTERVAL '2000000 hours')`)
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_ivovf") })
	h.expectRows(`SELECT `+ivText("SUM(iv)")+` FROM it_ivovf WHERE id <= 3`, "178956970 years 7 mons")
	h.expectRows(`SELECT id, `+ivText("SUM(iv) OVER (ORDER BY id ROWS BETWEEN 1 PRECEDING AND CURRENT ROW)")+`
		FROM it_ivovf WHERE id IN (1, 2, 4) ORDER BY id`,
		"1|-1 mons", "2|178956970 years 6 mons", "4|178956970 years 7 mons -2147483647 days")
	for _, sql := range []string{
		`SELECT SUM(iv) FROM it_ivovf WHERE id IN (2, 3)`,
		`SELECT AVG(iv) FROM it_ivovf WHERE id IN (2, 3)`,
		`SELECT SUM(iv) FROM it_ivovf WHERE id IN (4, 5)`,
		`SELECT AVG(iv) FROM it_ivovf WHERE id IN (4, 5)`,
		`SELECT SUM(iv) OVER (ORDER BY id ROWS BETWEEN CURRENT ROW AND 1 FOLLOWING) FROM it_ivovf WHERE id <= 3`,
		// The time part of an interval is nanoseconds in 64 bits (Postgres
		// has microseconds and returns 4000000:00:00).
		`SELECT SUM(iv) FROM it_ivovf WHERE id IN (6, 7)`,
	} {
		h.expectError(sql, "interval out of range")
	}
	// AVG's sum may be beyond the nanoseconds, as in Postgres.
	h.expectRows(`SELECT `+ivText("AVG(iv)")+` FROM it_ivovf WHERE id IN (6, 7)`, "2000000:00:00")
}
