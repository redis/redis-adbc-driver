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
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestAggregateIntegerInputs(t *testing.T) {
	types := map[string]ColType{"x": typeInt64, "y": typeInt64}
	col := func(name string) Expr { return &ColumnRef{Name: name} }
	lit := func(n int64) Expr { return &Literal{V: intValue(typeInt64, n)} }
	shared := func() Expr { return &Binary{Op: "+", L: col("x"), R: lit(1)} }
	f1 := &Func{Name: "SUM", Args: []Expr{&Binary{Op: "*", L: shared(), R: lit(2)}}}
	f2 := &Func{Name: "AVG", Args: []Expr{&Binary{Op: "-", L: shared(), R: lit(3)}}}
	p := compileAggregateInputs([]*Func{f1, f2}, types)
	if len(p.nodes) != 7 {
		t.Fatalf("shared DAG: %d nodes", len(p.nodes))
	}
	for _, n := range []int64{0, 9, -5, math.MaxInt64, math.MinInt64} {
		row := map[string]Value{"x": intValue(typeInt64, n)}
		p.nextRow()
		env := &evalEnv{row: row, types: types}
		for i, f := range []*Func{f1, f2} {
			got, err := p.value(p.roots[i], row)
			want, werr := env.eval(f.Args[0])
			if (err == nil) != (werr == nil) || (err != nil && err.Error() != werr.Error()) {
				t.Fatalf("%d: %v %v", n, err, werr)
			}
			if err == nil && (got.T != want.T || got.I != want.I || got.Null != want.Null) {
				t.Fatalf("%d: %v %v", n, got, want)
			}
		}
	}
	p.nextRow()
	got, err := p.value(p.roots[0], map[string]Value{"x": nullValue(typeInt64)})
	if err != nil || !got.Null {
		t.Fatalf("NULL: %v %v", got, err)
	}
	// NULL on the left does not suppress overflow on the right.
	f := &Func{Name: "SUM", Args: []Expr{&Binary{Op: "+", L: col("y"), R: shared()}}}
	p = compileAggregateInputs([]*Func{f}, types)
	p.nextRow()
	_, err = p.value(p.roots[0], map[string]Value{"x": intValue(typeInt64, math.MaxInt64), "y": nullValue(typeInt64)})
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("right operand error: %v", err)
	}
}

func TestAggregateInputFallback(t *testing.T) {
	cases := []*Func{
		{Name: "SUM", Args: []Expr{&ColumnRef{Name: "f"}}},
		{Name: "SUM", Args: []Expr{&ColumnRef{Name: "d"}}},
		{Name: "SUM", Distinct: true, Args: []Expr{&ColumnRef{Name: "x"}}},
		{Name: "SUM", Filter: &Literal{V: boolValue(false)}, Args: []Expr{&ColumnRef{Name: "x"}}},
		{Name: "SUM", Args: []Expr{&Binary{Op: "/", L: &ColumnRef{Name: "x"}, R: &Literal{V: intValue(typeInt64, 0)}}}},
		{Name: "SUM", Args: []Expr{&Func{Name: "RANDOM"}}},
	}
	p := compileAggregateInputs(cases, map[string]ColType{"x": typeInt64, "f": typeFloat64, "d": decimalType(20, 3)})
	for i, id := range p.roots {
		if id != -1 {
			t.Fatalf("fallback %d compiled", i)
		}
	}
}

func TestSQLAggregateExpressionInputs(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_agg_inputs")
	h.exec("CREATE TABLE it_agg_inputs (x BIGINT, d NUMERIC(20,3), f DOUBLE PRECISION)")
	sql := "SELECT SUM(x+1) AS same, SUM((x+1)*2) AS same, AVG(x+1), COUNT(x+1), MIN(x+1), MAX(x+1) FROM it_agg_inputs"
	h.expectRows(sql, "NULL|NULL|NULL|0|NULL|NULL")
	h.exec("INSERT INTO it_agg_inputs VALUES (1,1.001,1.25),(2,2.002,2.5),(NULL,NULL,NULL)")
	h.expectRows(sql, "5|10|2.5|2|2|3")
	h.expectRows("SELECT SUM(d+0.001), SUM(f+0.5), SUM(x+1) FROM it_agg_inputs", "3.005|4.75|5")
	h.expectRows("SELECT x, SUM(x+1), SUM((x+1)*2) FROM it_agg_inputs GROUP BY x ORDER BY x", "1|2|4", "2|3|6", "NULL|NULL|NULL")
	h.expectRows("SELECT SUM(x+1) FILTER (WHERE x=1), SUM(x/0) FILTER (WHERE FALSE) FROM it_agg_inputs", "2|NULL")

	h.exec("DELETE FROM it_agg_inputs")
	h.exec("INSERT INTO it_agg_inputs(d,f) VALUES (9999999999999999.001,10000000000000000),(-9999999999999999.000,-10000000000000000),(NULL,1)")
	h.expectRows("SELECT SUM(d+0.001) AS same, SUM(d+0.001) AS same, SUM(f+1),SUM(f) FROM it_agg_inputs", "0.003|0.003|2|1")
	h.exec("DELETE FROM it_agg_inputs")
	h.exec("INSERT INTO it_agg_inputs(x) VALUES (9223372036854775807),(-9223372036854775808)")
	// Cancellation of the final sums cannot hide a per-row overflow.
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err = st.SetSqlQuery(h.ctx, "SELECT SUM(x),SUM(x+1) FROM it_agg_inputs"); err != nil {
		t.Fatal(err)
	}
	if rdr, _, err := st.ExecuteQuery(h.ctx); err == nil {
		rdr.Release()
		t.Fatal("expected per-row overflow")
	}
}

func TestAggregateIntegerBoundariesAndErrorOrder(t *testing.T) {
	for _, op := range []string{"+", "-", "*"} {
		for _, a := range []int64{math.MinInt64, math.MinInt64 + 1, -1, 0, 1, math.MaxInt64 - 1, math.MaxInt64} {
			for _, b := range []int64{math.MinInt64, -1, 0, 1, math.MaxInt64} {
				x := &Binary{Op: op, L: &ColumnRef{Name: "a"}, R: &ColumnRef{Name: "b"}}
				p := compileAggregateInputs([]*Func{{Name: "SUM", Args: []Expr{x}}}, map[string]ColType{"a": typeInt64, "b": typeInt64})
				row := map[string]Value{"a": intValue(typeInt64, a), "b": intValue(typeInt64, b)}
				p.nextRow()
				v, err := p.value(p.roots[0], row)
				want, werr := (&evalEnv{row: row}).eval(x)
				// Independent exact arithmetic checks the shared overflow helper.
				z := new(big.Int)
				switch op {
				case "+":
					z.Add(big.NewInt(a), big.NewInt(b))
				case "-":
					z.Sub(big.NewInt(a), big.NewInt(b))
				case "*":
					z.Mul(big.NewInt(a), big.NewInt(b))
				}
				if (err == nil) != z.IsInt64() || (err == nil && v.I != z.Int64()) {
					t.Fatalf("exact boundary %d %s %d: %v %v", a, op, b, v, err)
				}
				if (err == nil) != (werr == nil) || (err != nil && err.Error() != werr.Error()) || (err == nil && v.I != want.I) {
					t.Fatalf("%d %s %d: %v %v; %v %v", a, op, b, v, err, want, werr)
				}
			}
		}
	}
	h := newSQLHarness(t)
	h.dropTables("it_agg_input_error")
	h.exec("CREATE TABLE it_agg_input_error (x BIGINT)")
	h.exec("INSERT INTO it_agg_input_error VALUES (9223372036854775807)")
	for _, tc := range []struct{ sql, message string }{
		{"SELECT SUM(x+1), SUM(x/0) FROM it_agg_input_error", "integer overflow"},
		{"SELECT SUM(x/0), SUM(x+1) FROM it_agg_input_error", "division by zero"},
	} {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = st.SetSqlQuery(h.ctx, tc.sql); err != nil {
			t.Fatal(err)
		}
		rdr, _, err := st.ExecuteQuery(h.ctx)
		if rdr != nil {
			rdr.Release()
		}
		st.Close(h.ctx)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("error order: %s: %v", tc.sql, err)
		}
	}
}
