// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "strings"

func gcdLCMFunction(name string) bool { return name == "GCD" || name == "LCM" }

// gcdLCMTypes resolves only the three PostgreSQL GCD/LCM signatures. Smallint
// can widen to any of them, but is not itself an overload. There is no preferred
// type among these candidates, so tied exact-match counts remain ambiguous.
// Planning and evaluation share this resolver; neither NULLs nor empty input
// can bypass a signature error or an invalid unknown-literal conversion.
func gcdLCMTypes(f *Func, args []ColType) (ColType, []ColType, error) {
	if len(args) != 2 {
		return ColType{}, nil, noSuchFunction(f.Name, false, args)
	}
	unknown := func(i int) bool { return args[i].Kind == KindNull || i < len(f.Args) && unknownLiteral(f.Args[i]) }
	best, count := -1, 0
	var selected ColType
	for _, candidate := range []ColType{typeInt32, typeInt64, decimalType(76, 0)} {
		valid, score := true, 0
		for i, arg := range args {
			if unknown(i) {
				continue
			}
			switch arg.Kind {
			case KindInt16:
			case KindInt32:
				valid = candidate.Kind == KindInt32 || candidate.Kind == KindInt64 || candidate.Kind == KindDecimal
			case KindInt64:
				valid = candidate.Kind == KindInt64 || candidate.Kind == KindDecimal
			case KindDecimal:
				valid = candidate.Kind == KindDecimal
			default:
				valid = false
			}
			if !valid {
				break
			}
			if arg.Kind == candidate.Kind {
				score++
			}
		}
		if !valid {
			continue
		}
		if score > best {
			best, count, selected = score, 1, candidate
		} else if score == best {
			count++
		}
	}
	if count == 0 {
		return ColType{}, nil, noSuchFunction(f.Name, false, args)
	}
	if count != 1 {
		return ColType{}, nil, stateErr(stateAmbiguousFunction, "function %s is not unique", strings.ToLower(f.Name))
	}
	targets := make([]ColType, 2)
	scale := int32(0)
	for i, arg := range args {
		targets[i] = selected
		literal := i < len(f.Args) && unknownLiteral(f.Args[i])
		if selected.Kind == KindDecimal {
			switch {
			case literal:
				// Use the existing bounded PostgreSQL numeric lexer (including bases,
				// underscores and exponent/range checks), without rounding to a guessed
				// scale. The coefficient still must fit the driver's 76-digit boundary.
				v, err := parseNumericText(f.Args[i].(*Literal).V.S, decimalType(76, 0))
				if err != nil {
					return ColType{}, nil, err
				}
				if decimalDigits(v.D) > 76 {
					return ColType{}, nil, numericFieldOverflow(decimalType(76, v.T.Scale))
				}
				targets[i] = decimalType(76, v.T.Scale)
			case arg.Kind != KindNull:
				// Keep known exact values in their original representation; widening
				// their coefficients to the result scale happens inside the arithmetic.
				targets[i] = arg
			}
			scale = max(scale, targets[i].Scale)
		}
		if literal {
			if err := checkLiteral(f.Args[i], targets[i]); err != nil {
				return ColType{}, nil, err
			}
		}
	}
	if selected.Kind == KindDecimal {
		selected = decimalType(76, scale)
	}
	return selected, targets, nil
}
