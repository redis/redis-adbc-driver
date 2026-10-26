// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

// Text read as an integer, as Postgres 16's int2in, int4in and int8in read
// it (pg_strtoint16_safe and the others in numutils.c): optional
// whitespace, an optional sign, then decimal digits, or 0x, 0o or 0b and
// hexadecimal, octal or binary digits, then optional whitespace. A single
// underscore may separate two digits, and follow the 0x, 0o or 0b
// ('0x_1F'). So '1.5' and '1e3' are invalid_text_representation, not 2 and
// 1000, and a value past the type's range is numeric_value_out_of_range.
// That is also the error once the digits read overflow, whatever follows
// them ('99999999999x' as an INTEGER).

// intTypeName is the name Postgres gives an integer type in these errors.
func intTypeName(k Kind) string {
	switch k {
	case KindInt16:
		return "smallint"
	case KindInt32:
		return "integer"
	}
	return "bigint"
}

// digitValue is c's value as a digit in base, or -1.
func digitValue(c byte, base uint64) int {
	var d int
	switch {
	case c >= '0' && c <= '9':
		d = int(c - '0')
	case c >= 'a' && c <= 'f':
		d = int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		d = int(c-'A') + 10
	default:
		return -1
	}
	if uint64(d) >= base {
		return -1
	}
	return d
}

// parseIntText reads s as an integer of kind k (KindInt16, KindInt32 or
// KindInt64), as Postgres does.
func parseIntText(s string, k Kind) (int64, error) {
	invalid := func() (int64, error) {
		return 0, stateErr(stateInvalidText, "invalid input syntax for type %s: \"%s\"", intTypeName(k), s)
	}
	outOfRange := func() (int64, error) {
		return 0, stateErr(stateNumericRange, "value \"%s\" is out of range for type %s", s, intTypeName(k))
	}
	lo, _ := intRange(k)
	minMag := uint64(-(lo + 1)) + 1 // |lo|, as an unsigned number
	i := 0
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		neg = s[i] == '-'
		i++
	}
	base := uint64(10)
	if i+1 < len(s) && s[i] == '0' {
		switch s[i+1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 10 {
			i += 2
		}
	}
	first := i
	var n uint64
	for i < len(s) {
		if d := digitValue(s[i], base); d >= 0 {
			if n > minMag/base {
				return outOfRange()
			}
			n = n*base + uint64(d)
			i++
			continue
		}
		if s[i] != '_' {
			break
		}
		// An underscore is followed by a digit, and in a decimal number
		// also follows one.
		if base == 10 && i == first {
			return invalid()
		}
		i++
		if i == len(s) || digitValue(s[i], base) < 0 {
			return invalid()
		}
	}
	if i == first {
		return invalid()
	}
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	if i != len(s) {
		return invalid()
	}
	switch {
	case neg && n > minMag, !neg && n > minMag-1:
		return outOfRange()
	case neg:
		return -int64(n), nil // wraps to lo for n = |lo|
	}
	return int64(n), nil
}
