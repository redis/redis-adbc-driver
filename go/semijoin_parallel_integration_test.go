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
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// newSQLHarnessWithOptions is newSQLHarness on a database of its own, with
// extra database options (a worker budget).
func newSQLHarnessWithOptions(t *testing.T, extra map[string]string) *sqlHarness {
	t.Helper()
	uri := os.Getenv("REDIS_URI")
	if uri == "" {
		t.Skip("set REDIS_URI to run SQL integration tests")
	}
	ctx := context.Background()
	opts := map[string]string{adbc.OptionKeyURI: uri}
	maps.Copy(opts, extra)
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, retainedTestDatabaseOptions(opts))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	stmt, err := conn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stmt.(*statementImpl).conn.store.client.AddHook(pinOriginHook(t, "sql"))
	if err := stmt.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return &sqlHarness{t: t, ctx: ctx, conn: conn}
}

// A page's probe batches (probeSemiJoin) run on concurrent workers when the
// worker budget grants them, with the results, per-row fallbacks and errors
// of running them one after another.
func TestSQLSemiJoinParallelProbes(t *testing.T) {
	// Probe mechanics: keep probing inner sides wholeBuildByCost would build (#662).
	keepSemiProbes(t)
	serial := newSQLHarnessWithOptions(t, map[string]string{OptionStringQueryWorkerBudget: "1"})
	parallel := newSQLHarnessWithOptions(t, map[string]string{OptionStringQueryWorkerBudget: "4", OptionStringQueryWorkers: "4"})
	h := serial
	drop := func() {
		h.exec("DROP TABLE IF EXISTS it_sjp_big")
		h.exec("DROP TABLE IF EXISTS it_sjp_o")
	}
	drop()
	t.Cleanup(drop)

	// 2,000 orders of one to four lines from suppliers 1 … 5; order 1777 has
	// a line whose z is 0. The outer table adds a NULL and a missing order.
	type line struct{ ok, sk, z int }
	var lines []line
	var vals []string
	for ok := 1; ok <= 2000; ok++ {
		for j := 0; j < 1+ok%4; j++ {
			l := line{ok: ok, sk: 1 + (ok*3+j*(1+ok%3))%5, z: 1}
			if ok == 1777 && j == 0 {
				l.z = 0
			}
			lines = append(lines, l)
			vals = append(vals, fmt.Sprintf("(%d, %d, %d)", l.ok, l.sk, l.z))
		}
	}
	if len(lines) <= semiJoinEagerRows {
		t.Fatalf("%d lines do not exceed the eager build size", len(lines))
	}
	h.exec("CREATE TABLE it_sjp_big (ok BIGINT, sk BIGINT, z INTEGER)")
	h.exec("INSERT INTO it_sjp_big VALUES " + strings.Join(vals, ", "))
	vals = vals[:0]
	for ok := 1; ok <= 2000; ok++ {
		vals = append(vals, fmt.Sprintf("(%d)", ok))
	}
	h.exec("CREATE TABLE it_sjp_o (ok BIGINT)")
	h.exec("INSERT INTO it_sjp_o VALUES " + strings.Join(vals, ", ") + ", (NULL), (5000)")

	// Lines of orders with another supplier and none higher than theirs.
	want := 0
	for _, l1 := range lines {
		other, higher := false, false
		for _, l := range lines {
			if l.ok == l1.ok {
				other = other || l.sk != l1.sk
				higher = higher || l.sk > l1.sk
			}
		}
		if other && !higher {
			want++
		}
	}
	joined := `SELECT COUNT(*) FROM it_sjp_o o JOIN it_sjp_big l1 ON l1.ok = o.ok
		WHERE EXISTS (SELECT 1 FROM it_sjp_big l2 WHERE l2.ok = l1.ok AND l2.sk <> l1.sk)
		AND NOT EXISTS (SELECT 1 FROM it_sjp_big l3 WHERE l3.ok = l1.ok AND l3.sk > l1.sk)`
	// Orders with a line from a supplier other than 3 (an inner side larger
	// than the eager build).
	notSupplier3 := 0
	for ok := 1; ok <= 2000; ok++ {
		for _, l := range lines {
			if l.ok == ok && l.sk != 3 {
				notSupplier3++
				break
			}
		}
	}
	scanned := `SELECT COUNT(*) FROM it_sjp_o o WHERE EXISTS (SELECT 1 FROM it_sjp_big b WHERE b.ok = o.ok AND b.sk <> 3)`
	for _, c := range []struct {
		what  string
		h     *sqlHarness
		pages bool // parallel probe pages expected
	}{{"serial", serial, false}, {"parallel", parallel, true}} {
		for _, q := range []struct{ sql, want string }{{joined, fmt.Sprint(want)}, {scanned, fmt.Sprint(notSupplier3)}} {
			before := subqueryStats.parallelProbes.Load()
			w := c.h.expectWork(q.sql, q.want)
			pages := subqueryStats.parallelProbes.Load() - before
			if w.runs != 0 || w.semiJoins < 3 || (pages > 0) != c.pages {
				t.Errorf("%s %q: %+v and %d parallel probe pages, want bounded probes, no per-row runs and parallel pages %v",
					c.what, q.sql[:40], w, pages, c.pages)
			}
		}
		// A probe that fails (10 / 0 in order 1777's group) stops probing; the
		// per-row path then reports the query's own error.
		c.h.expectError(`SELECT COUNT(*) FROM it_sjp_o o WHERE EXISTS (SELECT 1 FROM it_sjp_big b WHERE b.ok = o.ok AND 10 / b.z > 0)`,
			"division by zero")
		// Without that order every probe succeeds.
		rows, _ := c.h.query(`SELECT COUNT(*) FROM it_sjp_o o WHERE o.ok <> 1777 AND EXISTS (SELECT 1 FROM it_sjp_big b WHERE b.ok = o.ok AND 10 / b.z > 0)`)
		if !slices.Equal(rows, []string{"1999"}) {
			t.Errorf("%s: probes without the failing order: %q, want 1999", c.what, rows)
		}
	}
}
