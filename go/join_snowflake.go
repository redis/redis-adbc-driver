// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"maps"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	goredis "github.com/redis/go-redis/v9"
)

// Snowflake joins (TPC-H Q3/Q5/Q7/Q8/Q9) are inner equi-joins whose
// dimensions also join each other: customer through orders, nation through
// supplier. The eager join materializes every intermediate row, and its
// greedy order can join two dimensions on a low-cardinality key first
// (customer ⋈ supplier on nation), so a 10% TPC-H subset already needs GBs.
//
// planSnowflakeShape keeps the streamed fact of planStarShape and replaces
// each single-table dimension by a branch: the subtree of a spanning tree,
// rooted at the fact, over same-type column equalities. A branch is
// pre-joined on its own (the existing eager join over just its tables and
// conditions) and probed by its edge columns, like a star dimension.
// Remaining equalities and other pure multi-table conditions are a residual
// checked on every complete output row. Only shapes planStarShape rejects are
// planned here, so existing star plans keep their behavior.

// snowflakeBranchRows bounds all rows retained by one snowflake shape's
// branches. A larger total falls back to the eager join before any fact row
// is read, as an over-cap filtered star dimension does. Retained rows keep
// only output and residual columns; TPC-H SF1 Q9 retains about 2.3 million
// (all orders and partsupp, plus green parts and suppliers).
var snowflakeBranchRows = 4000000

// snowflakeMinFactRows is the smallest streamed fact a snowflake plans for.
// Smaller joins keep the eager plan and its row order. Tests lower it.
var snowflakeMinFactRows int64 = 65536

// snowflakeKeyType adds DATE to the exact star key types: equal dates are
// identical values, so keys and group representatives are unambiguous.
func snowflakeKeyType(t ColType) bool {
	return starKeyType(t) || t.Kind == KindDate
}

// Pure snowflake predicates compare columns and literals of one value class
// (numbers, strings, dates or booleans), so evaluating them before, after or
// without the eager join order can't raise a different error.
func snowflakePurePredicate(x Expr, types map[string]ColType) bool {
	switch v := x.(type) {
	case nil:
		return true
	case *Literal:
		return v.V.Null || v.V.T.Kind == KindBool
	case *Unary:
		return v.Op == "NOT" && snowflakePurePredicate(v.X, types)
	case *Binary:
		switch v.Op {
		case "AND", "OR":
			return snowflakePurePredicate(v.L, types) && snowflakePurePredicate(v.R, types)
		case "=", "!=", "<>", "<", "<=", ">", ">=":
			l, lok := snowflakePredicateClass(v.L, types)
			r, rok := snowflakePredicateClass(v.R, types)
			return lok && rok && (l == r || l == 0 || r == 0)
		}
	}
	return false
}

// snowflakePredicateClass is a comparison operand's value class: 1 numbers,
// 2 strings, 3 dates, 4 booleans; 0 for a NULL literal, which matches any.
func snowflakePredicateClass(x Expr, types map[string]ColType) (int, bool) {
	class := func(t ColType) (int, bool) {
		switch {
		case t.Kind == KindBool:
			return 4, true
		case t.Kind == KindDate:
			return 3, true
		case t.Kind == KindString && t.Collation == "":
			return 2, true
		case t.Kind.isInteger() || t.Kind == KindDecimal:
			return 1, true
		}
		return 0, false
	}
	switch v := x.(type) {
	case *ColumnRef:
		t, ok := types[v.Name]
		if !ok || v.Outer != 0 || v.whole != nil {
			return 0, false
		}
		return class(t)
	case *Literal:
		if v.V.Null {
			return 0, true
		}
		if v.Unknown {
			// An untyped string literal stays text against a string column; any
			// other comparison would parse it, which can fail.
			if v.V.T.Kind == KindString {
				return 2, true
			}
			return 0, false
		}
		return class(v.V.T)
	}
	return 0, false
}

type snowflakeEdge struct {
	a, b       int // items
	aCol, bCol string
	expr       Expr
}

// snowflakeEdgeOf returns the items and columns of `a.x = b.y` between two
// items, for columns of one exact key type.
func snowflakeEdgeOf(jp *joinPlan, x Expr) (snowflakeEdge, bool) {
	b, ok := x.(*Binary)
	if !ok || b.Op != "=" {
		return snowflakeEdge{}, false
	}
	l, lok := b.L.(*ColumnRef)
	r, rok := b.R.(*ColumnRef)
	if !lok || !rok || l.Outer != 0 || r.Outer != 0 || l.whole != nil || r.whole != nil {
		return snowflakeEdge{}, false
	}
	li, ri := -1, -1
	var lt, rt ColType
	for i, it := range jp.items {
		if c, ok := it.view().column(l.Name); ok {
			li, lt = i, c.Type
		}
		if c, ok := it.view().column(r.Name); ok {
			ri, rt = i, c.Type
		}
	}
	if li < 0 || ri < 0 || li == ri || lt != rt || !snowflakeKeyType(lt) {
		return snowflakeEdge{}, false
	}
	return snowflakeEdge{a: li, b: ri, aCol: l.Name, bCol: r.Name, expr: x}, true
}

func (e *executor) planSnowflakeShape(ctx context.Context, jp *joinPlan, need map[string]bool, residual Expr) (*starShape, bool) {
	if len(jp.items) < 2 || len(jp.merged) > 0 {
		return nil, false
	}
	s := &starShape{need: map[string]bool{}, filters: map[int][]Expr{}, filtered: true, dimensionRows: snowflakeBranchRows, snowflake: true, outNeed: map[string]bool{}}
	maps.Copy(s.need, need)
	maps.Copy(s.outNeed, need)
	types := map[string]ColType{}
	sourceRows := make([]int64, len(jp.items))
	s.fact = -1
	for i, it := range jp.items {
		if it.kind != "" && it.kind != "INNER" && it.kind != "CROSS" || it.nullable || it.lateral != nil || it.isolated ||
			it.tag != "" && need[it.tag] || len(it.scopeRels) > 0 || it.natural || len(it.using) > 0 || it.base.view != nil {
			return nil, false
		}
		if !it.base.isMem {
			for _, seg := range it.base.snapshotSegments() {
				if snapshotOwns(ctx, seg.Prefix) {
					return nil, false
				}
			}
		}
		n, ok := starRows(it.base)
		if !ok || n == 0 {
			return nil, false
		}
		sourceRows[i] = n
		maps.Copy(types, it.view().types())
	}

	// Classify every condition: an item's own filter, an equality edge, or a
	// residual over several items. Pushed conjuncts already run in the eager
	// scans, so they stay filters whatever they compute.
	pushed := map[Expr]bool{}
	var conds []Expr
	for _, it := range jp.items {
		for _, x := range it.pushed {
			pushed[x] = true
		}
		if it.on != nil {
			conds = append(conds, conjuncts(it.on, nil)...)
		}
		conds = append(conds, it.extra...)
		conds = append(conds, it.pushed...)
	}
	if residual != nil {
		conds = append(conds, conjuncts(residual, nil)...)
	}
	var edges []snowflakeEdge
	var cross []Expr
	seen := map[Expr]bool{}
	for _, x := range conds {
		if seen[x] {
			continue
		}
		seen[x] = true
		idx, movable := jp.itemsOf(x)
		if !movable || len(idx) == 0 {
			return nil, false
		}
		if len(idx) == 1 {
			if !pushed[x] && !snowflakePurePredicate(x, types) {
				return nil, false
			}
			s.filters[idx[0]] = append(s.filters[idx[0]], x)
			columnRefs(x, s.need)
			continue
		}
		if len(idx) == 2 {
			if edge, ok := snowflakeEdgeOf(jp, x); ok {
				edges = append(edges, edge)
				continue
			}
		}
		if !snowflakePurePredicate(x, types) {
			return nil, false
		}
		cross = append(cross, x)
	}

	// The streamed fact is the hub among the large tables: of those with at
	// least half the largest table's rows, the one joined to the most other
	// tables (then the largest). Every other neighbour then becomes an
	// independent branch, as lineitem's part, supplier, partsupp and orders do
	// in TPC-H Q9, instead of one branch eagerly joining a second large table.
	// A referenced table with a quarter of its fact's rows (orders against
	// lineitem) never displaces the fact, however many tables it joins.
	largest := int64(0)
	for _, n := range sourceRows {
		largest = max(largest, n)
	}
	neighbours := make([]map[int]bool, len(jp.items))
	for i := range neighbours {
		neighbours[i] = map[int]bool{}
	}
	for _, edge := range edges {
		neighbours[edge.a][edge.b], neighbours[edge.b][edge.a] = true, true
	}
	for i, n := range sourceRows {
		if n*2 < largest {
			continue
		}
		if s.fact < 0 || len(neighbours[i]) > len(neighbours[s.fact]) ||
			len(neighbours[i]) == len(neighbours[s.fact]) && n > sourceRows[s.fact] {
			s.fact = i
		}
	}
	if sourceRows[s.fact] < snowflakeMinFactRows {
		return nil, false
	}

	// Spanning tree by layers from the fact. A table reachable from several
	// tables of the previous layer joins the one with the most rows: for
	// foreign keys that is the referencing side, so customer hangs off orders
	// (one row each) rather than off supplier by nation (a cross product).
	parent := make([]int, len(jp.items))
	for i := range parent {
		parent[i] = -2
	}
	parent[s.fact] = -1
	layer := []int{s.fact}
	for len(layer) > 0 {
		inLayer := map[int]bool{}
		for _, u := range layer {
			inLayer[u] = true
		}
		best := map[int]int{}
		for _, edge := range edges {
			for _, pair := range [][2]int{{edge.a, edge.b}, {edge.b, edge.a}} {
				u, v := pair[0], pair[1]
				if !inLayer[u] || parent[v] != -2 {
					continue
				}
				if p, ok := best[v]; !ok || sourceRows[u] > sourceRows[p] || sourceRows[u] == sourceRows[p] && u < p {
					best[v] = u
				}
			}
		}
		var next []int
		for v, u := range best {
			parent[v] = u
			next = append(next, v)
		}
		sort.Ints(next)
		layer = next
	}
	for _, p := range parent {
		if p == -2 {
			return nil, false // a cross product, never streamed
		}
	}
	isTree := func(edge snowflakeEdge) bool { return parent[edge.b] == edge.a || parent[edge.a] == edge.b }
	for _, edge := range edges {
		if !isTree(edge) {
			cross = append(cross, edge.expr)
		}
	}
	// branchOf is the fact's child whose subtree holds an item.
	branchOf := func(i int) int {
		for parent[i] != s.fact && parent[i] >= 0 {
			i = parent[i]
		}
		return i
	}

	for root := range jp.items {
		if parent[root] != s.fact {
			continue
		}
		var members []int
		for i := range jp.items {
			if i != s.fact && branchOf(i) == root {
				members = append(members, i)
			}
		}
		dim := starDimension{item: root}
		for _, edge := range edges {
			switch {
			case edge.a == s.fact && edge.b == root:
				dim.factCols, dim.dimCols = append(dim.factCols, edge.aCol), append(dim.dimCols, edge.bCol)
			case edge.b == s.fact && edge.a == root:
				dim.factCols, dim.dimCols = append(dim.factCols, edge.bCol), append(dim.dimCols, edge.aCol)
			default:
				continue
			}
			s.need[edge.aCol], s.need[edge.bCol] = true, true
		}
		for _, c := range dim.dimCols {
			s.outNeed[c] = true // bucket keys also derive fact demand
		}
		if len(dim.dimCols) == 0 {
			return nil, false
		}
		if len(members) > 1 {
			dim.branch, dim.branchItems = snowflakeBranchPlan(jp, members, root, s.filters, edges, cross, isTree)
		}
		// cross holds every condition but the tree edges (and filters).
		dim.unfiltered = starDimensionUnfiltered(jp, s.filters, members, cross)
		s.dims = append(s.dims, dim)
	}
	// Conditions inside one branch are joined there; the rest span branches.
	var rest []Expr
	for _, x := range cross {
		idx, _ := jp.itemsOf(x)
		roots := map[int]bool{}
		for _, i := range idx {
			if i == s.fact {
				roots[-1] = true
			} else {
				roots[branchOf(i)] = true
			}
		}
		inBranch := false
		if len(roots) == 1 {
			for r := range roots {
				inBranch = r >= 0 && snowflakeBranchHasPlan(s.dims, r)
			}
		}
		if !inBranch {
			rest = append(rest, x)
			columnRefs(x, s.need)
			columnRefs(x, s.outNeed)
		}
	}
	s.residual = andAll(rest)
	return s, true
}

// snowflakeParallelPredicate is parallelPredicate, also admitting constant
// operands that hold no subquery, such as TPC-H's CAST('1995-03-15' AS date):
// every worker evaluates them identically, and none can run a subquery once
// per worker (parallel workers own fresh executor caches).
func snowflakeParallelPredicate(x Expr) bool {
	if x == nil {
		return true
	}
	if isConstant(x) {
		subquery := false
		walkExpr(x, func(n Expr) {
			if _, ok := n.(*Subquery); ok {
				subquery = true
			}
		})
		if !subquery {
			return true
		}
	}
	switch v := x.(type) {
	case *Unary:
		return v.Op == "NOT" && snowflakeParallelPredicate(v.X)
	case *Binary:
		switch v.Op {
		case "AND", "OR", "=", "!=", "<>", "<", "<=", ">", ">=":
			return snowflakeParallelPredicate(v.L) && snowflakeParallelPredicate(v.R)
		}
	case *Func:
		if v.Name == "LIKE" || v.Name == "ILIKE" {
			for _, a := range v.Args {
				if !snowflakeParallelPredicate(a) {
					return false
				}
			}
			return true
		}
	}
	return parallelPredicate(x)
}

// snowflakeBroadFactShare: a filtered snowflake fact matching at least
// 1/snowflakeBroadFactShare of its row sequence is scanned in parallel.
const snowflakeBroadFactShare = 8

// broadSnowflakeFact reports whether the fact's Search filter matches a large
// share of its rows, by the same index count the join order uses. The count
// only selects a plan: the parallel scan still evaluates the full predicate.
func (e *executor) broadSnowflakeFact(ctx context.Context, base, view *tableMeta, query string) (bool, error) {
	last, err := e.store.rowSequence(ctx, base)
	if err != nil || last <= 0 {
		return false, nil
	}
	var n int64
	if base.SnapshotOverlay {
		if !e.snapshotIndexable(ctx, base) {
			return false, nil
		}
		n, err = e.store.countSnapshotCandidates(ctx, base, query)
	} else {
		n, err = e.store.countMatches(ctx, view.index(), query)
	}
	if err != nil {
		if ce := cancelled(ctx); ce != nil {
			return false, ce
		}
		return false, nil
	}
	return n*snowflakeBroadFactShare >= last, nil
}

// snowflakeParallelDimensionRows is the smallest single-table branch read in
// parallel exact-directory chunks (TPC-H Q9 reads all orders and partsupp).
var snowflakeParallelDimensionRows int64 = 65536

// scanParallelDimension reads a large single-table snowflake branch with the
// fact's exact-directory chunk reader. Workers collect rows per chunk; keep
// then receives them in chunk (row-ID) order, as the serial scan delivers
// them, so bucket order and output order do not depend on worker timing.
func (e *executor) scanParallelDimension(ctx context.Context, shape *starShape, jp *joinPlan, item int, params []Value, keep func([]map[string]Value) error) (handled bool, resultErr error) {
	it := jp.items[item]
	base := it.base
	if !shape.snowflake || !e.parallelTableEligible(ctx, base) {
		return false, nil
	}
	if n, ok := starRows(base); !ok || n < snowflakeParallelDimensionRows {
		return false, nil
	}
	view := it.view()
	need := map[string]bool{}
	for c := range shape.need {
		if strings.HasPrefix(c, it.prefix) {
			need[c] = true
		}
	}
	predicate := andAll(shape.filters[item])
	if !snowflakeParallelPredicate(predicate) {
		return false, nil
	}
	wp, err := e.planWhere(ctx, predicate, view, params)
	if err != nil {
		return true, err
	}
	if wp.none {
		return true, nil
	}
	if wp.keys != nil {
		return false, nil
	}
	return e.parallelTableRows(ctx, base, view, wp, predicate, need, params, int64(shape.dimensionCap()), keep)
}

// parallelTableEligible reports whether base can be read by the parallel
// exact-directory or shared-candidate chunk readers: a published, driver-
// owned managed table read under a statement pin, with no private segments.
func (e *executor) parallelTableEligible(ctx context.Context, base *tableMeta) bool {
	scope := snapshotOf(ctx)
	if e.workerBudget == nil || e.store == nil || !e.store.snapshots || scope == nil || scope.pin == nil ||
		base.isMem || base.view != nil || !managedRowIDTable(base) || snapshotDataID(base.prefix()) == "" || len(base.PendingCleanup) > 0 {
		return false
	}
	for _, seg := range base.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) {
			return false
		}
	}
	return true
}

// parallelTableRows reads one eligible table's rows matching predicate (wp is
// its plan) with parallel workers, and keep receives them in chunk (row-ID)
// order. With Flex, a filter's shared complete candidates are split across
// the workers (sharedFlexCandidates); otherwise workers read exact
// directories. The complete predicate is checked on every row. More than
// limit rows end the read with errStarDimensionCap. handled false means the
// table keeps its serial read.
func (e *executor) parallelTableRows(ctx context.Context, base, view *tableMeta, wp wherePlan, predicate Expr, need map[string]bool, params []Value, limit int64, keep func([]map[string]Value) error) (handled bool, err error) {
	scope := snapshotOf(ctx)
	var cands *snapshotCandidateScan
	var candBounds [][2]string
	if wp.query != "*" {
		if predicate == nil {
			return false, nil
		}
		var over bool
		if cands, candBounds, over, err = e.flexReaderPlan(ctx, view, wp.query); err != nil {
			return true, err
		}
		if cands == nil && !over {
			// Flex refusing the complete candidates proves a large set;
			// otherwise ask the same index count the join order uses.
			broad, err := e.broadSnowflakeFact(ctx, base, view, wp.query)
			if err != nil || !broad {
				return err != nil, err
			}
		}
	}
	var grant, chunks int
	var last int64
	defer func() {
		if grant > 0 && e.workerGrant != nil {
			e.workerGrant.Store(int64(grant))
		}
	}()
	if cands != nil {
		if grant, err = e.workerBudget.acquireBounded(ctx, e.queryWorkers, min(len(candBounds), 32)); err != nil {
			return true, cancelled(ctx)
		}
		defer e.workerBudget.release(grant)
		chunks = len(candBounds)
		// The Search query only selected the candidates; every row is
		// checked against the complete predicate.
		wp.residual, wp.original = predicate, predicate
	} else {
		last, err = e.store.rowSequence(ctx, base)
		if err == goredis.Nil {
			return false, nil
		}
		if err != nil {
			return true, wrapRedis(err, "reading snowflake dimension row sequence failed")
		}
		if last < 2048 {
			return false, nil
		}
		if grant, err = e.workerBudget.acquireBounded(ctx, e.queryWorkers, int(min(last/1024, 32))); err != nil {
			return true, cancelled(ctx)
		}
		defer e.workerBudget.release(grant)
		if chunks = starFactChunkCount(last, grant); chunks == 0 {
			return false, nil
		}
		// Exact directories, as the parallel fact reader: the complete filter
		// is checked on every row, never only its Search form.
		copy := *view
		copy.SnapshotOverlay = true
		view = &copy
		wp.query, wp.residual, wp.original = "*", predicate, predicate
	}
	capacity := ownedFactRowCapacity(len(need))
	results := make([][]map[string]Value, chunks)
	var total, next atomic.Int64
	// capped stops the workers at the row limit. Cancelling their shared
	// context instead would interrupt in-flight commands and close the
	// execution's connections, which the caller's fallback (the eager join, or
	// a spilled re-read) still needs (#654).
	var capped atomic.Bool
	var wg sync.WaitGroup
	var panicMu sync.Mutex
	var panicValue any
	child, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	for w := 0; w < grant; w++ {
		local := *e
		local.cache = newExecCache()
		wg.Add(1)
		go func(local *executor) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicMu.Lock()
					if panicValue == nil {
						panicValue = r
					}
					panicMu.Unlock()
					cancel(errStarProbeStopped)
				}
			}()
			for {
				chunk := int(next.Add(1) - 1)
				if chunk >= chunks || child.Err() != nil || capped.Load() {
					return
				}
				req := scanRequest{meta: view, where: wp, need: need, rowCapacity: capacity}
				if cands != nil {
					req.flexCandidates = cands.split(candBounds[chunk][0], candBounds[chunk][1], nil)
				} else {
					bounds := starFactChunkRange(last, chunks, chunk)
					bounds.cap = starChunkRows
					req.ranges = &bounds
				}
				err := local.scanPages(child, req, params, func(rows []map[string]Value) error {
					if capped.Load() || total.Add(int64(len(rows))) > limit {
						capped.Store(true)
						return errStarDimensionCap
					}
					results[chunk] = append(results[chunk], rows...)
					return nil
				})
				if err == errStarDimensionCap {
					return
				}
				if err != nil {
					cancel(err)
					return
				}
			}
		}(&local)
	}
	wg.Wait()
	if panicValue != nil {
		panic(panicValue)
	}
	if capped.Load() && child.Err() == nil {
		return true, errStarDimensionCap
	}
	if err := poolErr(ctx, child); err != nil {
		return true, err
	}
	if err := scope.pin.valid(ctx); err != nil {
		return true, err
	}
	for chunk := range results {
		if err := keep(results[chunk]); err != nil {
			return true, err
		}
		results[chunk] = nil
	}
	return true, nil
}

func snowflakeBranchHasPlan(dims []starDimension, root int) bool {
	for _, d := range dims {
		if d.item == root {
			return d.branch != nil
		}
	}
	return false
}

// snowflakeBranchPlan copies a branch's items into a join of their own: each
// item's filters stay pushed into its scan, and every tree edge and residual
// among the branch's items is a WHERE condition of that join.
func snowflakeBranchPlan(jp *joinPlan, members []int, root int, filters map[int][]Expr, edges []snowflakeEdge, cross []Expr, isTree func(snowflakeEdge) bool) (*joinPlan, []int) {
	order := append([]int{root}, nil...)
	for _, i := range members {
		if i != root {
			order = append(order, i)
		}
	}
	member := map[int]bool{}
	for _, i := range order {
		member[i] = true
	}
	var conds []Expr
	for _, edge := range edges {
		if member[edge.a] && member[edge.b] && isTree(edge) {
			conds = append(conds, edge.expr)
		}
	}
	for _, x := range cross {
		idx, _ := jp.itemsOf(x)
		inside := len(idx) > 0
		for _, i := range idx {
			inside = inside && member[i]
		}
		if inside {
			conds = append(conds, x)
		}
	}
	sub := &joinPlan{}
	for k, i := range order {
		it := *jp.items[i]
		it.kind, it.comma, it.on, it.extra, it.isolated, it.nullable, it.tag = "INNER", false, nil, nil, false, false, ""
		if k == 0 {
			it.kind = ""
		}
		it.pushed = append([]Expr(nil), filters[i]...)
		sub.items = append(sub.items, &it)
	}
	sub.items[len(sub.items)-1].extra = conds
	return sub, order
}
