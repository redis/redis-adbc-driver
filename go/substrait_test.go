// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sp "github.com/adbc-drivers/redis/go/internal/substrait/gen/substrait"
	ext "github.com/adbc-drivers/redis/go/internal/substrait/gen/substrait/extensions"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func subTestPlan(t testing.TB, table string, names []string, types ...string) *sp.Plan {
	t.Helper()
	schema := make([]json.RawMessage, len(types))
	for i, s := range types {
		schema[i] = json.RawMessage(s)
	}
	body := map[string]any{"version": map[string]any{"minorNumber": 108, "producer": "redis-adbc bounded-subset fixture generator"}, "executionBehavior": map[string]any{"variableEvalMode": "VARIABLE_EVALUATION_MODE_PER_PLAN"}, "relations": []any{map[string]any{"root": map[string]any{"names": names, "input": map[string]any{"read": map[string]any{"namedTable": map[string]any{"names": []string{table}}, "baseSchema": map[string]any{"names": names, "struct": map[string]any{"nullability": "NULLABILITY_REQUIRED", "types": schema}}}}}}}}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	p := new(sp.Plan)
	if err := protojson.Unmarshal(b, p); err != nil {
		t.Fatal(err)
	}
	return p
}

const subI64 = `{"i64":{"nullability":"NULLABILITY_REQUIRED"}}`
const subNullableI64 = `{"i64":{"nullability":"NULLABILITY_NULLABLE"}}`
const subStr = `{"string":{"nullability":"NULLABILITY_NULLABLE"}}`

func subBytes(t testing.TB, p *sp.Plan) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func subRef(i int32) *sp.Expression {
	return &sp.Expression{RexType: &sp.Expression_Selection{Selection: &sp.Expression_FieldReference{ReferenceType: &sp.Expression_FieldReference_DirectReference{DirectReference: &sp.Expression_ReferenceSegment{ReferenceType: &sp.Expression_ReferenceSegment_StructField_{StructField: &sp.Expression_ReferenceSegment_StructField{Field: i}}}}, RootType: &sp.Expression_FieldReference_RootReference_{RootReference: &sp.Expression_FieldReference_RootReference{}}}}}
}
func subLit(l *sp.Expression_Literal) *sp.Expression {
	return &sp.Expression{RexType: &sp.Expression_Literal_{Literal: l}}
}
func subBoolType(nullable bool) *sp.Type {
	n := sp.Type_NULLABILITY_REQUIRED
	if nullable {
		n = sp.Type_NULLABILITY_NULLABLE
	}
	return &sp.Type{Kind: &sp.Type_Bool{Bool: &sp.Type_Boolean{Nullability: n}}}
}
func subProject(p *sp.Plan, names []string, expressions []*sp.Expression, emit ...int32) {
	root := p.Relations[0].GetRoot()
	root.Input = &sp.Rel{RelType: &sp.Rel_Project{Project: &sp.ProjectRel{Input: root.Input, Expressions: expressions, Common: &sp.RelCommon{EmitKind: &sp.RelCommon_Emit_{Emit: &sp.RelCommon_Emit{OutputMapping: emit}}}}}}
	root.Names = names
}
func subFunctionPlan(p *sp.Plan, signature, urn string, args []*sp.Expression, out *sp.Type) *sp.Expression {
	p.ExtensionUrns = []*ext.SimpleExtensionURN{{ExtensionUrnAnchor: 0, Urn: urn}}
	p.Extensions = []*ext.SimpleExtensionDeclaration{{MappingType: &ext.SimpleExtensionDeclaration_ExtensionFunction_{ExtensionFunction: &ext.SimpleExtensionDeclaration_ExtensionFunction{ExtensionUrnReference: 0, FunctionAnchor: 0, Name: signature}}}}
	a := make([]*sp.FunctionArgument, len(args))
	for i, x := range args {
		a[i] = &sp.FunctionArgument{ArgType: &sp.FunctionArgument_Value{Value: x}}
	}
	return &sp.Expression{RexType: &sp.Expression_ScalarFunction_{ScalarFunction: &sp.Expression_ScalarFunction{FunctionReference: 0, Arguments: a, OutputType: out}}}
}
func TestSubstraitReadProjectTypes(t *testing.T) {
	p := subTestPlan(t, "literal.table", []string{"id", "text"}, subI64, subStr)
	n := subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_Null{Null: subBoolType(true)}})
	subProject(p, []string{"same", "same", "null"}, []*sp.Expression{n}, 1, 0, 2)
	decoded, err := decodeSubstrait(context.Background(), subBytes(t, p))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.table.Name != "literal.table" || decoded.schema.NumFields() != 3 || decoded.schema.Field(1).Nullable || !decoded.schema.Field(2).Nullable || decoded.schema.Field(2).Type.ID() != arrow.BOOL {
		t.Fatal(decoded.schema)
	}
	env := &evalEnv{row: map[string]Value{"id": intValue(ColType{Kind: KindInt64}, 9007199254740993), "text": stringValue("x\x00y")}}
	for i, want := range []Value{stringValue("x\x00y"), intValue(ColType{Kind: KindInt64}, 9007199254740993), nullValue(typeBool)} {
		got, err := env.eval(decoded.output[i].expr)
		if err != nil || got.I != want.I || got.S != want.S || got.Null != want.Null {
			t.Fatalf("%d: %+v, %v", i, got, err)
		}
	}
}
func TestSubstraitRejectBeforeRedis(t *testing.T) {
	base := subTestPlan(t, "t", []string{"id"}, subI64)
	cases := map[string]func(*sp.Plan){
		"version":  func(p *sp.Plan) { p.Version.MinorNumber = 61 },
		"behavior": func(p *sp.Plan) { p.ExecutionBehavior = nil },
		"file": func(p *sp.Plan) {
			p.Relations[0].GetRoot().Input.GetRead().ReadType = &sp.ReadRel_LocalFiles_{LocalFiles: &sp.ReadRel_LocalFiles{}}
		},
		"relation": func(p *sp.Plan) { p.Relations[0].GetRoot().Input = &sp.Rel{RelType: &sp.Rel_Join{Join: &sp.JoinRel{}}} },
		"schema": func(p *sp.Plan) {
			p.Relations[0].GetRoot().Input.GetRead().BaseSchema.Struct.Nullability = sp.Type_NULLABILITY_UNSPECIFIED
		},
		"ordinal": func(p *sp.Plan) { subProject(p, []string{"bad"}, []*sp.Expression{subRef(1)}, 1) },
		"emit":    func(p *sp.Plan) { subProject(p, []string{"bad"}, nil, -1) },
		"nullable": func(p *sp.Plan) {
			p.Relations[0].GetRoot().Input.GetRead().BaseSchema.Struct.Types[0].GetI64().Nullability = 0
		},
		"uri":                func(p *sp.Plan) { p.ExtensionUrns = []*ext.SimpleExtensionURN{{Urn: "https://example.com/code"}} },
		"unused declaration": func(p *sp.Plan) { subFunctionPlan(p, "random:", subComparison, nil, subBoolType(false)) },
		"duplicate anchor": func(p *sp.Plan) {
			subFunctionPlan(p, "equal:any_any", subComparison, nil, subBoolType(false))
			p.Extensions = append(p.Extensions, p.Extensions[0])
		},
		"parameter": func(p *sp.Plan) {
			subProject(p, []string{"bad"}, []*sp.Expression{{RexType: &sp.Expression_DynamicParameter{DynamicParameter: &sp.DynamicParameter{}}}}, 1)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := proto.Clone(base).(*sp.Plan)
			mutate(p)
			if _, err := decodeSubstrait(context.Background(), subBytes(t, p)); err == nil {
				t.Fatal("accepted unsupported plan")
			}
		})
	}
	data := subBytes(t, base)
	bad := map[string][]byte{"empty": nil, "truncated": data[:len(data)-1], "duplicate": append(slices.Clone(data), data...), "unknown": protowire.AppendVarint(protowire.AppendTag(slices.Clone(data), 123, protowire.VarintType), 1), "oversize": make([]byte, SubstraitMaxPlanBytes+1)}
	for name, b := range bad {
		t.Run(name, func(t *testing.T) {
			s := &statementImpl{query: "SELECT 7"}
			err := s.SetSubstraitPlan(context.Background(), b)
			if err == nil || s.query != "SELECT 7" || s.substrait != nil {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decodeSubstrait(ctx, data); !isStatus(err, adbc.StatusCancelled) {
		t.Fatal(err)
	}
}
func TestSubstraitExactLiterals(t *testing.T) {
	tr := subTranslator{ctx: context.Background()}
	max := new(big.Int).Sub(pow10(38), big.NewInt(1))
	le := make([]byte, 16)
	b := max.Bytes()
	for i := range b {
		le[i] = b[len(b)-1-i]
	}
	tests := []*sp.Expression_Literal{
		{LiteralType: &sp.Expression_Literal_Decimal_{Decimal: &sp.Expression_Literal_Decimal{Value: le, Precision: 38, Scale: 9}}},
		{LiteralType: &sp.Expression_Literal_PrecisionTimestampTz{PrecisionTimestampTz: &sp.Expression_Literal_PrecisionTimestamp{Precision: 9, Value: -1234567890123456789}}},
		{LiteralType: &sp.Expression_Literal_Fp64{Fp64: math.Copysign(0, -1)}},
	}
	for _, lit := range tests {
		x, err := tr.expression(subLit(lit), 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (&evalEnv{}).eval(x.expr); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := subLiteral(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_I16{I16: 32768}}); err == nil {
		t.Fatal("i16 overflow")
	}
}
func TestSubstraitThreeValuedFunctions(t *testing.T) {
	for _, op := range []string{"and", "or"} {
		for _, a := range []int{-1, 0, 1} {
			for _, b := range []int{-1, 0, 1} {
				literal := func(v int) *sp.Expression {
					if v < 0 {
						return subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_Null{Null: subBoolType(true)}})
					}
					return subLit(&sp.Expression_Literal{Nullable: true, LiteralType: &sp.Expression_Literal_Boolean{Boolean: v == 1}})
				}
				p := subTestPlan(t, "t", []string{"id"}, subI64)
				x := subFunctionPlan(p, op+":bool", subBoolean, []*sp.Expression{literal(a), literal(b)}, subBoolType(true))
				subProject(p, []string{"result"}, []*sp.Expression{x}, 1)
				plan, err := decodeSubstrait(context.Background(), subBytes(t, p))
				if err != nil {
					t.Fatal(err)
				}
				got, err := (&evalEnv{}).eval(plan.output[0].expr)
				if err != nil {
					t.Fatal(err)
				}
				want := -1
				if op == "and" {
					if a == 0 || b == 0 {
						want = 0
					} else if a == 1 && b == 1 {
						want = 1
					}
				} else {
					if a == 1 || b == 1 {
						want = 1
					} else if a == 0 && b == 0 {
						want = 0
					}
				}
				if got.Null != (want < 0) || !got.Null && got.I != int64(want) {
					t.Fatalf("%s %d %d: %+v want %d", op, a, b, got, want)
				}
			}
		}
	}
}
func TestSubstraitBudgets(t *testing.T) {
	b := &subWork{rows: subMaxRows - 1, bytes: subMaxBytes - 1}
	if err := b.charge(1, 1); err != nil {
		t.Fatal(err)
	}
	if b.charge(1, 0) == nil || b.charge(0, 1) == nil {
		t.Fatal("budget overflow")
	}
	p := subTestPlan(t, "t", []string{"id"}, subI64)
	s := p.Relations[0].GetRoot().Input.GetRead().BaseSchema
	for len(s.Names) < 257 {
		s.Names = append(s.Names, "x")
		s.Struct.Types = append(s.Struct.Types, s.Struct.Types[0])
	}
	if _, err := decodeSubstrait(context.Background(), subBytes(t, p)); err == nil {
		t.Fatal("wide schema accepted")
	}
}
func TestSubstraitFixtures(t *testing.T) {
	p := subTestPlan(t, "it_substrait", []string{"id", "value"}, subI64, subStr)
	data := subBytes(t, p)
	dir := "testdata/substrait"
	path := filepath.Join(dir, "read.bin")
	if os.Getenv("UPDATE_SUBSTRAIT_FIXTURES") == "1" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		b, _ := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(p)
		if err := os.WriteFile(filepath.Join(dir, "read.json"), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(data, fixture) {
		t.Fatal("fixture differs from pinned producer")
	}
	if _, err := decodeSubstrait(context.Background(), fixture); err != nil {
		t.Fatal(err)
	}
}
func FuzzSubstrait(f *testing.F) {
	f.Add([]byte{})
	if b, err := os.ReadFile("testdata/substrait/read.bin"); err == nil {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > SubstraitMaxPlanBytes+1 {
			return
		}
		_, _ = decodeSubstrait(context.Background(), b)
	})
}

func TestSubstraitIdentityCastsAndFloatProjection(t *testing.T) {
	for _, lit := range []*sp.Expression_Literal{
		{LiteralType: &sp.Expression_Literal_Boolean{Boolean: true}},
		{LiteralType: &sp.Expression_Literal_I16{I16: -32768}},
		{LiteralType: &sp.Expression_Literal_I32{I32: math.MinInt32}},
		{LiteralType: &sp.Expression_Literal_I64{I64: math.MaxInt64}},
		{LiteralType: &sp.Expression_Literal_String_{String_: "'x.y\x00"}},
		{LiteralType: &sp.Expression_Literal_Binary{Binary: []byte{0, 255}}},
		{LiteralType: &sp.Expression_Literal_Fp32{Fp32: float32(math.Inf(1))}},
		{LiteralType: &sp.Expression_Literal_Fp64{Fp64: math.NaN()}},
	} {
		base, err := subLiteral(lit)
		if err != nil {
			t.Fatal(err)
		}
		// Construct the type with protobuf reflection from its logical Arrow mapping.
		kinds := map[Kind]string{KindBool: "bool", KindInt16: "i16", KindInt32: "i32", KindInt64: "i64", KindFloat32: "fp32", KindFloat64: "fp64", KindString: "string", KindBinary: "binary"}
		ty := new(sp.Type)
		if err := protojson.Unmarshal([]byte(fmt.Sprintf(`{"%s":{"nullability":"NULLABILITY_REQUIRED"}}`, kinds[base.t.typ.Kind])), ty); err != nil {
			t.Fatal(err)
		}
		cast := &sp.Expression{RexType: &sp.Expression_Cast_{Cast: &sp.Expression_Cast{Input: subLit(lit), Type: ty, FailureBehavior: sp.Expression_Cast_FAILURE_BEHAVIOR_THROW_EXCEPTION}}}
		tr := subTranslator{ctx: context.Background()}
		x, err := tr.expression(cast, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := (&evalEnv{}).eval(x.expr)
		if err != nil {
			t.Fatal(err)
		}
		want := base.expr.(*Literal).V
		if got.Null != want.Null || got.I != want.I || got.S != want.S || got.T != want.T || !(got.F == want.F || math.IsNaN(got.F) && math.IsNaN(want.F)) {
			t.Fatal(got, want)
		}
		cast.GetCast().FailureBehavior = sp.Expression_Cast_FAILURE_BEHAVIOR_RETURN_NULL
		if _, err := tr.expression(cast, 0); err == nil {
			t.Fatal("return-null cast accepted")
		}
	}
}
func TestSubstraitAdmissionBoundaryValues(t *testing.T) {
	base := subTestPlan(t, "t", []string{"id"}, subI64)
	for _, n := range []int{65536, 65537} {
		p := proto.Clone(base).(*sp.Plan)
		subProject(p, []string{"x"}, []*sp.Expression{subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_String_{String_: strings.Repeat("x", n)}})}, 1)
		_, err := decodeSubstrait(context.Background(), subBytes(t, p))
		if (err == nil) != (n == 65536) {
			t.Fatal(n, err)
		}
	}
	for _, n := range []int{256, 257} {
		p := proto.Clone(base).(*sp.Plan)
		names := make([]string, n)
		emit := make([]int32, n)
		subProject(p, names, nil, emit...)
		_, err := decodeSubstrait(context.Background(), subBytes(t, p))
		if (err == nil) != (n == 256) {
			t.Fatal(n, err)
		}
	}
	p := proto.Clone(base).(*sp.Plan)
	p.Version.Producer = strings.Repeat("x", SubstraitMaxPlanBytes-1000)
	size := len(subBytes(t, p))
	p.Version.Producer += strings.Repeat("x", SubstraitMaxPlanBytes-size)
	bytes := subBytes(t, p)
	if len(bytes) != SubstraitMaxPlanBytes {
		t.Fatal(len(bytes))
	}
	if _, err := decodeSubstrait(context.Background(), bytes); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeSubstrait(context.Background(), append(bytes, 0)); err == nil {
		t.Fatal("plan_bytes boundary")
	}
	// All declarations count, including unused but valid declarations.
	for _, n := range []int{64, 65} {
		p := proto.Clone(base).(*sp.Plan)
		subFunctionPlan(p, "equal:any_any", subComparison, nil, subBoolType(false))
		for i := 1; i < n; i++ {
			f := proto.Clone(p.Extensions[0]).(*ext.SimpleExtensionDeclaration)
			f.GetExtensionFunction().FunctionAnchor = uint32(i)
			p.Extensions = append(p.Extensions, f)
		}
		_, err := decodeSubstrait(context.Background(), subBytes(t, p))
		if (err == nil) != (n == 64) {
			t.Fatal(n, err)
		}
	}
}
func TestSubstraitAllTimestampPrecisions(t *testing.T) {
	for _, precision := range []int32{0, 3, 6, 9} {
		for _, tz := range []bool{false, true} {
			timestamp := &sp.Expression_Literal_PrecisionTimestamp{Precision: precision, Value: -123456789}
			lit := &sp.Expression_Literal{LiteralType: &sp.Expression_Literal_PrecisionTimestamp_{PrecisionTimestamp: timestamp}}
			if tz {
				lit.LiteralType = &sp.Expression_Literal_PrecisionTimestampTz{PrecisionTimestampTz: timestamp}
			}
			x, err := subLiteral(lit)
			if err != nil {
				t.Fatal(err)
			}
			v := x.expr.(*Literal).V
			if v.I != -123456789 || v.T.Unit != arrow.TimeUnit(precision/3) || (v.T.TZ == "UTC") != tz {
				t.Fatal(v)
			}
		}
	}
	for _, precision := range []int32{-1, 1, 2, 4, 5, 7, 8, 12} {
		if _, err := subTime(precision, false); err == nil {
			t.Fatal(precision)
		}
	}
}

func TestSubstraitPinnedDescriptors(t *testing.T) {
	bytes, err := os.ReadFile("internal/substrait/descriptor.pb")
	if err != nil {
		t.Fatal(err)
	}
	descriptors := new(descriptorpb.FileDescriptorSet)
	if err := proto.Unmarshal(bytes, descriptors); err != nil {
		t.Fatal(err)
	}
	for _, source := range descriptors.File {
		if !strings.HasPrefix(source.GetName(), "substrait/") {
			continue
		}
		generated, err := protoregistry.GlobalFiles.FindFileByPath(source.GetName())
		if err != nil {
			t.Fatal(err)
		}
		got := protodesc.ToFileDescriptorProto(generated)
		got.SourceCodeInfo = nil
		source.SourceCodeInfo = nil

		if !proto.Equal(got, source) {
			t.Fatalf("generated descriptor differs from pinned source: %s", source.GetName())
		}
	}
	manifest, err := os.ReadFile("internal/substrait/SHA256SUMS.json")
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	if err := json.Unmarshal(manifest, &hashes); err != nil {
		t.Fatal(err)
	}
	for path, want := range hashes {
		bytes, err := os.ReadFile(filepath.Join("internal/substrait", path))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(bytes)); got != want {
			t.Fatal(path, got, want)
		}
	}
}

func TestSubstraitNestedWireAndSchemaRejections(t *testing.T) {
	for _, name := range []string{"unknown nested", "enum", "duplicate oneof", "dangling function", "mismatched output", "decimal length", "decimal range", "nested field"} {
		t.Run(name, func(t *testing.T) {
			p := subTestPlan(t, "t", []string{"id"}, subI64)
			switch name {
			case "unknown nested":
				ty := p.Relations[0].GetRoot().Input.GetRead().BaseSchema.Struct.Types[0].GetI64()
				ty.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 99, protowire.VarintType), 1))
			case "enum":
				p.Relations[0].GetRoot().Input.GetRead().BaseSchema.Struct.Types[0].GetI64().Nullability = 99
			case "duplicate oneof":
				x := subRef(0)
				b := subBytesExpression(t, subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_I64{I64: 1}}))
				x.ProtoReflect().SetUnknown(b)
				subProject(p, []string{"x"}, []*sp.Expression{x}, 1)
			case "dangling function":
				x := subFunctionPlan(p, "equal:any_any", subComparison, []*sp.Expression{subRef(0), subRef(0)}, subBoolType(false))
				x.GetScalarFunction().FunctionReference = 99
				subProject(p, []string{"x"}, []*sp.Expression{x}, 1)
			case "mismatched output":
				x := subFunctionPlan(p, "equal:any_any", subComparison, []*sp.Expression{subRef(0), subRef(0)}, subBoolType(true))
				subProject(p, []string{"x"}, []*sp.Expression{x}, 1)
			case "decimal length":
				subProject(p, []string{"x"}, []*sp.Expression{subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_Decimal_{Decimal: &sp.Expression_Literal_Decimal{Precision: 38, Value: []byte{1}}}})}, 1)
			case "decimal range":
				subProject(p, []string{"x"}, []*sp.Expression{subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_Decimal_{Decimal: &sp.Expression_Literal_Decimal{Precision: 1, Value: append([]byte{10}, make([]byte, 15)...)}}})}, 1)
			case "nested field":
				x := subRef(0)
				x.GetSelection().GetDirectReference().GetStructField().Child = &sp.Expression_ReferenceSegment{}
				subProject(p, []string{"x"}, []*sp.Expression{x}, 1)
			}
			if _, err := decodeSubstrait(context.Background(), subBytes(t, p)); err == nil {
				t.Fatal("accepted", name)
			}
		})
	}
}
func subBytesExpression(t testing.TB, x *sp.Expression) []byte {
	t.Helper()
	b, err := proto.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestSubstraitDirectProject(t *testing.T) {
	p := subTestPlan(t, "t", []string{"id"}, subI64)
	root := p.Relations[0].GetRoot()
	root.Names = []string{"input", "computed"}
	root.Input = &sp.Rel{RelType: &sp.Rel_Project{Project: &sp.ProjectRel{Input: root.Input, Expressions: []*sp.Expression{subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_I64{I64: 42}})}}}}
	out, err := decodeSubstrait(context.Background(), subBytes(t, p))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.output) != 2 {
		t.Fatal(out.schema)
	}
	v, err := (&evalEnv{}).eval(out.output[1].expr)
	if err != nil || v.I != 42 {
		t.Fatal(v, err)
	}
}

func TestSubstraitExpressionAndExpansionBudgets(t *testing.T) {
	cast := func(x *sp.Expression, nullable bool) *sp.Expression {
		return &sp.Expression{RexType: &sp.Expression_Cast_{Cast: &sp.Expression_Cast{Input: x, Type: subBoolType(nullable), FailureBehavior: sp.Expression_Cast_FAILURE_BEHAVIOR_THROW_EXCEPTION}}}
	}
	// Exactly 4096 wire expressions are admitted; a discarded expression still
	// counts toward admission, even if Project.emit never selects it.
	for _, extra := range []bool{false, true} {
		p := subTestPlan(t, "t", []string{"id"}, subI64)
		expressions := make([]*sp.Expression, 256)
		for i := range expressions {
			x := subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_Boolean{Boolean: true}})
			for j := 0; j < 15; j++ {
				x = cast(x, false)
			}
			expressions[i] = x
		}
		if extra {
			expressions[255] = cast(expressions[255], false)
		}
		subProject(p, []string{"id"}, expressions, 0)
		_, err := decodeSubstrait(context.Background(), subBytes(t, p))
		if (err == nil) == extra {
			t.Fatal(extra, err)
		}
	}
	for _, extra := range []bool{false, true} {
		p := subTestPlan(t, "t", []string{"id"}, subI64)
		var tree func(int) *sp.Expression
		tree = func(depth int) *sp.Expression {
			if depth == 0 {
				return subLit(&sp.Expression_Literal{Nullable: true, LiteralType: &sp.Expression_Literal_Boolean{Boolean: true}})
			}
			return subFunctionPlan(p, "and:bool", subBoolean, []*sp.Expression{tree(depth - 1), tree(depth - 1)}, subBoolType(true))
		}
		x := cast(tree(4), true)
		if extra {
			x = cast(x, true)
		}
		names := make([]string, 256)
		emit := make([]int32, 256)
		for i := range emit {
			emit[i] = 1
		}
		subProject(p, names, []*sp.Expression{x}, emit...)
		_, err := decodeSubstrait(context.Background(), subBytes(t, p))
		if (err == nil) == extra {
			t.Fatal(extra, err)
		}
	}
}

func TestSubstraitWireIntegerOverflow(t *testing.T) {
	for _, name := range []string{"version", "enum", "packed ordinal"} {
		t.Run(name, func(t *testing.T) {
			p := subTestPlan(t, "t", []string{"id"}, subI64)
			switch name {
			case "version":
				p.Version.MinorNumber = 0
				p.Version.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 2, protowire.VarintType), 1<<32+108))
			case "enum":
				x := p.Relations[0].GetRoot().Input.GetRead().BaseSchema.Struct.Types[0].GetI64()
				x.Nullability = 0
				x.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 2, protowire.VarintType), 1<<32+2))
			case "packed ordinal":
				subProject(p, []string{"id"}, nil)
				x := p.Relations[0].GetRoot().Input.GetProject().Common.GetEmit()
				x.ProtoReflect().SetUnknown(protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), protowire.AppendVarint(nil, 1<<32)))
			}
			if _, err := decodeSubstrait(context.Background(), subBytes(t, p)); err == nil {
				t.Fatal("truncated overflowing wire integer")
			}
		})
	}
}

func TestSubstraitDiagnosticPrivacy(t *testing.T) {
	for _, status := range []adbc.Status{adbc.StatusUnauthorized, adbc.StatusIO, adbc.StatusNotFound, adbc.StatusTimeout} {
		err := subError(errorf(status, "physical-private-index secret-plan-value"), "metadata")
		if !isStatus(err, status) || strings.Contains(err.Error(), "private-index") || strings.Contains(err.Error(), "secret-plan") {
			t.Fatal(err)
		}
	}
	if subError(nil, "metadata") != nil {
		t.Fatal("nil error")
	}
	err := subBudget("plan_bytes")
	if subError(err, "admission").Error() != err.Error() {
		t.Fatal("lost stable budget name")
	}
}
