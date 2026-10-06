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

// Name resolution, subqueries, CTEs and derived tables.
//
// Every SELECT (and the target of UPDATE/DELETE) pushes a scope holding the
// relations its columns may come from. A column reference resolves in the
// innermost scope that has it (for t.col, an item visible as t: by its
// alias if it has one, else by its name; see resolveColumn); a reference
// that resolves in an enclosing scope is a correlated (outer) reference, and
// every subquery between the reference and its scope is marked correlated.
//
// Subqueries run through the same executor: uncorrelated ones once per
// statement (cached), correlated ones once per distinct set of outer values
// (memoised), unless they can run once as a semi-join (semijoin.go). CTEs and
// derived tables are run once and held in memory as a relation that the
// regular driver-side filter/sort/group paths read.
//
// As in Postgres, a derived table or CTE may read the columns of the queries
// around its level (the query whose FROM or WITH list holds it), but not the
// FROM items of that level: a derived table is planned with those queries'
// scopes and an empty one for its level, which holds what it can't read for
// errors (unseenRel), and a CTE with the scopes and WITH lists around its
// WITH list, wherever it is read. One that reads an enclosing query is
// planned as a correlated subquery instead of being run, and its rows are
// computed when its reader is scanned, once per distinct set of the outer
// values it reads (see lazyRelation).

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/apache/arrow-adbc/go/adbc"
)

type relation struct {
	name string // alias, table or CTE name
	// aliased is set when name is an alias. schema is the schema of a table
	// or view, "" for other items: without an alias, schema.name names the
	// item too.
	aliased bool
	schema  string
	meta    *tableMeta
	// prefix is prepended to resolved column names ("alias." in joins).
	prefix string
	// hidden are columns merged by a NATURAL JOIN: only a qualified
	// reference reads them, an unqualified one means the merged column.
	hidden map[string]bool
}

type scope struct {
	rels []relation
	// sq is the subquery whose body this scope is (nil at the top level).
	sq *Subquery
	// needs are this scope's columns read by correlated subqueries; the
	// scope's query must fetch them even if it doesn't use them itself.
	needs map[string]bool
	// mergeReturning is set for the RETURNING list of a MERGE, the only
	// place merge_action() may be called.
	mergeReturning bool
	// aggClause names the clause being bound, for Postgres's error for an
	// aggregate of this scope's level in it (`aggregate functions are not
	// allowed in WHERE`); it is empty where one may be: a SELECT's list,
	// HAVING, ORDER BY, QUALIFY, WINDOW and DISTINCT ON. inAggregate counts
	// the aggregate calls of this level whose arguments are being bound: an
	// aggregate of this level in them is nested.
	aggClause   string
	inAggregate int
	// outerAggs are the aggregates of this scope's level that its
	// subqueries hold (see bindAggregate).
	outerAggs []*outerAggregate
	// funcArgs is set for the scope of a FROM function's arguments, which
	// are LATERAL already (see lateralOnly).
	funcArgs bool
	// unseen are the FROM items of this scope's level that the query being
	// planned there can't read (see unseenRel).
	unseen []unseenRel
}

// unseenRel is a FROM item of a level that a FROM item of the same level
// can't read: for a derived table, the items before it, which only LATERAL
// makes visible, and for every FROM item of UPDATE, DELETE and MERGE, the
// target table. Only errors read them, as Postgres's errorMissingRTE and
// errorMissingColumn do (see lateralOnly), except forbidden ones. noHint is
// set if LATERAL wouldn't help: for the target, and for the items before
// the right side of a RIGHT or FULL join. forbidden is set for the target
// of UPDATE or DELETE as seen from a LATERAL item or a FROM function's
// arguments: Postgres finds it there, but reading it is an error
// (check_lateral_ref_ok), even if a later item has the column too.
type unseenRel struct {
	relation
	noHint, forbidden bool
}

// execCache holds per-statement results shared by nested executors.
type execCache struct {
	sub           map[*Subquery][][]Value
	inSets        map[*Subquery]*inSet
	rowSets       map[*Subquery]*rowSet
	inLists       map[*Binary]*inList // nil: evaluate the OR chain as written
	memo          map[*Subquery]map[string][][]Value
	semi          map[*Subquery]*semiResult
	ctes          map[*CTE]*tableMeta
	derived       map[*SelectStmt]*tableMeta
	materializing map[any]bool
	// working is the working table of each recursive CTE being evaluated.
	working map[*CTE]*tableMeta
	// extremes are the bounds of uncorrelated ANY / ALL subqueries.
	extremes map[*Subquery]*quantExtremes
	// prefixes caches prefixComplete by index and query (tags.go).
	prefixes map[string]bool
	// usedTemp is set when a name resolves to a temporary object.
	usedTemp bool
	// colReads, when set, is told of each column of a stored table that
	// the statement reads, outside the views it reads (viewDepth counts
	// those being planned): ALTER COLUMN … TYPE finds the views that read
	// a column this way (see viewDependencies).
	colReads  func(meta *tableMeta, column string)
	viewDepth int
	// loaded are the tables the statement read (see checkReads in rekey.go).
	loaded []*tableMeta
	// branchTypes are the result types of CASE and IIF expressions
	// (branchValue).
	branchTypes map[Expr]ColType
	// paramHints are the parameter types found while describing a
	// statement's parameters (params.go); nil otherwise.
	paramHints map[int]ColType
}

func newExecCache() *execCache {
	return &execCache{
		sub:           map[*Subquery][][]Value{},
		inSets:        map[*Subquery]*inSet{},
		rowSets:       map[*Subquery]*rowSet{},
		inLists:       map[*Binary]*inList{},
		memo:          map[*Subquery]map[string][][]Value{},
		semi:          map[*Subquery]*semiResult{},
		ctes:          map[*CTE]*tableMeta{},
		derived:       map[*SelectStmt]*tableMeta{},
		materializing: map[any]bool{},
		working:       map[*CTE]*tableMeta{},
		extremes:      map[*Subquery]*quantExtremes{},
		branchTypes:   map[Expr]ColType{},
	}
}

func (e *executor) ensureCache() {
	if e.cache == nil {
		e.cache = newExecCache()
	}
}

// newEnv creates an evaluation environment wired to this executor, so that
// subqueries and outer references can be evaluated.
func (e *executor) newEnv(ctx context.Context, types map[string]ColType, params []Value) *evalEnv {
	return &evalEnv{ctx: ctx, exec: e, outer: e.outer, types: types, params: params}
}

// pushScope pushes the scope of a query or statement over rels; aggClause
// is that of the clause bound first (see scope.aggClause).
func (e *executor) pushScope(rels []relation, aggClause string) *scope {
	sc := &scope{rels: rels, sq: e.pendingSq, needs: map[string]bool{}, aggClause: aggClause}
	e.pendingSq = nil
	e.scopes = append(e.scopes, sc)
	return sc
}

func (e *executor) popScope() { e.scopes = e.scopes[:len(e.scopes)-1] }

// tableRel is the relation of a table read under alias ("" for none).
func tableRel(meta *tableMeta, alias string) relation {
	if alias == "" {
		return relation{name: meta.Name, schema: meta.Schema, meta: meta}
	}
	return relation{name: alias, aliased: true, schema: meta.Schema, meta: meta}
}

// bindIn binds an expression in a new scope over one table read under alias
// ("" for none); meta may be nil for expressions without a FROM. clause
// names the clause for the error of an aggregate in it ("" for none). It
// returns the columns of that table read by correlated subqueries.
func (e *executor) bindIn(ctx context.Context, expr Expr, meta *tableMeta, alias, clause string) (map[string]bool, error) {
	var rels []relation
	if meta != nil {
		rels = []relation{tableRel(meta, alias)}
	}
	sc := e.pushScope(rels, clause)
	defer e.popScope()
	if err := e.bind(ctx, expr); err != nil {
		return nil, err
	}
	if containsWindow(expr) {
		return nil, errWindowPlacement()
	}
	if clause == "WHERE" {
		if err := e.conditionTypeError(expr, clause); err != nil {
			return nil, err
		}
	}
	return sc.needs, nil
}

// bind resolves the column references of expr against the scope stack and
// plans its subqueries.
func (e *executor) bind(ctx context.Context, expr Expr) error {
	var err error
	walkExprPruned(expr, func(x Expr) bool {
		if err != nil {
			return false
		}
		switch v := x.(type) {
		case *ColumnRef:
			if v.outerAgg != nil {
				err = e.bindOuterAggregate(ctx, v)
			} else {
				err = e.resolveColumn(v)
			}
		case *Binary:
			_, l := v.L.(*RowExpr)
			_, r := v.R.(*RowExpr)
			if l || r {
				// It binds its operands itself.
				err = e.bindRowOperator(ctx, v)
				return false
			}
		case *Subquery:
			err = e.planSubquery(ctx, v)
		case *Func:
			// A window call's own *Func is not visited, so this is a call
			// without OVER.
			if aggregateFuncs[v.Name] {
				// It binds its arguments itself.
				err = e.bindAggregate(ctx, v)
				return false
			}
			err = e.bindCall(ctx, v, nil)
			if err == nil && v.Name == "MERGE_ACTION" {
				err = e.checkMergeAction(v)
			}
		case *WindowFunc:
			err = e.bindCall(ctx, v.Func, v)
		}
		return err == nil
	})
	if err == nil {
		// Now that the types are known.
		err = e.checkRecords(expr)
	}
	if err == nil {
		if err = checkUnknowns(expr, e.scopeTypes, e.paramTypes); err != nil {
			return invalidArg(err)
		}
		e.noteParams(expr)
		if e.localLits != nil {
			e.noteLocalTimes(expr)
		}
	}
	return err
}

// ---- aggregates of enclosing queries ----
//
// As in Postgres (check_agg_arguments), an aggregate call belongs to the
// innermost query whose columns its arguments, ORDER BY and FILTER read (for
// an ordered-set aggregate, its WITHIN GROUP value and FILTER), or to its own
// query if they read none. In `SELECT v, (SELECT SUM(g.v) FROM g x WHERE x.id
// = 1) FROM g GROUP BY v`, SUM(g.v) is the outer query's: it adds g.v over
// each group of g, and makes a query without GROUP BY an aggregate one. Where
// the call may be is decided at that level: `SELECT id FROM g WHERE (SELECT
// MAX(g.id)) > 1` has an aggregate in its WHERE.
//
// Such a call is moved to its query while the subquery is bound: the query
// computes it with its own aggregates, into a hidden column of its groups,
// and the call becomes COALESCE(ref), which is ref, a reference to that
// column. For the subquery that is an outer column like any other, constant
// while it runs and part of its memo key.

// outerAggregate is an aggregate call that a subquery holds, of an enclosing
// query's level: fn is bound in that query's scope, which computes it into
// its hidden column name.
type outerAggregate struct {
	fn   *Func
	name string
	typ  ColType
}

// outerAggregates numbers the hidden columns. It is never reset: a cached
// statement planned again keeps the names it has, and new ones stay apart.
var outerAggregates atomic.Int64

// bindAggregate binds an aggregate call without OVER, in its query's scope.
func (e *executor) bindAggregate(ctx context.Context, f *Func) error {
	level, direct, known := e.aggregateLevel(f)
	if known && level > 0 {
		if direct >= 0 && direct < level {
			return errorf(adbc.StatusInvalidArgument, "outer-level aggregate cannot contain a lower-level variable in its direct arguments")
		}
		return e.hoistAggregate(ctx, f, level)
	}
	var sc *scope
	if n := len(e.scopes); n > 0 {
		sc = e.scopes[n-1]
		sc.inAggregate++
	}
	err := e.bindAggregateParts(ctx, f, sc)
	if sc != nil {
		sc.inAggregate--
	}
	if err != nil {
		return err
	}
	if !known && boundLevel(f) > 0 {
		// Its columns are found only now, in a subquery: they are an
		// enclosing query's, where binding it again would plan that subquery
		// at another level.
		return errorf(adbc.StatusNotImplemented, "outer-level aggregate with a subquery in its arguments is not supported")
	}
	if err := resolveCall(f, false); err != nil {
		return err
	}
	switch {
	case sc == nil:
	case sc.aggClause != "":
		return errorf(adbc.StatusInvalidArgument, "aggregate functions are not allowed in %s", sc.aggClause)
	case sc.inAggregate > 0:
		return errorf(adbc.StatusInvalidArgument, "aggregate function calls cannot be nested")
	}
	return nil
}

// bindAggregateParts binds the arguments, ORDER BY and FILTER of an aggregate
// call of the scope sc (nil without one).
func (e *executor) bindAggregateParts(ctx context.Context, f *Func, sc *scope) error {
	for _, a := range f.Args {
		if err := e.bind(ctx, a); err != nil {
			return err
		}
	}
	for _, o := range f.OrderBy {
		if err := e.bind(ctx, o.Expr); err != nil {
			return err
		}
	}
	if f.Filter == nil {
		return nil
	}
	if sc != nil {
		saved := sc.aggClause
		sc.aggClause = "FILTER"
		defer func() { sc.aggClause = saved }()
	}
	return e.bind(ctx, f.Filter)
}

// aggregateParts are the expressions that decide an aggregate call's level:
// its arguments (for an ordered-set aggregate, its WITHIN GROUP value
// instead), ORDER BY and FILTER.
func aggregateParts(f *Func) []Expr {
	parts := callArgs(f, nil)
	if orderedSetFuncs[f.Name] {
		parts = parts[len(f.Args):]
	}
	return parts
}

// aggregateLevel returns the level of an aggregate call before it is bound:
// how many queries up its columns' innermost query is. direct is the lowest
// level of an ordered-set aggregate's direct arguments (-1 without columns).
// known is false if a column doesn't resolve, or if the call has a
// subquery, whose columns are found only when it is planned.
func (e *executor) aggregateLevel(f *Func) (level, direct int, known bool) {
	known = true
	lowest := func(exprs []Expr) int {
		low := -1
		for _, x := range exprs {
			walkExpr(x, func(n Expr) {
				d := 0
				switch v := n.(type) {
				case *ColumnRef:
					if v.outerAgg != nil {
						d = v.Outer
					} else if depth, _, err := e.findColumn(v); err == nil {
						d = depth
					} else {
						known = false
					}
				case *Subquery:
					known = false
					return
				default:
					return
				}
				if low < 0 || d < low {
					low = d
				}
			})
		}
		return low
	}
	direct = -1
	if orderedSetFuncs[f.Name] {
		direct = lowest(f.Args)
	}
	return max(lowest(aggregateParts(f)), 0), direct, known
}

// boundLevel returns the level of a bound aggregate call: the lowest of its
// columns' and its subqueries' outer references.
func boundLevel(f *Func) int {
	low := -1
	lower := func(d int) {
		if low < 0 || d < low {
			low = d
		}
	}
	for _, x := range aggregateParts(f) {
		walkExpr(x, func(n Expr) {
			switch v := n.(type) {
			case *ColumnRef:
				lower(v.Outer)
			case *Subquery:
				for _, r := range v.outerRefs {
					lower(r.up)
				}
			}
		})
	}
	return max(low, 0)
}

// hoistAggregate moves an aggregate call of the query level levels up to it.
func (e *executor) hoistAggregate(ctx context.Context, f *Func, level int) error {
	fn := *f
	h := &outerAggregate{fn: &fn, name: hiddenColumn("outeragg", int(outerAggregates.Add(1)))}
	ref := &ColumnRef{Name: h.name, Outer: level, outerAgg: h}
	if err := e.bindOuterAggregate(ctx, ref); err != nil {
		return err
	}
	*f = Func{Name: "COALESCE", Args: []Expr{ref}}
	return nil
}

// bindOuterAggregate binds the reference to an aggregate of the query
// ref.Outer levels up, whenever its subquery is planned: it binds the
// aggregate in that query's scope, where it must be allowed, and makes it
// one of the query's aggregates.
func (e *executor) bindOuterAggregate(ctx context.Context, ref *ColumnRef) error {
	h := ref.outerAgg
	top := len(e.scopes) - 1
	if ref.Outer < 1 || ref.Outer > top {
		return errorf(adbc.StatusInternal, "aggregate of an enclosing query has no scope")
	}
	sc := e.scopes[top-ref.Outer]
	saved := e.scopes
	e.scopes = slices.Clip(e.scopes[:top-ref.Outer+1])
	err := e.bind(ctx, h.fn)
	if err == nil {
		if h.typ, err = inferType(h.fn, e.scopeTypes(), e.paramTypes); err != nil {
			err = invalidArg(err)
		}
	}
	e.scopes = saved
	if err != nil {
		return err
	}
	ref.OuterType = h.typ
	if !slices.Contains(sc.outerAggs, h) {
		sc.outerAggs = append(sc.outerAggs, h)
	}
	e.correlate(h.name, ref.Outer)
	return nil
}

// bindCall resolves a call while it is bound (resolveCall in funcs.go); w is
// its OVER, if any. A call is visited before its arguments, so when it is
// wrong its arguments are bound first: as in Postgres, an unknown column or
// function in an argument is reported before the call's own error, and the
// argument types of an unknown function are known.
func (e *executor) bindCall(ctx context.Context, f *Func, w *WindowFunc) error {
	err := resolveCall(f, w != nil)
	if err == nil {
		return nil
	}
	for _, a := range callArgs(f, w) {
		if aerr := e.bind(ctx, a); aerr != nil {
			return aerr
		}
	}
	if err == errUnknownFunction {
		return noSuchFunction(f.Name, f.Star, inferArgTypes(f.Args, e.scopeTypes(), e.paramTypes))
	}
	return err
}

// scopeTypes returns the types of the columns of the innermost scope, by
// the names column references resolve to.
func (e *executor) scopeTypes() map[string]ColType {
	types := map[string]ColType{}
	if n := len(e.scopes); n > 0 {
		for _, rel := range e.scopes[n-1].rels {
			for _, c := range rel.meta.Columns {
				types[rel.prefix+c.Name] = c.Type
			}
		}
	}
	return types
}

// resolveColumn resolves a column reference as Postgres does. An unqualified
// column is one of the innermost scope that has it. A qualified one, t.col,
// is a column of the item named t (see names) in the innermost scope that
// has one; if that item has no such column, it is an error, even if an
// outer t has one. An item with an alias is visible only by its alias: in a
// subquery `SELECT … FROM t x`, t.col is an enclosing query's column (#102).
// s.t.col names only a table or view t of schema s without an alias (#111).
func (e *executor) resolveColumn(c *ColumnRef) error {
	if c.written == "" {
		c.written = c.Name
	}
	c.Name = c.written
	depth, col, err := e.findColumn(c)
	if err != nil {
		return err
	}
	c.Name, c.Outer, c.OuterType = col.Name, depth, col.Type
	if depth > 0 {
		e.scopes[len(e.scopes)-1-depth].needs[col.Name] = true
		e.correlate(col.Name, depth)
	}
	return nil
}

// findColumn finds the column a reference names, as resolveColumn binds it,
// without binding it: depth is the number of scopes up from the innermost
// one, and col.Name the column's name there.
func (e *executor) findColumn(c *ColumnRef) (int, columnMeta, error) {
	ref := *c
	if ref.written != "" {
		ref.Name = ref.written
	}
	q := ref.qual()
	if err := q.checkCatalog(ref.Name); err != nil {
		return 0, columnMeta{}, err
	}
	top := len(e.scopes) - 1
	for depth := 0; depth <= top; depth++ {
		sc := e.scopes[top-depth]
		for _, u := range sc.unseen {
			if u.forbidden && (ref.Qualifier != "" && e.names(q, u.relation) || ref.Qualifier == "" && hasColumn(u.relation, ref.Name)) {
				return 0, columnMeta{}, errorf(adbc.StatusInvalidArgument,
					"invalid reference to FROM-clause entry for table %q; there is an entry for table %q, but it cannot be referenced from this part of the query",
					u.name, u.name)
			}
		}
		var match columnMeta
		var matchRel relation
		found, named := 0, 0
		for _, rel := range sc.rels {
			if ref.Qualifier != "" {
				if !e.names(q, rel) {
					continue
				}
				if named++; named > 1 {
					// Two tables t of different schemas.
					return 0, columnMeta{}, errorf(adbc.StatusInvalidArgument, "table reference %q is ambiguous", ref.Qualifier)
				}
			}
			if rel.meta.ambiguous(ref.Name) {
				return 0, columnMeta{}, errAmbiguousColumn(ref.Name)
			}
			col, ok := rel.meta.column(ref.Name)
			if !ok || (rel.prefix != "" && isSystemColumn(col.Name)) || (ref.Qualifier == "" && rel.hidden[col.Name]) {
				continue
			}
			found++
			if found == 1 {
				match, matchRel = col, rel
			}
		}
		if found > 1 {
			return 0, columnMeta{}, stateErrorf(stateAmbiguousColumn, "column reference %q is ambiguous; qualify it with a table alias", ref.Name)
		}
		if found == 1 {
			e.noteRead(matchRel.meta, match.Name)
			match.Name = matchRel.prefix + match.Name
			return depth, match, nil
		}
		if named > 0 {
			// The item visible as the qualifier here has no such column.
			return 0, columnMeta{}, noSuchColumn(&ref, sc, "")
		}
	}
	if ref.Qualifier != "" {
		return 0, columnMeta{}, e.missingFromEntry(q)
	}
	var sc *scope
	if top >= 0 {
		sc = e.scopes[top]
	}
	return 0, columnMeta{}, noSuchColumn(&ref, sc, e.lateralColumn(ref.Name))
}

// noteRead tells colReads (if set) that the statement reads a column of
// meta, if meta is a stored table and no view being planned reads it.
func (e *executor) noteRead(meta *tableMeta, column string) {
	c := e.cache
	if c == nil || c.colReads == nil || c.viewDepth > 0 || meta == nil ||
		meta.isMem || meta.join != nil || meta.view != nil || meta.lazy != nil {
		return
	}
	c.colReads(meta, column)
}

// correlate marks the subqueries whose bodies are the scopes from the
// innermost one up to the one depth levels up (not included) as reading its
// column name.
func (e *executor) correlate(name string, depth int) {
	top := len(e.scopes) - 1
	for k := 0; k < depth; k++ {
		sq := e.scopes[top-k].sq
		if sq == nil {
			continue
		}
		sq.correlated = true
		sq.outerUses++
		ref := outerRef{name: name, up: depth - k - 1}
		if !containsRef(sq.outerRefs, ref) {
			sq.outerRefs = append(sq.outerRefs, ref)
		}
	}
}

// qualName is the qualifier of a column reference or of rel.*: a FROM
// item's name, optionally with a schema and catalog. item is set for the
// planner's references (see ColumnRef.item).
type qualName struct{ catalog, schema, name, item string }

func (c *ColumnRef) qual() qualName {
	return qualName{catalog: c.Catalog, schema: c.Schema, name: c.Qualifier, item: c.item}
}

// String returns the qualifier as written.
func (q qualName) String() string {
	var parts []string
	for _, p := range []string{q.catalog, q.schema, q.name} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}

// checkCatalog is Postgres's error for a qualifier naming another catalog;
// last is the rest of the reference (its column, or "*").
func (q qualName) checkCatalog(last string) error {
	if q.catalog == "" || strings.EqualFold(q.catalog, catalogName) {
		return nil
	}
	return errorf(adbc.StatusNotImplemented, "cross-database references are not implemented: %s.%s", q, last)
}

// names reports whether a qualifier names rel, as Postgres's
// refnameNamespaceItem matches it: t names the item visible as t (by its
// alias if it has one), and s.t only a table or view t of schema s that has
// no alias.
func (e *executor) names(q qualName, rel relation) bool {
	if q.item != "" {
		return rel.prefix == q.item
	}
	if !strings.EqualFold(rel.name, q.name) {
		return false
	}
	return q.schema == "" || (!rel.aliased && e.inSchema(rel, q.schema))
}

// inSchema reports whether rel is a table or view of schema, where pg_temp
// is the connection's temporary schema. Unlike resolveTable, it doesn't mark
// the statement as using a temporary object: only a FROM item does that.
func (e *executor) inSchema(rel relation, schema string) bool {
	if isTempAlias(schema) {
		schema = e.store.tempSchema()
	}
	return rel.schema != "" && strings.EqualFold(rel.schema, schema)
}

// readsTable reports whether rel is the table or view q names, under any
// alias. Without a schema, that is any table, view or CTE of that name
// (Postgres: the table the search path finds, or the CTE).
func (e *executor) readsTable(q qualName, rel relation) bool {
	if rel.meta == nil || !strings.EqualFold(rel.meta.Name, q.name) {
		return false
	}
	return q.schema == "" || e.inSchema(rel, q.schema)
}

// noSuchColumn is the error for a column that the scope sc (nil for none)
// doesn't have, followed by detail if it isn't empty.
func noSuchColumn(c *ColumnRef, sc *scope, detail string) error {
	name := c.Name
	if c.Qualifier != "" {
		name = c.Qualifier + "." + c.Name
	}
	msg := fmt.Sprintf("column %q does not exist", name)
	if sc != nil && len(sc.rels) == 1 {
		msg = fmt.Sprintf("column %q does not exist in table %q", name, sc.rels[0].meta.Name)
	}
	if detail != "" {
		msg += "; " + detail
	}
	return stateErrorf(stateUndefinedColumn, "%s", msg)
}

// lateralColumn is Postgres's detail and hint (errorMissingColumn) for an
// unqualified column that no visible item has, when FROM items that only
// LATERAL would make visible have it (see lateralOnly); "" if none does.
func (e *executor) lateralColumn(name string) string {
	rels, hint := e.lateralOnly(func(rel relation) bool { return hasColumn(rel, name) })
	switch {
	case len(rels) == 0:
		return ""
	case len(rels) > 1:
		return fmt.Sprintf("there are columns named %q, but they are in tables that cannot be referenced from this part of the query; try using a table-qualified name", name)
	}
	detail := fmt.Sprintf("there is a column named %q in table %q, but it cannot be referenced from this part of the query", name, rels[0].name)
	if hint {
		detail += "; to reference that column, you must mark this subquery with LATERAL"
	}
	return detail
}

// hasColumn reports whether an unqualified reference to the column name
// would read rel's (see findColumn).
func hasColumn(rel relation, name string) bool {
	if rel.meta == nil {
		return false
	}
	col, ok := rel.meta.column(name)
	return ok && !(rel.prefix != "" && isSystemColumn(col.Name)) && !rel.hidden[col.Name] || rel.meta.ambiguous(name)
}

// fromColumns counts the FROM items of the query being bound (the innermost
// scope, not the queries around it) that an unqualified reference to the
// column name would read.
func (e *executor) fromColumns(name string) int {
	n := 0
	if top := len(e.scopes) - 1; top >= 0 {
		for _, rel := range e.scopes[top].rels {
			if hasColumn(rel, name) {
				n++
			}
		}
	}
	return n
}

// mentions reports whether q names rel, or the table rel reads (under an
// alias): the items Postgres's errorMissingRTE looks for.
func (e *executor) mentions(q qualName, rel relation) bool {
	return rel.name != "" && (strings.EqualFold(rel.name, q.name) || e.readsTable(q, rel))
}

// missingFromEntry is Postgres's error (errorMissingRTE) for a qualifier
// that names no FROM item visible here. If an item of some level, innermost
// first, reads the table it names or has its name, the reference is
// invalid: Postgres's hint names the item's alias when it is the table
// under an alias that is visible here, and otherwise its detail says the
// item can't be referenced from here (as for s2.t where s1.t is read).
// The items of a level that can't be read from it come first, as in its
// range table (see unseenRel): the detail names the item, and a hint says
// to add LATERAL if that would help (lateralHint). Else the entry is
// missing.
func (e *executor) missingFromEntry(q qualName) error {
	for i := len(e.scopes) - 1; i >= 0; i-- {
		for _, u := range e.scopes[i].unseen {
			if !e.mentions(q, u.relation) {
				continue
			}
			if u.forbidden {
				// Postgres looks for its alias, finds it, and fails there.
				return unreadableEntry(qualName{name: u.name}, u.name, false)
			}
			return unreadableEntry(q, u.name, e.lateralHint(u))
		}
		for j, rel := range e.scopes[i].rels {
			if !e.mentions(q, rel) {
				continue
			}
			if rel.aliased && !strings.EqualFold(rel.name, q.name) && e.visibleAt(i, j) {
				return errorf(adbc.StatusInvalidArgument,
					"invalid reference to FROM-clause entry for table %q; perhaps you meant to reference the table alias %q", q.name, rel.name)
			}
			return errorf(adbc.StatusInvalidArgument,
				"invalid reference to FROM-clause entry for table %q; there is an entry for table %q, but it cannot be referenced from this part of the query",
				q.name, rel.name)
		}
	}
	return errorf(adbc.StatusInvalidArgument, "missing FROM-clause entry for table %q", q.name)
}

// unreadableEntry is Postgres's error for a qualifier q that names an item
// that can't be referenced here, named entry, with the hint to add LATERAL
// if hint is set.
func unreadableEntry(q qualName, entry string, hint bool) error {
	msg := fmt.Sprintf("invalid reference to FROM-clause entry for table %q; there is an entry for table %q, but it cannot be referenced from this part of the query",
		q.name, entry)
	if hint {
		msg += "; to reference that table, you must mark this subquery with LATERAL"
	}
	return errorf(adbc.StatusInvalidArgument, "%s", msg)
}

// lateralEntry is Postgres's error for a qualifier that names no item in
// scope, when a FROM item that can't be read here is one that q mentions
// (see lateralOnly): its detail names the first, and its hint may say to
// add LATERAL. It is nil if there is no such item.
func (e *executor) lateralEntry(q qualName) error {
	rels, hint := e.lateralOnly(func(rel relation) bool { return e.mentions(q, rel) })
	if len(rels) == 0 {
		return nil
	}
	return unreadableEntry(q, rels[0].name, hint)
}

// lateralOnly returns the FROM items that can't be read here (see
// unseenRel) that match accepts, innermost level first: Postgres finds
// them, but they are not visible from here. hint reports whether
// Postgres's error for the first says to add LATERAL (see lateralHint).
func (e *executor) lateralOnly(match func(relation) bool) (rels []relation, hint bool) {
	for i := len(e.scopes) - 1; i >= 0; i-- {
		for _, u := range e.scopes[i].unseen {
			if !match(u.relation) {
				continue
			}
			if rels == nil {
				hint = e.lateralHint(u)
			}
			rels = append(rels, u.relation)
		}
	}
	return rels, hint
}

// lateralHint reports whether Postgres's error for a reference to u says
// to add LATERAL: not if LATERAL wouldn't make it readable (see
// unseenRel), or if the reference is in the arguments of a function in
// FROM, which are LATERAL already.
func (e *executor) lateralHint(u unseenRel) bool {
	return !u.noHint && (len(e.scopes) == 0 || !e.scopes[len(e.scopes)-1].funcArgs)
}

// visibleAt reports whether item j of scope i is the one its name refers to
// from the innermost scope.
func (e *executor) visibleAt(i, j int) bool {
	name := e.scopes[i].rels[j].name
	for k := len(e.scopes) - 1; k >= 0; k-- {
		hit := -1
		for n, rel := range e.scopes[k].rels {
			if strings.EqualFold(rel.name, name) {
				if hit >= 0 {
					return false // ambiguous
				}
				hit = n
			}
		}
		if hit >= 0 {
			return k == i && hit == j
		}
	}
	return false
}

func containsRef(refs []outerRef, r outerRef) bool {
	for _, x := range refs {
		if x == r {
			return true
		}
	}
	return false
}

// planSubquery plans the body of a subquery in a nested scope.
func (e *executor) planSubquery(ctx context.Context, sq *Subquery) error {
	width := 1 // of the operand
	if r, ok := sq.X.(*RowExpr); ok {
		if err := e.bindRowOperand(ctx, r); err != nil {
			return err
		}
		width = len(r.Items)
	} else if sq.X != nil {
		if err := e.bind(ctx, sq.X); err != nil {
			return err
		}
	}
	if sq.Kind == SubqueryExists && sq.Select.Limit == nil && len(sq.Select.GroupBy) == 0 && sq.Select.Having == nil {
		// Only existence matters.
		one := int64(1)
		sq.Select.Limit = &one
	}
	// Planned again (a shared node, a cached statement), it finds its outer
	// references again.
	sq.outerUses, sq.semi, sq.outerRefs, sq.correlated = 0, nil, nil, false
	e.pendingSq = sq
	plan, err := e.planSelect(ctx, sq.Select, e.paramTypes)
	e.pendingSq = nil
	if err != nil {
		return err
	}
	switch {
	case sq.rowCompare:
		// (a, b) = (SELECT x, y …)
		if err := subqueryWidthError(len(plan.items), sq.Width); err != nil {
			return err
		}
	case sq.Width > 0:
		if len(plan.items) != sq.Width {
			return errorf(adbc.StatusInvalidArgument, "number of columns does not match number of values")
		}
	case sq.Kind == SubqueryIn || sq.Kind == SubqueryAny || sq.Kind == SubqueryAll:
		if err := subqueryWidthError(len(plan.items), width); err != nil {
			return err
		}
		if err := e.recordOperandError(sq, plan); err != nil {
			return err
		}
	case sq.Kind != SubqueryExists && len(plan.items) != 1:
		return stateErrorf(stateSyntax, "subquery must return only one column") // as in Postgres
	}
	sq.plan = plan
	e.planSemiJoin(ctx, sq)
	return nil
}

// ---- in-memory relations (CTEs, derived tables) ----

// memTable builds a relation from query results. Its columns may share a
// name (see nameColumns).
func memTable(name string, cols []resultColumn, rows [][]Value, rename []string) (*tableMeta, error) {
	if rename != nil && len(rename) != len(cols) {
		return nil, errorf(adbc.StatusInvalidArgument, "%q has %d columns but %d column names were given", name, len(cols), len(rename))
	}
	meta := &tableMeta{Name: name, isMem: true}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
		if rename != nil {
			names[i] = rename[i]
		}
		meta.Columns = append(meta.Columns, columnMeta{Type: c.Type, Nullable: true})
	}
	meta.nameColumns(names)
	meta.mem = make([]map[string]Value, len(rows))
	for r, row := range rows {
		m := make(map[string]Value, len(row))
		for i, v := range row {
			m[meta.Columns[i].Name] = v
		}
		meta.mem[r] = m
	}
	return meta, nil
}

// nameColumns names the columns of an in-memory relation. As in Postgres,
// they may share a name (SELECT 1, 2 names both ?column?), which is fine
// until a reference reads one (SELECT "?column?" … is ambiguous). A column
// whose name another one has too is output by that name (its label), and
// held in the rows under a name of its own, which SQL can't write.
func (t *tableMeta) nameColumns(names []string) {
	count := map[string]int{}
	for _, n := range names {
		count[strings.ToLower(n)]++
	}
	t.dups = nil
	for i, n := range names {
		c := &t.Columns[i]
		c.Name, c.label = n, ""
		if count[strings.ToLower(n)] > 1 {
			c.Name, c.label = fmt.Sprintf("%s\x00%d", n, i+1), n
			if t.dups == nil {
				t.dups = map[string]bool{}
			}
			t.dups[strings.ToLower(n)] = true
		}
	}
}

// columnLabel is the name a column of an in-memory relation is held under
// in its rows as errors name it: without the position nameColumns adds to
// a name that other columns have too.
func columnLabel(name string) string {
	if i := strings.LastIndexByte(name, 0); i > 0 && i < len(name)-1 && strings.Trim(name[i+1:], "0123456789") == "" {
		return name[:i]
	}
	return name
}

// ambiguous reports whether more than one column of an in-memory relation
// has name (see nameColumns).
func (t *tableMeta) ambiguous(name string) bool { return t.dups[strings.ToLower(name)] }

// errAmbiguousColumn is Postgres's error for a reference to a column name
// that several columns have.
func errAmbiguousColumn(name string) error {
	return stateErrorf(stateAmbiguousColumn, "column reference %q is ambiguous", name)
}

// lazyRelation is a derived table or WITH query that reads an enclosing
// query: its rows are computed when the query that reads it is scanned
// (see lazyRows), for the enclosing rows it is scanned for. sq is its
// query, planned as a correlated subquery whose body is that of the derived
// table or WITH query (the queries around it are its enclosing scopes), so
// that it runs once per distinct set of outer values (see subqueryRows). up
// is the number of levels between the query that reads it and the level
// whose FROM or WITH list it is in, where sq is evaluated: 0 for a derived
// table. scopes are the scopes around that level, as it was planned.
type lazyRelation struct {
	sq     *Subquery
	up     int
	scopes []*scope
}

// materialize plans the query of a derived table or WITH query, as level's
// FROM or WITH list holds it (see fromQuery): it is run once, unless it
// reads an enclosing query, when its relation is computed as its reader is
// scanned (see lazyRelation). While only planning, it plans the query and
// returns an empty relation with its columns (see empty.go).
func (e *executor) materialize(ctx context.Context, key any, name string, sel *SelectStmt, rename []string, level *scope) (*tableMeta, error) {
	if e.cache.materializing[key] {
		if def, ok := key.(*CTE); ok && def.Recursive {
			return nil, errorf(adbc.StatusNotImplemented, "mutual recursion between WITH items is not implemented")
		}
		return nil, errorf(adbc.StatusInvalidArgument, "%q refers to itself; recursive references need WITH RECURSIVE", name)
	}
	e.cache.materializing[key] = true
	defer delete(e.cache.materializing, key)
	sq := &Subquery{Select: sel, Kind: SubqueryLateral}
	restore := e.fromQuery(level, sq)
	plan, err := e.planSelect(ctx, sel, e.paramTypes)
	restore()
	if err != nil {
		return nil, err
	}
	if sq.correlated {
		sq.plan = plan
		meta, err := memTable(name, plan.columns(), nil, rename)
		if err == nil {
			meta.lazy = &lazyRelation{sq: sq, scopes: slices.Clone(e.scopes)}
		}
		return meta, err
	}
	if e.planOnly {
		return memTable(name, plan.columns(), nil, rename)
	}
	rows, err := e.runSelect(ctx, plan, e.params)
	if err != nil {
		return nil, err
	}
	return memTable(name, plan.columns(), rows, rename)
}

// fromQuery prepares the planning of the query of a derived table or WITH
// query as the body of sq: it sees the queries around level, the level
// whose FROM or WITH list holds it, which is pushed as a scope without
// items (it may only read level's unseen items, for errors). The returned
// function restores the scopes.
func (e *executor) fromQuery(level *scope, sq *Subquery) func() {
	scopes, pending := e.scopes, e.pendingSq
	e.scopes = append(slices.Clip(scopes), level)
	e.pendingSq = sq
	return func() { e.scopes, e.pendingSq = scopes, pending }
}

// pushCTEs makes a statement's WITH list visible while it is planned; the
// returned function removes it again.
func (e *executor) pushCTEs(with []CTE) (func(), error) {
	if len(with) == 0 {
		return func() {}, nil
	}
	defs := map[string]*CTE{}
	for i := range with {
		key := strings.ToLower(with[i].Name)
		if _, dup := defs[key]; dup {
			return nil, errorf(adbc.StatusInvalidArgument, "WITH query name %q specified more than once", with[i].Name)
		}
		defs[key] = &with[i]
	}
	e.ctes = append(e.ctes, defs)
	e.cteLevel = append(e.cteLevel, len(e.scopes))
	return func() {
		e.ctes, e.cteLevel = e.ctes[:len(e.ctes)-1], e.cteLevel[:len(e.cteLevel)-1]
	}, nil
}

// lookupCTE finds a CTE visible from the current query, and the index of
// its WITH list in e.ctes.
func (e *executor) lookupCTE(name string) (*CTE, int) {
	for i := len(e.ctes) - 1; i >= 0; i-- {
		if def, ok := e.ctes[i][strings.ToLower(name)]; ok {
			return def, i
		}
	}
	return nil, -1
}

// cteRelation is the relation of a WITH query of the list e.ctes[list]
// read here. Its query is planned as Postgres plans it, where its WITH list
// is: it sees the queries around the list's query and the WITH lists up to
// its own, not the list's query's FROM items or anything nested in it. One
// that reads an enclosing query is computed for the rows of the list's
// level, from wherever it is read (see lazyRelation), and every query from
// here up to that level reads what it reads.
func (e *executor) cteRelation(ctx context.Context, def *CTE, list int) (*tableMeta, error) {
	// Fewer scopes when a query is planned without its enclosing ones
	// (semijoin.go): a CTE that reads them fails to plan there.
	level := min(e.cteLevel[list], len(e.scopes))
	m, ok := e.cache.ctes[def]
	if ok && m.lazy != nil && !slices.Equal(m.lazy.scopes, e.scopes[:level]) {
		ok = false
	}
	if !ok {
		var err error
		if m, err = e.planCTE(ctx, def, list, level); err != nil {
			return nil, err
		}
		if !e.planOnly {
			e.cache.ctes[def] = m
		}
	}
	if m.lazy == nil {
		return m, nil
	}
	lz := *m.lazy
	lz.up = len(e.scopes) - level
	// The level of the query reading it, whose FROM item it is.
	e.scopes = append(e.scopes, &scope{sq: e.pendingSq, needs: map[string]bool{}})
	for _, ref := range lz.sq.outerRefs {
		e.correlate(ref.name, lz.up+ref.up)
	}
	e.popScope()
	out := *m
	out.lazy = &lz
	return &out, nil
}

// planCTE materializes a WITH query of the list e.ctes[list], planned with
// only the first level scopes (those around its list's query) and the WITH
// lists up to its own.
func (e *executor) planCTE(ctx context.Context, def *CTE, list, level int) (*tableMeta, error) {
	scopes, ctes, levels := e.scopes, e.ctes, e.cteLevel
	e.scopes = slices.Clip(scopes[:level])
	e.ctes, e.cteLevel = slices.Clip(ctes[:list+1]), slices.Clip(levels[:list+1])
	defer func() { e.scopes, e.ctes, e.cteLevel = scopes, ctes, levels }()
	return e.materializeCTE(ctx, def)
}

// fromRelation resolves the FROM clause of a SELECT (without joins) to a
// relation.
func (e *executor) fromRelation(ctx context.Context, sel *SelectStmt) (relation, error) {
	return e.resolveFromItem(ctx, sel.From, sel.FromSelect, sel.FromAlias, nil)
}

// resolveFromItem resolves one FROM item: a table, a view, a CTE, or a
// derived table (which can't read unseen, the items of its level, see
// unseenRel). The relation is named by the alias, or else by the item's
// name; meta is nil if there is no item.
func (e *executor) resolveFromItem(ctx context.Context, table *TableName, sub *SelectStmt, alias string, unseen []unseenRel) (relation, error) {
	rel := relation{name: alias, aliased: alias != ""}
	switch {
	case sub != nil:
		if m, ok := e.cache.derived[sub]; ok {
			rel.meta = m
			return rel, nil
		}
		// The level of the query whose FROM item it is (the pending
		// subquery's, whose scope is pushed later), as for LATERAL.
		level := &scope{sq: e.pendingSq, needs: map[string]bool{}, unseen: unseen, aggClause: "FROM clause of their own query level"}
		m, err := e.materialize(ctx, sub, alias, sub, nil, level)
		if err != nil {
			return relation{}, err
		}
		if !e.planOnly && m.lazy == nil {
			// An empty stand-in isn't kept: another reference may read
			// it. Nor is a lazy relation: it was planned for the scopes
			// here.
			e.cache.derived[sub] = m
		}
		rel.meta = m
		return rel, nil
	case table != nil:
		if alias == "" {
			rel.name = table.Name
		}
		if isInfoSchema(table.Schema) && (table.Catalog == "" || table.Catalog == catalogName) {
			meta, err := e.infoSchemaTable(ctx, table.Name)
			rel.meta, rel.schema = meta, infoSchema
			return rel, err
		}
		if table.Schema == "" && table.Catalog == "" {
			if def, list := e.lookupCTE(table.Name); def != nil {
				if m, ok := e.cache.working[def]; ok {
					// The recursive reference of a recursive CTE.
					rel.meta = m
					return rel, nil
				}
				m, err := e.cteRelation(ctx, def, list)
				if err != nil {
					return relation{}, err
				}
				rel.meta = m
				return rel, nil
			}
		}
		meta, err := e.loadTable(ctx, *table)
		var ae adbc.Error
		if err != nil && asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
			// Not a table: maybe a view.
			schema, name, rerr := e.resolveTable(*table)
			if rerr != nil {
				return relation{}, err
			}
			v, verr := e.store.getView(ctx, schema, name)
			if verr != nil {
				return relation{}, err
			}
			meta, err = e.viewRelation(ctx, v, rel.name)
		}
		if err != nil {
			return relation{}, err
		}
		rel.meta, rel.schema = meta, meta.Schema
		return rel, nil
	}
	return relation{}, nil
}

// ---- subquery evaluation ----

// subqueryRows returns the rows of a subquery evaluated in env.
func (e *executor) subqueryRows(ctx context.Context, sq *Subquery, env *evalEnv) ([][]Value, error) {
	if sq.plan == nil {
		return nil, errorf(adbc.StatusInternal, "subquery was not planned")
	}
	e.ensureCache()
	key := ""
	if sq.correlated {
		var b strings.Builder
		for _, ref := range sq.outerRefs {
			v, err := env.lookupUp(ref.name, ref.up)
			if err != nil {
				return nil, err
			}
			if v.Null {
				b.WriteString("N\x00")
			} else {
				fmt.Fprintf(&b, "V%s\x00", v.Text())
			}
		}
		key = b.String()
		if rows, ok := e.cache.memo[sq][key]; ok {
			return rows, nil
		}
	} else if rows, ok := e.cache.sub[sq]; ok {
		return rows, nil
	}
	child := *e
	child.scopes, child.pendingSq = nil, nil
	if sq.correlated {
		child.outer = env
	}
	subqueryStats.runs.Add(1)
	rows, err := child.runSelect(ctx, sq.plan, e.params)
	if err != nil {
		return nil, err
	}
	if sq.correlated {
		if e.cache.memo[sq] == nil {
			e.cache.memo[sq] = map[string][][]Value{}
		}
		e.cache.memo[sq][key] = rows
	} else {
		e.cache.sub[sq] = rows
	}
	return rows, nil
}

// lazyRows computes the rows of a lazy relation (see lazyRelation), with
// meta's column names: its query's rows for the enclosing rows the
// executor runs for.
func (e *executor) lazyRows(ctx context.Context, meta *tableMeta, params []Value) ([]map[string]Value, error) {
	lz := meta.lazy
	env := e.newEnv(ctx, nil, params)
	for i := 0; i < lz.up; i++ {
		if env.outer == nil {
			return nil, errorf(adbc.StatusInternal, "%q has no enclosing row", meta.Name)
		}
		env = env.outer
	}
	rows, err := e.subqueryRows(ctx, lz.sq, env)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]Value, len(rows))
	for r, row := range rows {
		m := make(map[string]Value, len(row))
		for i, c := range meta.Columns {
			m[c.Name] = row[i]
		}
		out[r] = m
	}
	return out, nil
}

// lookupUp reads a column of the row `up` environments above env.
func (env *evalEnv) lookupUp(name string, up int) (Value, error) {
	cur := env
	for i := 0; i < up; i++ {
		if cur.outer == nil {
			return Value{}, fmt.Errorf("outer reference %q has no enclosing row", name)
		}
		cur = cur.outer
	}
	if v, ok := cur.row[name]; ok {
		return v, nil
	}
	if t, ok := cur.types[name]; ok {
		return nullValue(t), nil
	}
	return Value{}, fmt.Errorf("unknown outer column %q", name)
}

func (env *evalEnv) evalSubquery(sq *Subquery) (Value, error) {
	if env.exec == nil {
		return Value{}, fmt.Errorf("subqueries are not supported here")
	}
	if sq.semi != nil {
		if v, ok, err := env.exec.evalSemiJoin(env, sq); ok || err != nil {
			return v, err
		}
	}
	rows, err := env.exec.subqueryRows(env.ctx, sq, env)
	if err != nil {
		return Value{}, err
	}
	switch sq.Kind {
	case SubqueryExists:
		return boolValue(len(rows) > 0), nil
	case SubqueryScalar:
		switch len(rows) {
		case 0:
			return nullValue(sq.plan.items[0].typ), nil
		case 1:
			return rows[0][0], nil
		}
		return Value{}, fmt.Errorf("more than one row returned by a subquery used as an expression")
	case SubqueryAny, SubqueryAll:
		if r, ok := sq.X.(*RowExpr); ok {
			return env.evalRowQuantified(sq, r, rows)
		}
		return env.evalQuantified(sq, rows)
	}
	// IN
	if r, ok := sq.X.(*RowExpr); ok {
		return env.evalRowIn(sq, r, rows)
	}
	x, err := env.eval(sq.X)
	if err != nil {
		return Value{}, err
	}
	set := &inSet{rows: rows, noIndex: true, zone: env.zone()}
	if !sq.correlated {
		// The same rows for every outer row: probe a hash set.
		set = env.exec.cachedInSet(sq, rows)
	}
	return set.eval(x, sq.Not), nil
}

// evalRowColumn evaluates one column of a subquery assigned to a column
// list. Its rows are cached (or memoised per outer row), so the subquery
// runs once for all the columns. No row sets the columns to NULL; more than
// one is an error, as in Postgres.
func (env *evalEnv) evalRowColumn(rc *RowColumn) (Value, error) {
	if env.exec == nil {
		return Value{}, fmt.Errorf("subqueries are not supported here")
	}
	rows, err := env.exec.subqueryRows(env.ctx, rc.Sub, env)
	if err != nil {
		return Value{}, err
	}
	switch len(rows) {
	case 0:
		return nullValue(rc.Sub.plan.items[rc.Index].typ), nil
	case 1:
		return rows[0][rc.Index], nil
	}
	return Value{}, fmt.Errorf("more than one row returned by a subquery used as an expression")
}
