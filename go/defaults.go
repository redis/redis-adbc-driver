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

// Column defaults.
//
// A column's DEFAULT is stored as SQL text in the table metadata
// (columnMeta.Default; a local-time literal as its instant, see
// creationzone.go) and checked when it is defined: it may not read
// columns, run subqueries, take parameters or call aggregates, GROUPING or
// window functions, its calls are resolved as in a query (funcs.go), and it
// is type-checked without evaluating computed values. Unknown string literals
// still take their target input type at definition time.
// A NULL default is not stored, as in Postgres. Inserted
// rows that give a column no value (or DEFAULT) get its default: INSERT …
// VALUES, INSERT … DEFAULT VALUES, INSERT … SELECT with a column list,
// MERGE's INSERT and bulk ingest of columns the Arrow data lacks. Each
// default is evaluated once per statement, so every row of a statement gets
// the same CURRENT_TIMESTAMP (which is fixed per statement anyway); only a
// volatile default (RANDOM()) is evaluated for every row.
//
// Missing values
//
// ADD COLUMN … DEFAULT v doesn't rewrite the existing rows. As Postgres does
// with its "missing value", the column records v (Missing) and the row id
// high-water mark at that moment (MissingThrough, read in the transaction
// that adds the column), and a row with __rowid <= MissingThrough that has
// no field for the column reads v. Rows inserted afterwards have higher ids,
// so for them an absent field still means NULL. The rest keeps every read
// and write consistent with that:
//
//   - A statement that read the metadata before the column was added
//     doesn't write it, but it allocates its row ids in one transaction with
//     a read of the metadata (store.insertRows), so when its ids are above
//     the mark it sees the new column and writes v.
//   - Setting the column of such an old row to NULL (UPDATE, MERGE) can't
//     just delete the field: it also writes a marker field (nullMarker),
//     which setting a value removes again (and DROP COLUMN with the field).
//   - The index has no entry for an absent field, so a predicate that v
//     satisfies is widened to the rows with __rowid <= MissingThrough in the
//     index and re-checked on the rows (widenMissing in query.go), and the
//     index never sorts or aggregates such a column.
//   - TRUNCATE drops the missing values together with the rows: the table
//     moves to a new key prefix (truncateTables), where every row is new.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// nullMarker is the HASH field that says a row's column was set to NULL,
// for a row that would otherwise read the column's missing value. Columns
// can't start with "__", so it is no column's field.
func nullMarker(field string) string { return "__null_" + field }

// missingValue returns the value a row whose HASH has no field for the
// column reads: its missing value if the row existed when the column was
// added (and hasn't set it to NULL since), otherwise NULL.
func (c columnMeta) missingValue(row aggRow) (Value, error) {
	if c.MissingThrough > 0 {
		if _, ok := row[nullMarker(c.field())]; !ok {
			if id, err := strconv.ParseInt(row[rowIDField], 10, 64); err == nil {
				return c.missingValueForID(id)
			}
		}
	}
	return nullValue(c.Type), nil
}

// The caller has validated the row ID and excluded an explicit NULL marker.
// Map and positional readers share the same default visibility/type decoder.
func (c columnMeta) missingValueForID(id int64) (Value, error) {
	if c.MissingThrough > 0 && id <= c.MissingThrough {
		return decodeStored(c.Missing, c.Type)
	}
	return nullValue(c.Type), nil
}

// readsMissing reports whether the row with the given key reads the
// column's missing value when its HASH has no field for it.
func (c columnMeta) readsMissing(meta *tableMeta, key string) bool {
	if c.MissingThrough == 0 {
		return false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(key, meta.prefix()), 10, 64)
	return err == nil && id <= c.MissingThrough
}

// addedMissing returns the fields (name, value, …) that rows written with
// meta need for the columns with a missing value that the table's current
// metadata (raw JSON) has and meta doesn't: the rows know nothing about
// those columns, which were added after meta was read.
func addedMissing(meta *tableMeta, raw string) []any {
	if !strings.Contains(raw, `"missing_through"`) {
		return nil // the common case, without decoding the metadata
	}
	var cur tableMeta
	if json.Unmarshal([]byte(raw), &cur) != nil {
		return nil
	}
	known := map[string]bool{}
	for _, c := range meta.Columns {
		known[c.field()] = true
	}
	var out []any
	for _, c := range cur.Columns {
		if c.MissingThrough > 0 && !known[c.field()] {
			out = append(out, c.field(), c.Missing)
		}
	}
	return out
}

// checkDefault checks the DEFAULT of a column definition (see the top of
// this file) and returns the text to store, "" for a literal NULL default.
// It never evaluates a computed default or consumes volatile/sequence values.
func (e *executor) checkDefault(ctx context.Context, def ColumnDef) (string, error) {
	var err error
	walkExpr(def.Default, func(x Expr) {
		if err != nil {
			return
		}
		switch v := x.(type) {
		case *ColumnRef:
			err = errorf(adbc.StatusInvalidArgument, "cannot use column reference in DEFAULT expression")
		case *Subquery:
			err = errorf(adbc.StatusInvalidArgument, "cannot use subquery in DEFAULT expression")
		case *Param:
			err = errorf(adbc.StatusInvalidArgument, "cannot use parameter in DEFAULT expression")
		case *WindowFunc:
			err = errorf(adbc.StatusInvalidArgument, "window functions are not allowed in DEFAULT expressions")
		case *Func:
			switch {
			case isSRF(v.Name):
				err = stateErrorf(stateFeatureNotSupported, "set-returning functions are not allowed in DEFAULT expressions")
			case aggregateFuncs[v.Name]:
				err = errorf(adbc.StatusInvalidArgument, "aggregate functions are not allowed in DEFAULT expressions")
			case v.Name == "GROUPING":
				err = errorf(adbc.StatusInvalidArgument, "grouping operations are not allowed in DEFAULT expressions")
			}
		}
	})
	if err == nil {
		// Its calls are checked like a query's (funcs.go): a NULL argument
		// doesn't hide an unknown function or a wrong argument count.
		err = checkCalls(def.Default)
	}
	if err != nil {
		return "", err
	}
	if err := checkUnknowns(def.Default, func() map[string]ColType { return nil }, nil); err != nil {
		return "", columnErr(def.Name, err)
	}
	// A direct unknown literal is converted by parse analysis, even in a
	// branch that will not execute. A cast of typed text remains an expression.
	walkExpr(def.Default, func(x Expr) {
		if err != nil {
			return
		}
		// Single-field INTERVAL literals use a parser-internal call. This
		// is literal input parsing, not evaluation of a computed expression.
		if f, ok := x.(*Func); ok && f.Name == "__INTERVAL" && len(f.Args) == 2 {
			value, vok := f.Args[0].(*Literal)
			unit, uok := f.Args[1].(*Literal)
			if vok && uok && value.V.T.Kind == KindString && !value.V.Null {
				if fields, ok := typmodFields[unit.V.S]; ok {
					_, err = typedIntervalLiteral(value.V.S, intervalTypmod{fields: fields, precision: -1})
				}
			}
		}
		if f, ok := x.(*Func); ok && (f.Name == "__JSON" || f.Name == "__JSONB") {
			to := typeJSON
			if f.Name == "__JSONB" {
				to = typeJSONB
			}
			from, inferErr := inferType(f.Args[0], nil, nil)
			if inferErr != nil {
				err = inferErr
				return
			}
			if !defaultCastable(from, to) {
				err = stateErr(stateCannotCoerce, "cannot cast type %s to %s", pgTypeName(from, false), pgTypeName(to, false))
				return
			}
			if unknownLiteral(f.Args[0]) {
				err = e.checkDefaultLiteral(f.Args[0].(*Literal), to)
			}
		}
		if b, ok := x.(*Binary); ok && arithmeticOps[b.Op] {
			types := inferArgTypes([]Expr{b.L, b.R}, nil, nil)
			if types[0].Kind == KindBool || types[1].Kind == KindBool {
				err = stateErr(stateUndefinedFunction, "operator does not exist: %s %s %s", sigTypeName(types[0]), b.Op, sigTypeName(types[1]))
				return
			}
		}
		if c, ok := x.(*Cast); ok && c.OnError == nil {
			from, inferErr := inferType(c.X, nil, nil)
			if inferErr != nil {
				err = inferErr
				return
			}
			canCast := defaultCastable(from, c.T)
			if !canCast {
				err = stateErr(stateCannotCoerce, "cannot cast type %s to %s", pgTypeName(from, false), pgTypeName(c.T, false))
				return
			}
			if unknownLiteral(c.X) {
				err = e.checkDefaultLiteral(c.X.(*Literal), c.T)
			}
			if c.T.Kind == KindArray {
				err = e.checkDefaultArrayLiterals(c.X, c.T)
			}
		}
	})
	if err != nil {
		// Errors inside an explicit cast retain cannot_coerce (42846);
		// only the outer assignment translates it to datatype_mismatch.
		return "", invalidArg(err)
	}
	if unknownLiteral(def.Default) {
		if err := e.checkDefaultLiteral(def.Default.(*Literal), def.Type); err != nil {
			return "", columnErr(def.Name, err)
		}
	} else {
		t, err := inferType(def.Default, nil, nil)
		if err != nil {
			return "", invalidArg(err)
		}
		if !defaultAssignable(t, def.Type) {
			return "", stateErrorf(stateDatatypeMismatch, "column %q is of type %s but default expression is of type %s", def.Name, pgTypeName(def.Type, false), pgTypeName(t, false))
		}
	}
	if l, ok := def.Default.(*Literal); ok && l.V.Null {
		return "", nil
	}
	// An unknown NULL directly coerced to its input type becomes a NULL
	// constant. Keep conversion expressions such as (NULL::integer)::text:
	// their stored type still matters to later ALTER TYPE validation.
	if c, ok := def.Default.(*Cast); ok && c.OnError == nil {
		if l, ok := c.X.(*Literal); ok && l.V.Null && l.V.T.Kind == KindNull && c.T == def.Type {
			return "", nil
		}
	}

	if f, ok := def.Default.(*Func); ok && (f.Name == "__JSON" || f.Name == "__JSONB") {
		if l, ok := f.Args[0].(*Literal); ok && l.V.Null && l.V.T.Kind == KindNull {
			t, _ := jsonFuncType(f)
			if t == def.Type {
				return "", nil
			}
		}
	}

	// Freeze local-time literals; values of computed expressions stay deferred.
	return e.foldDefault(def.DefaultText, def.Type), nil
}

// Default assignment must retain logical UUID identity and array element
// compatibility; their physical string/array kinds alone do not imply a cast.
func defaultAssignable(from, to ColType) bool {
	if from.Kind == KindNull {
		return true
	}
	if to.Alias == "uuid" {
		return from.Alias == "uuid"
	}
	if from.Kind.isJSON() && to.Kind.isJSON() {
		return true
	}
	if from.Kind == KindArray && to.Kind == KindArray {
		return defaultAssignable(from.element(), to.element())
	}
	return assignable(from, to)
}

// Explicit casts use element cast compatibility, rather than assignment
// compatibility; UUID retains its logical identity despite physical strings.
func defaultCastable(from, to ColType) bool {
	if to.Kind.isJSON() {
		return from.Kind == KindNull || from.Kind == KindString || from.Kind.isJSON()
	}

	if from.Kind == KindArray && to.Kind == KindArray {
		return defaultCastable(from.element(), to.element())
	}
	if from.Alias == "uuid" && to.Kind != KindString && to.Kind != KindNull {
		return false
	}
	if to.Alias == "uuid" && from.Kind != KindString && from.Kind != KindNull {
		return false
	}
	return castable(from, to)
}

// PostgreSQL pushes an explicit array target into ARRAY constructors while
// resolving unknown elements. Typed text expressions remain deferred casts.
func (e *executor) checkDefaultArrayLiterals(expr Expr, to ColType) error {
	f, ok := expr.(*Func)
	if !ok || f.Name != "__ARRAY" {
		return nil
	}
	for _, arg := range f.Args {
		if unknownLiteral(arg) {
			if err := e.checkDefaultLiteral(arg.(*Literal), to.element()); err != nil {
				return err
			}
		}
		if nested, ok := arg.(*Func); ok && nested.Name == "__ARRAY" {
			if err := e.checkDefaultArrayLiterals(arg, to); err != nil {
				return err
			}
		}
	}
	return nil
}

// Parse an unknown literal as its input type, without assignment typmods.
// Numeric input can be valid before precision/scale is enforced at use time.
func (e *executor) checkDefaultLiteral(l *Literal, t ColType) error {
	if t.Kind == KindDecimal {
		_, err := parseNumericText(l.V.S, decimalType(maxNumericIntDigits, 0))
		return err
	}
	_, err := coerceIn(l.V, t.withoutTypmod(), e.zone())
	return err
}

// ADD COLUMN materializes existing rows, unlike CREATE/SET DEFAULT. Keep its
// immediate value conversion separate from definition-only validation.
func (e *executor) addedDefaultValue(ctx context.Context, def ColumnDef) (Value, error) {
	v, err := e.newEnv(ctx, nil, nil).eval(def.Default)
	if err != nil {
		return Value{}, invalidArg(err)
	}
	v, err = coerceIn(v, def.Type, e.zone())
	if err != nil {
		return Value{}, columnErr(def.Name, err)
	}
	return fitLength(def.Type, v)
}

// columnDefaults computes the defaults of a table's columns for the rows of
// one statement.
type columnDefaults struct {
	meta    *tableMeta
	env     *evalEnv
	exprs   []Expr  // parsed when first needed
	vals    []Value // the defaults computed once for the statement
	done    []bool
	planned []bool
}

func (e *executor) columnDefaults(ctx context.Context, meta *tableMeta) *columnDefaults {
	n := len(meta.Columns)
	return &columnDefaults{meta: meta, env: e.newEnv(ctx, nil, nil),
		exprs: make([]Expr, n), vals: make([]Value, n), done: make([]bool, n), planned: make([]bool, n)}
}

// value returns the default of column i, coerced to its type: evaluated
// once per statement, or for every row if it is volatile.
func (d *columnDefaults) value(i int) (Value, error) {
	c := d.meta.Columns[i]
	if c.Default == "" {
		return nullValue(c.Type), nil
	}
	if d.done[i] {
		return d.vals[i], nil
	}
	if err := d.parse(i); err != nil {
		return Value{}, err
	}
	v, err := d.env.eval(d.exprs[i])
	if err != nil {
		return Value{}, invalidArg(err)
	}
	cv, err := coerceIn(v, c.Type, d.env.zone())
	if err != nil {
		return Value{}, columnErr(c.Name, err)
	}
	if !hasVolatile(d.exprs[i]) {
		d.vals[i], d.done[i] = cv, true
	}
	return cv, nil
}

// parse resolves stored definitions without consuming a default value.
func (d *columnDefaults) parse(i int) error {
	if d.exprs[i] != nil || d.meta.Columns[i].Default == "" {
		return nil
	}
	c := d.meta.Columns[i]
	x, err := parseExprText(c.Default)
	if err != nil {
		return errorf(adbc.StatusInternal, "invalid DEFAULT %q of column %q: %v", c.Default, c.Name, err)
	}
	if err := checkCalls(x); err != nil {
		return err
	}
	d.exprs[i] = x
	return nil
}

// SQL planning inspects referenced immutable defaults even with an empty source,
// as PostgreSQL does. A stable/volatile expression stays deferred; the folder's
// explicit pure-function list never executes session or sequence operations.
func (d *columnDefaults) plan(i int) error {
	if d.planned[i] || d.meta.Columns[i].Default == "" {
		return nil
	}
	if err := d.parse(i); err != nil {
		return err
	}
	v, known, err := (&casePlanner{env: d.env}).fold(d.exprs[i])
	if err != nil {
		return invalidArg(err)
	}
	if known {
		c := d.meta.Columns[i]
		v, err = coerceIn(v, c.Type, d.env.zone())
		if err != nil {
			return columnErr(c.Name, err)
		}
		v, err = fitLength(c.Type, v)
		if err != nil {
			return err
		}
		d.vals[i], d.done[i] = v, true
	}
	d.planned[i] = true
	return nil
}

func (d *columnDefaults) planInsert(targets []int, exprs []Expr, selectInput bool, override string) error {
	given := make([]bool, len(d.meta.Columns))
	for j, i := range targets {
		if override == "USER" && d.meta.Columns[i].Identity != "" {
			continue
		}
		if !selectInput {
			if j >= len(exprs) {
				continue
			}
			if _, ok := exprs[j].(*DefaultValue); ok {
				continue
			}
		}
		given[i] = true
	}
	for i, yes := range given {
		if !yes {
			if err := d.plan(i); err != nil {
				return err
			}
		}
	}
	return nil
}

// fill sets the columns of an inserted row that were given no value (given
// is false) to their defaults.
func (d *columnDefaults) fill(row []Value, given []bool) error {
	for i := range row {
		if given[i] {
			continue
		}
		v, err := d.value(i)
		if err != nil {
			return err
		}
		row[i] = v
	}
	return nil
}
