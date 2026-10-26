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
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestSQLAggregatePages(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_agg_pages")
	h.exec("CREATE TABLE it_agg_pages (g INTEGER, v BIGINT, pad VARCHAR)")
	st := &store{client: h.rawClient()}
	meta, err := st.getTable(h.ctx, defaultSchema, "it_agg_pages")
	if err != nil {
		t.Fatal(err)
	}
	const n = 25001
	const wide int64 = 9007199254740993
	counts, valid := make([]int, 37), make([]int, 37)
	sums := make([]big.Int, 37)
	distinct := make([]map[int64]bool, 37)
	for start := 0; start < n; start += pipelineChunk {
		pipe := st.client.Pipeline()
		for i := start; i < min(start+pipelineChunk, n); i++ {
			g := i % 37
			fields := []any{rowIDField, i + 1, "g", g, "pad", "discarded payload"}
			counts[g]++
			if i%7 != 0 {
				v := wide
				if i%2 == 1 {
					v = -v
				}
				fields = append(fields, "v", strconv.FormatInt(v, 10))
				valid[g]++
				sums[g].Add(&sums[g], big.NewInt(v))
				if distinct[g] == nil {
					distinct[g] = map[int64]bool{}
				}
				distinct[g][v] = true
			}
			pipe.HSet(h.ctx, meta.prefix()+strconv.Itoa(i+1), fields...)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	sql := "SELECT g, COUNT(*), COUNT(v), SUM(v), COUNT(DISTINCT v), AVG(v) FROM it_agg_pages GROUP BY g ORDER BY g"
	rows, _ := h.query(sql)
	if len(rows) != 37 {
		t.Fatalf("got %d groups", len(rows))
	}
	for i, row := range rows {
		cells := strings.Split(row, "|")
		want := fmt.Sprintf("%d|%d|%d|%s|%d", i, counts[i], valid[i], sums[i].String(), len(distinct[i]))
		if strings.Join(cells[:5], "|") != want {
			t.Fatalf("got %s, want %s", row, want)
		}
		avg, _ := new(big.Rat).SetFrac(&sums[i], big.NewInt(int64(valid[i]))).Float64()
		got, err := strconv.ParseFloat(cells[5], 64)
		if err != nil || math.Abs(got-avg) > 1e-12*math.Max(1, math.Abs(avg)) {
			t.Fatalf("AVG: got %s, want %g", cells[5], avg)
		}
	}
	base := "SELECT g, SUM(v), COUNT(*) FROM it_agg_pages GROUP BY g HAVING COUNT(*) > 0 ORDER BY SUM(v) DESC NULLS FIRST, g DESC"
	all, _ := h.query(base)
	for _, lim := range []struct{ off, n int }{{0, 1}, {2, 7}, {30, 20}, {37, 1}} {
		got, _ := h.query(fmt.Sprintf("%s LIMIT %d OFFSET %d", base, lim.n, lim.off))
		want := all[min(lim.off, len(all)):min(lim.off+lim.n, len(all))]
		if !slices.Equal(got, want) {
			t.Fatalf("LIMIT/OFFSET: got %q, want %q", got, want)
		}
	}
	// Aggregate input columns and the internal row id must not survive in
	// every group's representative map.
	parsed, err := ParseScript("SELECT g, SUM(v) FROM it_agg_pages GROUP BY g")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: st, schema: defaultSchema, pushdown: PushdownExact, cache: newExecCache()}
	plan, err := e.planSelect(h.ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := e.driverAggregate(h.ctx, plan, wherePlan{query: "*"}, plan.aggregateCalls(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if len(g.rep) != 1 {
			t.Fatalf("representative retains %v", g.rep)
		}
	}
	h.expectRows("SELECT COUNT(*), SUM(v), AVG(v) FROM it_agg_pages WHERE g = -1", "0|NULL|NULL")
	h.expectRows("SELECT g, SUM(v) FROM it_agg_pages WHERE g = -1 GROUP BY g")
	h.expectRows("SELECT COUNT(*), SUM(v) FROM it_agg_pages WHERE __rowid IN (1, 2, 3)", "3|0")

	// A cancelled visitor stops before the next cursor page.
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	visits := 0
	err = e.scanPages(ctx, scanRequest{meta: meta, where: wherePlan{query: "*"}, need: map[string]bool{"v": true}}, nil, func(rows []map[string]Value) error {
		visits++
		if len(rows) >= n {
			t.Fatal("materialized the full scan")
		}
		cancel()
		return cancelled(ctx)
	})
	if !isStatus(err, adbc.StatusCancelled) || visits != 1 {
		t.Fatalf("got %v, %d pages", err, visits)
	}
	// Accumulator errors use the same cursor-close path.
	sentinel := errors.New("stop reduction")
	err = e.scanPages(context.Background(), scanRequest{meta: meta, where: wherePlan{query: "*"}, need: map[string]bool{"v": true}}, nil, func([]map[string]Value) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	// FT.INFO on the cursor's coordinator must report no cursor left open.
	info, err := st.searchDo(h.ctx, meta.index(), "FT.INFO", meta.index()).Result()
	if err != nil {
		t.Fatal(err)
	}
	fields, ok := info.([]any)
	if !ok {
		t.Fatalf("unexpected FT.INFO reply %T", info)
	}
	found := false
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] != "cursor_stats" {
			continue
		}
		stats, ok := fields[i+1].([]any)
		if !ok {
			t.Fatalf("unexpected cursor stats %T", fields[i+1])
		}
		for j := 0; j+1 < len(stats); j += 2 {
			if stats[j] == "index_total" {
				found = true
				if stats[j+1] != int64(0) {
					t.Fatalf("leaked cursor: %v", stats)
				}
			}
		}
	}
	if !found {
		t.Fatal("FT.INFO omitted index cursor count")
	}
}

func TestAggregateSharedMemoryRows(t *testing.T) {
	meta := &tableMeta{isMem: true, Columns: []columnMeta{{Name: "g", Type: typeInt32}, {Name: "v", Type: typeInt64}}, mem: []map[string]Value{
		{"g": intValue(typeInt32, 1), "v": intValue(typeInt64, 10)},
		{"g": intValue(typeInt32, 1), "v": intValue(typeInt64, 20)},
	}}
	fn := &Func{Name: "SUM", Args: []Expr{&ColumnRef{Name: "v"}}}
	plan := &selectPlan{meta: meta, sel: &SelectStmt{GroupBy: []Expr{&ColumnRef{Name: "g"}}}, items: []planItem{{expr: &ColumnRef{Name: "g"}}, {expr: fn}}}
	e := &executor{}
	for i := 0; i < 2; i++ {
		groups, err := e.driverAggregate(context.Background(), plan, wherePlan{}, []*Func{fn}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 || len(groups[0].rep) != 1 {
			t.Fatalf("groups: %v", groups)
		}
		if meta.mem[0]["v"].I != 10 || meta.mem[1]["v"].I != 20 {
			t.Fatal("pruned shared source rows")
		}
	}
}
