// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"math"
	"math/big"
	"slices"
	"strings"
	"unicode/utf8"

	sp "github.com/adbc-drivers/redis/go/internal/substrait/gen/substrait"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const substraitVersion = "0.108.0"
const subComparison = "extension:io.substrait:functions_comparison"
const subBoolean = "extension:io.substrait:functions_boolean"

type subType struct {
	typ      ColType
	nullable bool
}
type subExpr struct {
	expr  Expr
	t     subType
	nodes int
}
type subPlan struct {
	table  TableName
	names  []string
	input  []subType
	output []subExpr
	schema *arrow.Schema
	where  Expr
}
type subTranslator struct {
	ctx    context.Context
	funcs  map[uint32]string
	inputs []subExpr
	nodes  int
}

func subIdentifier(s string) bool {
	return s != "" && len(s) <= 1024 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func subTime(precision int32, tz bool) (ColType, error) {
	var unit arrow.TimeUnit
	switch precision {
	case 0:
		unit = arrow.Second
	case 3:
		unit = arrow.Millisecond
	case 6:
		unit = arrow.Microsecond
	case 9:
		unit = arrow.Nanosecond
	default:
		return ColType{}, subUnsupported("timestamp precision")
	}
	t := ColType{Kind: KindTimestamp, Unit: unit}
	if tz {
		t.TZ = "UTC"
	}
	return t, nil
}
func subDecimal(p, s int32) (ColType, error) {
	if p < 1 || p > 38 || s < 0 || s > p {
		return ColType{}, subInvalid("decimal type")
	}
	return decimalType(p, s), nil
}
func subTypeOf(t *sp.Type) (subType, error) {
	if t == nil || t.Kind == nil {
		return subType{}, subInvalid("type")
	}
	m := t.ProtoReflect()
	var f protoreflect.FieldDescriptor
	m.Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool { f = fd; return false })
	if f == nil {
		return subType{}, subInvalid("type kind")
	}
	v := m.Get(f).Message()
	fs := v.Descriptor().Fields()
	variation := fs.ByName("type_variation_reference")
	nullability := fs.ByName("nullability")
	if variation == nil || nullability == nil || v.Get(variation).Uint() != 0 {
		return subType{}, subUnsupported("type variation")
	}
	n := v.Get(nullability).Enum()
	if n != 1 && n != 2 {
		return subType{}, subInvalid("nullability")
	}
	out := subType{nullable: n == 1}
	var err error
	switch string(f.Name()) {
	case "bool":
		out.typ = typeBool
	case "i16":
		out.typ = ColType{Kind: KindInt16}
	case "i32":
		out.typ = ColType{Kind: KindInt32}
	case "i64":
		out.typ = ColType{Kind: KindInt64}
	case "fp32":
		out.typ = ColType{Kind: KindFloat32}
	case "fp64":
		out.typ = ColType{Kind: KindFloat64}
	case "string":
		out.typ = typeString
	case "binary":
		out.typ = typeBinary
	case "decimal":
		out.typ, err = subDecimal(int32(v.Get(fs.ByName("precision")).Int()), int32(v.Get(fs.ByName("scale")).Int()))
	case "precision_timestamp", "precision_timestamp_tz":
		out.typ, err = subTime(int32(v.Get(fs.ByName("precision")).Int()), string(f.Name()) == "precision_timestamp_tz")
	default:
		return subType{}, subUnsupported("type")
	}
	return out, err
}

// These signatures are the compound names of the pinned extension overloads,
// not arbitrary SQL function names. 'any' arguments must still have matching
// supported concrete types at every call site.
var subSignatures = map[string]string{
	"equal:any_any": "=", "not_equal:any_any": "<>", "lt:any_any": "<", "lte:any_any": "<=", "gt:any_any": ">", "gte:any_any": ">=",
	"is_null:any": "IS NULL", "is_not_null:any": "IS NOT NULL",
	"and:bool": "AND", "or:bool": "OR", "not:bool": "NOT",
}

func translateSubstrait(ctx context.Context, p *sp.Plan) (*subPlan, error) {
	v := p.Version
	if v == nil || v.MajorNumber != 0 || v.MinorNumber != 108 || v.PatchNumber != 0 || v.GitHash != "" {
		return nil, subUnsupported("version")
	}
	if p.ExecutionBehavior == nil || p.ExecutionBehavior.VariableEvalMode != sp.ExecutionBehavior_VARIABLE_EVALUATION_MODE_PER_PLAN {
		return nil, subUnsupported("execution behavior")
	}
	if len(p.ExtensionUrns) > 2 || len(p.Extensions) > 64 {
		return nil, subBudget("extensions")
	}
	urns := map[uint32]string{}
	for _, u := range p.ExtensionUrns {
		if _, ok := urns[u.ExtensionUrnAnchor]; ok {
			return nil, subInvalid("duplicate URN anchor")
		}
		if u.Urn != subComparison && u.Urn != subBoolean {
			return nil, subUnsupported("extension URN")
		}
		urns[u.ExtensionUrnAnchor] = u.Urn
	}
	tr := subTranslator{ctx: ctx, funcs: map[uint32]string{}}
	for _, e := range p.Extensions {
		f := e.GetExtensionFunction()
		if f == nil {
			return nil, subUnsupported("extension declaration")
		}
		if _, ok := tr.funcs[f.FunctionAnchor]; ok {
			return nil, subInvalid("duplicate function anchor")
		}
		urn, ok := urns[f.ExtensionUrnReference]
		if !ok {
			return nil, subInvalid("extension reference")
		}
		op, ok := subSignatures[f.Name]
		if !ok {
			return nil, subUnsupported("function signature")
		}
		isBoolean := op == "AND" || op == "OR" || op == "NOT"
		if (urn == subBoolean) != isBoolean {
			return nil, subUnsupported("function namespace")
		}
		tr.funcs[f.FunctionAnchor] = op
	}
	if len(p.Relations) != 1 || p.Relations[0].GetRoot() == nil {
		return nil, subInvalid("root")
	}
	root := p.Relations[0].GetRoot()
	rel := root.Input
	var project *sp.ProjectRel
	var filter *sp.FilterRel
	if rel != nil && rel.GetProject() != nil {
		project = rel.GetProject()
		rel = project.Input
	}
	if rel != nil && rel.GetFilter() != nil {
		filter = rel.GetFilter()
		rel = filter.Input
	}
	if rel == nil || rel.GetRead() == nil {
		return nil, subUnsupported("relation grammar")
	}
	read := rel.GetRead()
	table := read.GetNamedTable()
	if table == nil || len(table.Names) < 1 || len(table.Names) > 3 {
		return nil, subInvalid("named table")
	}
	for _, n := range table.Names {
		if !subIdentifier(n) {
			return nil, subInvalid("table component")
		}
	}
	out := &subPlan{table: TableName{Name: table.Names[len(table.Names)-1]}}
	if len(table.Names) > 1 {
		out.table.Schema = table.Names[len(table.Names)-2]
	}
	if len(table.Names) == 3 {
		out.table.Catalog = table.Names[0]
		if out.table.Catalog != catalogName {
			return nil, errorf(adbc.StatusNotFound, "Substrait: catalog not found")
		}
	}
	bs := read.BaseSchema
	if bs == nil || bs.Struct == nil || bs.Struct.Nullability != sp.Type_NULLABILITY_REQUIRED || bs.Struct.TypeVariationReference != 0 || len(bs.Names) == 0 || len(bs.Names) != len(bs.Struct.Types) {
		return nil, subInvalid("base schema")
	}
	if len(bs.Names) > 256 {
		return nil, subBudget("input_columns")
	}
	seen := map[string]bool{}
	for i, name := range bs.Names {
		if !subIdentifier(name) || seen[name] {
			return nil, subInvalid("base column name")
		}
		seen[name] = true
		typ, err := subTypeOf(bs.Struct.Types[i])
		if err != nil {
			return nil, err
		}
		out.names = append(out.names, name)
		out.input = append(out.input, typ)
		tr.inputs = append(tr.inputs, subExpr{expr: &ColumnRef{Name: name}, t: typ, nodes: 1})
	}
	if _, err := subEmit(read.Common, false, len(tr.inputs)); err != nil {
		return nil, err
	}
	filterNodes := 0
	if filter != nil {
		if _, err := subEmit(filter.Common, false, len(tr.inputs)); err != nil {
			return nil, err
		}
		x, err := tr.expression(filter.Condition, 0)
		if err != nil {
			return nil, err
		}
		if x.t.typ.Kind != KindBool {
			return nil, subInvalid("filter type")
		}
		out.where = x.expr
		filterNodes = x.nodes
	}
	vector := slices.Clone(tr.inputs)
	if project != nil {
		if len(project.Expressions) > 256 {
			return nil, subBudget("project_expressions")
		}
		for _, e := range project.Expressions {
			x, err := tr.expression(e, 0)
			if err != nil {
				return nil, err
			}
			vector = append(vector, x)
		}
		emit, err := subEmit(project.Common, true, len(vector))
		if err != nil {
			return nil, err
		}
		selected := make([]subExpr, len(emit))
		for i, n := range emit {
			selected[i] = vector[n]
		}
		vector = selected
	}
	if len(vector) == 0 || len(root.Names) != len(vector) {
		return nil, subInvalid("root names")
	}
	if len(vector) > 256 {
		return nil, subBudget("output_columns")
	}
	fields := make([]arrow.Field, len(vector))
	nodes := filterNodes
	for i, x := range vector {
		// Root names are labels, not identifiers. Empty and duplicate labels are valid.
		if len(root.Names[i]) > 1024 || !utf8.ValidString(root.Names[i]) || strings.ContainsRune(root.Names[i], 0) {
			return nil, subInvalid("output name")
		}
		nodes += x.nodes
		if nodes > 8192 {
			return nil, subBudget("translated_nodes")
		}
		fields[i] = arrowField(root.Names[i], x.t.typ, x.t.nullable)
	}
	out.output = vector
	out.schema = arrow.NewSchema(fields, nil)
	return out, nil
}
func subEmit(c *sp.RelCommon, project bool, width int) ([]int, error) {
	out := make([]int, width)
	for i := range out {
		out[i] = i
	}
	if c == nil || c.GetDirect() != nil {
		return out, nil
	}
	emit := c.GetEmit()
	if emit == nil {
		return nil, subInvalid("relation common")
	}
	if !project {
		return nil, subUnsupported("emit outside Project")
	}
	if len(emit.OutputMapping) > 256 {
		return nil, subBudget("output_columns")
	}
	out = make([]int, len(emit.OutputMapping))
	for i, n := range emit.OutputMapping {
		if n < 0 || int(n) >= width {
			return nil, subInvalid("emit ordinal")
		}
		out[i] = int(n)
	}
	return out, nil
}
func (tr *subTranslator) expression(e *sp.Expression, depth int) (subExpr, error) {
	if err := cancelled(tr.ctx); err != nil {
		return subExpr{}, err
	}
	tr.nodes++
	if tr.nodes > 4096 {
		return subExpr{}, subBudget("expression_nodes")
	}
	if depth > 32 {
		return subExpr{}, subBudget("expression_depth")
	}
	if e == nil {
		return subExpr{}, subInvalid("expression")
	}
	if lit := e.GetLiteral(); lit != nil {
		return subLiteral(lit)
	}
	if ref := e.GetSelection(); ref != nil {
		seg := ref.GetDirectReference()
		if ref.GetRootReference() == nil || seg == nil || seg.GetStructField() == nil {
			return subExpr{}, subUnsupported("field reference")
		}
		i := seg.GetStructField().Field
		if i < 0 || int(i) >= len(tr.inputs) {
			return subExpr{}, subInvalid("field ordinal")
		}
		return tr.inputs[i], nil
	}
	if cast := e.GetCast(); cast != nil {
		x, err := tr.expression(cast.Input, depth+1)
		if err != nil {
			return subExpr{}, err
		}
		t, err := subTypeOf(cast.Type)
		if err != nil {
			return subExpr{}, err
		}
		if cast.FailureBehavior != sp.Expression_Cast_FAILURE_BEHAVIOR_THROW_EXCEPTION || t != x.t {
			return subExpr{}, subUnsupported("cast")
		}
		x.nodes++
		return x, nil
	}
	f := e.GetScalarFunction()
	if f == nil {
		return subExpr{}, subUnsupported("expression")
	}
	op, ok := tr.funcs[f.FunctionReference]
	if !ok {
		return subExpr{}, subInvalid("function reference")
	}
	arity := 2
	if op == "NOT" || strings.HasPrefix(op, "IS ") {
		arity = 1
	}
	if len(f.Arguments) != arity {
		return subExpr{}, subInvalid("function arity")
	}
	args := make([]subExpr, arity)
	nodes := 1
	for i, a := range f.Arguments {
		x, err := tr.expression(a.GetValue(), depth+1)
		if err != nil {
			return subExpr{}, err
		}
		args[i] = x
		nodes += x.nodes
	}
	t := subType{typ: typeBool}
	var ast Expr
	switch op {
	case "IS NULL", "IS NOT NULL":
		ast = &IsNull{X: args[0].expr, Not: op == "IS NOT NULL"}
	case "AND", "OR", "NOT":
		for _, a := range args {
			if a.t.typ.Kind != KindBool {
				return subExpr{}, subInvalid("boolean argument")
			}
		}
		// The pinned boolean overloads declare nullable inputs/output.
		t.nullable = true
		if op == "NOT" {
			ast = &Unary{Op: "NOT", X: args[0].expr}
		} else {
			ast = &Binary{Op: op, L: args[0].expr, R: args[1].expr}
		}
	default:
		a, b := args[0].t, args[1].t
		if a.typ != b.typ {
			return subExpr{}, subInvalid("comparison types")
		}
		if a.typ.Kind.isFloat() || a.typ.Kind == KindBool && op != "=" && op != "<>" {
			return subExpr{}, subUnsupported("comparison type")
		}
		t.nullable = a.nullable || b.nullable
		ast = &Binary{Op: op, L: args[0].expr, R: args[1].expr}
	}
	declared, err := subTypeOf(f.OutputType)
	if err != nil {
		return subExpr{}, err
	}
	if declared != t {
		return subExpr{}, subInvalid("function output type")
	}
	return subExpr{expr: ast, t: t, nodes: nodes}, nil
}
func subLiteral(l *sp.Expression_Literal) (subExpr, error) {
	if l.TypeVariationReference != 0 {
		return subExpr{}, subUnsupported("literal variation")
	}
	var v Value
	var err error
	nullable := l.Nullable
	switch x := l.LiteralType.(type) {
	case *sp.Expression_Literal_Boolean:
		v = boolValue(x.Boolean)
	case *sp.Expression_Literal_I16:
		if x.I16 < math.MinInt16 || x.I16 > math.MaxInt16 {
			return subExpr{}, subInvalid("i16 literal")
		}
		v = intValue(ColType{Kind: KindInt16}, int64(x.I16))
	case *sp.Expression_Literal_I32:
		v = intValue(ColType{Kind: KindInt32}, int64(x.I32))
	case *sp.Expression_Literal_I64:
		v = intValue(ColType{Kind: KindInt64}, x.I64)
	case *sp.Expression_Literal_Fp32:
		v = floatValue(ColType{Kind: KindFloat32}, float64(x.Fp32))
	case *sp.Expression_Literal_Fp64:
		v = floatValue(ColType{Kind: KindFloat64}, x.Fp64)
	case *sp.Expression_Literal_String_:
		v = stringValue(x.String_)
	case *sp.Expression_Literal_Binary:
		v = binaryValue(string(x.Binary))
	case *sp.Expression_Literal_Null:
		t, e := subTypeOf(x.Null)
		if e != nil {
			return subExpr{}, e
		}
		if !t.nullable {
			return subExpr{}, subInvalid("required null literal")
		}
		v = nullValue(t.typ)
		nullable = true
	case *sp.Expression_Literal_Decimal_:
		d := x.Decimal
		t, e := subDecimal(d.Precision, d.Scale)
		if e != nil {
			return subExpr{}, e
		}
		if len(d.Value) != 16 {
			return subExpr{}, subInvalid("decimal literal length")
		}
		b := slices.Clone(d.Value)
		slices.Reverse(b)
		n := new(big.Int).SetBytes(b)
		if b[0]&128 != 0 {
			n.Sub(n, new(big.Int).Lsh(big.NewInt(1), 128))
		}
		if new(big.Int).Abs(new(big.Int).Set(n)).Cmp(pow10(d.Precision)) >= 0 {
			return subExpr{}, subInvalid("decimal literal range")
		}
		v = Value{T: t, D: n}
	case *sp.Expression_Literal_PrecisionTimestamp_:
		v.T, err = subTime(x.PrecisionTimestamp.Precision, false)
		v.I = x.PrecisionTimestamp.Value
	case *sp.Expression_Literal_PrecisionTimestampTz:
		v.T, err = subTime(x.PrecisionTimestampTz.Precision, true)
		v.I = x.PrecisionTimestampTz.Value
	default:
		return subExpr{}, subUnsupported("literal")
	}
	if err != nil {
		return subExpr{}, err
	}
	if len(v.S) > 65536 {
		return subExpr{}, subBudget("literal_bytes")
	}
	return subExpr{expr: &Literal{V: v}, t: subType{typ: v.T, nullable: nullable}, nodes: 1}, nil
}

func (p *subPlan) bind(ctx context.Context, e *executor) (*selectPlan, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	schema, name, err := e.resolveTable(p.table)
	if err != nil {
		return nil, subInvalid("table binding")
	}
	if schema == foreignSchema {
		return nil, subUnsupported("unregistered table")
	}
	meta, err := e.store.getTable(ctx, schema, name)
	if err != nil {
		var ae adbc.Error
		if asAdbc(err, &ae) && ae.Code == adbc.StatusNotFound {
			if ok, ve := e.store.viewExists(ctx, schema, name); ve != nil {
				return nil, ve
			} else if ok {
				return nil, subUnsupported("view")
			}
		}
		return nil, err
	}
	if meta.MaterializedSQL != "" || meta.Foreign || meta.isMem {
		return nil, subUnsupported("table kind")
	}
	if len(meta.Columns) != len(p.input) {
		return nil, subInvalid("base schema width")
	}
	for i, c := range meta.Columns {
		typ := c.Type
		typ.Text = false // TEXT/VARCHAR map to the same unbounded Substrait string.
		if c.Name != p.names[i] || typ != p.input[i].typ || c.Nullable != p.input[i].nullable {
			return nil, subInvalid("base schema mismatch")
		}
	}
	if e.cache == nil {
		e.cache = newExecCache()
	}
	e.cache.loaded = append(e.cache.loaded, meta)
	plan := &selectPlan{meta: meta, sel: &SelectStmt{From: &p.table, Where: p.where}}
	for i, x := range p.output {
		plan.items = append(plan.items, planItem{expr: x.expr, name: p.schema.Field(i).Name, typ: x.t.typ})
	}
	return plan, nil
}
