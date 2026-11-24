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

// Hash sets for IN subqueries, and semi-joins for correlated subqueries.
//
//   - `x IN (SELECT …)` probes a hash set of the subquery's values instead of
//     comparing x with every value. The set gives exactly the answer of
//     comparing one by one with compareValues, including SQL's NULL rules and
//     cross-type equality (1 = 1.0 = 1.00); values and type pairs the hash
//     can't answer exactly are compared one by one.
//   - A correlated EXISTS / IN subquery whose only outer references are
//     top-level `inner = outer` conjuncts of its WHERE can run once as a
//     semi-join: without those conjuncts, projecting their inner sides (and,
//     for IN, its value), grouped by those keys in a hash table that each
//     outer row probes. NOT EXISTS and NOT IN are the negation and [NOT] IN's
//     own logic over the matching group. An EXISTS body may also compare
//     inner columns with outer columns of the same class (`l2.l_suppkey <>
//     l1.l_suppkey`, TPC-H Q21): its group keeps those inner values, and
//     some row of the group must make every such comparison TRUE. A small
//     inner side is read up front (and can then filter the outer table in
//     its index); a large one is probed a page of outer rows at a time, or
//     read once running per outer row has cost about as much, so that a few
//     outer rows don't pay for reading all of it. Anything else (other correlation,
//     aggregates, LIMIT, keys of different types, …) runs per outer row,
//     memoised.

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
)

// subqueryStats counts subquery work; tests read it to tell which path a
// query took.
var subqueryStats struct {
	runs      atomic.Int64 // subquery bodies run by subqueryRows
	semiJoins atomic.Int64 // semi-join bodies run
	inSets    atomic.Int64 // IN hash sets built
	inLists   atomic.Int64 // rows answered by a literal IN list's hash set
	// parallelProbes counts probe pages whose batches ran on concurrent
	// workers (runSemiProbes).
	parallelProbes atomic.Int64
}

// ---- equality keys ----

// eqClass groups the kinds that compareValues compares in one way.
type eqClass uint8

const (
	clsNone        eqClass = iota // no key: a kind not listed here
	clsNum                        // integers and decimals, compared exactly
	clsBool                       // compared with integers and decimals as 0/1
	clsFloat                      // floats (NaNs are equal)
	clsStr                        // strings and binary, compared bytewise
	clsInterval                   // compared by total length
	clsDate                       // compared by day
	clsTime                       // compared after converting units
	clsTimestamp                  // compared after converting units
	clsTimestampTZ                // compared after converting units; apart from TIMESTAMP (the session time zone)
	clsTimeTZ                     // compared by time and offset; apart from TIME (the session time zone)
	clsJSONB
	numClasses
)

func classOf(t ColType) eqClass {
	switch k := t.Kind; {
	case k == KindJSONB:
		return clsJSONB
	case k.isInteger() || k == KindDecimal:
		return clsNum
	case k == KindBool:
		return clsBool
	case k.isFloat():
		return clsFloat
	case k == KindString || k == KindBinary:
		return clsStr
	case k == KindInterval:
		return clsInterval
	case k == KindDate:
		return clsDate
	case k == KindTime:
		return clsTime
	case k == KindTimeTZ:
		return clsTimeTZ
	case k == KindTimestamp && t.TZ != "":
		return clsTimestampTZ
	case k == KindTimestamp:
		return clsTimestamp
	}
	return clsNone
}

// eqKey returns a non-NULL value's class and its key within the class: two
// values of a class compare equal exactly when their keys are equal
// (joinKey's normalization; integers, booleans and decimals share keys).
func eqKey(v Value) (eqClass, string) {
	c := classOf(v.T)
	if c == clsDate {
		return c, strconv.FormatInt(v.I, 10)
	}
	if c == clsNone {
		return c, ""
	}
	k, _ := joinKey(v)
	return c, k
}

// floatKey is the key of a number compared as a double (an integer or decimal
// compared with a float).
func floatKey(v Value) string {
	f, _ := v.asFloat()
	k, _ := joinKey(floatValue(typeFloat64, f))
	return k
}

// classTag is the prefix that keeps the keys of different classes apart in
// one map. Integers, decimals and booleans compare exactly with each other,
// so they share a tag.
var classTag = [numClasses]string{clsNum: "n", clsBool: "n", clsFloat: "f", clsStr: "s",
	clsInterval: "i", clsDate: "d", clsTime: "t", clsTimestamp: "p", clsTimestampTZ: "z", clsTimeTZ: "w"}

func exactNum(c eqClass) bool { return c == clsNum || c == clsBool }

// ---- IN hash sets ----

// inSetLinear is the size up to which an IN set compares values one by one
// rather than building a hash index.
const inSetLinear = 8

// inSet answers `x IN (values)` for the values in column col of rows, with
// the result of comparing x with each value by compareValues.
type inSet struct {
	rows [][]Value
	col  int
	// noIndex makes the set compare one by one (a result used only once).
	noIndex bool
	// zone is the session time zone values of different types compare in.
	zone tzZone

	built      bool
	sawNull    bool
	classes    uint16              // eqClass bits of the hashed values
	keys       map[string]struct{} // classTag + eqKey of each hashed value
	odd        []Value             // values without a key, compared one by one
	numAsFloat map[string]struct{} // floatKey of the clsNum values, on demand
	char       bool                // a value is CHAR: compare one by one
}

// eval applies [NOT] IN's three-valued logic: an empty set gives FALSE (TRUE
// for NOT IN) even for a NULL x; otherwise a NULL x, or no match when the set
// contains a NULL, gives NULL.
func (s *inSet) eval(x Value, not bool) Value {
	if len(s.rows) == 0 {
		return boolValue(not)
	}
	if x.Null {
		return nullValue(typeBool)
	}
	found, sawNull := s.contains(x)
	switch {
	case found:
		return boolValue(!not)
	case sawNull:
		return nullValue(typeBool)
	}
	return boolValue(not)
}

// linear compares x with every value.
func (s *inSet) linear(x Value) (found, sawNull bool) {
	for _, row := range s.rows {
		v := row[s.col]
		if v.Null {
			sawNull = true
			continue
		}
		if c, ok := compareIn(x, v, s.zone); ok && c == 0 {
			return true, sawNull
		}
	}
	return false, sawNull
}

func (s *inSet) build() {
	s.built = true
	s.keys = make(map[string]struct{}, len(s.rows))
	for _, row := range s.rows {
		v := row[s.col]
		if v.Null {
			s.sawNull = true
			continue
		}
		s.char = s.char || v.T.isChar()
		c, k := eqKey(v)
		if c == clsNone {
			s.odd = append(s.odd, v)
			continue
		}
		s.classes |= 1 << c
		s.keys[classTag[c]+k] = struct{}{}
	}
	subqueryStats.inSets.Add(1)
}

// contains reports whether x (not NULL) equals a value of the set, and
// whether the set contains a NULL.
func (s *inSet) contains(x Value) (found, sawNull bool) {
	if s.noIndex || len(s.rows) <= inSetLinear {
		return s.linear(x)
	}
	if !s.built {
		s.build()
	}
	cx, kx := eqKey(x)
	if cx == clsNone || s.char || x.T.isChar() {
		// CHAR values compare without trailing spaces (lengths.go).
		return s.linear(x)
	}
	slow := false
	for c := clsNum; c < numClasses; c++ {
		if s.classes&(1<<c) == 0 {
			continue
		}
		var key string
		switch {
		case exactNum(cx) && exactNum(c), cx == c:
			key = classTag[c] + kx
		case cx == clsNum && c == clsFloat:
			// An integer or decimal compared with a float: as doubles.
			key = classTag[c] + floatKey(x)
		case cx == clsFloat && c == clsNum:
			if s.numAsFloat == nil {
				s.numAsFloat = map[string]struct{}{}
				for _, row := range s.rows {
					if v := row[s.col]; !v.Null && classOf(v.T) == clsNum {
						s.numAsFloat[floatKey(v)] = struct{}{}
					}
				}
			}
			if _, ok := s.numAsFloat[kx]; ok {
				return true, s.sawNull
			}
			continue
		default:
			// Strings compared with other types are converted first, and
			// so are dates compared with timestamps: compare one by one.
			slow = true
			continue
		}
		if _, ok := s.keys[key]; ok {
			return true, s.sawNull
		}
	}
	if slow {
		return s.linear(x)
	}
	for _, v := range s.odd {
		if c, ok := compareIn(x, v, s.zone); ok && c == 0 {
			return true, s.sawNull
		}
	}
	return false, s.sawNull
}

// cachedInSet returns the hash set of an uncorrelated IN subquery's rows,
// built once per statement.
func (e *executor) cachedInSet(sq *Subquery, rows [][]Value) *inSet {
	if s, ok := e.cache.inSets[sq]; ok {
		return s
	}
	s := &inSet{rows: rows, zone: e.zone()}
	e.cache.inSets[sq] = s
	return s
}

// ---- semi-joins ----

// semiJoin is the decorrelated form of a correlated EXISTS / IN subquery.
type semiJoin struct {
	// plan is the subquery without its correlation conjuncts. Its first
	// len(outer) items are their inner sides; for IN, the subquery's value
	// (or values, for a row IN) follows; for EXISTS, the inner columns of
	// resid follow.
	plan *selectPlan
	// outer are the outer columns the keys are compared with, and types their
	// types. Keys and outer values must be of the same class (and time unit),
	// where comparing them never fails and equal keys mean equal values.
	outer []outerRef
	types []ColType
	// resid are an EXISTS body's other correlation conjuncts, each comparing
	// an inner column with an outer column (TPC-H Q21's `l2.l_suppkey <>
	// l1.l_suppkey`). They are evaluated on the rows of the outer row's group.
	resid []semiResidual
}

// semiResidual is a correlation conjunct `inner op outer` (or `outer op
// inner`) of a decorrelated EXISTS. Both sides are columns of the same class,
// so comparing them never fails; it is evaluated by binaryOp, as the per-row
// path evaluates it.
type semiResidual struct {
	op        string
	col       int // the inner column's item in plan
	outer     outerRef
	typ       ColType // the outer column's type
	innerLeft bool    // the inner column is the left operand
}

// semiResidualConjunct returns a conjunct's comparison of an inner column with
// an immediately enclosing query's column (not `=`, which is a key pair).
func semiResidualConjunct(c Expr) (op string, inner, outer *ColumnRef, innerLeft, ok bool) {
	b, isBinary := c.(*Binary)
	if !isBinary || b.nested || b.Op == "=" || !isComparison(b.Op) {
		return "", nil, nil, false, false
	}
	l, lok := b.L.(*ColumnRef)
	r, rok := b.R.(*ColumnRef)
	if !lok || !rok || l.whole != nil || r.whole != nil || l.outerAgg != nil || r.outerAgg != nil {
		return "", nil, nil, false, false
	}
	switch {
	case l.Outer == 0 && r.Outer == 1:
		return b.Op, l, r, true, true
	case l.Outer == 1 && r.Outer == 0:
		return b.Op, r, l, false, true
	}
	return "", nil, nil, false, false
}

// residualsHold reports whether every residual conjunct is TRUE for an inner
// row, given the outer values (NULL compares as NULL, never TRUE). ok is
// false when a comparison fails, which same-class columns never do; the
// per-row path then reports the original outcome.
func (sj *semiJoin) residualsHold(row []Value, outer []Value, z tzZone) (hold, ok bool) {
	for i, rc := range sj.resid {
		l, r := row[rc.col], outer[i]
		if !rc.innerLeft {
			l, r = r, l
		}
		v, err := binaryOp(rc.op, l, r, z)
		if err != nil {
			return false, false
		}
		if b, valid := truthy(v); !valid || !b {
			return false, true
		}
	}
	return true, true
}

// residualOuter returns an outer row's values for the residual conjuncts; ok
// is false when a value is not of its planned class.
func (sj *semiJoin) residualOuter(lookup func(outerRef) (Value, error)) ([]Value, bool, error) {
	out := make([]Value, len(sj.resid))
	for i, rc := range sj.resid {
		v, err := lookup(rc.outer)
		if err != nil {
			return nil, false, err
		}
		if !v.Null && !sameEqClass(v.T, rc.typ) {
			return nil, false, nil
		}
		out[i] = v
	}
	return out, true, nil
}

// sameEqClass reports whether values of types a and b compare by their eqKey
// (and never fail to compare).
func sameEqClass(a, b ColType) bool {
	if a.isChar() || b.isChar() {
		return false // compared without trailing spaces (lengths.go)
	}
	ca, cb := classOf(a), classOf(b)
	if exactNum(ca) && exactNum(cb) {
		return true
	}
	if ca == clsNone || ca != cb {
		return false
	}
	return (ca != clsTime && ca != clsTimestamp && ca != clsTimestampTZ) || a.Unit == b.Unit
}

// semiKey returns the key of a non-NULL key value expected to be of type t;
// ok is false when the value is of another class.
func semiKey(v Value, t ColType) (string, bool) {
	if !sameEqClass(v.T, t) {
		return "", false
	}
	c, k := eqKey(v)
	return k, c != clsNone
}

// planSemiJoin plans sq (a planned, correlated subquery) as a semi-join when
// that gives the same result: every outer reference in its body is the outer
// side of a top-level `inner = outer` conjunct of its WHERE (for EXISTS, with
// at least one such conjunct, also of a top-level comparison of an inner
// column with an outer column of the same class), the body has no
// aggregates, GROUP BY, HAVING, window functions, OFFSET or (for IN) LIMIT,
// and the inner and outer sides of each conjunct are of the same class.
// EXISTS also requires a plain select list (columns, literals, *), which it
// doesn't need to evaluate. The body must read one table or in-memory
// relation (a CTE, a derived table or a computed view), whose size
// semiJoinState can tell, and call no volatile function.
func (e *executor) planSemiJoin(ctx context.Context, sq *Subquery) {
	sel, plan := sq.Select, sq.plan
	if !sq.correlated || (sq.Kind != SubqueryExists && sq.Kind != SubqueryIn) || sel.SetOp != nil || plan.meta == nil ||
		plan.aggregate || len(sel.GroupBy) > 0 || sel.Having != nil || sel.Offset != nil || sel.Where == nil ||
		plan.windowed() || len(sel.Windows) > 0 {
		return
	}
	if len(sel.Joins) > 0 || plan.meta.view != nil || plan.meta.join != nil || plan.meta.lazy != nil {
		return
	}
	// DISTINCT ON keeps one row per key after the correlation filter; the
	// semi-join body (which drops it) would see every row.
	if len(sel.DistinctOn) > 0 {
		return
	}
	// RANDOM() would be drawn once per inner row for all outer rows.
	if hasVolatile(sel.Where) {
		return
	}
	if sq.Kind == SubqueryIn {
		for _, it := range sel.Items {
			if hasVolatile(it.Expr) {
				return
			}
		}
	}
	if sel.Limit != nil && (sq.Kind == SubqueryIn || *sel.Limit < 1) {
		return
	}
	if sq.Kind == SubqueryExists {
		for _, it := range sel.Items {
			switch x := it.Expr.(type) {
			case *Literal, *Param:
			case *ColumnRef:
				if x.Outer > 0 {
					return
				}
			default:
				if !it.Star {
					return
				}
			}
		}
	}
	type pair struct {
		inner Expr
		outer *ColumnRef
	}
	type residual struct {
		op           string
		inner, outer *ColumnRef
		innerLeft    bool
	}
	var pairs []pair
	var resid []residual
	var rest []Expr
	for _, c := range conjuncts(sel.Where, nil) {
		if b, ok := c.(*Binary); ok && b.Op == "=" {
			if r, ok := b.R.(*ColumnRef); ok && r.Outer == 1 {
				pairs = append(pairs, pair{inner: b.L, outer: r})
				continue
			}
			if l, ok := b.L.(*ColumnRef); ok && l.Outer == 1 {
				pairs = append(pairs, pair{inner: b.R, outer: l})
				continue
			}
		}
		if sq.Kind == SubqueryExists {
			if op, inner, outer, innerLeft, ok := semiResidualConjunct(c); ok {
				resid = append(resid, residual{op: op, inner: inner, outer: outer, innerLeft: innerLeft})
				continue
			}
		}
		rest = append(rest, c)
	}
	// Each pair and residual holds one outer reference; any other (in the
	// inner sides, the other conjuncts, the select list, ORDER BY, JOIN … ON
	// or a nested subquery) keeps the per-row path.
	if len(pairs) == 0 || sq.outerUses != len(pairs)+len(resid) {
		return
	}
	dsel := &SelectStmt{With: sel.With, Joins: sel.Joins, From: sel.From, FromSelect: sel.FromSelect, FromGroup: sel.FromGroup,
		FromAlias: sel.FromAlias, Where: andAll(rest)}
	for _, p := range pairs {
		dsel.Items = append(dsel.Items, SelectItem{Expr: p.inner})
	}
	if sq.Kind == SubqueryIn {
		dsel.Items = append(dsel.Items, sel.Items...)
	}
	for _, rc := range resid {
		dsel.Items = append(dsel.Items, SelectItem{Expr: rc.inner})
	}
	// Plan it with no enclosing scopes: it reads nothing from the outer row.
	savedScopes, savedSq := e.scopes, e.pendingSq
	e.scopes, e.pendingSq = nil, nil
	dplan, err := e.planSelect(ctx, dsel, e.paramTypes)
	e.scopes, e.pendingSq = savedScopes, savedSq
	if err != nil || len(dplan.items) != len(dsel.Items) {
		return
	}
	sj := &semiJoin{plan: dplan}
	for i, p := range pairs {
		if !sameEqClass(dplan.items[i].typ, p.outer.OuterType) {
			return
		}
		sj.outer = append(sj.outer, outerRef{name: p.outer.Name})
		sj.types = append(sj.types, p.outer.OuterType)
	}
	for j, rc := range resid {
		col := len(pairs) + j
		if !sameEqClass(dplan.items[col].typ, rc.outer.OuterType) {
			return
		}
		sj.resid = append(sj.resid, semiResidual{op: rc.op, col: col, outer: outerRef{name: rc.outer.Name},
			typ: rc.outer.OuterType, innerLeft: rc.innerLeft})
	}
	sq.semi = sj
}

// semiJoinEagerRows is the size of inner side up to which a semi-join is
// built before the outer rows are read, so that its keys can also filter them
// in the index (see semiJoinTerm).
const semiJoinEagerRows = maxUnionTerms

// rowsPerRun is roughly how many rows a scan reads in the time one per-row
// run of a subquery takes: an index query round trip (~0.4–0.6 ms locally)
// against ~5 µs per row read in cursor pages.
const rowsPerRun = 100

// semiResult is a semi-join's state for one statement. Building it reads the
// whole inner side, which costs more than running the subquery for a few
// outer rows; so a large inner side is built only once the per-row runs have
// cost about as much, and until then the subquery runs per outer row.
type semiResult struct {
	eager bool // build before reading the outer rows (small or in memory)
	after int  // otherwise, build after this many per-row runs
	built bool
	// failed means the body failed to run or returned a key it can't hash;
	// the subquery then keeps running per outer row.
	failed bool
	rows   int // rows with no NULL key
	// groups maps the key tuple of each row to the rows with that key (for
	// IN, as the set of values; for EXISTS and a row IN, nil).
	groups map[string]*inSet
	// rowGroups are a row IN's groups, as sets of rows.
	rowGroups map[string]*rowSet
	// distinct holds the distinct values of each key, up to maxUnionTerms+1.
	distinct [][]Value
	// probed holds the keys whose complete inner groups a batched probe read
	// (see probeSemiJoin) while the whole inner side remains unbuilt.
	probed map[string]bool
	// probeFailed stops probing: the key column is not a safe index union,
	// a key can't be hashed or a probe failed. Per-row runs then report the
	// original query's results and errors.
	probeFailed bool
}

// semiProbeSubqueries returns the decorrelated EXISTS / IN subqueries directly
// in a scan's residual. Their outer keys are columns of the scanned row
// (walkExpr does not enter subquery bodies).
func semiProbeSubqueries(residual Expr) []*Subquery {
	var out []*Subquery
	walkExpr(residual, func(x Expr) {
		if sq, ok := x.(*Subquery); ok && sq.semi != nil {
			if _, row := sq.X.(*RowExpr); !row {
				out = append(out, sq)
			}
		}
	})
	return out
}

// semiRowKey is evalSemiJoin's key for an outer row: null when an outer value
// is NULL (`inner = NULL` is never true), !ok when a value can't be hashed.
func (sj *semiJoin) semiRowKey(lookup func(outerRef) (Value, error)) (key string, null, ok bool, err error) {
	var buf []byte
	for i, ref := range sj.outer {
		v, err := lookup(ref)
		if err != nil {
			return "", false, false, err
		}
		if v.Null {
			null = true
			continue
		}
		k, ok := semiKey(v, sj.types[i])
		if !ok {
			return "", false, false, nil
		}
		buf = appendKey(buf, k)
	}
	return string(buf), null, true, nil
}

// semiProbeKeys bounds one probe's key union. Search re-evaluates a union for
// each result page, so per-key cost rises past a few hundred keys (measured
// 0.41 ms/key at 250 and 0.60 ms/key at 1,000 keys of four rows each).
const semiProbeKeys = 256

// semiProbePage is how many in-memory rows (a join's, see scanMem) one
// probeSemiJoin call reads the groups of; the batches stay bounded by
// semiProbeKeys.
const semiProbePage = 4096

// probeSemiJoin reads the complete inner groups of a page of outer rows, in
// batches of at most semiProbeKeys distinct first-key values. Each batch is
// the decorrelated body with `inner = v1 OR …`, a union on the indexed inner
// key column, so a large inner side is never scanned per outer row (TPC-H
// Q4) nor read whole for a selective outer side. Only keys of this page are
// recorded, so every recorded group is complete; rows the union returns for
// other keys are ignored. The outer residual still evaluates the subquery.
func (e *executor) probeSemiJoin(ctx context.Context, sq *Subquery, rows []map[string]Value, types map[string]ColType) {
	r := e.semiJoinState(ctx, sq)
	if r.built || r.failed || r.eager || r.probeFailed || len(rows) == 0 {
		return
	}
	sj := sq.semi
	if len(sj.plan.items) == 0 || sj.plan.meta == nil || sj.plan.meta.isMem {
		r.probeFailed = true
		return
	}
	probeExpr := sj.plan.items[0].expr
	cm, ok := lookupColumn(sj.plan.meta, probeExpr)
	if !ok || !cm.Indexed || !simpleName(cm.field()) {
		r.probeFailed = true
		return
	}
	if r.probed == nil {
		r.probed = map[string]bool{}
	}
	if r.groups == nil {
		r.groups = map[string]*inSet{}
	}
	pendingFirst := map[string]string{} // outer key -> its first-key hash
	firstSeen := map[string]bool{}
	var firstValues []Value
	var firstKeys []string
	for _, row := range rows {
		key, null, ok, err := sj.semiRowKey(func(ref outerRef) (Value, error) {
			if v, ok := row[ref.name]; ok {
				return v, nil
			}
			if t, ok := types[ref.name]; ok {
				return nullValue(t), nil
			}
			return Value{}, fmt.Errorf("unknown outer column %q", ref.name)
		})
		if err != nil || !ok {
			r.probeFailed = true
			return
		}
		if null || r.probed[key] {
			continue
		}
		if _, seen := pendingFirst[key]; seen {
			continue
		}
		v := row[sj.outer[0].name]
		first, ok := semiKey(v, sj.types[0])
		if !ok {
			r.probeFailed = true
			return
		}
		// Do not introduce a new proof for integer Search equality outside
		// exact-double range; the per-row path remains authoritative.
		if cm.Type.Kind.isInteger() {
			if cv, err := coerceIn(v, cm.Type, e.zone()); err != nil || cv.Null || cv.I > exactDoubleLimit || cv.I < -exactDoubleLimit {
				r.probeFailed = true
				return
			}
		}
		pendingFirst[key] = first
		if !firstSeen[first] {
			firstSeen[first] = true
			firstValues = append(firstValues, v)
			firstKeys = append(firstKeys, first)
		}
	}
	n := len(sj.outer)
	// IN and residual EXISTS evaluate the group's rows; plain EXISTS only
	// needs the key.
	keepRows := sq.Kind == SubqueryIn || len(sj.resid) > 0
	var batches []*semiProbeBatch
	for start := 0; start < len(firstValues); start += semiProbeKeys {
		end := min(start+semiProbeKeys, len(firstValues))
		chunk := firstValues[start:end]
		if q, ok := unionQuery(cm, chunk, e.zone()); !ok || q == "" {
			r.probeFailed = true
			return
		}
		var cond Expr
		for _, v := range chunk {
			eq := &Binary{Op: "=", L: probeExpr, R: &Literal{V: v}}
			if cond == nil {
				cond = eq
			} else {
				cond = &Binary{Op: "OR", L: cond, R: eq}
			}
		}
		plan := *sj.plan
		sel := *plan.sel
		sel.Where = andAll([]Expr{sel.Where, cond})
		plan.sel = &sel
		batches = append(batches, &semiProbeBatch{plan: &plan, firstKeys: firstKeys[start:end]})
	}
	e.runSemiProbes(ctx, batches)
	if err := cancelled(ctx); err != nil {
		return // the evaluation itself reports cancellation
	}
	// Record the batches in order up to the first that failed (or did not
	// run because one failed), as running them one after another would.
	for _, b := range batches {
		if !b.ran || b.err != nil {
			r.probeFailed = true
			return
		}
		inChunk := make(map[string]bool, len(b.firstKeys))
		for _, k := range b.firstKeys {
			inChunk[k] = true
		}
		newly := map[string]bool{}
		for key, first := range pendingFirst {
			if inChunk[first] {
				newly[key] = true
			}
		}
		var buf []byte
	next:
		for _, row := range b.rows {
			buf = buf[:0]
			for i := 0; i < n; i++ {
				if row[i].Null {
					continue next
				}
				k, ok := semiKey(row[i], sj.types[i])
				if !ok {
					r.probeFailed = true
					return
				}
				buf = appendKey(buf, k)
			}
			if !newly[string(buf)] {
				continue
			}
			g, ok := r.groups[string(buf)]
			if keepRows {
				if !ok {
					g = &inSet{col: n, zone: e.zone()}
					r.groups[string(buf)] = g
				}
				g.rows = append(g.rows, row)
			} else if !ok {
				r.groups[string(buf)] = nil
			}
		}
		for key := range newly {
			r.probed[key] = true
		}
	}
}

// semiProbeBatch is one probe of probeSemiJoin: the body with a key union,
// the first-key hashes of its keys and, once run, its rows or error.
type semiProbeBatch struct {
	plan      *selectPlan
	firstKeys []string
	rows      [][]Value
	err       error
	ran       bool
}

// runSemiProbes runs a page's probe batches, concurrently when the worker
// budget grants more than one worker: each probe waits on its own round
// trips (keyset Search pages and HASH fetches), so they overlap well. Each
// worker runs on its own isolated executor and cache (the statement's cache
// is not safe for concurrent writes) with no worker budget, so a probe nested
// in its body runs serially rather than waiting for tokens this one holds.
// Workers take batches in order and start none after a failure. They share
// the statement's context and never cancel a context of their own: a
// cancelled command context interrupts every socket of the execution
// (executionTransport.arm), which would break the per-row fallback.
func (e *executor) runSemiProbes(ctx context.Context, batches []*semiProbeBatch) {
	var stop atomic.Bool
	run := func(ex *executor, b *semiProbeBatch) bool {
		if stop.Load() || cancelled(ctx) != nil {
			return false
		}
		subqueryStats.semiJoins.Add(1)
		b.rows, b.err = ex.runSelect(ctx, b.plan, e.params)
		b.ran = true
		if b.err != nil {
			stop.Store(true)
		}
		return b.err == nil
	}
	workers := 1
	if len(batches) > 1 && e.parallelProbes(ctx, batches[0].plan.meta) {
		grant, err := e.workerBudget.acquireBounded(ctx, e.queryWorkers, len(batches))
		if err != nil {
			return // cancelled: probeSemiJoin reports nothing, the evaluation does
		}
		defer e.workerBudget.release(grant)
		workers = grant
	}
	if workers < 2 {
		for _, b := range batches {
			if !run(e.isolatedChild(), b) {
				return
			}
		}
		return
	}
	subqueryStats.parallelProbes.Add(1)
	var next atomic.Int64
	var wg sync.WaitGroup
	// A worker's panic is raised again here once every worker has stopped,
	// as running the batches one after another would raise it.
	var panicOnce sync.Once
	var panicValue any
	for range workers {
		local := e.isolatedChild()
		local.cache = newExecCache()
		local.workerBudget = nil
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					stop.Store(true)
					panicOnce.Do(func() { panicValue = p })
				}
			}()
			for {
				i := int(next.Add(1) - 1)
				if i >= len(batches) {
					return
				}
				if !run(local, batches[i]) {
					return
				}
			}
		}()
	}
	wg.Wait()
	if panicValue != nil {
		panic(panicValue)
	}
}

// parallelProbes reports whether probes of meta may run on concurrent
// workers: there is a worker budget, and nothing the workers would share is
// written while they run (data-modifying CTE state, retained or partition
// execution, GPU predicates, or a privately owned snapshot segment).
func (e *executor) parallelProbes(ctx context.Context, meta *tableMeta) bool {
	if e.workerBudget == nil || e.gpuRequested || e.gpu != nil || e.dmlSnapshots != nil || e.dmlRunning != nil ||
		e.retained != nil || e.partitionDirectTable != nil || e.partitionForeignTable != "" ||
		ctx.Value(retainedAllocationKey{}) != nil || meta == nil {
		return false
	}
	for _, seg := range meta.snapshotSegments() {
		if snapshotOwns(ctx, seg.Prefix) {
			return false
		}
	}
	return true
}

func appendKey(b []byte, k string) []byte {
	b = strconv.AppendInt(b, int64(len(k)), 10)
	b = append(b, ':')
	return append(b, k...)
}

// semiJoinState returns a semi-join's state, sizing its inner side (with an
// index count) the first time.
func (e *executor) semiJoinState(ctx context.Context, sq *Subquery) *semiResult {
	e.ensureCache()
	if r, ok := e.cache.semi[sq]; ok {
		return r
	}
	r := &semiResult{}
	e.cache.semi[sq] = r
	plan := sq.semi.plan
	if plan.meta.isMem || plan.meta.SnapshotOverlay && !e.snapshotIndexable(ctx, plan.meta) {
		// Each per-row run filters the whole relation anyway.
		r.eager = true
		return r
	}
	child := e.isolatedChild()
	wp, err := child.planWhere(ctx, plan.sel.Where, plan.meta, e.params)
	if err != nil {
		r.failed = true
		return r
	}
	n := int64(len(wp.keys))
	if wp.keys == nil && !wp.none {
		if plan.meta.SnapshotOverlay {
			n, err = e.store.countSnapshotCandidates(ctx, plan.meta, wp.query)
		} else {
			n, err = e.store.countMatches(ctx, plan.meta.index(), wp.query)
		}
		if err != nil {
			r.failed = true
			return r
		}
	}
	r.eager = n <= semiJoinEagerRows
	r.after = int(n / rowsPerRun)
	return r
}

// isolatedChild returns an executor for a query that reads nothing from the
// enclosing queries.
func (e *executor) isolatedChild() *executor {
	child := *e
	child.scopes, child.pendingSq, child.outer = nil, nil, nil
	return &child
}

// buildSemiJoin runs a semi-join's body and groups its rows by key.
func (e *executor) buildSemiJoin(ctx context.Context, sq *Subquery, r *semiResult) {
	sj := sq.semi
	subqueryStats.semiJoins.Add(1)
	rows, err := e.isolatedChild().runSelect(ctx, sj.plan, e.params)
	if err != nil {
		// The body may fail on rows the per-row path never reads (its
		// conjuncts are no longer narrowed by the outer values); that path
		// reports any error the original query has.
		r.failed = true
		return
	}
	n := len(sj.outer)
	r.groups = map[string]*inSet{}
	rowX, isRow := sq.X.(*RowExpr)
	if isRow {
		r.rowGroups = map[string]*rowSet{}
	}
	r.distinct = make([][]Value, n)
	seen := make([]map[string]bool, n)
	for i := range seen {
		seen[i] = map[string]bool{}
	}
	keys := make([]string, n)
	var buf []byte
next:
	for _, row := range rows {
		for i := 0; i < n; i++ {
			if row[i].Null {
				continue next // never equal to anything
			}
		}
		buf = buf[:0]
		for i := 0; i < n; i++ {
			k, ok := semiKey(row[i], sj.types[i])
			if !ok {
				r.failed, r.groups, r.distinct = true, nil, nil
				return
			}
			keys[i] = k
			buf = appendKey(buf, k)
		}
		r.rows++
		for i, k := range keys {
			if len(r.distinct[i]) <= maxUnionTerms && !seen[i][k] {
				seen[i][k] = true
				r.distinct[i] = append(r.distinct[i], row[i])
			}
		}
		g, ok := r.groups[string(buf)]
		if isRow {
			rg := r.rowGroups[string(buf)]
			if rg == nil {
				rg = &rowSet{col: n, n: len(rowX.Items), zone: e.zone()}
				r.rowGroups[string(buf)] = rg
				r.groups[string(buf)] = nil
			}
			rg.rows = append(rg.rows, row)
		} else if sq.Kind == SubqueryIn || len(sj.resid) > 0 {
			if !ok {
				g = &inSet{col: n, zone: e.zone()}
				r.groups[string(buf)] = g
			}
			g.rows = append(g.rows, row)
		} else if !ok {
			r.groups[string(buf)] = nil
		}
	}
	r.built = true
}

// evalSemiJoin evaluates a decorrelated subquery for env's row. ok is false
// when it must be evaluated per row instead: running per row is still
// cheaper, the body failed, or an outer value is not of its key's class.
func (e *executor) evalSemiJoin(env *evalEnv, sq *Subquery) (Value, bool, error) {
	r := e.semiJoinState(env.ctx, sq)
	// Page probes replace per-row runs; a failed probe resumes the original
	// per-row and adaptive whole-build strategy.
	if !r.built && !r.failed && (r.probed == nil || r.probeFailed) && (r.eager || len(e.cache.memo[sq]) >= r.after) {
		e.buildSemiJoin(env.ctx, sq, r)
	}
	if !r.built && r.probed == nil {
		return Value{}, false, nil
	}
	sj := sq.semi
	key, null, ok, err := sj.semiRowKey(func(ref outerRef) (Value, error) { return env.lookupUp(ref.name, ref.up) })
	if err != nil {
		return Value{}, false, err
	}
	if !ok {
		return Value{}, false, nil
	}
	if !r.built {
		_, row := sq.X.(*RowExpr)
		if row || !null && !r.probed[key] {
			// Not read by a page probe: run it per row.
			return Value{}, false, nil
		}
	}
	var g *inSet
	found := false
	if !null {
		g, found = r.groups[key]
	}
	if sq.Kind == SubqueryExists {
		if !found || len(sj.resid) == 0 {
			return boolValue(found), true, nil
		}
		// Some row of the group must make every residual conjunct TRUE.
		outer, ok, err := sj.residualOuter(func(ref outerRef) (Value, error) { return env.lookupUp(ref.name, ref.up) })
		if err != nil {
			return Value{}, false, err
		}
		if !ok {
			return Value{}, false, nil
		}
		exists := false
		for _, row := range g.rows {
			hold, ok := sj.residualsHold(row, outer, env.zone())
			if !ok {
				return Value{}, false, nil
			}
			exists = exists || hold
		}
		return boolValue(exists), true, nil
	}
	if rowX, ok := sq.X.(*RowExpr); ok {
		x, err := env.evalItems(rowX.Items)
		if err != nil {
			return Value{}, true, err
		}
		if !found {
			return boolValue(sq.Not), true, nil // no rows
		}
		return r.rowGroups[key].eval(x, sq.Not), true, nil
	}
	x, err := env.eval(sq.X)
	if err != nil {
		return Value{}, true, err
	}
	if !found {
		return boolValue(sq.Not), true, nil // no rows
	}
	return g.eval(x, sq.Not), true, nil
}

// semiJoinTerm turns a decorrelated EXISTS / IN conjunct of a WHERE clause
// into an index query on the outer table, when its inner side is small enough
// to build first: one that matches nothing when the subquery has no rows, or
// a union of the inner keys when there are at most maxUnionTerms of them and
// the outer column is indexed. The conjunct itself is still checked on the
// rows.
func (e *executor) semiJoinTerm(ctx context.Context, sq *Subquery, meta *tableMeta) (string, bool) {
	r := e.semiJoinState(ctx, sq)
	if !r.built && !r.failed && r.eager {
		e.buildSemiJoin(ctx, sq, r)
	}
	if !r.built {
		return "", false
	}
	if r.rows == 0 {
		return noMatchQuery, true
	}
	for i, ref := range sq.semi.outer {
		cm, ok := meta.column(ref.name)
		if !ok || !cm.Indexed || !simpleName(cm.field()) || len(r.distinct[i]) > maxUnionTerms {
			continue
		}
		if q, ok := unionQuery(cm, r.distinct[i], e.zone()); ok {
			return q, true
		}
	}
	return "", false
}
