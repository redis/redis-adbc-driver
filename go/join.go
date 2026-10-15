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

// Joins.
//
// RediSearch cannot join, so joins run in the driver, with as much work as
// possible pushed into each table's own index query:
//
//   - A WHERE predicate on one table is pushed into that table's scan, unless
//     the table is on the NULL-supplying side of an outer join (pushing it
//     below the join would change the result).
//   - An ON predicate on the joined-in table of an INNER or LEFT join is
//     pushed into that table's scan.
//   - WHERE predicates linking several inner-joined tables become join
//     conditions, so `FROM a, b WHERE a.x = b.y` is a hash join, not a cross
//     product.
//   - Equality conditions drive a hash join. When the joined-in table's key
//     column is indexed and the rows joined so far have at most
//     maxUnionTerms distinct keys, only the matching rows are fetched, with an
//     index union query (an index lookup join).
//
// Joined rows key their columns as "alias.column". The joined relation is an
// in-memory relation, so WHERE, GROUP BY, HAVING, ORDER BY and LIMIT then run
// through the regular driver-side paths.

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

type joinItem struct {
	alias string // the name it is visible as (see relation)
	// aliased and schema are as in relation.
	aliased  bool
	schema   string
	base     *tableMeta // table, CTE or derived table
	prefix   string     // alias + "." (see add)
	kind     string     // "" for the first item; INNER, LEFT, RIGHT, FULL, CROSS
	on       Expr       // bound join condition (may be nil)
	nullable bool       // on the NULL-supplying side of an outer join

	pushed []Expr // predicates on this table alone, applied in its scan
	extra  []Expr // WHERE predicates attached as join conditions here

	// tag, if set, names a value added to each of this item's rows: the
	// row's HASH key for a table, TRUE for an in-memory relation. MERGE,
	// UPDATE … FROM and DELETE … USING use it to find the target row of a
	// joined row, and to tell which side of an outer join a row came from.
	tag string

	// lateral is set for a LATERAL subquery or a table function, whose rows
	// are computed when the join runs (see lateral.go).
	lateral *lateralItem
	// natural is NATURAL JOIN; usingOn is then its condition over the
	// common columns (see planNatural).
	natural bool
	using   []string
	usingOn Expr
	// comma is set for an item after a comma, which starts a new FROM-list
	// item (see JoinClause.Comma): the items from the last one on are the
	// left side of a join (see start).
	comma bool
	// visible are the row keys of the columns an unqualified reference in
	// the item's join condition may read, when NATURAL JOINs before it
	// merge columns (see planNatural); nil if they don't.
	visible map[string]bool
	// isolated is set for the items of a FROM-list item after the first one
	// that has a RIGHT or FULL join: they are joined on their own, and then
	// to the rows of the items before them (see runJoin).
	isolated bool
}

type joinPlan struct {
	items []*joinItem
	// star is the column order of SELECT * when there is a NATURAL JOIN,
	// and merged are the columns it merges with COALESCE (FULL joins).
	star   []columnMeta
	merged []mergedColumn
	// err is an error found by finish, reported by bindJoin.
	err error
	// unseen is the target table of UPDATE, DELETE or MERGE, which its
	// FROM items can't read (see unseenRel).
	unseen []unseenRel
}

// planJoin resolves the FROM items of a query with joins. It returns the
// joined relation (column names "alias.column") and the scope relations.
func (e *executor) planJoin(ctx context.Context, sel *SelectStmt) (*tableMeta, []relation, *joinPlan, error) {
	jp := &joinPlan{}
	first := JoinClause{Table: sel.From, Select: sel.FromSelect, Func: sel.FromFunc, Lateral: sel.FromLateral,
		Alias: sel.FromAlias, Columns: sel.FromColumns}
	items := append([]JoinClause{first}, sel.Joins...)
	// Items that can't change the result, given the ON conditions that are
	// never true, are only planned (see joinReads).
	kinds := make([]string, len(items))
	never := make([]bool, len(items))
	for i, jc := range sel.Joins {
		kinds[i+1], never[i+1] = readsKind(jc.Kind, jc.Comma), e.neverTrue(ctx, jc.On)
	}
	read, _ := joinReads(kinds, never, make([]bool, len(items)))
	for i, jc := range items {
		restore := e.planningOnly(!read[i])
		err := e.addFromItem(ctx, jp, jc)
		restore()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	joined, rels := jp.finish()
	return joined, rels, jp, nil
}

// addJoinItem resolves a FROM item (table, view, CTE or derived table) and
// adds it to the join.
func (e *executor) addJoinItem(ctx context.Context, jp *joinPlan, kind string, table *TableName, sub *SelectStmt, alias string) error {
	rel, err := e.resolveFromItem(ctx, table, sub, alias, jp.unseenBy(kind))
	if err != nil {
		return err
	}
	if rel.meta.view != nil {
		// Joins read each item fully; compute the view now (unless only
		// planning).
		var rows []map[string]Value
		if !e.planOnly {
			if rows, err = e.runView(ctx, rel.meta.view, nil, nil, e.params); err != nil {
				return err
			}
		}
		m := *rel.meta
		m.view, m.mem = nil, rows
		rel.meta = &m
	}
	if rel.name == "" {
		return errorf(adbc.StatusInvalidArgument, "a subquery in FROM must have an alias")
	}
	_, err = jp.add(kind, rel)
	return err
}

// add appends a resolved relation to the join. As in Postgres
// (checkNameSpaceConflicts), no two items may have the same name, except
// two tables or views without aliases from different schemas (s1.t and
// s2.t). Their row keys are kept apart: the second one's prefix is
// "t\x00<n>.", which can't start or be started by another item's
// ("alias.", or mergedPrefix's).
func (jp *joinPlan) add(kind string, rel relation) (*joinItem, error) {
	prefix := rel.name + "."
	for _, it := range jp.items {
		if !(it.alias == rel.name) {
			continue
		}
		if it.aliased || rel.aliased || it.schema == "" || rel.schema == "" || (it.schema == rel.schema) {
			return nil, errorf(adbc.StatusInvalidArgument, "table name %q specified more than once; use aliases", rel.name)
		}
		prefix = rel.name + "\x00" + strconv.Itoa(len(jp.items)) + "."
	}
	it := &joinItem{alias: rel.name, aliased: rel.aliased, schema: rel.schema, base: rel.meta, prefix: prefix, kind: kind}
	jp.items = append(jp.items, it)
	return it, nil
}

// rel is the item's relation in the query's scope.
func (it *joinItem) rel() relation {
	return relation{name: it.alias, aliased: it.aliased, schema: it.schema, meta: it.base, prefix: it.prefix}
}

// unseenBy returns what a derived table joined as kind can't read of its
// level (see unseenRel): the target of the DML statement, and the items
// before it, which LATERAL would make readable, except the left side of a
// RIGHT or FULL join (the items since the last comma). As in Postgres, a
// LATERAL item there may read the FROM-list items before that.
func (jp *joinPlan) unseenBy(kind string) []unseenRel {
	var out []unseenRel
	for _, u := range jp.unseen {
		u.forbidden = false // only a LATERAL item finds it
		out = append(out, u)
	}
	left := len(jp.items)
	if left > 0 && (kind == "RIGHT" || kind == "FULL") {
		left = jp.start(left - 1)
	}
	for i, it := range jp.items {
		out = append(out, unseenRel{relation: it.rel(), noHint: i >= left})
	}
	return out
}

// start returns the index of the first item of the FROM-list item that
// item i is part of: the item after the last comma up to i (see
// JoinClause.Comma).
func (jp *joinPlan) start(i int) int {
	for i > 0 && !jp.items[i].comma {
		i--
	}
	return i
}

// rels are the relations of the items added so far.
func (jp *joinPlan) rels() []relation {
	rels := make([]relation, len(jp.items))
	for i, it := range jp.items {
		rels[i] = it.rel()
	}
	return rels
}

// ref is a reference to the item's column col, bound to the item itself
// (another item may have its name).
func (it *joinItem) ref(col string) *ColumnRef {
	return &ColumnRef{Qualifier: it.alias, Name: col, item: it.prefix}
}

// keyLabel is a joined row's key as errors name it: alias.column, without
// the number in a second item's prefix (see add), or in the column's name
// (see columnLabel).
func keyLabel(key string) string {
	if i := strings.IndexByte(key, 0); i > 0 {
		if j := strings.IndexByte(key[i:], '.'); j > 0 {
			key = key[:i] + key[i+j:]
		}
	}
	return columnLabel(key)
}

// finish works out which items are NULL-supplying and returns the joined
// relation and the scope relations.
func (jp *joinPlan) finish() (*tableMeta, []relation) {
	// Nullability: LEFT makes the new item nullable, RIGHT the items before
	// it in its FROM-list item (its left side), FULL both.
	for i, it := range jp.items {
		left := jp.items[jp.start(i):i]
		switch it.kind {
		case "LEFT":
			it.nullable = true
		case "RIGHT":
			for _, prev := range left {
				prev.nullable = true
			}
		case "FULL":
			it.nullable = true
			for _, prev := range left {
				prev.nullable = true
			}
		}
	}
	// The FROM-list items after the first that have a RIGHT or FULL join.
	for i, it := range jp.items {
		if start := jp.start(i); start > 0 && (it.kind == "RIGHT" || it.kind == "FULL") {
			for j := start; j < len(jp.items) && (j == start || !jp.items[j].comma); j++ {
				jp.items[j].isolated = true
			}
		}
	}
	jp.checkIsolated()
	joined := &tableMeta{Name: "join", isMem: true, join: jp}
	rels := make([]relation, len(jp.items))
	for i, it := range jp.items {
		for _, c := range it.base.Columns {
			joined.Columns = append(joined.Columns, columnMeta{
				Name: it.prefix + c.Name, label: c.outputName(), Type: c.Type, Nullable: true,
			})
		}
		rels[i] = it.rel()
	}
	rels = jp.planNatural(joined, rels)
	return joined, rels
}

// checkIsolated leaves in jp.err the error for a LATERAL item or FROM
// function of an isolated FROM-list item (see joinItem.isolated) that reads
// the items before it, which the driver joins to it afterwards.
func (jp *joinPlan) checkIsolated() {
	for i, it := range jp.items {
		if !it.isolated || it.lateral == nil || jp.err != nil {
			continue
		}
		for _, prev := range jp.items[:jp.start(i)] {
			for c := range it.lateral.need {
				if strings.HasPrefix(c, prev.prefix) && jp.err == nil {
					jp.err = errorf(adbc.StatusNotImplemented,
						"a LATERAL reference to %q from a later FROM-list item with a RIGHT or FULL JOIN is not implemented", prev.alias)
				}
			}
		}
	}
}

// bindJoin binds the ON / USING conditions (called with the join scope
// pushed). As in Postgres (transformJoinOnClause), a join's condition reads
// only its two sides: the items of its FROM-list item up to it (see
// JoinClause.Comma), with the columns merged by the NATURAL JOINs among
// them (see onScope). It is bound in a copy of the join scope that holds
// just those, which shares its needs and subquery. The items of the
// FROM-list items before it are in Postgres's range table, so its errors
// find them (as unseen items, after the scope's own, see unseenRel); the
// items after it aren't there yet.
func (e *executor) bindJoin(ctx context.Context, sel *SelectStmt, jp *joinPlan) error {
	if jp.err != nil {
		return jp.err
	}
	for _, m := range jp.merged {
		if err := e.bind(ctx, m.expr); err != nil {
			return err
		}
	}
	top := len(e.scopes) - 1
	sc := e.scopes[top]
	defer func() { e.scopes[top] = sc }()
	for i, jc := range sel.Joins {
		k := i + 1
		it := jp.items[k]
		start := jp.start(k)
		cond := jc.On
		if jc.Natural || len(jc.Using) > 0 {
			cond = it.usingOn
		}

		if cond == nil {
			it.on = nil
			continue
		}
		on := *sc
		on.rels, on.unseen = jp.onScope(k, sc.rels), slices.Clone(sc.unseen)
		for _, prev := range jp.items[:start] {
			on.unseen = append(on.unseen, unseenRel{relation: prev.rel(), noHint: true})
		}
		e.scopes[top] = &on
		if err := e.bindClause(ctx, cond, "JOIN conditions"); err != nil {
			return err
		}
		if containsWindow(cond) {
			return errorf(adbc.StatusInvalidArgument, "window functions are not allowed in JOIN conditions")
		}
		it.on = cond
	}
	return nil
}

// onScope returns the relations of rels (the query's scope) that item k's
// join condition reads: those of the items of its FROM-list item up to k,
// and of the columns merged by the NATURAL FULL JOINs among them. The
// columns merged into another one by then are hidden from unqualified
// references (see joinItem.visible).
func (jp *joinPlan) onScope(k int, rels []relation) []relation {
	start := jp.start(k)
	var out []relation
	for _, rel := range rels {
		in := false
		for j := start; j <= k; j++ {
			in = in || rel.prefix == jp.items[j].prefix ||
				j < k && rel.prefix == fmt.Sprintf("%s%d.", mergedPrefix, j)
		}
		if !in {
			continue
		}
		rel.hidden = nil
		if visible := jp.items[k].visible; visible != nil {
			for _, c := range rel.meta.Columns {
				if !visible[rel.prefix+c.Name] {
					if rel.hidden == nil {
						rel.hidden = map[string]bool{}
					}
					rel.hidden[c.Name] = true
				}
			}
		}
		out = append(out, rel)
	}
	return out
}

// itemsOf returns the indexes of the join items an expression reads, and
// whether it can be moved (it has no correlated subquery, whose references
// would be invisible here, and no volatile function, which would be
// evaluated again on the joined rows).
func (jp *joinPlan) itemsOf(expr Expr) ([]int, bool) {
	movable := true
	refs := map[string]bool{}
	walkExpr(expr, func(x Expr) {
		switch v := x.(type) {
		case *ColumnRef:
			if v.Outer == 0 {
				refs[v.Name] = true
			}
		case *Subquery:
			if v.correlated {
				movable = false
			}
		case *Func:
			if volatileFuncs[v.Name] {
				movable = false
			}
		}
	})
	// A column merged by a NATURAL FULL JOIN reads the columns it merges.
	for _, m := range jp.merged {
		if refs[m.key] {
			columnRefs(m.expr, refs)
		}
	}
	var out []int
	for i, it := range jp.items {
		for name := range refs {
			if strings.HasPrefix(name, it.prefix) {
				out = append(out, i)
				break
			}
		}
	}
	return out, movable
}

// planPushdown distributes WHERE and ON predicates (see the file comment).
// WHERE is still evaluated in full on the joined rows, so a pushed predicate
// only has to be exact, not complete.
func (jp *joinPlan) planPushdown(where Expr) {
	for _, it := range jp.items {
		it.pushed, it.extra = nil, nil
	}
	for i, it := range jp.items {
		if it.on == nil || !(it.kind == "INNER" || it.kind == "LEFT") {
			continue
		}
		for _, c := range conjuncts(it.on, nil) {
			if idx, ok := jp.itemsOf(c); ok && len(idx) == 1 && idx[0] == i {
				it.pushed = append(it.pushed, c)
			}
		}
	}
	if where == nil {
		return
	}
	for _, c := range conjuncts(where, nil) {
		idx, ok := jp.itemsOf(c)
		if !ok || len(idx) == 0 {
			continue
		}
		nullable := false
		for _, i := range idx {
			nullable = nullable || jp.items[i].nullable
		}
		if nullable {
			continue
		}
		if len(idx) == 1 {
			jp.items[idx[0]].pushed = append(jp.items[idx[0]].pushed, c)
		} else if last := idx[len(idx)-1]; !jp.items[last].isolated || idx[0] >= jp.start(last) {
			// Not if it reads the items before an isolated FROM-list item,
			// which aren't in its rows when it is joined (see runJoin).
			jp.items[last].extra = append(jp.items[last].extra, c)
		}
	}
}

// view returns the relation's columns renamed alias.column (reading the
// original HASH fields), for scanning it as part of a join.
func (it *joinItem) view() *tableMeta {
	v := *it.base
	v.Columns = make([]columnMeta, len(it.base.Columns))
	for i, c := range it.base.Columns {
		c.Field = c.field()
		c.Name = it.prefix + c.Name
		v.Columns[i] = c
	}
	if it.base.isMem {
		v.mem = make([]map[string]Value, len(it.base.mem))
		for r, row := range it.base.mem {
			m := make(map[string]Value, len(row))
			for k, val := range row {
				m[it.prefix+k] = val
			}
			v.mem[r] = m
		}
	}
	return &v
}

// joinKey normalizes a value for hash-join equality: values that compare
// equal (e.g. 1, 1.0 and 1.00) get the same key, and so do NaNs, which are
// equal to each other as in Postgres. NULLs never match.
func joinKey(v Value) (string, bool) {
	if v.Null {
		return "", false
	}
	switch k := v.T.Kind; {
	case k.isInteger() || k == KindBool:
		return "n" + strconv.FormatInt(v.I, 10), true
	case k == KindDecimal:
		s := formatDecimal(v.D, v.T.Scale)
		if strings.Contains(s, ".") {
			s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		}
		if s == "-0" {
			s = "0"
		}
		return "n" + s, true
	case k.isFloat():
		if v.F == math.Trunc(v.F) && math.Abs(v.F) < 1e15 {
			return "n" + strconv.FormatInt(int64(v.F), 10), true
		}
		if math.IsNaN(v.F) {
			return "nNaN", true
		}
		// Shortest decimal form, matching how NUMERIC values render.
		return "n" + strconv.FormatFloat(v.F, 'f', -1, 64), true
	case k == KindString || k == KindBinary:
		if v.T.Fixed {
			return "s" + trimPadding(v.S), true // see lengths.go
		}
		return "s" + v.S, true
	case k == KindInterval:
		// Equal lengths are equal intervals ('1 day' = '24 hours').
		return "i" + intervalTotal(v).String(), true
	case k == KindTimeTZ:
		return timeTZKey(v), true
	case k == KindTimestamp || k == KindTime:
		// Seconds plus nanoseconds, so different units compare equal.
		per := unitsPerSecond[v.T.Unit]
		sec := floorDiv(v.I, per)
		frac := (v.I - sec*per) * (1_000_000_000 / per)
		return k.String()[:2] + strconv.FormatInt(sec, 10) + "." + strconv.FormatInt(frac, 10), true
	case k == KindRecord:
		return recordJoinKey(v), true
	case k == KindArray:
		return "a" + rowKey(v.fields()), true
	}
	return v.T.Kind.String() + v.Text(), true
}

type equiPair struct {
	left, right Expr
}

// joinStep is one step of a join: scan item and combine it with the rows
// joined so far using conds.
type joinStep struct {
	item  int
	kind  string
	conds []Expr
}

// joinOrder returns the steps of the join. Outer joins keep the written
// order. Inner joins (INNER, CROSS, comma) start from the table whose pushed
// filters match the fewest rows (counted by its index), then repeatedly add
// the smallest table connected to the ones already joined; each condition is
// applied at the first step where all its tables are available.
func (e *executor) joinOrder(ctx context.Context, jp *joinPlan, wps []wherePlan) ([]joinStep, error) {
	inner := true
	for _, it := range jp.items {
		// LATERAL items and table functions keep the written order too.
		if (it.kind != "" && it.kind != "INNER" && it.kind != "CROSS") || it.lateral != nil {
			inner = false
		}
	}
	condsOf := func(it *joinItem) []Expr {
		var c []Expr
		if it.on != nil {
			c = conjuncts(it.on, nil)
		}
		return append(c, it.extra...)
	}
	if !inner {
		steps := make([]joinStep, len(jp.items))
		for i, it := range jp.items {
			steps[i] = joinStep{item: i, kind: it.kind, conds: condsOf(it)}
		}
		return steps, nil
	}
	counts := make([]int64, len(jp.items))
	for i, it := range jp.items {
		switch {
		case it.base.isMem:
			counts[i] = int64(len(it.base.mem))
		case wps[i].none:
		case wps[i].keys != nil:
			counts[i] = int64(len(wps[i].keys))
		default:
			n, err := e.store.countMatches(ctx, it.base.index(), wps[i].query)
			if err != nil {
				return nil, err
			}
			counts[i] = n
		}
	}
	type poolCond struct {
		expr  Expr
		items []int
		used  bool
	}
	var pool []*poolCond
	for i, it := range jp.items {
		for _, c := range condsOf(it) {
			idx, movable := jp.itemsOf(c)
			if !movable {
				// The columns its subqueries read aren't in idx: it waits
				// for every item its ON may read (see bindJoin).
				idx = nil
				for j := jp.start(i); j <= i; j++ {
					idx = append(idx, j)
				}
			}
			pool = append(pool, &poolCond{expr: c, items: idx})
		}
	}
	chosen := map[int]bool{}
	var steps []joinStep
	for len(steps) < len(jp.items) {
		best, bestConnected := -1, false
		for i := range jp.items {
			if chosen[i] {
				continue
			}
			connected := false
			for _, pc := range pool {
				if slices.Contains(pc.items, i) && len(pc.items) > 1 {
					others := true
					for _, x := range pc.items {
						if x != i && !chosen[x] {
							others = false
						}
					}
					connected = connected || others
				}
			}
			better := best < 0 ||
				(connected && !bestConnected) ||
				(connected == bestConnected && counts[i] < counts[best])
			if len(steps) == 0 {
				better = best < 0 || counts[i] < counts[best]
			}
			if better {
				best, bestConnected = i, connected
			}
		}
		chosen[best] = true
		st := joinStep{item: best, kind: "INNER"}
		if len(steps) == 0 {
			st.kind = ""
		}
		for _, pc := range pool {
			if pc.used {
				continue
			}
			ready := true
			for _, x := range pc.items {
				if !chosen[x] {
					ready = false
				}
			}
			// Conditions without column references wait for the first real join.
			if ready && (len(pc.items) > 0 || len(steps) > 0) {
				st.conds = append(st.conds, pc.expr)
				pc.used = true
			}
		}
		steps = append(steps, st)
	}
	return steps, nil
}

// runJoin executes the join, returning rows keyed alias.column. need lists
// the (prefixed) columns the rest of the query reads.
func (e *executor) runJoin(ctx context.Context, jp *joinPlan, need map[string]bool, params []Value) ([]map[string]Value, error) {
	// Every column read by join conditions must be fetched too.
	allNeed := map[string]bool{}
	for k := range need {
		allNeed[k] = true
	}
	for _, it := range jp.items {
		if it.on != nil {
			columnRefs(it.on, allNeed)
		}
		for _, x := range it.extra {
			columnRefs(x, allNeed)
		}
		if it.lateral != nil {
			maps.Copy(allNeed, it.lateral.need)
		}
	}
	types := map[string]ColType{}
	for _, it := range jp.items {
		for _, c := range it.base.Columns {
			types[it.prefix+c.Name] = c.Type
		}
	}
	for _, m := range jp.merged {
		columnRefs(m.expr, allNeed)
		types[m.key] = m.typ
	}
	env := e.newEnv(ctx, types, params)

	// Items that can't change the result aren't read (see joinReads): those
	// left out by ON conditions that are never true and by in-memory items
	// without rows, then also by items whose own filters match nothing.
	kinds := make([]string, len(jp.items))
	never := make([]bool, len(jp.items))
	empty := make([]bool, len(jp.items))
	for i, it := range jp.items {
		kinds[i], never[i] = readsKind(it.kind, it.comma), i > 0 && e.neverTrue(ctx, it.on)
		empty[i] = it.lateral == nil && it.base.isMem && it.base.view == nil && it.base.join == nil && it.base.lazy == nil &&
			len(it.base.mem) == 0
	}
	read, none := joinReads(kinds, never, empty)
	if none {
		return nil, nil
	}

	// Each item's own filters, pushed into its scan.
	views := make([]*tableMeta, len(jp.items))
	wps := make([]wherePlan, len(jp.items))
	for i, it := range jp.items {
		views[i] = it.view()
		if !read[i] {
			wps[i] = wherePlan{query: "*"}
			continue
		}
		wp, err := e.planWhere(ctx, andAll(it.pushed), views[i], params)
		if err != nil {
			return nil, err
		}
		wps[i] = wp
		empty[i] = empty[i] || wp.none
	}
	if read, none = joinReads(kinds, never, empty); none {
		return nil, nil
	}
	steps, err := e.joinOrder(ctx, jp, wps)
	if err != nil {
		return nil, err
	}

	// The rows of an isolated FROM-list item (see joinItem.isolated) are
	// joined on their own, starting from its first item, and then to outer,
	// the rows of the items before it (which the steps keep in written
	// order, as for every outer join).
	var left, outer []map[string]Value
	isolating := false
	endIsolated := func() error {
		if !isolating {
			return nil
		}
		isolating = false
		var err error
		left, err = e.joinRows(env, "CROSS", outer, left, nil, nil)
		return err
	}
	avail := map[int]bool{}
	for k, st := range steps {
		i, it, view, wp := st.item, jp.items[st.item], views[st.item], wps[st.item]
		// first is set for the step that starts the rows joined so far.
		first := k == 0
		if it.comma {
			if err := endIsolated(); err != nil {
				return nil, err
			}
			if it.isolated {
				if len(left) == 0 {
					return nil, nil
				}
				outer, left, isolating, first = left, nil, true, true
			}
		}

		// Split equality conditions (joined so far ⋈ this item) from the rest.
		var pairs []equiPair
		var filters []Expr
		for _, c := range st.conds {
			if b, ok := c.(*Binary); ok && b.Op == "=" && !first {
				li, lok := jp.itemsOf(b.L)
				ri, rok := jp.itemsOf(b.R)
				if lok && rok && len(li) > 0 && len(ri) > 0 {
					switch {
					case within(li, avail) && onlyItem(ri, i):
						pairs = append(pairs, equiPair{left: b.L, right: b.R})
						continue
					case within(ri, avail) && onlyItem(li, i):
						pairs = append(pairs, equiPair{left: b.R, right: b.L})
						continue
					}
				}
			}
			filters = append(filters, c)
		}
		avail[i] = true
		if !read[i] {
			// The item can't change the result: a LEFT or FULL item adds
			// nothing to the rows joined so far, and otherwise those rows
			// are replaced later.
			if st.kind == "LEFT" || st.kind == "FULL" {
				if err := jp.fillMerged(env, i, left); err != nil {
					return nil, err
				}
			} else {
				left = nil
			}
			continue
		}

		itemNeed := map[string]bool{}
		for c := range allNeed {
			if strings.HasPrefix(c, it.prefix) {
				itemNeed[c] = true
			}
		}
		innerLike := st.kind == "INNER" || st.kind == "LEFT" || st.kind == "CROSS"
		if !first && len(left) == 0 && innerLike {
			// Nothing on the left to join or extend; a later RIGHT/FULL
			// join may still add rows.
			left = nil
			continue
		}
		if !first && innerLike {
			never, err := neverTrue(env, filters)
			if err != nil {
				return nil, err
			}
			if never {
				// A condition such as ON FALSE matches no row of this item, so
				// it isn't read: an inner join is empty, and a left join keeps
				// the left rows with this item's columns NULL.
				if st.kind != "LEFT" {
					left = nil
				}
				continue
			}
		}
		if it.lateral != nil {
			if left, err = e.lateralJoin(env, first, st, it, wp, left, pairs, filters); err != nil {
				return nil, err
			}
			if err := jp.fillMerged(env, i, left); err != nil {
				return nil, err
			}
			continue
		}
		if !first && innerLike && wp.keys == nil && !view.isMem {
			// Index lookup join: fetch only rows whose key matches the left.
			for _, p := range pairs {
				cm, ok := lookupColumn(view, p.right)
				if !ok || !cm.Indexed || !simpleName(cm.field()) {
					continue
				}
				seen := map[string]bool{}
				var values []Value
				for _, row := range left {
					if err := cancelled(ctx); err != nil {
						return nil, err
					}
					env.row = row
					v, err := env.eval(p.left)
					if err != nil {
						return nil, invalidArg(err)
					}
					if key, ok := joinKey(v); ok && !seen[key] {
						seen[key] = true
						values = append(values, v)
					}
				}
				if len(values) > maxUnionTerms {
					continue
				}
				if q, ok := unionQuery(cm, values, e.zone()); ok {
					switch {
					case q == noMatchQuery:
						wp.none = true // no key of the left rows can match
					case wp.query == "*":
						wp.query = q
					default:
						wp.query += " " + q
					}
				}
				break
			}
		}
		keys, right, err := e.scan(ctx, scanRequest{meta: view, where: wp, need: itemNeed}, params)
		if err != nil {
			return nil, err
		}
		if it.tag != "" {
			for r, row := range right {
				if keys != nil {
					row[it.tag] = stringValue(keys[r])
				} else {
					row[it.tag] = boolValue(true)
				}
			}
		}
		if first {
			// Conditions on the first table alone that could not be pushed.
			left = right
			if len(st.conds) > 0 {
				if left, err = e.joinRows(env, "INNER", left, []map[string]Value{{}}, nil, st.conds); err != nil {
					return nil, err
				}
			}
			continue
		}
		left, err = e.joinRows(env, st.kind, left, right, pairs, filters)
		if err != nil {
			return nil, err
		}
		if err := jp.fillMerged(env, i, left); err != nil {
			return nil, err
		}
	}
	if err := endIsolated(); err != nil {
		return nil, err
	}
	return left, nil
}

// lookupColumn returns the column a join key expression reads unchanged: a
// column reference, or a cast of one to the column's own type or of a
// VARCHAR(n) column to text (dbt writes `a.id::text = b.id::text` for text
// columns).
func lookupColumn(view *tableMeta, x Expr) (columnMeta, bool) {
	cast, isCast := x.(*Cast)
	if isCast {
		x = cast.X
	}
	ref, ok := x.(*ColumnRef)
	if !ok {
		return columnMeta{}, false
	}
	cm, ok := view.column(ref.Name)
	if !ok || (isCast && !sameText(cm.Type, cast.T)) {
		return columnMeta{}, false
	}
	return cm, true
}

// neverTrue reports whether one of conds has no column references (such as
// ON FALSE) and is not true, so that no pair of rows can satisfy them all.
func neverTrue(env *evalEnv, conds []Expr) (bool, error) {
	for _, c := range conds {
		if !isConstant(c) {
			continue
		}
		v, err := env.eval(c)
		if err != nil {
			return false, invalidArg(err)
		}
		if b, ok := truthy(v); !ok || !b {
			return true, nil
		}
	}
	return false, nil
}

func within(idx []int, set map[int]bool) bool {
	for _, x := range idx {
		if !set[x] {
			return false
		}
	}
	return true
}

func onlyItem(idx []int, i int) bool { return len(idx) == 1 && idx[0] == i }

func mergeRows(a, b map[string]Value) map[string]Value {
	m := make(map[string]Value, len(a)+len(b))
	for k, v := range a {
		m[k] = v
	}
	for k, v := range b {
		m[k] = v
	}
	return m
}

// joinRows combines left and right rows with a hash join on pairs (or a
// nested loop when there are none), keeping pairs that also pass filters.
func (e *executor) joinRows(env *evalEnv, kind string, left, right []map[string]Value, pairs []equiPair, filters []Expr) ([]map[string]Value, error) {
	// A pair with a CHAR side compares strings without trailing spaces
	// (lengths.go), so its keys drop them on both sides.
	char := make([]bool, len(pairs))
	// A pair of a DATE or TIMESTAMP with a TIMESTAMP WITH TIME ZONE (or a
	// DATE with a TIMESTAMP, or a TIME with a TIME WITH TIME ZONE) is keyed
	// as the latter, converted in the session time zone, as compareIn
	// compares them.
	keyType := make([]ColType, len(pairs))
	for i, p := range pairs {
		lt, _ := inferType(p.left, env.types, nil)
		rt, _ := inferType(p.right, env.types, nil)
		char[i] = lt.isChar() || rt.isChar()
		temporal := func(t ColType) bool { return t.Kind == KindDate || t.Kind == KindTimestamp }
		if temporal(lt) && temporal(rt) && (lt.Kind != rt.Kind || lt.TZ != rt.TZ) {
			keyType[i] = timestampType(arrow.Second, "") // a date's unit
			if isTimestampTZ(lt) || isTimestampTZ(rt) {
				keyType[i].TZ = "UTC"
			}
		}
		if lt.isTimeOfDay() && rt.isTimeOfDay() && lt.Kind != rt.Kind {
			keyType[i] = ColType{Kind: KindTimeTZ, Unit: arrow.Second}
		}
	}
	zone := env.zone()
	keyOf := func(row map[string]Value, side func(equiPair) Expr) (string, bool, error) {
		env.row = row
		var b strings.Builder
		for i, p := range pairs {
			v, err := env.eval(side(p))
			if err != nil {
				return "", false, invalidArg(err)
			}
			if char[i] && v.T.Kind == KindString && !v.Null {
				v = Value{T: ColType{Kind: KindString, Fixed: true}, S: v.S}
			}
			if kt := keyType[i]; kt.Kind != KindNull && !v.Null && (v.T.Kind != kt.Kind || v.T.TZ != kt.TZ) {
				// The value's own unit, so that nothing is rounded.
				if v.T.Kind == KindTimestamp || v.T.isTimeOfDay() {
					kt.Unit = v.T.Unit
				}
				if v, err = coerceIn(v, kt, zone); err != nil {
					return "", false, nil // matches nothing, as compareIn
				}
			}
			k, ok := joinKey(v)
			if !ok {
				return "", false, nil
			}
			b.WriteString(k)
			b.WriteByte(0)
		}
		return b.String(), true, nil
	}
	leftSide := func(p equiPair) Expr { return p.left }
	rightSide := func(p equiPair) Expr { return p.right }

	// Bucket the right rows by key.
	buckets := map[string][]int{}
	if len(pairs) > 0 {
		for j, r := range right {
			if err := cancelled(env.ctx); err != nil {
				return nil, err
			}
			k, ok, err := keyOf(r, rightSide)
			if err != nil {
				return nil, err
			}
			if ok {
				buckets[k] = append(buckets[k], j)
			}
		}
	}
	matchedRight := make([]bool, len(right))
	var out []map[string]Value
	passes := func(row map[string]Value) (bool, error) {
		env.row = row
		for _, f := range filters {
			v, err := env.eval(f)
			if err != nil {
				return false, invalidArg(err)
			}
			if b, ok := truthy(v); !ok || !b {
				return false, nil
			}
		}
		return true, nil
	}
	// A constant condition that isn't true (ON FALSE) matches nothing; don't
	// try every pair.
	never, err := neverTrue(env, filters)
	if err != nil {
		return nil, err
	}
	for _, l := range left {
		if err := cancelled(env.ctx); err != nil {
			return nil, err
		}
		var candidates []int
		switch {
		case never:
		case len(pairs) > 0:
			k, ok, err := keyOf(l, leftSide)
			if err != nil {
				return nil, err
			}
			if ok {
				candidates = buckets[k]
			}
		default:
			candidates = make([]int, len(right))
			for j := range right {
				candidates[j] = j
			}
		}
		matched := false
		for _, j := range candidates {
			// Every pair, as one left row can meet many right ones.
			if err := cancelled(env.ctx); err != nil {
				return nil, err
			}
			row := mergeRows(l, right[j])
			ok, err := passes(row)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, row)
				matched = true
				matchedRight[j] = true
			}
		}
		if !matched && (kind == "LEFT" || kind == "FULL") {
			out = append(out, l)
		}
	}
	if kind == "RIGHT" || kind == "FULL" {
		for j, r := range right {
			if !matchedRight[j] {
				out = append(out, r)
			}
		}
	}
	return out, nil
}
