// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// snowflakeFixture is a small TPC-H-shaped schema as CTEs: duplicate order
// keys, NULL join keys, lineitems without orders or suppliers, a nation
// without a region and a supplier/customer without a nation.
func snowflakeFixture() string {
	var b strings.Builder
	values := func(name, cols string, rows []string) {
		if b.Len() == 0 {
			b.WriteString("WITH ")
		} else {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s(%s) AS (VALUES %s)", name, cols, strings.Join(rows, ","))
	}
	values("region", "r_regionkey,r_name", []string{"(0,'AFRICA')", "(1,'AMERICA')", "(2,'ASIA')"})
	var nation []string
	for n := 0; n < 6; n++ {
		nation = append(nation, fmt.Sprintf("(%d,'N%d',%d)", n, n, n%3))
	}
	nation = append(nation, "(6,'NOREGION',NULL)")
	values("nation", "n_nationkey,n_name,n_regionkey", nation)
	var supplier []string
	for s := 1; s <= 30; s++ {
		key := fmt.Sprint(s % 7)
		if s == 30 {
			key = "NULL"
		}
		supplier = append(supplier, fmt.Sprintf("(%d,%s)", s, key))
	}
	values("supplier", "s_suppkey,s_nationkey", supplier)
	var customer []string
	for c := 1; c <= 40; c++ {
		key := fmt.Sprint(c % 7)
		if c == 40 {
			key = "NULL"
		}
		customer = append(customer, fmt.Sprintf("(%d,%s)", c, key))
	}
	values("customer", "c_custkey,c_nationkey", customer)
	var orders []string
	for o := 1; o <= 120; o++ {
		cust := fmt.Sprint(o%41 + 1)
		if o%17 == 0 {
			cust = "NULL"
		}
		orders = append(orders, fmt.Sprintf("(%d,%s,CAST('1994-%02d-%02d' AS DATE))", o, cust, o%12+1, o%28+1))
	}
	orders = append(orders, "(5,7,CAST('1994-03-03' AS DATE))") // a duplicate order key
	values("orders", "o_orderkey,o_custkey,o_orderdate", orders)
	var part []string
	for p := 1; p <= 25; p++ {
		name := "plain"
		if p%3 == 0 {
			name = "dark green"
		}
		part = append(part, fmt.Sprintf("(%d,'%s')", p, name))
	}
	values("part", "p_partkey,p_name", part)
	var partsupp []string
	for p := 1; p <= 25; p++ {
		for s := 1; s <= 31; s += 3 {
			partsupp = append(partsupp, fmt.Sprintf("(%d,%d,%d.25)", p, s, (p*s)%17))
		}
	}
	values("partsupp", "ps_partkey,ps_suppkey,ps_cost", partsupp)
	var lineitem []string
	for i := 0; i < 500; i++ {
		order := fmt.Sprint(i%125 + 1)
		if i%53 == 0 {
			order = "NULL"
		}
		lineitem = append(lineitem, fmt.Sprintf("(%s,%d,%d,%d.50,%d)", order, i%31+1, i%26+1, i%97, i%4+1))
	}
	values("lineitem", "l_orderkey,l_suppkey,l_partkey,l_price,l_qty", lineitem)
	return b.String() + " "
}

var snowflakeQueries = map[string]string{
	// Q5: c_nationkey = s_nationkey is a cross-branch residual, not a join.
	"q5": `SELECT n_name, SUM(l_price) AS revenue, COUNT(*) FROM customer, orders, lineitem, supplier, nation, region
		WHERE c_custkey = o_custkey AND l_orderkey = o_orderkey AND l_suppkey = s_suppkey AND c_nationkey = s_nationkey
		AND s_nationkey = n_nationkey AND n_regionkey = r_regionkey AND r_name = 'ASIA'
		AND o_orderdate >= CAST('1994-03-01' AS DATE) GROUP BY n_name ORDER BY n_name`,
	// Q3: a date group key and an ORDER BY … LIMIT over the aggregate.
	"q3": `SELECT l_orderkey, SUM(l_price) AS revenue, o_orderdate FROM customer, orders, lineitem
		WHERE c_nationkey = 2 AND c_custkey = o_custkey AND l_orderkey = o_orderkey AND o_orderdate < CAST('1994-09-01' AS DATE)
		GROUP BY l_orderkey, o_orderdate ORDER BY revenue DESC, l_orderkey LIMIT 7`,
	// Q7: a derived table (scanMem) and an OR across two nation aliases.
	"q7": `SELECT supp, cust, COUNT(*), SUM(v) FROM (SELECT n1.n_name AS supp, n2.n_name AS cust, l_price AS v
		FROM supplier, lineitem, orders, customer, nation n1, nation n2
		WHERE s_suppkey = l_suppkey AND o_orderkey = l_orderkey AND c_custkey = o_custkey
		AND s_nationkey = n1.n_nationkey AND c_nationkey = n2.n_nationkey
		AND ((n1.n_name = 'N1' AND n2.n_name = 'N2') OR (n1.n_name = 'N2' AND n2.n_name = 'N1'))) x
		GROUP BY supp, cust ORDER BY supp, cust`,
	// Q9: a composite partsupp key and an arbitrary pushed filter (LIKE).
	"q9": `SELECT nation, SUM(amount) FROM (SELECT n_name AS nation, l_price - ps_cost * l_qty AS amount
		FROM part, supplier, lineitem, partsupp, orders, nation
		WHERE s_suppkey = l_suppkey AND ps_suppkey = l_suppkey AND ps_partkey = l_partkey AND p_partkey = l_partkey
		AND o_orderkey = l_orderkey AND s_nationkey = n_nationkey AND p_name LIKE '%green%') p GROUP BY nation ORDER BY nation`,
	// A materialized join with duplicates: the multiset must not change.
	"rows": `SELECT l_orderkey, o_orderkey, c_custkey, l_price FROM lineitem, orders, customer
		WHERE l_orderkey = o_orderkey AND o_custkey = c_custkey AND c_nationkey < 3`,
}

func withSnowflake(t *testing.T, minFact int64) {
	t.Helper()
	old := snowflakeMinFactRows
	snowflakeMinFactRows = minFact
	t.Cleanup(func() { snowflakeMinFactRows = old })
}

func snowflakeRun(t *testing.T, q string, minFact int64) ([]string, string) {
	t.Helper()
	old := snowflakeMinFactRows
	snowflakeMinFactRows = minFact
	defer func() { snowflakeMinFactRows = old }()
	// Diagnostics first: derived tables are materialized while planning.
	ps, err := ParseScript(snowflakeFixture() + q)
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{cache: newExecCache(), diagnostics: &executionDiagnostics{}}
	p, err := e.planSelect(context.Background(), ps[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamed := e.diagnostics.plan.Load() != nil && strings.Contains(string(*e.diagnostics.plan.Load()), "streaming_snowflake_join")
	rows, err := e.runSelect(context.Background(), p, nil)
	if err != nil {
		t.Fatal(err)
	}
	kind := ""
	if raw := e.diagnostics.plan.Load(); raw != nil {
		var marker map[string]any
		if err := json.Unmarshal(*raw, &marker); err != nil {
			t.Fatal(err)
		}
		kind, _ = marker["kind"].(string)
	}
	if streamed {
		kind = "streaming_snowflake_join"
	}
	var out []string
	for _, r := range rows {
		var fields []string
		for _, v := range r {
			fields = append(fields, partitionValueString(v)+"/"+v.T.Kind.String())
		}
		out = append(out, strings.Join(fields, "|"))
	}
	return out, kind
}

// Before the snowflake plan, every query here ran the eager join (Q5 joins
// customer to supplier on nation first); afterwards each streams its fact
// and returns exactly the eager result.
func TestSnowflakeJoinsMatchEagerJoins(t *testing.T) {
	for name, q := range snowflakeQueries {
		t.Run(name, func(t *testing.T) {
			want, wantKind := snowflakeRun(t, q, math.MaxInt64)
			got, kind := snowflakeRun(t, q, 1)
			if wantKind == "streaming_snowflake_join" {
				t.Fatal("the eager baseline streamed")
			}
			if kind != "streaming_snowflake_join" {
				t.Fatalf("plan kind %q, want streaming_snowflake_join", kind)
			}
			if len(want) == 0 {
				t.Fatal("fixture query returned no rows")
			}
			if name == "rows" {
				sort.Strings(got)
				sort.Strings(want)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v\nwant %v", got, want)
			}
		})
	}
}

func snowflakeTestShape(t *testing.T, q string) (*executor, *selectPlan, *joinPlan) {
	t.Helper()
	withSnowflake(t, 1)
	e, p := deferredTestPlan(t, snowflakeFixture()+q)
	if p.meta.join == nil {
		t.Fatal("not a join")
	}
	return e, p, p.meta.join
}

func snowflakeItem(t *testing.T, jp *joinPlan, prefix string) int {
	t.Helper()
	for i, it := range jp.items {
		if it.prefix == prefix {
			return i
		}
	}
	t.Fatalf("no item %s", prefix)
	return -1
}

// The Q5 tree hangs customer off orders (the larger candidate parent) and
// nation/region off supplier; the nation equality is a residual instead of a
// many-to-many join of customers and suppliers.
func TestSnowflakeQ5TreeAvoidsManyToManyJoin(t *testing.T) {
	e, p, jp := snowflakeTestShape(t, snowflakeQueries["q5"])
	if _, ok := e.planStarShape(context.Background(), jp, map[string]bool{}, p.sel.Where); ok {
		t.Fatal("pure star admitted a snowflake")
	}
	s, ok := e.planSnowflakeShape(context.Background(), jp, map[string]bool{}, p.sel.Where)
	if !ok {
		t.Fatal("snowflake rejected")
	}
	if jp.items[s.fact].prefix != "lineitem." {
		t.Fatalf("fact %s", jp.items[s.fact].prefix)
	}
	branches := map[string][]string{}
	for _, d := range s.dims {
		root := jp.items[d.item].prefix
		members := []string{root}
		for _, i := range d.branchItems[min(1, len(d.branchItems)):] {
			members = append(members, jp.items[i].prefix)
		}
		sort.Strings(members[1:])
		branches[root] = members
	}
	want := map[string][]string{"orders.": {"orders.", "customer."}, "supplier.": {"supplier.", "nation.", "region."}}
	if !reflect.DeepEqual(branches, want) {
		t.Fatalf("branches %v, want %v", branches, want)
	}
	need := map[string]bool{}
	columnRefs(s.residual, need)
	if !need["customer.c_nationkey"] || !need["supplier.s_nationkey"] || len(need) != 2 {
		t.Fatalf("residual columns %v", need)
	}
}

// The branch cap falls back to the eager join before any fact row is read.
func TestSnowflakeBranchCapFallsBackBeforeFacts(t *testing.T) {
	e, p, jp := snowflakeTestShape(t, snowflakeQueries["q5"])
	old := snowflakeBranchRows
	snowflakeBranchRows = 3
	defer func() { snowflakeBranchRows = old }()
	visited := false
	handled, err := e.streamJoinPages(context.Background(), jp, map[string]bool{"nation.n_name": true}, p.sel.Where, nil, true, func([]map[string]Value) error {
		visited = true
		return nil
	})
	if err != nil || handled || visited {
		t.Fatalf("handled=%v visited=%v err=%v", handled, visited, err)
	}
	snowflakeBranchRows = old
	got, _ := snowflakeRun(t, snowflakeQueries["q5"], 1)
	want, _ := snowflakeRun(t, snowflakeQueries["q5"], math.MaxInt64)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSnowflakeRejectsUnsafeShapes(t *testing.T) {
	for name, q := range map[string]string{
		"outer join":         `SELECT COUNT(*) FROM lineitem LEFT JOIN orders ON l_orderkey = o_orderkey JOIN customer ON o_custkey = c_custkey`,
		"cross product":      `SELECT COUNT(*) FROM lineitem, orders, region WHERE l_orderkey = o_orderkey`,
		"function residual":  `SELECT COUNT(*) FROM lineitem, orders, customer WHERE l_orderkey = o_orderkey AND o_custkey = c_custkey AND l_qty + c_nationkey > 3`,
		"date against text":  `SELECT COUNT(*) FROM lineitem, orders, customer WHERE l_orderkey = o_orderkey AND o_custkey = c_custkey AND o_orderdate < CAST(c_custkey AS VARCHAR)`,
		"mixed type key":     `SELECT COUNT(*) FROM lineitem, orders, customer WHERE l_orderkey = o_orderkey AND CAST(o_custkey AS DOUBLE) = c_custkey`,
		"correlated filters": `SELECT COUNT(*) FROM lineitem, orders WHERE l_orderkey = o_orderkey AND EXISTS (SELECT 1 FROM customer WHERE c_custkey = o_custkey)`,
	} {
		t.Run(name, func(t *testing.T) {
			e, p, jp := snowflakeTestShape(t, q)
			if _, ok := e.planSnowflakeShape(context.Background(), jp, map[string]bool{}, p.sel.Where); ok {
				t.Fatal("unsafe shape admitted")
			}
		})
	}
}

func TestSnowflakeMinimumFactKeepsSmallJoinsEager(t *testing.T) {
	e, p, jp := snowflakeTestShape(t, snowflakeQueries["q5"])
	snowflakeMinFactRows = 501 // lineitem has 500 rows
	if _, ok := e.planSnowflakeShape(context.Background(), jp, map[string]bool{}, p.sel.Where); ok {
		t.Fatal("small fact admitted")
	}
}

func TestSnowflakePredicateClasses(t *testing.T) {
	types := map[string]ColType{"d": {Kind: KindDate}, "s": typeString, "i": typeInt64, "n": decimalType(10, 2)}
	col := func(n string) Expr { return &ColumnRef{Name: n} }
	text := func(s string) Expr { return &Literal{V: stringValue(s), Unknown: true} }
	for _, c := range []struct {
		x    Expr
		pure bool
	}{
		{&Binary{Op: "=", L: col("s"), R: text("x")}, true},
		{&Binary{Op: "<", L: col("d"), R: text("1994-01-01")}, false}, // the literal would be parsed
		{&Binary{Op: "<", L: col("d"), R: col("d")}, true},
		{&Binary{Op: "=", L: col("i"), R: col("n")}, true},
		{&Binary{Op: "=", L: col("i"), R: col("s")}, false},
		{&Binary{Op: "=", L: col("d"), R: col("i")}, false},
		{&Binary{Op: "OR", L: &Binary{Op: "=", L: col("s"), R: text("a")}, R: &Binary{Op: "=", L: col("i"), R: &Literal{V: intValue(typeInt64, 1)}}}, true},
		{&Binary{Op: "+", L: col("i"), R: col("i")}, false},
	} {
		if got := snowflakePurePredicate(c.x, types); got != c.pure {
			b, _ := json.Marshal(c.x)
			t.Errorf("%s: pure=%v, want %v", b, got, c.pure)
		}
	}
}

// Q7's OR implies one-of-two-values filters for both nation aliases; the
// complete OR is still returned (and stays the residual).
func TestImpliedLiteralDisjunctions(t *testing.T) {
	eq := func(c, v string) Expr {
		return &Binary{Op: "=", L: &ColumnRef{Name: c}, R: &Literal{V: stringValue(v), Unknown: true}}
	}
	and := func(l, r Expr) Expr { return &Binary{Op: "AND", L: l, R: r} }
	or := &Binary{Op: "OR", L: and(eq("n1.n_name", "FRANCE"), eq("n2.n_name", "GERMANY")), R: and(eq("n1.n_name", "GERMANY"), eq("n2.n_name", "FRANCE"))}
	got := map[string]int{}
	for _, x := range impliedJoinConjuncts(or) {
		if x == Expr(or) {
			got["whole"]++
			continue
		}
		if c, ok := literalDisjunctionColumn(x); ok {
			got[c]++
		}
	}
	if !reflect.DeepEqual(got, map[string]int{"whole": 1, "n1.n_name": 1, "n2.n_name": 1}) {
		t.Fatalf("implied %v", got)
	}
	// A column restricted in only one branch implies nothing.
	half := &Binary{Op: "OR", L: and(eq("a", "x"), eq("b", "y")), R: eq("b", "z")}
	for _, x := range impliedJoinConjuncts(half) {
		if c, ok := literalDisjunctionColumn(x); ok && c == "a" {
			t.Fatal("a one-branch column was implied")
		}
	}
	// Column-to-column equalities are join keys, not literal lists.
	if _, ok := literalDisjunctionColumn(&Binary{Op: "=", L: &ColumnRef{Name: "a"}, R: &ColumnRef{Name: "b"}}); ok {
		t.Fatal("column equality treated as a literal")
	}
}

// With the default thresholds, a Q5-shaped join over a 70,000-row fact
// streams instead of joining customers to suppliers by nation in memory.
// This regression uses only the pre-existing diagnostics contract.
func TestSnowflakeQ5StreamsByDefault(t *testing.T) {
	fixture := snowflakeFixture()
	var lineitem []string
	for i := 0; i < 70000; i++ {
		lineitem = append(lineitem, fmt.Sprintf("(%d,%d,%d.50)", i%125+1, i%31+1, i%97))
	}
	fixture += ", big(b_orderkey,b_suppkey,b_price) AS (VALUES " + strings.Join(lineitem, ",") + ") "
	q := `SELECT n_name, SUM(b_price), COUNT(*) FROM customer, orders, big, supplier, nation, region
		WHERE c_custkey = o_custkey AND b_orderkey = o_orderkey AND b_suppkey = s_suppkey AND c_nationkey = s_nationkey
		AND s_nationkey = n_nationkey AND n_regionkey = r_regionkey AND r_name = 'ASIA' GROUP BY n_name ORDER BY n_name`
	e, p := deferredTestPlan(t, fixture+q)
	e.diagnostics = &executionDiagnostics{}
	rows, err := e.runSelect(context.Background(), p, nil)
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	raw := e.diagnostics.plan.Load()
	if raw == nil || !strings.Contains(string(*raw), `"kind":"streaming_snowflake_join"`) {
		t.Fatalf("Q5-shaped join did not stream: %v", raw)
	}
}

// The fact is the hub of the large tables: a larger leaf (partsupp in the
// TPC-H subset) does not displace lineitem, and a referenced table with
// several neighbours but under half the fact's rows (orders) does not either.
func TestSnowflakeFactIsTheLargeHub(t *testing.T) {
	var ps []string
	for i := 0; i < 600; i++ {
		ps = append(ps, fmt.Sprintf("(%d,%d,%d)", i%26+1, i%31+1, i))
	}
	fixture := snowflakeFixture() + ", bigps(b_partkey,b_suppkey,b_cost) AS (VALUES " + strings.Join(ps, ",") + ") "
	for name, c := range map[string]struct{ q, fact string }{
		"larger leaf": {`SELECT COUNT(*) FROM part, supplier, lineitem, bigps, orders WHERE p_partkey = l_partkey AND s_suppkey = l_suppkey
			AND b_partkey = l_partkey AND b_suppkey = l_suppkey AND o_orderkey = l_orderkey`, "lineitem."},
		"referenced hub": {`SELECT COUNT(*) FROM customer, orders, lineitem WHERE c_custkey = o_custkey AND l_orderkey = o_orderkey`, "lineitem."},
	} {
		t.Run(name, func(t *testing.T) {
			withSnowflake(t, 1)
			e, p := deferredTestPlan(t, fixture+c.q)
			s, ok := e.planSnowflakeShape(context.Background(), p.meta.join, map[string]bool{}, p.sel.Where)
			if !ok {
				t.Fatal("snowflake rejected")
			}
			if got := p.meta.join.items[s.fact].prefix; got != c.fact {
				t.Fatalf("fact %s, want %s", got, c.fact)
			}
		})
	}
}

// Parallel readers refuse subqueries (each worker has its own cache) but
// accept constant casts, as in TPC-H's CAST('1995-03-15' AS date).
func TestSnowflakeParallelPredicate(t *testing.T) {
	ship := &ColumnRef{Name: "l.l_shipdate"}
	date := &Cast{X: &Literal{V: stringValue("1995-03-15"), Unknown: true}, T: ColType{Kind: KindDate}}
	sub := &Subquery{Kind: SubqueryExists, Select: &SelectStmt{}}
	for _, c := range []struct {
		x  Expr
		ok bool
	}{
		{&Binary{Op: ">", L: ship, R: date}, true},
		{&Binary{Op: "AND", L: &Binary{Op: ">", L: ship, R: date}, R: &Func{Name: "LIKE", Args: []Expr{&ColumnRef{Name: "p.p_name"}, &Literal{V: stringValue("%green%")}}}}, true},
		{&Binary{Op: "AND", L: &Binary{Op: ">", L: ship, R: date}, R: sub}, false},
		{&Binary{Op: "=", L: ship, R: &Cast{X: ship, T: ColType{Kind: KindDate}}}, false},
	} {
		if got := snowflakeParallelPredicate(c.x); got != c.ok {
			t.Errorf("%#v: got %v, want %v", c.x, got, c.ok)
		}
	}
}

// A large multi-table branch is itself a snowflake: Q5's orders⋈customer
// branch streams orders through the parallel readers with customer as its
// dimension, instead of the eager join scanning both serially (which Flex
// does over every row of the whole directory).
func TestSnowflakeLargeBranchStreamsRecursively(t *testing.T) {
	e, p, jp := snowflakeTestShape(t, snowflakeQueries["q5"])
	s, ok := e.planSnowflakeShape(context.Background(), jp, map[string]bool{}, p.sel.Where)
	if !ok {
		t.Fatal("snowflake rejected")
	}
	var orders *starDimension
	for i := range s.dims {
		if jp.items[s.dims[i].item].prefix == "orders." {
			orders = &s.dims[i]
		}
	}
	if orders == nil || orders.branch == nil {
		t.Fatal("orders is not a multi-table branch")
	}
	need := map[string]bool{"orders.o_orderkey": true, "customer.c_nationkey": true}
	sub, ok := e.planSnowflakeShape(context.Background(), orders.branch, need, nil)
	if !ok {
		t.Fatal("the orders branch was not admitted as its own snowflake")
	}
	if got := orders.branch.items[sub.fact].prefix; got != "orders." {
		t.Fatalf("branch fact %s, want orders.", got)
	}
	rows, streamed, err := e.snowflakeJoinRows(context.Background(), orders.branch, need, nil)
	if err != nil || !streamed {
		t.Fatalf("streamed=%v err=%v", streamed, err)
	}
	eager, err := e.runJoin(context.Background(), orders.branch, need, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := func(rows []map[string]Value) []string {
		var out []string
		for _, r := range rows {
			out = append(out, partitionValueString(r["orders.o_orderkey"])+"|"+partitionValueString(r["customer.c_nationkey"]))
		}
		sort.Strings(out)
		return out
	}
	if a, b := text(rows), text(eager); len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Fatalf("streamed branch %d rows, eager %d rows, equal=%v", len(a), len(b), reflect.DeepEqual(a, b))
	}
	// Below the minimum fact size the branch keeps the eager join.
	snowflakeMinFactRows = math.MaxInt64
	if _, streamed, err := e.snowflakeJoinRows(context.Background(), orders.branch, need, nil); err != nil || streamed {
		t.Fatalf("small branch streamed=%v err=%v", streamed, err)
	}
}
