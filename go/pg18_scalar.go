// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode/utf8"
)

func pg18ScalarType(name string, args []ColType) (ColType, bool) {
	switch name {
	case "FORMAT":
		return typeString, true
	case "SCALE":
		return typeInt32, true
	case "TO_NUMBER":
		return decimalType(76, 16), true
	case "DIV":
		return decimalType(76, 0), true
	case "GCD", "LCM":
		if len(args) >= 2 {
			return commonType(args[0], args[1]), true
		}
		return typeInt32, true
	case "TRIM_SCALE":
		if len(args) > 0 {
			return args[0], true
		}
		return decimalType(38, 9), true
	case "MAKE_INTERVAL":
		return typeInterval, true
	case "^":
		return typeFloat64, true
	case "&", "|":
		return commonType(args[0], args[1]), true
	case "<<", ">>":
		return args[0], true
	case "PG_INPUT_ERROR_INFO":
		return inputErrorType(), true
	case "__OVERLAPS", "PG_INPUT_IS_VALID", "ISFINITE":
		return typeBool, true
	case "CHR", "TRANSLATE", "ENCODE", "QUOTE_LITERAL", "QUOTE_NULLABLE":
		return typeString, true
	case "SHA256", "FLOAT8SEND":
		return typeBinary, true
	case "ASCII", "NUM_NONNULLS", "NUM_NULLS", "WIDTH_BUCKET":
		return typeInt32, true
	case "SINH", "COSH", "TANH":
		return typeFloat64, true
	case "JUSTIFY_HOURS":
		return typeInterval, true
	}
	return ColType{}, false
}

func pg18Scalar(f *Func, args []Value, z tzZone) (Value, error) {
	typ, _ := pg18ScalarType(f.Name, argTypes(args))
	if f.Name == "__OVERLAPS" {
		return pgOverlaps(f, args, z)
	}
	if f.Name == "PG_INPUT_ERROR_INFO" {
		return pgInputErrorInfo(args, z)
	}
	if f.Name == "FORMAT" {
		return pgFormat(args, z)
	}
	if f.Name == "NUM_NONNULLS" || f.Name == "NUM_NULLS" {
		n := int64(0)
		for _, v := range args {
			if v.Null == (f.Name == "NUM_NULLS") {
				n++
			}
		}
		return intValue(typeInt32, n), nil
	}
	if f.Name == "QUOTE_NULLABLE" && args[0].Null {
		return stringValue("NULL"), nil
	}
	for _, a := range args {
		if a.Null {
			return nullValue(typ), nil
		}
	}
	text := func(i int) string { return args[i].textIn(z) }
	switch f.Name {
	case "TO_NUMBER":
		return pgToNumber(args[0].Text(), args[1].Text())
	case "DIV":
		a, as, ok := asDecimal(args[0])
		if !ok {
			var err error
			args[0], err = Coerce(args[0], decimalType(76, 16))
			if err != nil {
				return Value{}, err
			}
			a, as, _ = asDecimal(args[0])
		}
		b, bs, ok := asDecimal(args[1])
		if !ok {
			var err error
			args[1], err = Coerce(args[1], decimalType(76, 16))
			if err != nil {
				return Value{}, err
			}
			b, bs, _ = asDecimal(args[1])
		}
		if b.Sign() == 0 {
			return Value{}, stateErr(stateDivisionByZero, "division by zero")
		}
		n, d := new(big.Int).Set(a), new(big.Int).Set(b)
		if bs > as {
			n.Mul(n, pow10(bs-as))
		} else {
			d.Mul(d, pow10(as-bs))
		}
		return decimalValue(n.Quo(n, d), 76, 0), nil
	case "GCD", "LCM":
		if !typ.Kind.isInteger() {
			return Value{}, stateErr(stateUndefinedFunction, "non-integer gcd/lcm is not supported")
		}
		a, b := big.NewInt(args[0].I), big.NewInt(args[1].I)
		v := new(big.Int).GCD(nil, nil, a, b)
		if f.Name == "LCM" {
			if v.Sign() != 0 {
				v.Quo(new(big.Int).Mul(a, b), v)
				v.Abs(v)
			}
		}
		lo, hi := intRange(typ.Kind)
		if !v.IsInt64() || v.Int64() < lo || v.Int64() > hi {
			return Value{}, stateErr(stateNumericRange, "integer out of range")
		}
		return intValue(typ, v.Int64()), nil
	case "SCALE", "TRIM_SCALE":
		v := args[0]
		if v.T.Kind != KindDecimal {
			var err error
			v, err = Coerce(v, decimalType(76, 0))
			if err != nil {
				return Value{}, err
			}
		}
		if f.Name == "SCALE" {
			return intValue(typeInt32, int64(max(v.T.Scale, 0))), nil
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
		return decimalValue(n, max(decimalDigits(n), scale, 1), scale), nil
	case "MAKE_INTERVAL":
		ints := make([]int64, 6)
		for i := range ints {
			n, err := intArg(f.Name, "interval field", args[i])
			if err != nil {
				return Value{}, err
			}
			if n < int64(math.MinInt32) || n > int64(math.MaxInt32) {
				return Value{}, stateErr(stateNumericRange, "integer out of range")
			}
			ints[i] = n
		}
		seconds, err := Coerce(args[6], typeFloat64)
		if err != nil {
			return Value{}, err
		}
		micros := math.RoundToEven(seconds.F * 1e6)
		if math.IsNaN(micros) || math.IsInf(micros, 0) || micros >= float64(math.MaxInt64) || micros < float64(math.MinInt64) {
			return Value{}, stateErr(stateDatetimeRange, "interval out of range")
		}
		ns := new(big.Int).Mul(big.NewInt(ints[4]), big.NewInt(nsPerHour))
		ns.Add(ns, new(big.Int).Mul(big.NewInt(ints[5]), big.NewInt(nsPerMinute)))
		ns.Add(ns, new(big.Int).Mul(big.NewInt(int64(micros)), big.NewInt(1000)))
		if !ns.IsInt64() {
			return Value{}, stateErr(stateDatetimeRange, "interval out of range")
		}
		return intervalValue(ints[0]*12+ints[1], ints[2]*7+ints[3], ns.Int64())
	case "^":
		a, err := Coerce(args[0], typeFloat64)
		if err != nil {
			return Value{}, err
		}
		b, err := Coerce(args[1], typeFloat64)
		if err != nil {
			return Value{}, err
		}
		out := math.Pow(a.F, b.F)
		if math.IsInf(out, 0) && !math.IsInf(a.F, 0) && !math.IsInf(b.F, 0) {
			return Value{}, stateErr(stateNumericRange, "value out of range: overflow")
		}
		if math.IsNaN(out) && !math.IsNaN(a.F) && !math.IsNaN(b.F) {
			return Value{}, stateErr(statePower, "a negative number raised to a non-integer power yields a complex result")
		}
		return floatValue(typeFloat64, out), nil
	case "&", "|", "<<", ">>":
		n := args[0].I
		switch f.Name {
		case "&":
			n &= args[1].I
		case "|":
			n |= args[1].I
		case "<<", ">>":
			mask := uint64(63)
			if typ.Kind != KindInt64 {
				mask = 31
			}
			shift := uint64(args[1].I) & mask
			if f.Name == "<<" {
				n <<= shift
			} else {
				n >>= shift
			}
		}
		if typ.Kind == KindInt16 {
			n = int64(int16(n))
		} else if typ.Kind == KindInt32 {
			n = int64(int32(n))
		}
		return intValue(typ, n), nil
	case "PG_INPUT_IS_VALID":
		t, err := parseTypeText(text(1))
		if err != nil {
			return Value{}, stateErr(stateUndefinedObject, "type %q does not exist", text(1))
		}
		_, err = coerceIn(stringValue(text(0)), t, z)
		return boolValue(err == nil), nil
	case "TRANSLATE":
		from, to := []rune(text(1)), []rune(text(2))
		mapping := map[rune]string{}
		for i, r := range from {
			if _, ok := mapping[r]; !ok {
				mapping[r] = ""
				if i < len(to) {
					mapping[r] = string(to[i])
				}
			}
		}
		var b strings.Builder
		for _, r := range text(0) {
			if s, ok := mapping[r]; ok {
				b.WriteString(s)
			} else {
				b.WriteRune(r)
			}
		}
		return stringValue(b.String()), nil
	case "ENCODE":
		data := []byte(args[0].S)
		switch strings.ToLower(text(1)) {
		case "hex":
			return stringValue(hex.EncodeToString(data)), nil
		case "base64":
			s := base64.StdEncoding.EncodeToString(data)
			var b strings.Builder
			for len(s) > 76 {
				b.WriteString(s[:76])
				b.WriteByte('\n')
				s = s[76:]
			}
			b.WriteString(s)
			return stringValue(b.String()), nil
		case "escape":
			var b strings.Builder
			for _, c := range data {
				switch {
				case c == '\\':
					b.WriteString(`\\`)
				case c == 0 || c >= 128:
					fmt.Fprintf(&b, `\%03o`, c)
				default:
					b.WriteByte(c)
				}
			}
			return stringValue(b.String()), nil
		}
		return Value{}, stateErr(stateInvalidParameter, "unrecognized encoding: %q", text(1))
	case "SHA256":
		sum := sha256.Sum256([]byte(args[0].S))
		return binaryValue(string(sum[:])), nil
	case "FLOAT8SEND":
		v, err := Coerce(args[0], typeFloat64)
		if err != nil {
			return Value{}, err
		}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], math.Float64bits(v.F))
		return binaryValue(string(b[:])), nil
	case "QUOTE_LITERAL", "QUOTE_NULLABLE":
		s := strings.ReplaceAll(text(0), "'", "''")
		prefix := ""
		if strings.Contains(s, `\`) {
			s = strings.ReplaceAll(s, `\`, `\\`)
			prefix = "E"
		}
		return stringValue(prefix + "'" + s + "'"), nil
	case "CHR":
		n, err := intArg(f.Name, "character code", args[0])
		if err != nil {
			return Value{}, err
		}
		if n == 0 {
			return Value{}, stateErr(stateInvalidParameter, "null character not permitted")
		}
		if n < 0 || n > utf8.MaxRune || !utf8.ValidRune(rune(n)) {
			return Value{}, stateErr(stateInvalidParameter, "requested character too large for encoding")
		}
		return stringValue(string(rune(n))), nil
	case "ASCII":
		if text(0) == "" {
			return intValue(typeInt32, 0), nil
		}
		r, _ := utf8.DecodeRuneInString(text(0))
		return intValue(typeInt32, int64(r)), nil
	case "SINH", "COSH", "TANH":
		x, err := numericArg(f.Name, args[0])
		if err != nil {
			return Value{}, err
		}
		v, _ := x.asFloat()
		var out float64
		switch f.Name {
		case "SINH":
			out = math.Sinh(v)
		case "COSH":
			out = math.Cosh(v)
		case "TANH":
			out = math.Tanh(v)
		}
		if math.IsInf(out, 0) && !math.IsInf(v, 0) {
			return Value{}, stateErr(stateNumericRange, "value out of range: overflow")
		}
		return floatValue(typeFloat64, out), nil
	case "WIDTH_BUCKET":
		return pgWidthBucket(args)
	case "ISFINITE":
		switch args[0].T.Kind {
		case KindDate, KindTimestamp, KindInterval:
			return boolValue(true), nil
		}
		return Value{}, stateErr(stateUndefinedFunction, "function isfinite(%s) does not exist", args[0].T.SQLName())
	case "JUSTIFY_HOURS":
		v := args[0]
		if v.T.Kind != KindInterval {
			return Value{}, stateErr(stateUndefinedFunction, "function justify_hours(%s) does not exist", v.T.SQLName())
		}
		days := int64(v.Days) + v.I/nsPerDay
		nanos := v.I % nsPerDay
		if days > 0 && nanos < 0 {
			days--
			nanos += nsPerDay
		} else if days < 0 && nanos > 0 {
			days++
			nanos -= nsPerDay
		}
		return intervalValue(int64(v.Months), days, nanos)
	}
	return Value{}, fmt.Errorf("unsupported PostgreSQL function %s", f.Name)
}

func pgWidthBucket(args []Value) (Value, error) {
	n, err := intArg("WIDTH_BUCKET", "count", args[3])
	if err != nil {
		return Value{}, err
	}
	if n <= 0 || n > math.MaxInt32 {
		return Value{}, stateErr(stateInvalidParameter, "count must be greater than zero and fit integer")
	}
	rat := func(v Value) (*big.Rat, error) {
		x, e := numericArg("WIDTH_BUCKET", v)
		if e != nil {
			return nil, e
		}
		switch {
		case x.T.Kind.isInteger():
			return new(big.Rat).SetInt64(x.I), nil
		case x.T.Kind == KindDecimal:
			r := new(big.Rat).SetInt(x.D)
			if x.T.Scale >= 0 {
				r.Quo(r, new(big.Rat).SetInt(pow10(x.T.Scale)))
			} else {
				r.Mul(r, new(big.Rat).SetInt(pow10(-x.T.Scale)))
			}
			return r, nil
		default:
			v, _ := x.asFloat()
			r := new(big.Rat).SetFloat64(v)
			if r == nil {
				return nil, stateErr(stateInvalidParameter, "operand and bounds must be finite")
			}
			return r, nil
		}
	}
	lo, err := rat(args[1])
	if err != nil {
		return Value{}, err
	}
	hi, err := rat(args[2])
	if err != nil {
		return Value{}, err
	}
	cmp := lo.Cmp(hi)
	if cmp == 0 {
		return Value{}, stateErr(stateInvalidParameter, "lower bound cannot equal upper bound")
	}
	// Infinite operands are outside finite bounds; only bounds and NaN
	// operands are invalid, matching PostgreSQL's float overload.
	if args[0].T.Kind.isFloat() && math.IsInf(args[0].F, 0) {
		bucket := int64(0)
		if (args[0].F > 0) == (cmp < 0) {
			bucket = n + 1
		}
		if bucket > math.MaxInt32 {
			return Value{}, stateErr(stateNumericRange, "integer out of range")
		}
		return intValue(typeInt32, bucket), nil
	}
	x, err := rat(args[0])
	if err != nil {
		return Value{}, err
	}
	if cmp > 0 {
		x.Neg(x)
		lo.Neg(lo)
		hi.Neg(hi)
	}
	bucket := int64(0)
	if x.Cmp(hi) >= 0 {
		bucket = n + 1
	} else if x.Cmp(lo) >= 0 {
		q := new(big.Rat).Quo(new(big.Rat).Sub(x, lo), new(big.Rat).Sub(hi, lo))
		q.Mul(q, new(big.Rat).SetInt64(n))
		bucket = new(big.Int).Quo(q.Num(), q.Denom()).Int64() + 1
	}
	if bucket > math.MaxInt32 {
		return Value{}, stateErr(stateNumericRange, "integer out of range")
	}
	return intValue(typeInt32, bucket), nil
}
