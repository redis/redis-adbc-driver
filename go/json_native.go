// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "strings"

var (
	typeJSON  = ColType{Kind: KindJSON}
	typeJSONB = ColType{Kind: KindJSONB}
)

func (k Kind) isJSON() bool { return k == KindJSON || k == KindJSONB }

// JSON text is authoritative in the HASH. Distinct persisted kinds ensure
// older drivers reject these types rather than silently treating them as text.
func coerceJSON(v Value, t ColType) (Value, error) {
	if v.Null {
		return nullValue(t), nil
	}
	if v.T.Kind != KindString && !v.T.Kind.isJSON() {
		return Value{}, stateErr(stateCannotCoerce, "cannot cast %s to %s", v.T.SQLName(), t.SQLName())
	}
	text := v.S
	if t.Kind == KindJSONB {
		s, err := normalizeJSONB(text)
		if err != nil {
			return Value{}, err
		}
		text = s
	} else if _, err := parseJSON(text, false); err != nil {
		return Value{}, err
	}
	return Value{T: t, S: text}, nil
}

// Resolve the new PostgreSQL forms by logical argument type before scanning,
// so an empty input cannot hide a wrong operator/function signature.
func jsonNativeCallError(f *Func, args []ColType) error {
	expected := []ColType(nil)
	operator := false
	switch f.Name {
	case "?":
		expected = []ColType{typeJSONB, typeString}
		operator = true
	case "?|", "?&", "#-":
		expected = []ColType{typeJSONB, arrayOf(typeString)}
		operator = true
	case "JSONB_SET":
		expected = []ColType{typeJSONB, arrayOf(typeString), typeJSONB, typeBool}
	default:
		if _, ok := jsonSRFTypes(f.Name); !ok {
			return nil
		}
		t := typeJSON
		if strings.HasPrefix(f.Name, "JSONB_") {
			t = typeJSONB
		}
		expected = []ColType{t}
	}
	for i, actual := range args {
		if i >= len(expected) {
			break
		}
		want := expected[i]
		unknown := unknownLiteral(f.Args[i])
		_, parameter := f.Args[i].(*Param)
		valid := actual.Kind == KindNull || actual.Kind == want.Kind
		if want.Kind == KindArray && actual.Kind == KindArray {
			valid = actual.element().Kind == KindString
		}
		if unknown {
			if err := checkLiteral(f.Args[i], want); err != nil {
				return err
			}
			continue
		}
		// Arrow UTF-8 parameters retain the established JSON text binding contract.
		if parameter && actual.Kind == KindString {
			continue
		}
		if !valid {
			if operator && len(args) == 2 {
				return stateErr(stateUndefinedFunction, "operator does not exist: %s %s %s", sigTypeName(args[0]), f.Name, sigTypeName(args[1]))
			}
			return noSuchFunction(f.Name, false, args)
		}
	}
	return nil
}
