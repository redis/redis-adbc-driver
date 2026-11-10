// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"math/big"
	"strings"
)

// Numeric pictures use C-locale decimal/group separators. Reject unsupported
// directives explicitly rather than interpreting a date/time picture as numeric.
func numericPicture(picture string) (mask string, fill bool, err error) {
	fill = strings.Contains(strings.ToUpper(picture), "FM")
	mask = strings.ReplaceAll(strings.ToUpper(picture), "FM", "")
	if len(mask) > 4096 {
		return "", false, stateErr(stateInvalidParameter, "numeric format too long")
	}
	for _, c := range mask {
		if !strings.ContainsRune("90DG.,S", c) {
			return "", false, stateErr(stateFeatureNotSupported, "unsupported numeric format directive %q", string(c))
		}
	}
	if strings.Count(mask, "D")+strings.Count(mask, ".") > 1 || strings.Count(mask, "S") > 1 {
		return "", false, stateErr(stateInvalidParameter, "invalid numeric format picture")
	}
	return mask, fill, nil
}
func numericToChar(v Value, picture string) (Value, error) {
	mask, fill, err := numericPicture(picture)
	if err != nil {
		return Value{}, err
	}
	point := strings.IndexAny(mask, "D.")
	scale := int32(0)
	if point >= 0 {
		for _, c := range mask[point+1:] {
			if c == '9' || c == '0' {
				scale++
			}
		}
	}
	if scale > 76 {
		return Value{}, stateErr(stateNumericRange, "numeric format precision exceeds 76 digits")
	}
	if v.T.Kind != KindDecimal {
		v, err = Coerce(v, decimalType(76, scale))
		if err != nil {
			return Value{}, err
		}
	}
	neg := v.D.Sign() < 0
	n := new(big.Int).Abs(rescaleDecimal(v.D, v.T.Scale, scale))
	text := formatDecimal(n, scale)
	integer, fraction, _ := strings.Cut(text, ".")
	idx := len(integer) - 1
	var chars = []byte(mask)
	end := len(mask)
	if point >= 0 {
		end = point
	}
	overflow := false
	for i := end - 1; i >= 0; i-- {
		switch mask[i] {
		case '9', '0':
			if idx >= 0 {
				chars[i] = integer[idx]
				idx--
			} else if mask[i] == '0' {
				chars[i] = '0'
			} else {
				chars[i] = ' '
			}
		case 'G', ',':
			if idx >= 0 {
				chars[i] = ','
			} else {
				chars[i] = ' '
			}
		case 'S':
			chars[i] = '+'
			if neg {
				chars[i] = '-'
			}
		}
	}
	if idx >= 0 {
		overflow = true
	}
	if point >= 0 {
		chars[point] = '.'
		j := 0
		for i := point + 1; i < len(mask); i++ {
			switch mask[i] {
			case '9', '0':
				if overflow {
					chars[i] = '#'
				} else {
					chars[i] = fraction[j]
				}
				j++
			case 'S':
				chars[i] = '+'
				if neg {
					chars[i] = '-'
				}
			}
		}
	}
	if overflow {
		for i := range chars {
			if mask[i] == '9' || mask[i] == '0' {
				chars[i] = '#'
			}
		}
	}
	result := string(chars)
	if !strings.ContainsRune(mask, 'S') {
		if neg {
			leading := len(result) - len(strings.TrimLeft(result, " "))
			result = result[:leading] + "-" + result[leading:]
		} else {
			result = " " + result
		}
	}
	if fill {
		result = strings.TrimSpace(result)
		if point >= 0 {
			for i := len(mask) - 1; i > point && mask[i] == '9' && strings.HasSuffix(result, "0"); i-- {
				result = strings.TrimSuffix(result, "0")
			}
			result = strings.TrimSuffix(result, ".")
		}
	}
	return stringValue(result), nil
}
func pgToNumber(text, picture string) (Value, error) {
	mask, _, err := numericPicture(picture)
	if err != nil {
		return Value{}, err
	}
	decimal := strings.ContainsAny(mask, "D.")
	var b strings.Builder
	for _, c := range text {
		switch {
		case c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == '+' || c == '-':
			b.WriteRune(c)
		case c == '.' && decimal:
			b.WriteRune(c)
		case c == ',' || c == ' ':
		default:
			return Value{}, invalidText("numeric", text)
		}
	}
	n, scale, err := parseDecimal(b.String())
	if err != nil {
		return Value{}, err
	}
	return decimalValue(n, max(decimalDigits(n), scale, 1), scale), nil
}
