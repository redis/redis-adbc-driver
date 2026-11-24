// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"github.com/apache/arrow-adbc/go/adbc"
	"strconv"
	"strings"
)

const starDimensionRows = 262144
const starOutputPage = 256

type starDimension struct {
	item              int
	factCols, dimCols []string
	buckets           map[string][]map[string]Value
	// branch pre-joins a snowflake branch rooted at item (planSnowflakeShape);
	// branchItems are its join items. A nil branch scans item alone.
	branch      *joinPlan
	branchItems []int
}
type starShape struct {
	fact     int
	dims     []starDimension
	need     map[string]bool
	filters  map[int][]Expr
	filtered bool
	// Snowflake shapes check residual on every complete output row, with an
	// evaluator per probing worker (newEnv), and retain up to dimensionRows
	// dimension rows (0 means starDimensionRows).
	snowflake     bool
	residual      Expr
	dimensionRows int
	newEnv        func() *evalEnv
	// parallelDimensions counts dimensions read by scanParallelDimension.
	parallelDimensions int
	// outNeed are the columns output rows must keep (the consumer's and the
	// residual's); retained snowflake dimension rows are compacted to them.
	outNeed map[string]bool
}

// compactDimensionRow keeps only the output columns of a retained row.
func (s *starShape) compactDimensionRow(row map[string]Value) map[string]Value {
	if s.outNeed == nil {
		return row
	}
	kept := 0
	for k := range row {
		if s.outNeed[k] {
			kept++
		}
	}
	if kept == len(row) {
		return row
	}
	out := make(map[string]Value, kept)
	for k, v := range row {
		if s.outNeed[k] {
			out[k] = v
		}
	}
	return out
}

func (s *starShape) dimensionCap() int {
	if s.dimensionRows > 0 {
		return s.dimensionRows
	}
	return starDimensionRows
}

// Reordering input is allowed only for exact, order-insensitive reduction.
// In particular ordered/distinct aggregates, floating group representatives,
// subqueries, volatile functions and checked integer arithmetic stay eager.
func streamingStarAggregate(plan *selectPlan, aggs []*Func) bool {
	if plan.meta.join == nil || len(plan.sel.GroupBy) == 0 || len(plan.outerAggs) > 0 {
		return false
	}
	safe := true
	check := func(x Expr) {
		walkExpr(x, func(n Expr) {
			switch v := n.(type) {
			case *Subquery:
				safe = false
			case *Func:
				if volatileFuncs[v.Name] {
					safe = false
				}
			}
		})
	}
	for _, it := range plan.items {
		check(it.expr)
	}
	check(plan.having)
	check(plan.qualify)
	for _, o := range plan.order {
		check(o.expr)
	}
	if !safe {
		return false
	}
	types := plan.meta.types()
	for _, g := range plan.sel.GroupBy {
		c, ok := g.(*ColumnRef)
		// Equal dates are identical, so a date group's representative is too.
		if !ok || c.Outer != 0 || c.whole != nil || !snowflakeKeyType(types[c.Name]) || types[c.Name].Fixed {
			return false
		}
	}
	if len(aggs) == 0 {
		return false
	}
	for _, f := range aggs {
		if f.Distinct || f.Filter != nil || len(f.OrderBy) > 0 || f.WithinGroup {
			return false
		}
		switch f.Name {
		case "COUNT", "SUM", "AVG", "MIN", "MAX":
		default:
			return false
		}
		if f.Star {
			if f.Name != "COUNT" {
				return false
			}
			continue
		}
		if len(f.Args) != 1 || !starAggregateInput(f.Args[0], types) {
			return false
		}
	}
	return true
}
func starKeyType(t ColType) bool {
	return t.Kind.isInteger() || t.Kind == KindDecimal || t.Kind == KindString && t.Collation == ""
}
func starAggregateInput(x Expr, types map[string]ColType) bool {
	switch v := x.(type) {
	case *ColumnRef:
		return v.Outer == 0 && v.whole == nil && starKeyType(types[v.Name]) && !types[v.Name].Fixed
	case *Literal:
		return v.V.Null || v.V.T.Kind.isInteger() || v.V.T.Kind == KindDecimal
	case *Case:
		if v.Operand != nil {
			return false
		}
		t, err := inferType(v, types, nil)
		if err != nil || !starKeyType(t) || t.Fixed {
			return false
		}
		for _, w := range v.Whens {
			if !starPurePredicate(w.When, types) || !starAggregateInput(w.Then, types) {
				return false
			}
		}
		return v.Else == nil || starAggregateInput(v.Else, types)
	case *Binary:
		if !starAggregateInput(v.L, types) || !starAggregateInput(v.R, types) {
			return false
		}
		t, err := inferType(x, types, nil)
		if err != nil || t.Kind != KindDecimal {
			return false
		}
		switch v.Op {
		case "+", "-", "*":
			return true
		case "/":
			l, ok := v.R.(*Literal)
			if !ok || l.V.Null {
				return false
			}
			if l.V.T.Kind.isInteger() {
				return l.V.I != 0
			}
			return l.V.T.Kind == KindDecimal && l.V.D.Sign() != 0
		}
	}
	return false
}
func starRows(m *tableMeta) (int64, bool) {
	if m.view != nil || m.lazy != nil || m.group != nil || m.join != nil || m.Live || m.Unmanaged || len(m.PendingCleanup) > 0 {
		return 0, false
	}
	if m.isMem {
		return int64(len(m.mem)), true
	}
	if m.SnapshotRows == nil {
		return 0, false
	}
	var n int64
	for _, s := range m.snapshotSegments() {
		if s.Rows < 0 || n > int64(^uint64(0)>>1)-s.Rows {
			return 0, false
		}
		n += s.Rows
	}
	return n, true
}

// Only column equalities are admitted. Their same-type keys are exactly the
// existing joinKey contract, with no casts, temporal normalization or errors.
func (e *executor) planStarShape(ctx context.Context, jp *joinPlan, need map[string]bool, residual Expr) (*starShape, bool) {
	if len(jp.items) < 2 || len(jp.merged) > 0 {
		return nil, false
	}
	s := &starShape{need: map[string]bool{}, filters: map[int][]Expr{}}
	types := map[string]ColType{}
	for _, it := range jp.items {
		for k, t := range it.view().types() {
			types[k] = t
		}
	}
	for k := range need {
		s.need[k] = true
	}
	var maxRows int64 = -1
	sourceRows := make([]int64, len(jp.items))
	for i, it := range jp.items {
		if it.kind != "" && it.kind != "INNER" && it.kind != "CROSS" || it.lateral != nil || it.isolated || it.tag != "" && need[it.tag] || len(it.scopeRels) > 0 || it.natural || len(it.using) > 0 {
			return nil, false
		}
		// Private owned directories can change while staged work proceeds; their
		// declared row counts are not immutable published admission bounds.
		if !it.base.isMem {
			for _, seg := range it.base.snapshotSegments() {
				if snapshotOwns(ctx, seg.Prefix) {
					return nil, false
				}
			}
		}
		n, ok := starRows(it.base)
		if it.base.view != nil {
			n, ok = e.deferredUnionRowBound(ctx, it.base)
		}
		if !ok || n == 0 {
			return nil, false
		}
		sourceRows[i] = n
		if n > maxRows {
			maxRows = n
			s.fact = i
		}
	}
	var dimRows, physicalDimRows int64
	for i, it := range jp.items {
		if i != s.fact {
			// Only the streamed fact may be deferred; every retained dimension
			// still satisfies the established direct-source admission proof.
			if it.base.view != nil {
				return nil, false
			}
			n := sourceRows[i]
			physicalDimRows = min(int64(starDimensionRows+1), physicalDimRows+min(n, int64(starDimensionRows+1)))
			if len(it.pushed) == 0 {
				dimRows += n
			}
			if dimRows > starDimensionRows {
				return nil, false
			}
			s.dims = append(s.dims, starDimension{item: i})
		}
	}
	s.filtered = physicalDimRows > starDimensionRows
	var conds []Expr
	for _, it := range jp.items {
		if it.on != nil {
			conds = append(conds, conjuncts(it.on, nil)...)
		}
		conds = append(conds, it.extra...)
		conds = append(conds, it.pushed...)
	}
	if residual != nil {
		conds = append(conds, conjuncts(residual, nil)...)
	}
	seen := map[*Binary]bool{}
	for _, x := range conds {
		idx, movable := jp.itemsOf(x)
		if movable && len(idx) == 1 && starPurePredicate(x, types) {
			s.filters[idx[0]] = append(s.filters[idx[0]], x)
			columnRefs(x, s.need)
			continue
		}
		b, ok := x.(*Binary)
		if !ok || b.Op != "=" {
			return nil, false
		}
		if seen[b] {
			continue
		}
		seen[b] = true
		l, lok := b.L.(*ColumnRef)
		r, rok := b.R.(*ColumnRef)
		if !lok || !rok || l.Outer != 0 || r.Outer != 0 || l.whole != nil || r.whole != nil {
			return nil, false
		}
		li, ri := -1, -1
		var lt, rt ColType
		for i, it := range jp.items {
			if c, ok := it.view().column(l.Name); ok {
				li = i
				lt = c.Type
			}
			if c, ok := it.view().column(r.Name); ok {
				ri = i
				rt = c.Type
			}
		}
		if li == ri || li < 0 || ri < 0 || lt != rt || !starKeyType(lt) {
			return nil, false
		}
		if ri == s.fact {
			li, ri = ri, li
			l, r = r, l
		}
		if li != s.fact {
			return nil, false
		}
		s.need[l.Name] = true
		s.need[r.Name] = true
		for i := range s.dims {
			if s.dims[i].item == ri {
				s.dims[i].factCols = append(s.dims[i].factCols, l.Name)
				s.dims[i].dimCols = append(s.dims[i].dimCols, r.Name)
			}
		}
	}
	for _, d := range s.dims {
		if len(d.dimCols) == 0 {
			return nil, false
		}
	}
	return s, true
}
func starRowKey(row map[string]Value, cols []string) (string, bool) {
	var b strings.Builder
	for _, c := range cols {
		v, ok := row[c]
		if !ok {
			return "", false
		}
		k, ok := joinKey(v)
		if !ok {
			return "", false
		}
		b.WriteString(strconv.Itoa(len(k)))
		b.WriteByte(':')
		b.WriteString(k)
	}
	return b.String(), true
}

// The callback never sees more than starOutputPage rows. Only small dimensions
// are retained; each complete fact page is released before fetching the next.
func (e *executor) scanStarJoinPages(ctx context.Context, req scanRequest, params []Value, visit func([]map[string]Value) error) (bool, error) {
	return e.streamJoinPages(ctx, req.meta.join, req.need, req.where.residual, params, true, visit)
}

// snowflakeJoinRows is the materialized join of scanMem through a streamed
// snowflake shape, when one applies. Its WHERE residual is re-checked by the
// caller on the returned rows, as for runJoin.
func (e *executor) snowflakeJoinRows(ctx context.Context, jp *joinPlan, need map[string]bool, params []Value) ([]map[string]Value, bool, error) {
	var rows []map[string]Value
	handled, err := e.streamJoinPages(ctx, jp, need, nil, params, false, func(page []map[string]Value) error {
		if err := retainedAllocate(ctx, int64(len(page)), 256); err != nil {
			return err
		}
		rows = append(rows, page...)
		return nil
	})
	return rows, handled, err
}

// streamJoinPages streams an admitted star (when star is set) or snowflake
// shape. A shape whose dimensions exceed their cap falls back before visit.
func (e *executor) streamJoinPages(ctx context.Context, jp *joinPlan, reqNeed map[string]bool, residual Expr, params []Value, star bool, visit func([]map[string]Value) error) (bool, error) {
	if ctx.Value(retainedAllocationKey{}) != nil {
		return false, nil
	}
	var shape *starShape
	ok := false
	if star {
		shape, ok = e.planStarShape(ctx, jp, reqNeed, residual)
	}
	if !ok {
		shape, ok = e.planSnowflakeShape(ctx, jp, reqNeed, residual)
	}
	if !ok {
		return false, nil
	}
	kind := "streaming_star_join"
	if shape.snowflake {
		kind = "streaming_snowflake_join"
	}
	e.diagnostics.setPhase(kind)
	e.diagnostics.setPlan(map[string]any{"kind": kind, "dimension_count": len(shape.dims), "output_page_rows": starOutputPage})
	if shape.residual != nil {
		types := map[string]ColType{}
		for _, it := range jp.items {
			for k, t := range it.view().types() {
				types[k] = t
			}
		}
		shape.newEnv = func() *evalEnv { return e.newEnv(ctx, types, params) }
	}
	scanItem := func(i int, consume func([]map[string]Value) error) error {
		it := jp.items[i]
		view := it.view()
		need := map[string]bool{}
		for c := range shape.need {
			if strings.HasPrefix(c, it.prefix) {
				need[c] = true
			}
		}
		wp, err := e.planWhere(ctx, andAll(shape.filters[i]), view, params)
		if err != nil {
			return err
		}
		if wp.none {
			return nil
		}
		sub := scanRequest{meta: view, where: wp, need: need}
		if it.base.view != nil || !it.base.isMem {
			return e.scanPages(ctx, sub, params, consume)
		}
		// Unit/in-memory sources also qualify one page at a time, without touching
		// a shared CTE cache or retaining a second complete fact relation.
		env := e.newEnv(ctx, view.types(), params)
		for start := 0; start < len(it.base.mem); start += starOutputPage {
			if err := cancelled(ctx); err != nil {
				return err
			}
			rows := make([]map[string]Value, 0, starOutputPage)
			for _, r := range it.base.mem[start:min(start+starOutputPage, len(it.base.mem))] {
				m := map[string]Value{}
				for k, v := range r {
					if need[it.prefix+k] {
						m[it.prefix+k] = v
					}
				}
				if wp.residual != nil {
					env.row = m
					v, err := env.eval(wp.residual)
					if err != nil {
						return invalidArg(err)
					}
					if yes, ok := truthy(v); !ok || !yes {
						continue
					}
				}
				rows = append(rows, m)
			}
			if err := consume(rows); err != nil {
				return err
			}
		}
		return nil
	}
	observed := 0
	branchRows := make([]int, len(shape.dims))
	for d := range shape.dims {
		dim := &shape.dims[d]
		dim.buckets = map[string][]map[string]Value{}
		keep := func(rows []map[string]Value) error {
			for _, row := range rows {
				observed++
				branchRows[d]++
				if observed > shape.dimensionCap() {
					if shape.filtered {
						return errStarDimensionCap
					}
					return errorf(adbc.StatusIO, "streamed join dimension exceeds admitted row bound")
				}
				if err := cancelled(ctx); err != nil {
					return err
				}
				if k, ok := starRowKey(row, dim.dimCols); ok {
					dim.buckets[k] = append(dim.buckets[k], shape.compactDimensionRow(row))
				}
			}
			return nil
		}
		var err error
		if dim.branch != nil {
			// The branch's own eager join, over just its tables and conditions.
			need := map[string]bool{}
			for c := range shape.need {
				for _, i := range dim.branchItems {
					if strings.HasPrefix(c, jp.items[i].prefix) {
						need[c] = true
					}
				}
			}
			var rows []map[string]Value
			if rows, err = e.runJoin(ctx, dim.branch, need, params); err == nil {
				err = keep(rows)
			}
		} else {
			var parallel bool
			if parallel, err = e.scanParallelDimension(ctx, shape, jp, dim.item, params, keep); !parallel {
				err = scanItem(dim.item, keep)
			} else {
				shape.parallelDimensions++
			}
		}
		if err != nil {
			if err == errStarDimensionCap {
				e.diagnostics.setPhase("serial_aggregate")
				e.diagnostics.setPlan(map[string]any{"kind": "eager_join_fallback", "reason": "filtered_dimension_cap", "snowflake": shape.snowflake})
				return false, nil
			}
			return true, err
		}
	}
	if shape.snowflake {
		e.diagnostics.setPhase(kind)
		e.diagnostics.setPlan(map[string]any{"kind": kind, "fact": shape.fact, "dimension_rows": branchRows,
			"dimension_cap": shape.dimensionCap(), "residual": shape.residual != nil, "output_page_rows": starOutputPage,
			"parallel_dimensions": shape.parallelDimensions})
	}
	demand, err := starFactDimensionDemand(ctx, jp.items[shape.fact].view(), shape.dims)
	if err != nil {
		return true, err
	}
	if demand != nil {
		shape.filters[shape.fact] = append(shape.filters[shape.fact], demand)
	}
	if handled, err := e.scanParallelStarFacts(ctx, shape, jp, params, visit); handled {
		return true, err
	}
	var pending error
	out := make([]map[string]Value, 0, starOutputPage)
	flush := func() {
		if len(out) > 0 && pending == nil {
			pending = visit(out)
		}
		out = make([]map[string]Value, 0, starOutputPage)
	}
	var env *evalEnv
	if shape.residual != nil {
		env = shape.newEnv()
	}
	var probe func(int, map[string]Value) error
	probe = func(d int, row map[string]Value) error {
		if err := cancelled(ctx); err != nil {
			return err
		}
		if pending != nil {
			return nil
		}
		if d == len(shape.dims) {
			if env != nil {
				if ok, err := starResidualPasses(env, shape.residual, row); err != nil || !ok {
					return err
				}
			}
			out = append(out, row)
			if len(out) == starOutputPage {
				flush()
			}
			return nil
		}
		dim := &shape.dims[d]
		key, ok := starRowKey(row, dim.factCols)
		if !ok {
			return nil
		}
		for _, r := range dim.buckets[key] {
			if err := probe(d+1, mergeRows(row, r)); err != nil {
				return err
			}
		}
		return nil
	}
	err = scanItem(shape.fact, func(rows []map[string]Value) error {
		for _, r := range rows {
			if err := probe(0, r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	flush()
	if err := cancelled(ctx); err != nil {
		return true, err
	}
	return true, pending
}

// starResidualPasses evaluates a snowflake residual on a complete output row.
func starResidualPasses(env *evalEnv, residual Expr, row map[string]Value) (bool, error) {
	env.row = row
	v, err := env.eval(residual)
	env.row = nil
	if err != nil {
		return false, invalidArg(err)
	}
	ok, valid := truthy(v)
	return valid && ok, nil
}

// Pure local filters can run before an INNER join without side effects or
// expression errors. Keep casts, functions, subqueries and arithmetic eager.
func starPurePredicate(x Expr, types map[string]ColType) bool {
	switch v := x.(type) {
	case nil:
		return true
	case *Literal:
		return v.V.Null || v.V.T.Kind == KindBool
	case *Unary:
		return v.Op == "NOT" && starPurePredicate(v.X, types)
	case *Binary:
		switch v.Op {
		case "AND", "OR":
			return starPurePredicate(v.L, types) && starPurePredicate(v.R, types)
		case "=", "!=", "<>", "<", "<=", ">", ">=":
			return starPredicateValue(v.L, types) && starPredicateValue(v.R, types)
		}
	}
	return false
}
func starPredicateValue(x Expr, types map[string]ColType) bool {
	switch v := x.(type) {
	case *ColumnRef:
		t, ok := types[v.Name]
		return ok && v.Outer == 0 && v.whole == nil && (starKeyType(t) || t.Kind == KindBool)
	case *Literal:
		return v.V.Null || starKeyType(v.V.T) || v.V.T.Kind == KindBool
	}
	return false
}

var errStarDimensionCap = errors.New("filtered streamed join dimension cap")
