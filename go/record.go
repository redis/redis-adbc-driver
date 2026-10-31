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

// Rows as values: Postgres's anonymous records.
//
// A row constructor that the parser doesn't rewrite into comparisons of its
// items (rowvalue.go) is a value of type record (KindRecord): `SELECT (a,
// b)`, `ORDER BY ROW(a, b)`, a row nested in another, a row in a derived
// table's select list. Its fields are named f1, f2, … as in Postgres, and
// (r).f1 reads one.
//
//   - Output: a record is an Arrow struct whose children are named f1 … fn
//     and are each nullable, with the fields' own Arrow types (a nested
//     record is a nested struct). A NULL record is a NULL struct; a record
//     whose fields are all NULL is not NULL, as in Postgres. The column of
//     `SELECT (a, b)` is named "row".
//   - Text: as Postgres's record_out writes it, `(1,"a b",,t)`: a NULL
//     field is empty, and a field is quoted when it is empty or has a quote,
//     a backslash, a parenthesis, a comma or white space, with quotes and
//     backslashes doubled. Fields are written as their types' output
//     functions write them: t / f for booleans, \x and hex digits for binary
//     values, a CHAR value with its padding.
//   - Comparison of two record values (a nested row, a record column,
//     sorting, DISTINCT, GROUP BY) is Postgres's btree record comparison:
//     field by field, a NULL field equal to a NULL field and greater than
//     any value. So it is never NULL unless a record is. Records of
//     different lengths whose common fields are equal can't be compared.
//     A comparison of two row constructors is still rewritten into
//     comparisons of their items, with SQL's NULL rules.
//   - IS NULL is true for a NULL record or one whose fields are all NULL,
//     IS NOT NULL for a record none of whose fields is NULL; a field that is
//     a record is not NULL, whatever its fields are.
//   - An anonymous SQL record can't be stored: a table column, CREATE TABLE … AS and
//     CREATE VIEW can't have type record, nor can a value written to a
//     column be one. Named Arrow structs use stored_struct.go and have a
//     separate persisted field schema.

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// ---- types ----

// recordType is the field types of a record type. recordOf interns them,
// so that record types with the same fields are the same ColType.
type recordType struct {
	named  []structField
	fields []ColType
}

var recordTypes sync.Map // recordSignature → *recordType

// recordOf is the record type with the given field types.
func recordOf(fields []ColType) ColType {
	key := recordSignature(fields)
	r, ok := recordTypes.Load(key)
	if !ok {
		r, _ = recordTypes.LoadOrStore(key, &recordType{fields: append([]ColType(nil), fields...)})
	}
	return ColType{Kind: KindRecord, Rec: r.(*recordType)}
}

// recordSignature identifies a list of field types. A nested record's type
// is interned, so its pointer identifies it.
func recordSignature(fields []ColType) string {
	var b strings.Builder
	for _, t := range fields {
		fmt.Fprintf(&b, "%d/%d/%s/%d/%d/%d/%t/%t/%s/%s/%p;", t.Kind, t.Unit, t.TZ, t.Precision, t.Scale, t.Length, t.Fixed, t.Text, t.Alias, t.Collation, t.Rec)
	}
	return b.String()
}

// fieldTypes are the record's field types: none for a record whose fields
// aren't known.
func (r *recordType) fieldTypes() []ColType {
	if r == nil {
		return nil
	}
	return r.fields
}

// recordFieldName is the name of field i (from 0), as Postgres names an
// anonymous record's fields.
func recordFieldName(i int) string { return "f" + strconv.Itoa(i+1) }

// recordFieldIndex is the field (from 0) that name names, -1 if it names
// none: f1, f2, … in any case.
func recordFieldIndex(name string) int {
	if len(name) < 2 || (name[0] != 'f' && name[0] != 'F') || name[1] < '1' || name[1] > '9' {
		return -1
	}
	n, err := strconv.Atoi(name[1:])
	if err != nil || n < 1 {
		return -1
	}
	return n - 1
}

func (r *recordType) arrowType() arrow.DataType {
	fields := make([]arrow.Field, len(r.fieldTypes()))
	for i, t := range r.fieldTypes() {
		name, nullable := recordFieldName(i), true
		if r.named != nil {
			name, nullable = r.named[i].Name, r.named[i].Nullable
		}
		fields[i] = arrowField(name, t, nullable)
	}
	return arrow.StructOf(fields...)
}

// commonRecord is the type that holds the records of types a and b: the
// common type of each field when they have as many, and otherwise a (whose
// values b's can't be converted to).
func commonRecord(a, b ColType) ColType {
	af, bf := a.Rec.fieldTypes(), b.Rec.fieldTypes()
	switch {
	case a.Rec == nil:
		return b
	case b.Rec == nil || len(af) != len(bf):
		return a
	}
	out := make([]ColType, len(af))
	for i := range af {
		out[i] = commonType(af[i], bf[i])
	}
	return recordOf(out)
}

// ---- values ----

// recordValue is the record with the given field values.
func recordValue(fields []Value) Value { return Value{T: typeRecord, rec: &fields} }

// fields are a record's field values.
func (v Value) fields() []Value {
	if v.rec == nil {
		return nil
	}
	return *v.rec
}

// coerceRecord converts record v to record type t: each field to its type.
func coerceRecord(v Value, t ColType, z tzZone) (Value, error) {
	if t.Rec == nil {
		return v, nil
	}
	if t.namedRecord() && v.T.Rec != t.Rec {
		return Value{}, stateErr(stateDatatypeMismatch, "stored struct schema must match field names, types and nullability")
	}
	in, types := v.fields(), t.Rec.fields
	if len(in) != len(types) {
		return Value{}, stateErr(stateDatatypeMismatch, "cannot convert record value %q of %d fields to a record of %d", v.textIn(z), len(in), len(types))
	}
	out := make([]Value, len(in))
	for i, f := range in {
		if t.namedRecord() && f.Null && !t.Rec.named[i].Nullable {
			return Value{}, stateErr(stateNotNull, "struct field %q must not be NULL", t.Rec.named[i].Name)
		}
		c, err := coerceIn(f, types[i], z)
		if err != nil {
			return Value{}, err
		}
		out[i] = c
	}
	v.rec = &out
	if t.namedRecord() {
		v.T = t
	}
	return v, nil
}

// recordText is a record as Postgres's record_out writes it, in the session
// time zone z (see the top of this file).
func recordText(v Value, z tzZone) string {
	var b strings.Builder
	b.WriteByte('(')
	for i, f := range v.fields() {
		if i > 0 {
			b.WriteByte(',')
		}
		if f.Null {
			continue
		}
		s := fieldText(f, z)
		quote := s == ""
		for j := 0; j < len(s) && !quote; j++ {
			switch s[j] {
			case '"', '\\', '(', ')', ',', ' ', '\t', '\n', '\v', '\f', '\r':
				quote = true
			}
		}
		if quote {
			b.WriteByte('"')
		}
		for j := 0; j < len(s); j++ {
			if s[j] == '"' || s[j] == '\\' {
				b.WriteByte(s[j])
			}
			b.WriteByte(s[j])
		}
		if quote {
			b.WriteByte('"')
		}
	}
	b.WriteByte(')')
	return b.String()
}

// fieldText is a non-NULL field of a record as its type's output function
// writes it, which for some types isn't its text as a cast to text gives it.
func fieldText(v Value, z tzZone) string {
	switch {
	case v.T.Kind == KindBool:
		if v.I != 0 {
			return "t"
		}
		return "f"
	case v.T.Kind == KindBinary:
		return `\x` + hex.EncodeToString([]byte(v.S))
	case v.T.isChar():
		return v.S // with its padding
	}
	return v.textIn(z)
}

// compareRecords compares two non-NULL records as Postgres's btree record
// comparison does (see the top of this file); ok is false when a pair of
// fields can't be compared, or when the records have different lengths and
// their common fields are equal.
func compareRecords(a, b Value, z tzZone) (int, bool) {
	af, bf := a.fields(), b.fields()
	for i := 0; i < len(af) && i < len(bf); i++ {
		x, y := af[i], bf[i]
		switch {
		case x.Null && y.Null:
			continue
		case x.Null:
			return 1, true
		case y.Null:
			return -1, true
		}
		c, ok := compareIn(x, y, z)
		if !ok {
			return 0, false
		}
		if c != 0 {
			return c, true
		}
	}
	return 0, len(af) == len(bf)
}

// recordCompareError is the error for two values that compareIn can't
// compare, at least one of them a record (nil if neither is).
func recordCompareError(a, b Value) error {
	ak, bk := a.T.Kind, b.T.Kind
	switch {
	case ak == KindRecord && bk == KindRecord:
		af, bf := a.fields(), b.fields()
		if len(af) != len(bf) {
			return stateErr(stateDatatypeMismatch, "cannot compare record types with different numbers of columns")
		}
		for i := range af {
			if !af[i].Null && !bf[i].Null {
				if _, ok := compareIn(af[i], bf[i], utcZone); !ok {
					return stateErr(stateDatatypeMismatch, "cannot compare dissimilar column types %s and %s at record column %d",
						sigTypeName(af[i].T), sigTypeName(bf[i].T), i+1)
				}
			}
		}
		return stateErr(stateDatatypeMismatch, "cannot compare record with record")
	case ak == KindRecord || bk == KindRecord:
		return stateErr(stateUndefinedFunction, "operator does not exist: %s = %s", sigTypeName(a.T), sigTypeName(b.T))
	}
	return nil
}

// recordIsNull evaluates `v IS [NOT] NULL` for a record v (see the top of
// this file).
func recordIsNull(v Value, not bool) bool {
	if v.Null {
		return !not
	}
	for _, f := range v.fields() {
		if f.Null != !not {
			return false
		}
	}
	return true
}

// recordJoinKey is the key of a non-NULL record for hashing (joinKey):
// records compare equal exactly when their keys are equal, NULL fields
// included.
func recordJoinKey(v Value) string {
	var b strings.Builder
	b.WriteByte('r')
	for _, f := range v.fields() {
		if f.Null {
			b.WriteString("N;")
			continue
		}
		k, _ := joinKey(f)
		b.WriteString(strconv.Itoa(len(k)))
		b.WriteByte(':')
		b.WriteString(k)
	}
	return b.String()
}

// writeJSONRecord writes a record as Postgres's to_json does: an object of
// its fields, {"f1":1,"f2":"a"}.
func writeJSONRecord(b *strings.Builder, v Value, z tzZone) {
	b.WriteByte('{')
	for i, f := range v.fields() {
		if i > 0 {
			b.WriteByte(',')
		}
		name := recordFieldName(i)
		if v.T.namedRecord() {
			name = v.T.Rec.named[i].Name
		}
		writeJSONString(b, name)
		b.WriteByte(':')
		writeJSONValue(b, f, false, z)
	}
	b.WriteByte('}')
}

// appendRecord appends a non-NULL record (already coerced to t) to a
// struct builder created for t.ArrowType().
func appendRecord(b *array.StructBuilder, t ColType, v Value) error {
	types, fields := t.Rec.fieldTypes(), v.fields()
	if len(fields) != len(types) {
		return fmt.Errorf("record value of %d fields in a column of %d", len(fields), len(types))
	}
	b.Append(true)
	for i, f := range fields {
		if err := appendValue(b.FieldBuilder(i), types[i], f); err != nil {
			return err
		}
	}
	return nil
}

// ---- field selection ----

// FieldSelect is (x).name, a field of record x. An anonymous record's
// fields are f1, f2, … (Index is the one name names, from 0, or -1).
type FieldSelect struct {
	X     Expr
	Name  string
	Index int
}

func (*FieldSelect) exprNode() {}

// fieldSelectType is the type of (x).name for x of type t.
func fieldSelectType(fs *FieldSelect, t ColType) (ColType, error) {
	if t.Kind != KindRecord {
		return ColType{}, stateErrorf(stateWrongObjectType,
			"column notation .%s applied to type %s, which is not a composite type", fs.Name, sigTypeName(t))
	}
	types := t.Rec.fieldTypes()
	if t.namedRecord() {
		fs.Index = -1
		for i, f := range t.Rec.named {
			if f.Name == fs.Name {
				fs.Index = i
				break
			}
		}
	}
	if fs.Index < 0 || fs.Index >= len(types) {
		return ColType{}, stateErrorf(stateUndefinedColumn, "could not identify column %q in record data type", fs.Name)
	}
	return types[fs.Index], nil
}

func (env *evalEnv) evalFieldSelect(fs *FieldSelect) (Value, error) {
	v, err := env.eval(fs.X)
	if err != nil {
		return Value{}, err
	}
	if v.Null {
		return nullValue(typeNull), nil
	}
	if v.T.Kind != KindRecord {
		_, err := fieldSelectType(fs, v.T)
		return Value{}, err
	}
	f := v.fields()
	if fs.Index < 0 || fs.Index >= len(f) {
		return Value{}, stateErr(stateUndefinedColumn, "could not identify column %q in record data type", fs.Name)
	}
	return f[fs.Index], nil
}

// ---- where records can't go ----

// pseudoTypeError is Postgres's error for a column of type record in a
// table or view.
func pseudoTypeError(name string) error {
	return stateErrorf(stateTableDefinition, "column %q has pseudo-type record", name)
}

// checkStorableColumns reports the first column of a query's result that a
// table or view can't have (pseudoTypeError).
func checkStorableColumns(cols []resultColumn) error {
	for _, c := range cols {
		if c.Type.Kind == KindRecord && !c.Type.namedRecord() {
			return pseudoTypeError(c.Name)
		}
	}
	return nil
}

// recordColumnError is Postgres's error for a record written to a column
// (INSERT, UPDATE, MERGE), nil if t isn't a record.
func recordColumnError(col columnMeta, t ColType) error {
	if t.Kind != KindRecord || col.Type.namedRecord() && t == col.Type {
		return nil
	}
	return stateErrorf(stateDatatypeMismatch, "column %q is of type %s but expression is of type record", col.Name, sigTypeName(col.Type))
}

// recordWriteError is recordColumnError for a row constructor x written to
// column col (other records are found when they are converted).
func recordWriteError(col columnMeta, x Expr) error {
	if _, ok := x.(*RowExpr); ok {
		return recordColumnError(col, typeRecord)
	}
	return nil
}

// ---- binding ----

// checkRecords reports, once expr is bound (so that the types of its parts
// are known), the first use of a record where Postgres has none: a
// function or operator that doesn't take one, a cast to a type other than
// text or RECORD, a CASE of records and other values, a record as a
// condition of CASE or NOT, and (x).f of a value that isn't a record or a
// field it hasn't. (Operators with a row operand are checked while they
// are bound, by bindRowOperator.)
func (e *executor) checkRecords(expr Expr) error {
	var err error
	walkExprPruned(expr, func(x Expr) bool {
		if err != nil {
			return false
		}
		switch v := x.(type) {
		case *Unary:
			if _, ok := e.recordTypeOf(v.X); ok {
				err = recordUnaryError(v.Op)
			}
		case *Cast:
			if t, ok := e.recordTypeOf(v.X); ok && !castable(t, v.T) {
				err = stateErrorf(stateCannotCoerce, "cannot cast type record to %s", sigTypeName(v.T))
			}
		case *FieldSelect:
			err = e.fieldSelectError(v)
		case *Case:
			err = e.caseRecordError(v)
		case *Func:
			err = e.recordArgError(v)
		}
		return err == nil
	})
	return err
}

// fieldSelectError is the error for (x).name where x isn't a record or has
// no such field, nil otherwise.
func (e *executor) fieldSelectError(fs *FieldSelect) error {
	var t ColType
	if c, ok := fs.X.(*ColumnRef); ok {
		var found bool
		if t, found = e.refType(c); !found {
			return nil
		}
	} else {
		var err error
		if t, err = inferType(fs.X, e.scopeTypes(), e.paramTypes); err != nil {
			return nil // reported where its type is needed
		}
	}
	_, err := fieldSelectType(fs, t)
	return err
}

// mayBeRecord reports whether x may be of type record, so that the checks
// below infer its type only then.
func mayBeRecord(x Expr) bool {
	switch v := x.(type) {
	case *RowExpr, *FieldSelect, *ColumnRef, *Case, *RowColumn, *WindowFunc:
		return true
	case *Subquery:
		return v.Kind == SubqueryScalar
	case *Cast:
		return v.T.Kind == KindRecord
	case *Func:
		d, ok := callee(v)
		return ok && d.records
	}
	return false
}

// recordTypeOf is the type of x if it is a record, as bound in the current
// scope; ok is false otherwise.
func (e *executor) recordTypeOf(x Expr) (ColType, bool) {
	if c, ok := x.(*ColumnRef); ok {
		t, ok := e.refType(c)
		return t, ok && t.Kind == KindRecord
	}
	if !mayBeRecord(x) {
		return ColType{}, false
	}
	t, err := inferType(x, e.scopeTypes(), e.paramTypes)
	return t, err == nil && t.Kind == KindRecord
}

// refType is the type of a bound column reference, found without building
// scopeTypes' map; ok is false if it isn't found.
func (e *executor) refType(c *ColumnRef) (ColType, bool) {
	if c.Outer > 0 {
		return c.OuterType, true
	}
	if n := len(e.scopes); n > 0 {
		for _, rel := range e.scopes[n-1].rels {
			if !strings.HasPrefix(c.Name, rel.prefix) {
				continue
			}
			if col, ok := rel.meta.column(c.Name[len(rel.prefix):]); ok && rel.prefix+col.Name == c.Name {
				return col.Type, true
			}
		}
	}
	return ColType{}, false
}

// recordUnaryError is the error for the unary operator op (NOT or -)
// applied to a record.
func recordUnaryError(op string) error {
	if op == "NOT" {
		return stateErrorf(stateDatatypeMismatch, "argument of NOT must be type boolean, not type record")
	}
	return stateErrorf(stateUndefinedFunction, "operator does not exist: %s record", op)
}

// recordArgError is the error for a call (bound) with a record argument
// that its function doesn't take: Postgres's `function upper(record) does
// not exist`, or for an operator `operator does not exist: record ~~
// varchar`. Functions that take any type (funcDef.records) take records.
func (e *executor) recordArgError(f *Func) error {
	d, ok := callee(f)
	if !ok {
		return nil
	}
	// The functions whose result is one of their arguments need them all
	// records, or none.
	unify := d.impl == implCoalesce || f.Name == "GREATEST" || f.Name == "LEAST"
	if d.records && !unify {
		return nil
	}
	found := false
	for _, a := range f.Args {
		if _, ok := e.recordTypeOf(a); ok {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	types := inferArgTypes(f.Args, e.scopeTypes(), e.paramTypes)
	if unify {
		return unifyRecordError(f.Name, types)
	}
	op := ""
	switch f.Name {
	case "LIKE":
		op = "~~"
	case "ILIKE":
		op = "~~*"
	case "~", "~*", "->", "->>", "#>", "#>>":
		op = f.Name
	case "SIMILAR TO":
		op = "~"
	case "__JSON", "__JSONB":
		return stateErrorf(stateCannotCoerce, "cannot cast type record to %s", strings.ToLower(f.Name[2:]))
	case "__IS_JSON":
		return stateErrorf(stateDatatypeMismatch, "cannot use type record in IS JSON predicate")
	}
	if op != "" && len(types) >= 2 {
		return stateErrorf(stateUndefinedFunction, "operator does not exist: %s %s %s", sigTypeName(types[0]), op, sigTypeName(types[1]))
	}
	return noSuchFunction(f.Name, f.Star, types)
}

// unifyRecordError is Postgres's error for values of type record and of
// another type that one result holds (what is CASE, COALESCE, …): `CASE
// types record and bigint cannot be matched`. types are the values' types,
// in order; NULLs go with any type.
func unifyRecordError(what string, types []ColType) error {
	var first ColType
	have := false
	for _, t := range types {
		switch {
		case t.Kind == KindNull:
		case !have:
			first, have = t, true
		case (first.Kind == KindRecord) != (t.Kind == KindRecord):
			return stateErrorf(stateDatatypeMismatch, "%s types %s and %s cannot be matched", what, sigTypeName(first), sigTypeName(t))
		}
	}
	return nil
}

// caseRecordError is the error for a CASE whose results are records and
// other values, or a searched CASE with a record as a WHEN condition; nil
// otherwise.
func (e *executor) caseRecordError(c *Case) error {
	// As Postgres finds the result type: the ELSE first, then the THENs.
	var results []Expr
	if c.Else != nil {
		results = append(results, c.Else)
	}
	for _, w := range c.Whens {
		if c.Operand == nil {
			if _, ok := e.recordTypeOf(w.When); ok {
				return stateErrorf(stateDatatypeMismatch, "argument of CASE/WHEN must be type boolean, not type record")
			}
		}
		results = append(results, w.Then)
	}
	for _, r := range results {
		if _, ok := e.recordTypeOf(r); ok {
			return unifyRecordError("CASE", inferArgTypes(results, e.scopeTypes(), e.paramTypes))
		}
	}
	return nil
}

// recordOperandError is the error for an IN / ANY / ALL subquery whose
// operand, or an item of its row operand, is a record and its column isn't,
// or the other way round: `operator does not exist: record = integer`. It
// is checked once the subquery is planned, in the scope of its operand.
func (e *executor) recordOperandError(sq *Subquery, plan *selectPlan) error {
	for i, it := range subqueryOperands(sq) {
		if i >= len(plan.items) {
			break
		}
		ct := plan.items[i].typ
		lt, rec := e.recordTypeOf(it)
		if !rec {
			if ct.Kind != KindRecord {
				continue
			}
			t, err := inferType(it, e.scopeTypes(), e.paramTypes)
			if err != nil {
				continue
			}
			lt = t
		}
		if lt.Kind == KindNull || ct.Kind == KindNull || (lt.Kind == KindRecord) == (ct.Kind == KindRecord) {
			continue
		}
		op := sq.Op
		if sq.Kind == SubqueryIn {
			op = "="
		}
		return stateErrorf(stateUndefinedFunction, "operator does not exist: %s %s %s", sigTypeName(lt), op, sigTypeName(ct))
	}
	return nil
}

// conditionTypeError is Postgres's error for a record used as the condition
// of clause (WHERE, HAVING, QUALIFY, JOIN conditions), once it is bound;
// nil for other types.
func (e *executor) conditionTypeError(x Expr, clause string) error {
	if _, ok := e.recordTypeOf(x); !ok {
		return nil
	}
	if clause == "JOIN conditions" {
		clause = "JOIN/ON"
	}
	return stateErrorf(stateDatatypeMismatch, "argument of %s must be type boolean, not type record", clause)
}
