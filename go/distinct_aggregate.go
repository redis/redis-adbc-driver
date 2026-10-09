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
	"io"
	"strconv"

	goredis "github.com/redis/go-redis/v9"
)

// distinctColumn admits representations whose canonical stored bytes have
// exactly SQL's equality. Floating point, decimals and intervals can have
// different representations of equal values and retain the driver fallback.
func distinctColumn(meta *tableMeta, x Expr) (columnMeta, bool) {
	c, ok := x.(*ColumnRef)
	if !ok || c.Outer != 0 || isSystemColumn(c.Name) {
		return columnMeta{}, false
	}
	col, ok := meta.column(c.Name)
	if !ok || !col.Indexed || !simpleName(col.field()) || col.MissingThrough > 0 {
		return columnMeta{}, false
	}
	switch col.Type.Kind {
	case KindString, KindBool, KindInt16, KindInt32, KindInt64, KindDate, KindTime, KindTimestamp:
		return col, true
	}
	return columnMeta{}, false
}

// distinctRequest groups complete tuples, rather than adding per-shard
// distinct counts. The coordinator merges tuples across shards before returning them.
// GROUPBY itself keys groups by a 64-bit hash. For every column, TOLIST
// compares complete values even on a hash collision; MIN/MAX of exists()
// detects NULL/value mixtures. A group is accepted only when it contains one
// complete tuple. Otherwise the entire query falls back to row reduction.
//
// Safe sorting vectors need no LOAD. Otherwise LOAD * BEFORE GROUPBY replaces
// rounded numeric / NUL-truncated sorting vectors with raw HASH strings.
// Named LOAD (even with AS) still reads the sorting vector and is not exact.
func distinctRequest(meta *tableMeta, wp wherePlan, group, args []columnMeta) *aggRequest {
	fields := map[string]bool{}
	cols := append(append([]columnMeta(nil), group...), args...)
	var names []string
	raw := false
	for _, c := range cols {
		if fields[c.field()] {
			continue
		}
		fields[c.field()] = true
		names = append(names, c.field())
		raw = raw || !c.Indexed || !c.inPipeline() || !c.sortsExactly() || !c.groupable(PushdownExact)
	}
	for _, c := range meta.Columns {
		fields[c.field()] = true
	}
	var steps []any
	if raw {
		steps = append(steps, "LOAD", "*")
	}
	fresh := func(name string) string {
		for fields[name] {
			name += "_"
		}
		fields[name] = true
		return name
	}
	checks := make([]distinctCheck, 0, len(names))
	lists := map[string]string{}
	for i, name := range names {
		flag := fresh("__de" + strconv.Itoa(i))
		check := distinctCheck{field: name, flag: flag, lo: fresh("__dl" + strconv.Itoa(i)), hi: fresh("__dh" + strconv.Itoa(i)), list: fresh("__ds" + strconv.Itoa(i))}
		checks = append(checks, check)
		lists[check.list] = check.list
		steps = append(steps, "APPLY", "exists(@"+name+")", "AS", flag)
	}
	steps = append(steps, "GROUPBY", len(names))
	for _, name := range names {
		steps = append(steps, "@"+name)
	}
	for _, check := range checks {
		steps = append(steps, "REDUCE", "MIN", 1, "@"+check.flag, "AS", check.lo,
			"REDUCE", "MAX", 1, "@"+check.flag, "AS", check.hi,
			"REDUCE", "TOLIST", 1, "@"+check.field, "AS", check.list)
	}
	return &aggRequest{index: meta.index(), query: wp.query, rowFilter: meta.rowFilter(), groupBy: steps, width: 4 * len(names), listLengths: lists, distinctChecks: checks}
}

// distinctValues rejects noncanonical storage or a server that did not
// preserve raw values. Returning false makes the entire query use the exact
// row accumulator path; no partial counts are exposed to the caller.
func distinctValues(row aggRow, cols []columnMeta) ([]Value, bool) {
	vals := make([]Value, len(cols))
	for i, c := range cols {
		s, present := row[c.field()]
		if !present {
			vals[i] = nullValue(c.Type)
			continue
		}
		v, err := decodeStored(s, c.Type)
		if err != nil || encodeStored(v) != s {
			return nil, false
		}
		vals[i] = v
	}
	return vals, true
}

// indexCountDistinct consumes deduplicated tuples a cursor page at a time.
// It retains counts/representatives per SQL group, without client distinct
// sets. Independent COUNT DISTINCT calls use separate pipelines so distinct
// tuples from one call do not multiply those of another. Aggregate FILTER
// predicates must be exact index predicates or pure tuple comparisons. A separate group
// universe retains zero-count groups when every aggregate is filtered.
func (e *executor) indexCountDistinct(ctx context.Context, plan *selectPlan, wp wherePlan, aggs []*Func, params []Value) ([]aggGroup, bool, error) {
	meta := plan.meta
	if e.pushdown == PushdownNone || meta.isMem || meta.Adopted || wp.none || wp.keys != nil || len(plan.extraNeed) > 0 || len(aggs) == 0 {
		return nil, false, nil
	}
	var group []columnMeta
	groupNames := map[string]bool{}
	for _, x := range plan.sel.GroupBy {
		c, ok := distinctColumn(meta, x)
		if !ok {
			return nil, false, nil
		}
		group = append(group, c)
		groupNames[c.Name] = true
	}
	// Post-reduction expressions may only read grouping columns.
	outside := map[string]bool{}
	refs := func(x Expr) {
		walkOutsideAggregates(x, func(x Expr) {
			if c, ok := x.(*ColumnRef); ok && c.Outer == 0 {
				outside[c.Name] = true
			}
		})
	}
	for _, it := range plan.items {
		refs(it.expr)
	}
	for _, o := range plan.order {
		refs(o.expr)
	}
	refs(plan.having)
	refs(plan.qualify)
	for _, w := range plan.windows {
		for _, x := range w.fn.children() {
			refs(x)
		}
	}
	for name := range outside {
		if !groupNames[name] {
			return nil, false, nil
		}
	}
	type call struct {
		fn      *Func
		aliases []*Func
		args    []columnMeta
		where   wherePlan
	}
	calls := make([]call, 0, len(aggs))
	unfiltered := false
	for _, f := range aggs {
		if f.Name != "COUNT" || !f.Distinct || f.Star || len(f.Args) == 0 {
			return nil, false, nil
		}
		c := call{fn: f, where: wp}
		for _, x := range f.Args {
			col, ok := distinctColumn(meta, x)
			if !ok {
				return nil, false, nil
			}
			c.args = append(c.args, col)
		}
		if f.Filter != nil {
			var err error
			c.where, err = e.planWhere(ctx, andAll([]Expr{plan.sel.Where, f.Filter}), meta, params)
			if err != nil {
				return nil, false, err
			}
			if !c.where.none && c.where.keys != nil {
				return nil, false, nil
			}
		} else {
			unfiltered = true
		}
		if !distinctResidual(c.where.residual, group, c.args) && !c.where.none {
			return nil, false, nil
		}
		duplicate := false
		for i := range calls {
			if exprEqual(calls[i].fn, f) {
				calls[i].aliases = append(calls[i].aliases, f)
				duplicate = true
				break
			}
		}
		if !duplicate {
			calls = append(calls, c)
		}
	}
	// Older Search releases compared strings with strncmp, treating different
	// tails after a NUL as equal inside TOLIST. Enable that representation only
	// on releases with verified binary-safe equality. A denied MODULE LIST
	// leaves the query on the exact row path.
	binary := false
	checkBinary := func(cols []columnMeta) {
		for _, col := range cols {
			binary = binary || (col.Type.Kind == KindString && !col.sortsExactly())
		}
	}
	checkBinary(group)
	for _, c := range calls {
		checkBinary(c.args)
	}
	if binary && !e.binaryDistinctSupported(ctx) {
		return nil, false, nil
	}
	if !unfiltered && !distinctResidual(wp.residual, group, nil) {
		return nil, false, nil
	}
	groups := map[string]*aggGroup{}
	var order []string
	addGroup := func(vals []Value) *aggGroup {
		key := rowKey(vals)
		if g := groups[key]; g != nil {
			return g
		}
		g := &aggGroup{rep: map[string]Value{}, results: map[*Func]Value{}}
		for i, c := range group {
			g.rep[c.Name] = vals[i]
		}
		for _, f := range aggs {
			g.results[f] = intValue(typeInt64, 0)
		}
		groups[key] = g
		order = append(order, key)
		return g
	}
	if len(group) == 0 {
		addGroup(nil)
	}
	consume := func(req *aggRequest, c *call) (bool, error) {
		cur, err := e.store.openAggregate(ctx, req)
		if err != nil {
			return false, err
		}
		defer cur.close(ctx)
		where := wp
		if c != nil {
			where = c.where
		}
		env := e.newEnv(ctx, meta.types(), params)
		env.row = map[string]Value{}
		for {
			if err := cancelled(ctx); err != nil {
				return false, err
			}
			page, err := cur.next(ctx)
			if errors.Is(err, io.EOF) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			for _, row := range page {
				if !req.distinctTuple(row) {
					return false, nil
				}
				if err := cancelled(ctx); err != nil {
					return false, err
				}
				vals, ok := distinctValues(row, group)
				if !ok {
					return false, nil
				}
				var args []Value
				if c != nil {
					args, ok = distinctValues(row, c.args)
					if !ok {
						return false, nil
					}
				}
				if where.residual != nil {
					for i, col := range group {
						env.row[col.Name] = vals[i]
					}
					if c != nil {
						for i, col := range c.args {
							env.row[col.Name] = args[i]
						}
					}
					v, err := env.eval(where.residual)
					if err != nil {
						return false, invalidArg(err)
					}
					if b, valid := truthy(v); !valid || !b {
						continue
					}
				}
				g := addGroup(vals)
				if c == nil {
					continue
				}
				nonNull := true
				for _, v := range args {
					nonNull = nonNull && !v.Null
				}
				if nonNull {
					v := g.results[c.fn]
					v.I++
					g.results[c.fn] = v
				}
			}
		}
	}
	// Filtered pipelines omit entire groups. Get their universe first if no
	// unfiltered pipeline will create it (ungrouped COUNT always has one row).
	if !unfiltered && len(group) > 0 {
		ok, err := consume(distinctRequest(meta, wp, group, nil), nil)
		if !ok || err != nil {
			return nil, false, err
		}
	}
	for i := range calls {
		c := &calls[i]
		if c.where.none {
			continue
		}
		ok, err := consume(distinctRequest(meta, c.where, group, c.args), c)
		if !ok || err != nil {
			return nil, false, err
		}
	}
	out := make([]aggGroup, 0, len(order))
	for _, key := range order {
		g := groups[key]
		for _, c := range calls {
			for _, alias := range c.aliases {
				g.results[alias] = g.results[c.fn]
			}
		}
		out = append(out, *g)
	}
	return out, true, nil
}

// A residual predicate can run after deduplication only when it is a pure
// comparison of columns in that tuple. In particular, normalized string tag
// predicates need this exact recheck. Other expressions retain the row path.
func distinctResidual(expr Expr, group, args []columnMeta) bool {
	if expr == nil {
		return true
	}
	names := map[string]bool{}
	for _, c := range group {
		names[c.Name] = true
	}
	for _, c := range args {
		names[c.Name] = true
	}
	safe := true
	walkExpr(expr, func(x Expr) {
		switch x := x.(type) {
		case *ColumnRef:
			safe = safe && x.Outer == 0 && names[x.Name]
		case *Literal, *Param, *IsNull:
		case *Unary:
			safe = safe && x.Op == "NOT"
		case *Binary:
			switch x.Op {
			case "AND", "OR", "=", "<>", "!=", "<", ">", "<=", ">=":
			default:
				safe = false
			}
		default:
			safe = false
		}
	})
	return safe
}

// A hash group must contain exactly one value for each present column, and
// uniform NULL presence for every column. TOLIST keeps full-value equality;
// GROUPBY's hash is never sufficient evidence of equality on its own.
type distinctCheck struct{ field, flag, lo, hi, list string }

func (r *aggRequest) distinctTuple(row aggRow) bool {
	for _, c := range r.distinctChecks {
		lo, hi := row[c.lo], row[c.hi]
		if lo != hi || (lo != "0" && lo != "1") {
			return false
		}
		n, err := strconv.Atoi(row[c.list])
		if err != nil || n < 0 || n > 1 || (lo == "1" && n != 1) {
			return false
		}
		_, present := row[c.field]
		if present != (lo == "1") {
			return false
		}
	}
	return true
}

func (e *executor) binaryDistinctSupported(ctx context.Context) bool {
	if cc, ok := e.store.client.(*goredis.ClusterClient); ok {
		// During a rolling upgrade every shard that builds a TOLIST set must
		// use binary-safe equality, not just the cursor's coordinator.
		return cc.ForEachMaster(ctx, func(ctx context.Context, node *goredis.Client) error {
			if !binaryDistinctNode(ctx, node) {
				return errors.New("unverified binary distinct equality")
			}
			return nil
		}) == nil
	}
	return binaryDistinctNode(ctx, e.store.client)
}

func binaryDistinctNode(ctx context.Context, node processor) bool {
	cmd := goredis.NewCmd(ctx, "MODULE", "LIST")
	if err := node.Process(ctx, cmd); err != nil {
		return false
	}
	modules, ok := cmd.Val().([]any)
	if !ok {
		return false
	}
	for _, module := range modules {
		fields, ok := redisReplyFields(module)
		if !ok {
			continue
		}
		if fields["name"] == "search" || fields["name"] == "ft" {
			version, ok := redisReplyInt64(fields["ver"])
			return ok && version >= 80600
		}
	}
	return false
}
