// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "math/big"

// PostgreSQL exposes only factorial(bigint). Integer arguments widen, and an
// unknown literal takes bigint's input syntax. Typed numeric/text/float do not
// implicitly convert, even when NULL or used over an empty relation.
func factorialCallError(f *Func, args []ColType) error {
	if f.Name != "FACTORIAL" {
		return nil
	}
	if len(args) != 1 {
		return noSuchFunction(f.Name, false, args)
	}
	if len(f.Args) > 0 && unknownLiteral(f.Args[0]) {
		return checkLiteral(f.Args[0], typeInt64)
	}
	if args[0].Kind != KindNull && !args[0].Kind.isInteger() {
		return noSuchFunction(f.Name, false, args)
	}
	return nil
}

func pgFactorial(f *Func, v Value, z tzZone) (Value, error) {
	typ := decimalType(76, 0)
	if err := factorialCallError(f, []ColType{v.T}); err != nil {
		return Value{}, err
	}
	if v.Null {
		return nullValue(typ), nil
	}
	n, err := coerceIn(v, typeInt64, z)
	if err != nil {
		return Value{}, err
	}
	if n.I < 0 {
		return Value{}, stateErr(stateNumericRange, "factorial of a negative number is undefined")
	}
	// 56! has 75 digits; 57! has 77. Refuse before doing unbounded work or
	// materializing a result the existing Arrow decimal256 schema cannot hold.
	// PostgreSQL accepts through 32177!, so 57..32177 remain a documented gap.
	if n.I > 56 {
		return Value{}, stateErr(stateNumericRange, "factorial result exceeds Arrow decimal256 precision 76")
	}
	result := new(big.Int).MulRange(2, n.I)
	return decimalValue(result, 76, 0), nil
}
