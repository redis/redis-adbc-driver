// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "math/big"

// MIN_SCALE has a single numeric overload. Resolve it before strict NULL
// handling and before execution, including queries that produce no rows.
func minScaleCallError(f *Func, args []ColType) error {
	if f.Name != "MIN_SCALE" {
		return nil
	}
	t := args[0]
	if unknownLiteral(f.Args[0]) {
		_, err := minScaleLiteral(f.Args[0].(*Literal).V.S)
		return err
	}
	if t.Kind != KindNull && t.Kind != KindDecimal && !t.Kind.isInteger() {
		return noSuchFunction(f.Name, false, args)
	}
	return nil
}

// No Arrow numeric is materialized for an unknown literal: retain the exact
// parsed coefficient/scale, within the existing PostgreSQL numeric input bounds.
func minScaleLiteral(s string) (Value, error) {
	return parseNumericText(s, decimalType(maxNumericIntDigits, 0))
}

func pgMinScale(f *Func, v Value) (Value, error) {
	if err := minScaleCallError(f, []ColType{v.T}); err != nil {
		return Value{}, err
	}
	if v.Null {
		return nullValue(typeInt32), nil
	}
	if v.T.Kind == KindString {
		var err error
		v, err = minScaleLiteral(v.S)
		if err != nil {
			return Value{}, err
		}
	}
	if v.T.Kind.isInteger() || v.T.Scale <= 0 || v.D.Sign() == 0 {
		return intValue(typeInt32, 0), nil
	}
	n := new(big.Int).Set(v.D)
	scale := v.T.Scale
	for scale > 0 {
		q, r := new(big.Int).QuoRem(n, bigTen, new(big.Int))
		if r.Sign() != 0 {
			break
		}
		n = q
		scale--
	}
	return intValue(typeInt32, int64(scale)), nil
}
