// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "math/big"

// EXTRACT carries numeric identity and computes fractional fields from integer
// time components. DATE_PART converts this exact value to float8 at its boundary.
func extractNumeric(field string, v Value, z tzZone) (Value, error) {
	f := normalizeField(field)
	var old Value
	var err error
	switch v.T.Kind {
	case KindInterval:
		old, err = intervalPart(field, v)
	case KindTimeTZ:
		old, err = timeTZPart(field, v)
	default:
		old, err = datePart(field, v, z)
	}
	if err != nil {
		return Value{}, err
	}
	decimal := func(n *big.Int, scale int32) Value { return decimalValue(n, max(decimalDigits(n), scale, 1), scale) }
	if old.T.Kind.isInteger() {
		n := old.I
		if (f == "year" || f == "isoyear") && (v.T.Kind == KindDate || v.T.Kind == KindTimestamp) && n <= 0 {
			n--
		}
		return decimal(big.NewInt(n), 0), nil
	}
	// Second/millisecond/epoch are the only fractional fields supported by the
	// existing extractor. Never derive their numeric result from old.F.
	var seconds, nanos int64
	if v.T.Kind == KindInterval {
		if f == "epoch" {
			seconds = int64(v.Months/12)*31557600 + int64(v.Months%12)*2592000 + int64(v.Days)*86400
			n := new(big.Int).Mul(big.NewInt(seconds), big.NewInt(1e9))
			n.Add(n, big.NewInt(v.I))
			scale := int32(6)
			if v.I%1000 != 0 {
				scale = 9
			}
			return decimal(rescaleDecimal(n, 9, scale), scale), nil
		}
		seconds, nanos = v.I%nsPerMinute/1e9, v.I%1e9
	} else if v.T.isTimeOfDay() {
		seconds, nanos = splitUnits(v.I, v.T.Unit)
		if f == "epoch" && v.T.Kind == KindTimeTZ {
			seconds -= int64(v.Off)
		}
		if f != "epoch" {
			seconds %= 60
		}
	} else {
		tm, e := toTime(v)
		if e != nil {
			return Value{}, e
		}
		if f == "epoch" {
			seconds, nanos = tm.Unix(), int64(tm.Nanosecond())
		} else {
			tm, e = localFields(v, z)
			if e != nil {
				return Value{}, e
			}
			seconds, nanos = int64(tm.Second()), int64(tm.Nanosecond())
		}
	}
	n := new(big.Int).Mul(big.NewInt(seconds), big.NewInt(1e9))
	n.Add(n, big.NewInt(nanos))
	scale := int32(6)
	if nanos%1000 != 0 {
		scale = 9
	}
	n = rescaleDecimal(n, 9, scale)
	if f == "milliseconds" {
		scale -= 3
	}
	return decimal(n, scale), nil
}
