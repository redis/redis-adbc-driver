// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"errors"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

// OVERLAPS resolves the same temporal overload families as PostgreSQL. Unknown
// literals can acquire a temporal type; a text column cannot implicitly do so.
func overlapsTypes(f *Func, args []ColType) ([]ColType, error) {
	var candidates [][]ColType
	for _, base := range []ColType{timeType(arrow.Microsecond), {Kind: KindTimeTZ, Unit: arrow.Microsecond}, timestampType(arrow.Microsecond, ""), timestampType(arrow.Microsecond, "UTC")} {
		for mask := 0; mask < 4; mask++ {
			if base.Kind == KindTimeTZ && mask != 0 {
				continue
			}
			ts := []ColType{base, base, base, base}
			if mask&1 != 0 {
				ts[1] = typeInterval
			}
			if mask&2 != 0 {
				ts[3] = typeInterval
			}
			valid := true
			for i, t := range args {
				if t.Kind == KindNull || unknownLiteral(f.Args[i]) {
					continue
				}
				to := ts[i]
				switch {
				case t.Kind == KindInterval:
					valid = to.Kind == KindInterval
				case t.Kind == KindDate:
					valid = to.Kind == KindTimestamp
				case t.Kind == KindTimestamp:
					valid = to.Kind == KindTimestamp && (t.TZ == "" || to.TZ != "")
				case t.Kind == KindTime:
					valid = to.isTimeOfDay() || to.Kind == KindInterval
				case t.Kind == KindTimeTZ:
					valid = to.Kind == KindTimeTZ
				default:
					valid = false
				}
				if !valid {
					break
				}
			}
			if valid {
				candidates = append(candidates, ts)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, noSuchFunction("overlaps", false, args)
	}
	// Exact known-type matches win first, followed by the preferred datetime
	// type (timestamptz) at positions requiring an implicit conversion.
	for _, preferred := range []bool{false, true} {
		best := -1
		var keep [][]ColType
		for _, ts := range candidates {
			score := 0
			for i, t := range args {
				if t.Kind == KindNull || unknownLiteral(f.Args[i]) {
					continue
				}
				exact := t.Kind == ts[i].Kind && (t.TZ != "") == (ts[i].TZ != "")
				if !preferred && exact || preferred && !exact && ts[i].Kind == KindTimestamp && ts[i].TZ != "" {
					score++
				}
			}
			if score > best {
				best = score
				keep = nil
			}
			if score == best {
				keep = append(keep, ts)
			}
		}
		candidates = keep
	}
	// If all known arguments share a type, PostgreSQL can use that type
	// for unknown arguments when category selection would be ambiguous.
	known := typeNull
	same := true
	for i, t := range args {
		if t.Kind == KindNull || unknownLiteral(f.Args[i]) {
			continue
		}
		if known.Kind == KindNull {
			known = t
		} else if known.Kind != t.Kind || known.TZ != t.TZ {
			same = false
		}
	}
	if same && (known.Kind == KindDate || known.Kind == KindTimestamp || known.isTimeOfDay()) {
		var keep [][]ColType
		for _, ts := range candidates {
			valid := true
			for i, t := range args {
				if (t.Kind == KindNull || unknownLiteral(f.Args[i])) && ts[i].Kind == KindInterval && known.Kind != KindTime {
					valid = false
				}
			}
			if valid {
				keep = append(keep, ts)
			}
		}
		if len(keep) > 0 {
			candidates = keep
		}
	}
	for i, t := range args {
		if len(candidates) == 1 {
			break
		}
		if t.Kind != KindNull && !unknownLiteral(f.Args[i]) {
			continue
		}
		interval := candidates[0][i].Kind == KindInterval
		for _, ts := range candidates {
			if (ts[i].Kind == KindInterval) != interval {
				return nil, stateErr("42725", "function overlaps is not unique")
			}
		}
		var keep [][]ColType
		for _, ts := range candidates {
			if ts[i].Kind == KindTimestamp && ts[i].TZ != "" {
				keep = append(keep, ts)
			}
		}
		if len(keep) > 0 {
			candidates = keep
		}
	}
	if len(candidates) != 1 {
		return nil, stateErr("42725", "function overlaps is not unique")
	}
	ts := append([]ColType{}, candidates[0]...)
	// Preserve native Arrow precision when inputs have sub-microsecond values.
	unit := arrow.Microsecond
	for _, t := range args {
		if (t.Kind == KindTimestamp || t.isTimeOfDay()) && t.Unit > unit {
			unit = t.Unit
		}
	}
	for i := range ts {
		if ts[i].Kind != KindInterval {
			ts[i].Unit = unit
		}
	}
	return ts, nil
}

func temporalInputCallError(f *Func, args []ColType) error {
	if f.Name == "__OVERLAPS" {
		ts, err := overlapsTypes(f, args)
		if err != nil {
			return err
		}
		for i, a := range f.Args {
			if unknownLiteral(a) {
				if err := checkLiteral(a, ts[i]); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if f.Name == "PG_INPUT_ERROR_INFO" {
		for _, t := range args {
			if t.Kind != KindNull && (t.Kind != KindString || t.Alias != "") {
				return noSuchFunction(f.Name, false, args)
			}
		}
	}
	return nil
}

func pgOverlaps(f *Func, args []Value, z tzZone) (Value, error) {
	ts, err := overlapsTypes(f, argTypes(args))
	if err != nil {
		return Value{}, err
	}
	v := make([]Value, 4)
	for i, a := range args {
		if !a.Null && a.T.Kind == KindTime && ts[i].Kind == KindInterval {
			v[i], err = intervalValue(0, 0, a.I*(1_000_000_000/unitsPerSecond[a.T.Unit]))
		} else {
			v[i], err = coerceIn(a, ts[i], z)
		}
		if err != nil {
			return Value{}, err
		}
	}
	for _, i := range []int{0, 2} {
		if ts[i+1].Kind == KindInterval {
			v[i+1], err = binaryOp("+", v[i], v[i+1], z)
			if err != nil {
				return Value{}, err
			}
		}
		if v[i].Null {
			if v[i+1].Null {
				return nullValue(typeBool), nil
			}
			v[i], v[i+1] = v[i+1], v[i]
		} else if !v[i+1].Null {
			c, _ := compareIn(v[i], v[i+1], z)
			if c > 0 {
				v[i], v[i+1] = v[i+1], v[i]
			}
		}
	}
	c, _ := compareIn(v[0], v[2], z)
	if c == 0 {
		if v[1].Null || v[3].Null {
			return nullValue(typeBool), nil
		}
		return boolValue(true), nil
	}
	start, end, otherEnd := v[0], v[3], v[1]
	if c < 0 {
		start, end, otherEnd = v[2], v[1], v[3]
	}
	if end.Null {
		return nullValue(typeBool), nil
	}
	c, _ = compareIn(start, end, z)
	if c < 0 {
		return boolValue(true), nil
	}
	if otherEnd.Null {
		return nullValue(typeBool), nil
	}
	return boolValue(false), nil
}

var inputErrorTextType = ColType{Kind: KindString, Text: true}

var inputErrorNames = []string{"message", "detail", "hint", "sql_error_code"}

func inputErrorTypes() []ColType {
	return []ColType{inputErrorTextType, inputErrorTextType, inputErrorTextType, inputErrorTextType}
}
func inputErrorType() ColType {
	fields := make([]structField, 4)
	for i, n := range inputErrorNames {
		fields[i] = structField{Name: n, Type: inputErrorTextType, Nullable: true}
	}
	return queryRecordOf(fields)
}

// This is a scalar record with OUT fields, including for valid or NULL inputs.
// FROM exposes those fields as exactly one row; it is not a ProjectSet SRF.
func pgInputErrorInfo(args []Value, z tzZone) (Value, error) {
	fields := []Value{nullValue(inputErrorTextType), nullValue(inputErrorTextType), nullValue(inputErrorTextType), nullValue(inputErrorTextType)}
	result := func() Value {
		for i := range fields {
			fields[i].T = inputErrorTextType
		}
		v := recordValue(fields)
		v.T = inputErrorType()
		return v
	}
	if args[0].Null || args[1].Null {
		return nullValue(inputErrorType()), nil
	}
	name := args[1].S
	t, err := parseTypeText(strings.TrimPrefix(name, "pg_catalog."))
	if err != nil {
		return Value{}, stateErr(stateUndefinedObject, "type %q does not exist", name)
	}
	switch t.Kind {
	case KindBool, KindInt16, KindInt32, KindInt64, KindFloat32, KindFloat64, KindString:
	case KindDecimal:
		if !strings.Contains(name, "(") {
			return Value{}, stateErr(stateFeatureNotSupported, "input diagnostics for unconstrained numeric are not supported")
		}
	case KindArray:
		if !t.element().Kind.isInteger() {
			return Value{}, stateErr(stateFeatureNotSupported, "input diagnostics for this array element type are not supported")
		}
	default:
		return Value{}, stateErr(stateFeatureNotSupported, "input diagnostics for type %s are not supported", name)
	}
	_, err = coerceIn(stringValue(args[0].S), t, z)
	if err == nil && t.Kind == KindString {
		_, err = fitLength(t, stringValue(args[0].S))
	}
	if err == nil {
		return result(), nil
	}
	// Storage limitations are not evidence that PostgreSQL rejects the input.
	// Leave these explicit rather than returning a fabricated conversion error.
	if t.Kind == KindDecimal && (strings.Contains(err.Error(), "NUMERIC values can't") || strings.Contains(err.Error(), "decimal precision")) {
		return Value{}, stateErr(stateFeatureNotSupported, "input diagnostics for this numeric value exceed the driver's exact decimal representation")
	}
	state := sqlState(err)
	if state == "" || !strings.HasPrefix(state, "22") {
		return Value{}, err
	}
	msg := err.Error()
	var ae adbc.Error
	var se *sqlError
	if errors.As(err, &se) {
		msg = se.msg
	} else if errors.As(err, &ae) {
		msg = strings.TrimPrefix(ae.Msg, "[redis] ")
	}
	if t.Kind == KindArray && strings.HasPrefix(msg, "malformed array literal:") {
		text := strings.TrimSpace(args[0].S)
		if text == "" || (text[0] != '{' && text[0] != '[') {
			fields[1] = stringValue("Array value must start with \"{\" or dimension information.")
		}
	}
	if t.Kind == KindDecimal && strings.HasPrefix(msg, "numeric field overflow; a field") {
		detail := strings.TrimPrefix(msg, "numeric field overflow; ")
		fields[1] = stringValue(strings.ToUpper(detail[:1]) + detail[1:] + ".")
		msg = "numeric field overflow"
	}
	fields[0], fields[3] = stringValue(msg), stringValue(state)
	return result(), nil
}
