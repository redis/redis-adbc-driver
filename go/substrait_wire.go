// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strings"

	sp "github.com/adbc-drivers/redis/go/internal/substrait/gen/substrait"
	"github.com/apache/arrow-adbc/go/adbc"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// SubstraitMaxPlanBytes is also enforced before the C boundary copies input.
const SubstraitMaxPlanBytes = 1 << 20

func subInvalid(path string) error {
	return errorf(adbc.StatusInvalidArgument, "Substrait: invalid %s", path)
}
func subUnsupported(path string) error {
	return errorf(adbc.StatusNotImplemented, "Substrait: unsupported %s", path)
}
func subBudget(name string) error {
	return errorf(adbc.StatusInvalidArgument, "Substrait: %s budget exceeded", name)
}

// Preserve public statuses without carrying Redis command/index/physical-key
// details or decoder values into Substrait diagnostics.
func subError(err error, path string) error {
	if err == nil {
		return nil
	}
	var ae adbc.Error
	if asAdbc(err, &ae) {
		if strings.HasPrefix(ae.Msg, "[redis] Substrait:") {
			return err
		}
		return errorf(ae.Code, "Substrait: %s failed", path)
	}
	return errorf(adbc.StatusIO, "Substrait: %s failed", path)
}

// Keep the wire allowlist separate from the generated schema. Merely decoding
// a known protobuf field does not make it safe to execute. Wire traversal also
// rejects duplicate singular/oneof fields, which normal protobuf unmarshalling
// silently merges or overwrites, and validates discarded declarations.
var subFields = map[string]string{
	"Plan":              "version extension_urns extensions relations execution_behavior",
	"ExecutionBehavior": "variable_eval_mode",
	"Version":           "major_number minor_number patch_number git_hash producer",
	"PlanRel":           "root", "RelRoot": "input names", "Rel": "read filter project",
	"ReadRel": "common base_schema named_table", "ReadRel.NamedTable": "names",
	"FilterRel": "common input condition", "ProjectRel": "common input expressions",
	"RelCommon": "direct emit", "RelCommon.Direct": "", "RelCommon.Emit": "output_mapping",
	"NamedStruct": "names struct", "Type.Struct": "types nullability type_variation_reference",
	"Type":                                    "bool i16 i32 i64 fp32 fp64 string binary decimal precision_timestamp precision_timestamp_tz",
	"Expression":                              "literal selection scalar_function cast",
	"Expression.Literal":                      "boolean i16 i32 i64 fp32 fp64 string binary decimal precision_timestamp precision_timestamp_tz null nullable type_variation_reference",
	"Expression.Literal.Decimal":              "value precision scale",
	"Expression.Literal.PrecisionTimestamp":   "precision value",
	"Expression.Literal.PrecisionTimestampTZ": "precision value",
	"Expression.FieldReference":               "direct_reference root_reference",
	"Expression.FieldReference.RootReference": "",
	"Expression.ReferenceSegment":             "struct_field", "Expression.ReferenceSegment.StructField": "field",
	"Expression.ScalarFunction": "function_reference arguments output_type",
	"FunctionArgument":          "value", "Expression.Cast": "type input failure_behavior",
	"extensions.SimpleExtensionURN":                           "extension_urn_anchor urn",
	"extensions.SimpleExtensionDeclaration":                   "extension_function",
	"extensions.SimpleExtensionDeclaration.ExtensionFunction": "extension_urn_reference function_anchor name",
}

func subAllowed(md protoreflect.MessageDescriptor, fd protoreflect.FieldDescriptor) bool {
	name := strings.TrimPrefix(string(md.FullName()), "substrait.")
	fields, ok := subFields[name]
	if !ok && strings.HasPrefix(name, "Type.") {
		switch name {
		case "Type.Boolean", "Type.I16", "Type.I32", "Type.I64", "Type.FP32", "Type.FP64", "Type.String", "Type.Binary":
			fields, ok = "nullability type_variation_reference", true
		case "Type.Decimal":
			fields, ok = "precision scale nullability type_variation_reference", true
		case "Type.PrecisionTimestamp", "Type.PrecisionTimestampTZ":
			fields, ok = "precision nullability type_variation_reference", true
		}
	}
	return ok && strings.Contains(" "+fields+" ", " "+string(fd.Name())+" ")
}

type subWireWork struct{ expressions, relations int }

func subWire(ctx context.Context, data []byte, md protoreflect.MessageDescriptor, depth int) error {
	return new(subWireWork).validate(ctx, data, md, depth)
}
func (w *subWireWork) validate(ctx context.Context, data []byte, md protoreflect.MessageDescriptor, depth int) error {
	name := strings.TrimPrefix(string(md.FullName()), "substrait.")
	if name == "Expression" {
		w.expressions++
		if w.expressions > 4096 {
			return subBudget("expression_nodes")
		}
	}
	if name == "Rel" {
		w.relations++
		if w.relations > 3 {
			return subBudget("relations")
		}
	}
	if depth > 64 {
		return subBudget("wire_depth")
	}
	seen := map[protoreflect.FieldNumber]bool{}
	counts := map[protoreflect.FieldNumber]int{}
	oneofs := map[protoreflect.FullName]bool{}
	for len(data) > 0 {
		if err := cancelled(ctx); err != nil {
			return err
		}
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return subInvalid("wire")
		}
		data = data[n:]
		fd := md.Fields().ByNumber(protoreflect.FieldNumber(num))
		if fd == nil {
			return subInvalid("unknown field")
		}
		if !subAllowed(md, fd) {
			return subUnsupported(string(md.FullName()) + "." + string(fd.Name()))
		}
		if !fd.IsList() && seen[fd.Number()] {
			return subInvalid("duplicate field")
		}
		seen[fd.Number()] = true
		if o := fd.ContainingOneof(); o != nil {
			if oneofs[o.FullName()] {
				return subInvalid("duplicate oneof")
			}
			oneofs[o.FullName()] = true
		}
		expected := protowire.VarintType
		switch fd.Kind() {
		case protoreflect.MessageKind, protoreflect.StringKind, protoreflect.BytesKind:
			expected = protowire.BytesType
		case protoreflect.DoubleKind, protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
			expected = protowire.Fixed64Type
		case protoreflect.FloatKind, protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind:
			expected = protowire.Fixed32Type
		}
		if typ != expected && !(fd.IsPacked() && typ == protowire.BytesType) {
			return subInvalid("wire type")
		}
		n = protowire.ConsumeFieldValue(num, typ, data)
		if n < 0 {
			return subInvalid("wire value")
		}
		// Enforce repeated-count and payload ceilings before protobuf allocates
		// message objects or copies strings. Packed emits count their elements.
		count := 1
		if fd.IsPacked() && typ == protowire.BytesType {
			packed, k := protowire.ConsumeBytes(data)
			if k < 0 {
				return subInvalid("packed field")
			}
			count = 0
			for len(packed) > 0 {
				v, k := protowire.ConsumeVarint(packed)
				if int64(v) < -1<<31 || int64(v) > 1<<31-1 {
					return subInvalid("ordinal range")
				}
				if k < 0 {
					return subInvalid("packed ordinal")
				}
				packed = packed[k:]
				count++
				if count > 256 {
					return subBudget("output_columns")
				}
			}
		}
		counts[fd.Number()] += count
		key := name + "." + string(fd.Name())
		maximum, budget := 0, ""
		switch key {
		case "Plan.extension_urns":
			maximum, budget = 2, "extension_urns"
		case "Plan.extensions":
			maximum, budget = 64, "extensions"
		case "Plan.relations":
			maximum, budget = 1, "roots"
		case "NamedStruct.names", "Type.Struct.types":
			maximum, budget = 256, "input_columns"
		case "RelRoot.names", "RelCommon.Emit.output_mapping":
			maximum, budget = 256, "output_columns"
		case "ProjectRel.expressions":
			maximum, budget = 256, "project_expressions"
		case "Expression.ScalarFunction.arguments":
			maximum, budget = 2, "function_arguments"
		case "ReadRel.NamedTable.names":
			maximum, budget = 3, "table_components"
		}
		if maximum > 0 && counts[fd.Number()] > maximum {
			return subBudget(budget)
		}
		if typ == protowire.BytesType && fd.Kind() != protoreflect.MessageKind {
			payload, k := protowire.ConsumeBytes(data)
			if k < 0 {
				return subInvalid("payload")
			}
			switch key {
			case "Expression.Literal.Decimal.value":
				if len(payload) != 16 {
					return subInvalid("decimal literal length")
				}
			case "Expression.Literal.string", "Expression.Literal.binary":
				if len(payload) > 65536 {
					return subBudget("literal_bytes")
				}
			case "NamedStruct.names", "ReadRel.NamedTable.names", "RelRoot.names":
				if len(payload) > 1024 {
					return subBudget("identifier_bytes")
				}
			}
		}

		if typ == protowire.VarintType {
			v, k := protowire.ConsumeVarint(data)
			if k < 0 {
				return subInvalid("varint")
			}
			switch fd.Kind() {
			case protoreflect.Uint32Kind:
				if v > 1<<32-1 {
					return subInvalid("uint32 range")
				}
			case protoreflect.Int32Kind:
				if int64(v) < -1<<31 || int64(v) > 1<<31-1 {
					return subInvalid("int32 range")
				}
			case protoreflect.EnumKind:
				if v > 1<<31-1 {
					return subInvalid("enum range")
				}
			case protoreflect.BoolKind:
				if v > 1 {
					return subInvalid("boolean range")
				}
			}
		}
		if fd.Kind() == protoreflect.MessageKind {
			b, k := protowire.ConsumeBytes(data)
			if k < 0 {
				return subInvalid("message")
			}
			if err := w.validate(ctx, b, fd.Message(), depth+1); err != nil {
				return err
			}
		} else if fd.Kind() == protoreflect.EnumKind {
			v, k := protowire.ConsumeVarint(data)
			if k < 0 || fd.Enum().Values().ByNumber(protoreflect.EnumNumber(v)) == nil {
				return subInvalid("enum")
			}
		}
		data = data[n:]
	}
	return nil
}

func decodeSubstrait(ctx context.Context, data []byte) (*subPlan, error) {
	if len(data) > SubstraitMaxPlanBytes {
		return nil, subBudget("plan_bytes")
	}
	p := new(sp.Plan)
	if err := subWire(ctx, data, p.ProtoReflect().Descriptor(), 0); err != nil {
		return nil, err
	}
	if err := (proto.UnmarshalOptions{RecursionLimit: 65}).Unmarshal(data, p); err != nil {
		return nil, subInvalid("wire")
	}
	return translateSubstrait(ctx, p)
}
