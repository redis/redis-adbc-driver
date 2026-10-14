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
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// Large simple aggregates use bounded server tuple partitions, then an
// external exact merge. LIMIT is applied only after every partition has been
// reduced. Small queries and unsupported SQL keep their established plans.
const aggregatePartitionRows int64 = 65536

type tupleAggregatePlan struct {
	cols, distinct []columnMeta
	groupExprs     []Expr
	argExprs       map[int]Expr
	args           []int
	types          []ColType
	countAlias     string
	server         bool
	countGroups    bool
	partialGroups  bool
	functions      []*Func
	groupKeyMask   []bool
}

func tupleAggregateShape(plan *selectPlan, aggs []*Func) (*tupleAggregatePlan, bool) {
	meta := plan.meta
	if meta.isMem || meta.Adopted || meta.KeyRows || meta.AppIndex || len(plan.extraNeed) > 0 || len(plan.outerAggs) > 0 || plan.windowed() || plan.distinct || len(plan.distinctOn) > 0 || plan.qualify != nil || len(aggs) == 0 {
		return nil, false
	}
	if len(plan.sel.GroupBy) == 0 {
		distinct := false
		for _, f := range aggs {
			distinct = distinct || f.Distinct
		}
		if !distinct {
			return nil, false
		}
	}
	p := &tupleAggregatePlan{server: true, groupExprs: plan.sel.GroupBy, argExprs: map[int]Expr{}, functions: aggs}
	// An integer expression of columns already present as direct group keys
	// cannot refine their equality. Still evaluate every expression in SQL
	// order on every row: this only shortens the encoded key, never suppresses
	// an overflow or rewrites the projected expression (including NULLs).
	direct := map[string]bool{}
	for _, x := range p.groupExprs {
		if c, ok := x.(*ColumnRef); ok {
			direct[c.Name] = true
		}
	}
	for _, x := range p.groupExprs {
		keep := true
		if _, binary := x.(*Binary); binary && tupleInputExpr(x, meta.types()) {
			need := map[string]bool{}
			columnRefs(x, need)
			keep = false
			for name := range need {
				keep = keep || !direct[name]
			}
		}
		p.groupKeyMask = append(p.groupKeyMask, keep)
	}
	add := func(c columnMeta) int {
		for i, old := range p.cols {
			if old.Name == c.Name {
				return i
			}
		}
		p.cols = append(p.cols, c)
		return len(p.cols) - 1
	}
	for _, x := range plan.sel.GroupBy {
		if !tupleInputExpr(x, meta.types()) {
			return nil, false
		}
		need := map[string]bool{}
		columnRefs(x, need)
		for _, c := range meta.Columns {
			if need[c.Name] {
				if _, ok := distinctColumn(meta, &ColumnRef{Name: c.Name}); !ok {
					return nil, false
				}
				add(c)
			}
		}
	}
	// A single DISTINCT tuple (possibly requested under multiple aliases) is
	// sorted immediately after the group key, so equality needs no set.
	var distinctArgs []Expr
	for _, f := range aggs {
		if f.Filter != nil || len(f.OrderBy) > 0 || f.WithinGroup {
			return nil, false
		}
		if f.Distinct {
			if f.Name != "COUNT" || f.Star || len(f.Args) == 0 {
				return nil, false
			}
			if distinctArgs != nil {
				if len(distinctArgs) != len(f.Args) {
					return nil, false
				}
				for i, x := range f.Args {
					if !exprEqual(x, distinctArgs[i]) {
						return nil, false
					}
				}
			} else {
				distinctArgs = f.Args
				for _, x := range f.Args {
					c, ok := distinctColumn(meta, x)
					if !ok {
						return nil, false
					}
					p.distinct = append(p.distinct, c)
					add(c)
				}
			}
		}
	}
	for _, f := range aggs {
		switch f.Name {
		case "COUNT", "SUM", "AVG", "MIN", "MAX":
		default:
			return nil, false
		}
		arg := -1
		if f.Star {
			if f.Name != "COUNT" {
				return nil, false
			}
		} else if !f.Distinct {
			if len(f.Args) != 1 {
				return nil, false
			}
			x := f.Args[0]
			if !tupleInputExpr(x, meta.types()) {
				return nil, false
			}
			typ, err := inferType(x, meta.types(), nil)
			if err != nil || ((f.Name == "SUM" || f.Name == "AVG") && !typ.Kind.isInteger()) {
				return nil, false
			}
			if c, ok := distinctColumn(meta, x); ok {
				arg = add(c)
			} else {
				p.argExprs[len(p.args)] = x
				need := map[string]bool{}
				columnRefs(x, need)
				for _, c := range meta.Columns {
					if need[c.Name] {
						if _, ok := distinctColumn(meta, &ColumnRef{Name: c.Name}); !ok {
							return nil, false
						}
						add(c)
					}
				}
			}
		}
		p.args = append(p.args, arg)
		typ, err := inferType(f, meta.types(), nil)
		if err != nil {
			return nil, false
		}
		p.types = append(p.types, typ)
	}
	// Output expressions are deliberately simple: pruning final groups to
	// top-K must not evaluate volatile functions early or more than once.
	simple := func(x Expr) bool {
		for _, g := range plan.sel.GroupBy {
			if exprEqual(x, g) {
				return true
			}
		}
		switch x := x.(type) {
		case *Literal:
			return true
		case *ColumnRef:
			return x.Outer == 0 && groupedColumn(plan.sel.GroupBy, x.Name)
		case *Func:
			for _, f := range aggs {
				if x == f {
					return true
				}
			}
		}
		return false
	}
	for _, it := range plan.items {
		if !simple(it.expr) {
			return nil, false
		}
	}
	for _, o := range plan.order {
		if !simple(o.expr) {
			return nil, false
		}
	}
	p.countGroups = len(plan.sel.GroupBy) > 0
	for _, f := range aggs {
		p.countGroups = p.countGroups && f.Name == "COUNT" && f.Star && !f.Distinct
	}
	if p.countGroups {
		for _, x := range p.groupExprs {
			if _, column := x.(*ColumnRef); !column {
				// Exact source-tuple grouping requires LOAD * and TOLIST checks
				// before minute reduction. A narrow row scan avoids that work.
				p.server = false
				break
			}
		}
	}
	// Mixed integer SUM/AVG and string/integer MIN/MAX merge exact state by the
	// final SQL key. Reading just their inputs avoids LOAD * and source-tuple
	// TOLIST/existence checks on wide tables (including ClickBench Q32/Q33).
	p.partialGroups = !p.countGroups && len(p.distinct) == 0 && len(p.groupExprs) > 0
	for _, f := range aggs {
		if f.Star {
			p.partialGroups = p.partialGroups && f.Name == "COUNT"
			continue
		}
		typ, err := inferType(f.Args[0], meta.types(), nil)
		p.partialGroups = p.partialGroups && err == nil &&
			((f.Name == "SUM" || f.Name == "AVG") && typ.Kind.isInteger() ||
				(f.Name == "MIN" || f.Name == "MAX") && (typ.Kind.isInteger() || typ.Kind == KindString))
	}
	if p.partialGroups {
		p.server = false
	}
	if !tupleHavingExpr(plan.having, aggs) {
		return nil, false
	}
	p.countAlias = "__partition_count"
	for {
		found := false
		for _, c := range meta.Columns {
			found = found || c.field() == p.countAlias
		}
		if !found {
			break
		}
		p.countAlias += "_"
	}
	return p, true
}

func aggregateGroupLimit(err error) bool {
	// Only the documented cardinality failure is retryable; authentication,
	// timeouts, protocol errors and cancellation must remain visible.
	return err != nil && strings.Contains(err.Error(), "MAX_AGGREGATE_GROUPS") && strings.Contains(err.Error(), "GROUPBY")
}

func partitionWhere(wp wherePlan, lo, hi int64, tail bool) wherePlan {
	upper := strconv.FormatInt(hi, 10)
	if tail {
		upper = "+inf"
	}
	part := wp
	part.query = fmt.Sprintf("@%s:[%d %s]", rowIDField, lo, upper)
	if wp.query != "*" {
		part.query = fmt.Sprintf("(%s) %s", wp.query, part.query)
	}
	return part
}

// Sort by evaluated group and DISTINCT keys before other source values.
// Source values remain in the record so final projection uses a representative
// row with the driver's timestamp, string and NULL semantics.
func (p *tupleAggregatePlan) tuple(env *evalEnv, vals []Value, count int64) (spillTuple, error) {
	env.row = p.row(vals)
	keys, err := p.groupValues(env)
	if err != nil {
		return spillTuple{}, err
	}
	if len(p.groupKeyMask) > 0 {
		compact := keys[:0]
		for i, v := range keys {
			if p.groupKeyMask[i] {
				compact = append(compact, v)
			}
		}
		keys = compact
	}
	prefixCols := map[string]bool{}
	for _, x := range p.groupExprs {
		if c, ok := x.(*ColumnRef); ok {
			prefixCols[c.Name] = true
		}
	}
	for _, c := range p.distinct {
		keys = append(keys, env.row[c.Name])
		prefixCols[c.Name] = true
	}
	if !p.countGroups && !p.partialGroups {
		for i, c := range p.cols {
			if !prefixCols[c.Name] {
				keys = append(keys, vals[i])
			}
		}
	}
	row := spillTuple{key: rowKey(keys), raw: make([]string, len(vals)), null: make([]bool, len(vals)), count: count}
	for i, v := range vals {
		row.null[i] = v.Null
		if !v.Null {
			row.raw[i] = encodeStored(v)
		}
	}
	if p.partialGroups {
		for i, f := range p.functions {
			if f.Star {
				continue // COUNT(*) uses row.count
			}
			var v Value
			if p.args[i] >= 0 {
				v = vals[p.args[i]]
			} else {
				v, err = env.eval(p.argExprs[i])
				if err != nil {
					return spillTuple{}, invalidArg(err)
				}
			}
			n, sum := int64(0), "0"
			if !v.Null {
				n = count
				if f.Name == "SUM" || f.Name == "AVG" {
					sum = new(big.Int).Mul(big.NewInt(v.I), big.NewInt(n)).String()
				} else {
					sum = encodeStored(v)
				}
			}
			row.raw = append(row.raw, strconv.FormatInt(n, 10), sum)
			row.null = append(row.null, false, false)
		}
	}
	return row, nil
}
func (p *tupleAggregatePlan) row(vals []Value) map[string]Value {
	row := make(map[string]Value, len(vals))
	for i, c := range p.cols {
		row[c.Name] = vals[i]
	}
	return row
}

func (p *tupleAggregatePlan) partialOffset(aggregate int) int {
	offset := len(p.cols)
	for _, f := range p.functions[:aggregate] {
		if !f.Star {
			offset += 2
		}
	}
	return offset
}

// Partial sums are arbitrary-precision integer strings in the run. Counts
// preserve the AVG denominator and distinguish an all-NULL group from zero.
func (p *tupleAggregatePlan) combine(dst *spillTuple, src spillTuple) error {
	if p.partialGroups {
		i := len(p.cols)
		for fn, f := range p.functions {
			if f.Star {
				continue
			}
			n, err := strconv.ParseInt(dst.raw[i], 10, 64)
			m, otherErr := strconv.ParseInt(src.raw[i], 10, 64)
			if err != nil || otherErr != nil {
				return fmt.Errorf("invalid partial aggregate state")
			}
			if f.Name == "SUM" || f.Name == "AVG" {
				var a, b big.Int
				_, aok := a.SetString(dst.raw[i+1], 10)
				_, bok := b.SetString(src.raw[i+1], 10)
				if !aok || !bok {
					return fmt.Errorf("invalid partial sum")
				}
				dst.raw[i+1] = a.Add(&a, &b).String()
			} else if m > 0 {
				if n == 0 {
					dst.raw[i+1] = src.raw[i+1]
				} else {
					a, err := decodeStored(dst.raw[i+1], p.types[fn])
					if err != nil {
						return err
					}
					b, err := decodeStored(src.raw[i+1], p.types[fn])
					if err != nil {
						return err
					}
					cmp, _ := compareValues(b, a)
					if f.Name == "MIN" && cmp < 0 || f.Name == "MAX" && cmp > 0 {
						dst.raw[i+1] = src.raw[i+1]
					}
				}
			}
			dst.raw[i] = strconv.FormatInt(n+m, 10)
			i += 2
		}
	}
	dst.count += src.count
	return nil
}
func (p *tupleAggregatePlan) groupValues(env *evalEnv) ([]Value, error) {
	var vals []Value
	for _, x := range p.groupExprs {
		v, err := env.eval(x)
		if err != nil {
			return nil, invalidArg(err)
		}
		vals = append(vals, v)
	}
	return vals, nil
}

// Keep eligibility narrow: typed checked integer arithmetic, regex replacement,
// minute and character-length expressions admit no volatility or subqueries.
func tupleInputExpr(x Expr, types map[string]ColType) bool {
	switch x := x.(type) {
	case *ColumnRef:
		return x.Outer == 0
	case *Literal:
		return true
	case *Binary:
		inputs := compileAggregateInputs([]*Func{{Name: "SUM", Args: []Expr{x}}}, types)
		return inputs.roots[0] >= 0
	case *Func:
		if x.Name != "LENGTH" && x.Name != "DATE_PART" && x.Name != "REGEXP_REPLACE" {
			return false
		}
		for _, a := range x.Args {
			if !tupleInputExpr(a, types) {
				return false
			}
		}
		return true
	}
	return false
}
func tupleHavingExpr(x Expr, aggs []*Func) bool {
	if x == nil {
		return true
	}
	for _, f := range aggs {
		if x == f {
			return true
		}
	}
	switch x := x.(type) {
	case *Literal:
		return true
	case *Binary:
		switch x.Op {
		case "AND", "OR", "=", "!=", "<>", "<", "<=", ">", ">=":
			return tupleHavingExpr(x.L, aggs) && tupleHavingExpr(x.R, aggs)
		}
	}
	return false
}

func (p *tupleAggregatePlan) values(row spillTuple) ([]Value, error) {
	vals := make([]Value, len(p.cols))
	for i, c := range p.cols {
		if row.null[i] {
			vals[i] = nullValue(c.Type)
			continue
		}
		v, err := decodeStored(row.raw[i], c.Type)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

// Read an entire partition before accepting it. A group-limit error can
// occur on a later cursor page: discard that attempt before splitting or
// scanning, otherwise partial rows would be counted twice.
func (e *executor) aggregateTuplePartition(ctx context.Context, plan *selectPlan, p *tupleAggregatePlan, wp wherePlan, params []Value, lo, hi int64, tail bool, visit func(spillTuple) error) error {
	part := partitionWhere(wp, lo, hi, tail)
	if e.aggregateMetrics != nil {
		e.aggregateMetrics.partitions.Add(1)
	}
	env := e.newEnv(ctx, plan.meta.types(), params)
	emit := func(vals []Value, count int64) error {
		row, err := p.tuple(env, vals, count)
		if err != nil {
			return err
		}
		return visit(row)
	}
	scan := func() error {
		if e.aggregateMetrics != nil {
			e.aggregateMetrics.rowScans.Add(1)
			if p.server {
				e.aggregateMetrics.fallbacks.Add(1)
			}
		}
		need := map[string]bool{}
		for _, c := range p.cols {
			need[c.Name] = true
		}
		local := *e
		local.telemetry = nil
		if e.telemetry != nil {
			stats := &scanTelemetry{}
			local.telemetry = stats
			defer func() { e.telemetry.fetch.Add(stats.fetch.Load()) }()
		}
		return local.scanPages(ctx, scanRequest{meta: plan.meta, where: part, need: need}, params, func(rows []map[string]Value) error {
			for _, row := range rows {
				vals := make([]Value, len(p.cols))
				for i, c := range p.cols {
					v, ok := row[c.Name]
					if !ok {
						v = nullValue(c.Type)
					}
					vals[i] = v
				}
				if e.aggregateMetrics != nil {
					e.aggregateMetrics.tuples.Add(1)
					for i, v := range vals {
						if !v.Null {
							e.aggregateMetrics.valueBytes.Add(int64(len(p.cols[i].field()) + len(encodeStored(v))))
						}
					}
				}
				if err := emit(vals, 1); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if !p.server {
		return scan()
	}
	req := distinctRequest(plan.meta, part, p.cols, nil)
	req.groupBy = append(req.groupBy, "REDUCE", "COUNT", 0, "AS", p.countAlias)
	req.width++
	started := time.Now()
	var endFetch func()
	if e.aggregateMetrics != nil {
		endFetch = e.aggregateMetrics.fetch.start()
	}
	raw, err := e.store.aggregate(ctx, req)
	if endFetch != nil {
		endFetch()
	}
	if e.telemetry != nil {
		e.telemetry.fetch.Add(int64(time.Since(started)))
	}
	if e.aggregateMetrics != nil {
		e.aggregateMetrics.redisNanos.Add(int64(time.Since(started)))
		e.aggregateMetrics.tuples.Add(int64(len(raw)))
		for _, row := range raw {
			for k, v := range row {
				e.aggregateMetrics.valueBytes.Add(int64(len(k) + len(v)))
			}
		}
	}
	if aggregateGroupLimit(err) {
		if e.aggregateMetrics != nil {
			e.aggregateMetrics.retries.Add(1)
		}
		if tail || hi <= lo {
			return scan()
		}
		mid := lo + (hi-lo)/2
		if err = e.aggregateTuplePartition(ctx, plan, p, wp, params, lo, mid, false, visit); err != nil {
			return err
		}
		return e.aggregateTuplePartition(ctx, plan, p, wp, params, mid+1, hi, false, visit)
	}
	if err != nil {
		return err
	}
	// Validate before emitting any tuples, including tuple-dependent residuals.
	for _, row := range raw {
		_, ok := distinctValues(row, p.cols)
		count, err := strconv.ParseInt(row[p.countAlias], 10, 64)
		if !ok || !req.distinctTuple(row) || err != nil || count <= 0 {
			return scan()
		}
	}
	env.row = map[string]Value{}
	for _, row := range raw {
		if err := cancelled(ctx); err != nil {
			return err
		}
		vals, _ := distinctValues(row, p.cols)
		if part.residual != nil {
			for i, c := range p.cols {
				env.row[c.Name] = vals[i]
			}
			v, err := env.eval(part.residual)
			if err != nil {
				return invalidArg(err)
			}
			if b, ok := truthy(v); !ok || !b {
				continue
			}
		}
		count, _ := strconv.ParseInt(row[p.countAlias], 10, 64)
		if err := emit(vals, count); err != nil {
			return err
		}
	}
	return nil
}

func addWeightedInteger(a *accumulator, v Value, n int64) {
	if a.fn.Star {
		a.count += n
		return
	}
	if v.Null {
		return
	}
	switch a.fn.Name {
	case "SUM", "AVG":
		// Canonical integer inputs, so the sum has scale=exp=0. Multiply in
		// arbitrary precision instead of rounding to double or overflowing int64.
		var product, multiplicity big.Int
		product.SetInt64(v.I)
		multiplicity.SetInt64(n)
		product.Mul(&product, &multiplicity)
		a.sum.sum.Add(&a.sum.sum, &product)
		a.sum.n += n
	case "MIN", "MAX":
		a.add(v)
		n--
	}
	a.count += n
}

func (e *executor) partitionedAggregate(ctx context.Context, plan *selectPlan, wp wherePlan, aggs []*Func, params []Value, force bool) (groups []aggGroup, handled bool, resultErr error) {
	p, ok := tupleAggregateShape(plan, aggs)
	if !ok || wp.none || wp.keys != nil {
		return nil, false, nil
	}
	parentCtx, original := ctx, e
	defer func() {
		var sqlErr adbc.Error
		if handled && parentCtx.Err() == nil && errors.As(resultErr, &sqlErr) && (sqlErr.Code == adbc.StatusInvalidArgument || sqlErr.Code == adbc.StatusInvalidData) {
			// Deterministic expressions can still fail. Re-evaluate in the
			// original scan and argument order instead of reporting whichever
			// partition happened to finish first.
			groups, resultErr = original.serialDriverAggregate(parentCtx, plan, wp, aggs, params)
		}
	}()
	// Residual-only inputs must be retained in the server tuple before filtering.
	need := map[string]bool{}
	columnRefs(wp.residual, need)
	for _, c := range plan.meta.Columns {
		if !need[c.Name] {
			continue
		}
		found := false
		for _, old := range p.cols {
			found = found || old.Name == c.Name
		}
		if !found {
			if _, ok := distinctColumn(plan.meta, &ColumnRef{Name: c.Name}); !ok {
				return nil, false, nil
			}
			p.cols = append(p.cols, c)
		}
	}
	if !distinctResidual(wp.residual, p.cols, nil) {
		return nil, false, nil
	}
	last, err := e.store.client.Get(ctx, seqKey(plan.meta.Schema, plan.meta.Name)).Int64()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, wrapRedis(err, "reading aggregate row sequence failed")
	}
	if (!force && last < aggregatePartitionRows) || last >= exactDoubleLimit {
		return nil, false, nil
	}
	if e.pushdown == PushdownNone {
		p.server = false
	}
	for _, c := range p.cols {
		if c.Type.Kind == KindString && !c.sortsExactly() && !e.binaryDistinctSupported(ctx) {
			p.server = false
			break
		}
	}
	dir, err := os.MkdirTemp("", "redis-adbc-aggregate-*")
	if err != nil {
		return nil, true, wrapRedis(err, "creating aggregate spill directory failed")
	}
	defer os.RemoveAll(dir)
	workers := min(effectiveQueryWorkers(e.queryWorkers), int((last+aggregatePartitionRows-1)/aggregatePartitionRows))
	if e.workerBudget != nil {
		grant, err := e.workerBudget.acquireBounded(ctx, e.queryWorkers, workers)
		if err != nil {
			return nil, true, cancelled(ctx)
		}
		workers = grant
		defer e.workerBudget.release(grant)
	}
	if e.workerGrant != nil {
		e.workerGrant.Store(int64(workers))
	}
	if e.aggregateMetrics != nil {
		e.aggregateMetrics.used.Store(true)
		e.aggregateMetrics.workers.Store(int64(workers))
	}
	e.diagnostics.setPhase("partition_fetch")
	e.diagnostics.setPlan(map[string]any{"kind": "partitioned_exact", "server_source_tuples": p.server,
		"partial_groups": p.partialGroups, "count_groups": p.countGroups, "group_expressions": p.groupExprs,
		"encoded_group_key_mask": p.groupKeyMask,
		"input_columns":          p.cols, "residual": wp.residual, "search_query": wp.query, "partition_rows": aggregatePartitionRows})
	if e.workerBudget != nil && (e.queryWorkers == 0 || e.workerBudget.automatic()) {
		stats := &scanTelemetry{}
		local := *e
		local.telemetry = stats
		e = &local
		key := plan.meta.Schema + "." + plan.meta.Name + ":tuple:" + wp.query + fmt.Sprint(len(p.groupExprs))
		for _, c := range p.cols {
			key += ":" + c.Name
		}
		for _, f := range aggs {
			key += ":" + f.Name + strconv.FormatBool(f.Distinct)
		}
		start := e.workerBudget.begin(key)
		defer func() { e.workerBudget.finish(start, stats, resultErr != nil) }()
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan int64)
	spills := make([]tupleSpill, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := range workers {
		spills[w] = tupleSpill{dir: dir, prefix: fmt.Sprint(w), metrics: e.aggregateMetrics, combine: p.combine}
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := *e
			local.cache = newExecCache()
			for lo := range jobs {
				hi := min(last, lo+aggregatePartitionRows-1)
				started := time.Now()
				errs[w] = local.aggregateTuplePartition(ctx, plan, p, wp, params, lo, hi, hi == last, func(row spillTuple) error {
					if local.telemetry != nil {
						local.telemetry.rows.Add(row.count)
					}
					if local.aggregateMetrics != nil {
						local.aggregateMetrics.rows.Add(row.count)
					}
					return spills[w].add(ctx, row)
				})
				if local.aggregateMetrics != nil {
					local.aggregateMetrics.partitionNanos.Add(int64(time.Since(started)))
				}
				if errs[w] != nil {
					cancel(errs[w])
					return
				}
			}
			errs[w] = spills[w].flush(ctx)
			if errs[w] != nil {
				cancel(errs[w])
			}
		}(w)
	}
dispatch:
	for lo := int64(1); lo <= last; lo += aggregatePartitionRows {
		select {
		case jobs <- lo:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		cause := context.Cause(ctx)
		if cause != ctx.Err() {
			return nil, true, wrapRedis(cause, "partitioned aggregate failed")
		}
		return nil, true, cancelled(ctx)
	}
	combined := tupleSpill{dir: dir, prefix: "merge", metrics: e.aggregateMetrics, combine: p.combine}
	for w := range spills {
		combined.runs = append(combined.runs, spills[w].runs...)
	}
	started := time.Now()
	e.diagnostics.setPhase("aggregate_merge")
	var endMerge func()
	if e.aggregateMetrics != nil {
		endMerge = e.aggregateMetrics.merge.start()
	}
	out, err := e.reduceTupleRuns(ctx, plan, p, aggs, params, &combined)
	if endMerge != nil {
		endMerge()
	}
	if e.aggregateMetrics != nil {
		e.aggregateMetrics.mergeNanos.Add(int64(time.Since(started)))
	}
	if err != nil {
		return nil, true, wrapRedis(err, "exact aggregate merge failed")
	}
	return out, true, nil
}

func (e *executor) reduceTupleRuns(ctx context.Context, plan *selectPlan, p *tupleAggregatePlan, aggs []*Func, params []Value, spill *tupleSpill) ([]aggGroup, error) {
	if groups, handled, err := e.reduceCountGroups(ctx, plan, p, aggs, params, spill); handled {
		return groups, err
	}
	env := e.newEnv(ctx, plan.meta.types(), params)
	var out []aggGroup
	// Keep only final top-K groups for the simple supported projection. Ties
	// use the deterministic merge order; SQL does not define their order.
	limit := aggregateTopLimit(plan, math.MaxInt)
	if len(plan.order) == 0 && plan.sel.Limit != nil && *plan.sel.Limit > 0 {
		off := int64(0)
		if plan.sel.Offset != nil {
			off = *plan.sel.Offset
		}
		if off >= 0 && off < math.MaxInt && *plan.sel.Limit < math.MaxInt-off {
			limit = int(off + *plan.sel.Limit)
		}
	}
	top := aggregateTopK{order: plan.order}
	topGroups := map[int]aggGroup{}
	ordinal := 0
	var groupKey, distinctKey string
	haveGroup, haveDistinct := false, false
	var rep map[string]Value
	var accs []*accumulator
	emit := func() error {
		if !haveGroup {
			return nil
		}
		g := aggGroup{rep: rep, results: map[*Func]Value{}}
		for i, f := range aggs {
			v, err := accs[i].result(env, p.types[i])
			if err != nil {
				return invalidArg(err)
			}
			g.results[f] = v
		}
		env.row, env.aggs = g.rep, g.results
		if plan.having != nil {
			v, err := env.eval(plan.having)
			if err != nil {
				return invalidArg(err)
			}
			if b, ok := truthy(v); !ok || !b {
				return nil
			}
		}
		if limit == 0 {
			out = append(out, g)
			return nil
		}
		env.row, env.aggs = g.rep, g.results
		row := aggregateOutput{ordinal: ordinal}
		ordinal++
		for _, o := range plan.order {
			v, err := env.eval(o.expr)
			if err != nil {
				return invalidArg(err)
			}
			row.keys = append(row.keys, v)
		}
		if len(top.rows) < limit {
			// Only projected final state is retained. No per-group distinct sets.
			topGroups[row.ordinal] = g
			heap.Push(&top, row)
		} else if top.better(row, top.rows[0]) {
			delete(topGroups, top.rows[0].ordinal)
			topGroups[row.ordinal] = g
			top.rows[0] = row
			heap.Fix(&top, 0)
		}
		return nil
	}
	stop := errors.New("unordered aggregate output complete")
	err := spill.merge(ctx, func(row spillTuple) error {
		vals, err := p.values(row)
		if err != nil {
			return err
		}
		env.row = p.row(vals)
		groupVals, err := p.groupValues(env)
		if err != nil {
			return err
		}
		key := rowKey(groupVals)
		if !haveGroup || key != groupKey {
			if err := emit(); err != nil {
				return err
			}
			// Runs contain the entire input and are sorted by SQL group key.
			// The previous group's count is now complete in every run. An
			// unordered LIMIT permits returning these first complete groups.
			if limit > 0 && len(plan.order) == 0 && len(top.rows) == limit {
				return stop
			}
			haveGroup, groupKey, haveDistinct = true, key, false
			rep = p.row(vals)
			accs = make([]*accumulator, len(aggs))
			for i, f := range aggs {
				accs[i] = newAccumulator(f)
			}
		}
		env.row = p.row(vals)
		var dvals []Value
		valid := true
		for _, c := range p.distinct {
			for i, col := range p.cols {
				if col.Name == c.Name {
					dvals = append(dvals, vals[i])
					valid = valid && !vals[i].Null
					break
				}
			}
		}
		dkey := rowKey(dvals)
		fresh := valid && (!haveDistinct || distinctKey != dkey)
		if valid {
			distinctKey, haveDistinct = dkey, true
		}
		for i, f := range aggs {
			if p.partialGroups {
				if f.Star {
					accs[i].count += row.count
				} else {
					offset := p.partialOffset(i)
					n, err := strconv.ParseInt(row.raw[offset], 10, 64)
					if err != nil {
						return fmt.Errorf("invalid partial aggregate state")
					}
					if f.Name == "SUM" || f.Name == "AVG" {
						var sum big.Int
						if _, ok := sum.SetString(row.raw[offset+1], 10); !ok {
							return fmt.Errorf("invalid partial sum")
						}
						accs[i].count += n
						accs[i].sum.n += n
						accs[i].sum.sum.Add(&accs[i].sum.sum, &sum)
					} else if n > 0 {
						v, err := decodeStored(row.raw[offset+1], p.types[i])
						if err != nil {
							return err
						}
						accs[i].add(v)
						accs[i].count += n - 1
					}
				}
				continue
			}
			if f.Distinct {
				if fresh {
					accs[i].count++
				}
				continue
			}
			var v Value
			if p.args[i] >= 0 {
				v = vals[p.args[i]]
			}
			if x := p.argExprs[i]; x != nil {
				v, err = env.eval(x)
				if err != nil {
					return invalidArg(err)
				}
			}
			addWeightedInteger(accs[i], v, row.count)
		}
		return nil
	})
	stopped := errors.Is(err, stop)
	if err != nil && !stopped {
		return nil, err
	}
	if !haveGroup && len(p.groupExprs) == 0 {
		haveGroup = true
		rep = map[string]Value{}
		for _, f := range aggs {
			accs = append(accs, newAccumulator(f))
		}
	}
	if !stopped {
		if err := emit(); err != nil {
			return nil, err
		}
	}
	if limit > 0 {
		for _, row := range top.rows {
			out = append(out, topGroups[row.ordinal])
		}
	}
	return out, nil
}
