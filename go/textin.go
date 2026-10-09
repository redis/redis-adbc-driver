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

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Text read as a NUMERIC, a DOUBLE PRECISION or REAL, or a BOOLEAN, as
// Postgres 16 reads it (numeric_in in numeric.c, float8in_internal and
// float4in_internal in float.c, boolin in bool.c), with its errors: the
// text as written, quoted, in `invalid input syntax for type numeric:
// "abc"` (22P02), and the range errors of each type (22003). Integers are
// in intin.go; dates and times are read in value.go, and dateTimeTextError
// gives their errors. Spaces are C's (isCSpace): ' ', \t, \n, \v, \f, \r.

// invalidText is Postgres's error for text s that isn't a value of type
// typ (its name in Postgres: "numeric", "double precision").
func invalidText(typ, s string) error {
	return stateErr(stateInvalidText, `invalid input syntax for type %s: "%s"`, typ, s)
}

// trimCSpace is s without its leading and trailing C spaces.
func trimCSpace(s string) string {
	i, j := 0, len(s)
	for i < j && isCSpace(s[i]) {
		i++
	}
	for j > i && isCSpace(s[j-1]) {
		j--
	}
	return s[i:j]
}

// hasPrefixFold reports whether s starts with prefix, a lower-case ASCII
// word, in any case (ASCII only, as C's strncasecmp).
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if c := s[i]; c != prefix[i] && !(c >= 'A' && c <= 'Z' && c+'a'-'A' == prefix[i]) {
			return false
		}
	}
	return true
}

// Postgres's limits on a numeric (numeric.c): an exponent past
// PG_INT32_MAX / 2, more than (NUMERIC_WEIGHT_MAX + 1) × DEC_DIGITS digits
// before the point or more than NUMERIC_DSCALE_MAX after it is "value
// overflows numeric format".
const (
	maxNumericExponent  = math.MaxInt32 / 2
	maxNumericIntDigits = 131072
	maxNumericScale     = 16383
)

// parseNumericText reads s as a NUMERIC of type t, as Postgres's numeric_in
// does: optional spaces and a sign, then digits with an optional point
// ('1.', '.5') and exponent ('1.5e-3'), or 0x, 0o or 0b and hexadecimal,
// octal or binary digits, then optional spaces. A single underscore may
// separate two digits ('1_000.000_1', '1e1_0'), and follow 0x, 0o or 0b
// ('0x_1F'), but not the point. 'NaN' (without a sign), 'Infinity' and
// 'inf' are read too, and refused, since a NUMERIC here can't hold them
// (numericSpecialError). The value has the digits written, and coerceIn
// fits it to t.
func parseNumericText(s string, t ColType) (Value, error) {
	invalid := func() (Value, error) { return Value{}, invalidText("numeric", s) }
	i := 0
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	start := i
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if i == len(s) || !isDigit(s[i]) && s[i] != '.' {
		var f float64
		switch {
		case hasPrefixFold(s[start:], "nan"): // not after a sign
			f, i = math.NaN(), start+3
		case hasPrefixFold(s[i:], "infinity"):
			f, i = math.Inf(1), i+8
		case hasPrefixFold(s[i:], "inf"):
			f, i = math.Inf(1), i+3
		default:
			return invalid()
		}
		if trimCSpace(s[i:]) != "" {
			return invalid()
		}
		if neg {
			f = -f
		}
		return Value{}, numericSpecialError(f, t)
	}
	var digits strings.Builder
	exp := int64(0) // the value is digits × 10^exp
	if base := nonDecimalBase(s[i:]); base != 0 {
		i += 2
		n := new(big.Int)
		first := i
		b := big.NewInt(int64(base))
		for i < len(s) {
			if d := digitValue(s[i], uint64(base)); d >= 0 { // intin.go
				n.Mul(n, b).Add(n, big.NewInt(int64(d)))
				i++
				continue
			}
			if s[i] != '_' {
				break
			}
			i++
			if i == len(s) || digitValue(s[i], uint64(base)) < 0 {
				return invalid()
			}
		}
		if i == first {
			return invalid()
		}
		digits.WriteString(n.String())
	} else {
		dot := false
		if s[i] == '.' {
			dot = true
			i++
		}
		if i == len(s) || !isDigit(s[i]) {
			return invalid()
		}
	digitLoop:
		for i < len(s) {
			switch c := s[i]; {
			case isDigit(c):
				digits.WriteByte(c)
				if dot {
					exp--
				}
				i++
			case c == '.':
				if dot {
					return invalid()
				}
				dot = true
				i++
				if i < len(s) && s[i] == '_' {
					return invalid()
				}
			case c == '_':
				i++
				if i == len(s) || !isDigit(s[i]) {
					return invalid()
				}
			default:
				break digitLoop
			}
		}
		if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
			i++
			eneg := false
			if i < len(s) && (s[i] == '+' || s[i] == '-') {
				eneg = s[i] == '-'
				i++
			}
			if i == len(s) || !isDigit(s[i]) {
				return invalid()
			}
			e := int64(0)
			for i < len(s) {
				if isDigit(s[i]) {
					if e = e*10 + int64(s[i]-'0'); e > maxNumericExponent {
						return Value{}, errNumericOverflow
					}
					i++
					continue
				}
				if s[i] != '_' {
					break
				}
				i++
				if i == len(s) || !isDigit(s[i]) {
					return invalid()
				}
			}
			if eneg {
				e = -e
			}
			exp += e
		}
	}
	if trimCSpace(s[i:]) != "" {
		return invalid()
	}
	text := strings.TrimLeft(digits.String(), "0")
	if text == "" {
		text = "0"
	}
	if -exp > maxNumericScale || text != "0" && int64(len(text))+exp > maxNumericIntDigits {
		return Value{}, errNumericOverflow
	}
	d, _ := new(big.Int).SetString(text, 10)
	if neg {
		d.Neg(d)
	}
	if exp > 0 {
		if d.Sign() != 0 {
			if int64(len(text))+exp > int64(t.Precision-t.Scale) {
				// More digits before the point than t holds: refused as
				// coerceIn would, without writing them all out.
				return Value{}, numericFieldOverflow(t)
			}
			d.Mul(d, pow10(int32(exp)))
		}
		exp = 0
	}
	scale := int32(-exp)
	return decimalValue(d, max(decimalDigits(d), scale+1, 1), scale), nil
}

// nonDecimalBase is 16, 8 or 2 if s starts with 0x, 0o or 0b (in any case),
// and otherwise 0.
func nonDecimalBase(s string) int {
	if len(s) < 2 || s[0] != '0' {
		return 0
	}
	switch s[1] {
	case 'x', 'X':
		return 16
	case 'o', 'O':
		return 8
	case 'b', 'B':
		return 2
	}
	return 0
}

// numericFieldOverflow is Postgres's error (apply_typmod in numeric.c) for
// a value that doesn't fit NUMERIC type t, with its detail: "numeric field
// overflow; a field with precision 5, scale 2 must round to an absolute
// value less than 10^3".
func numericFieldOverflow(t ColType) error {
	limit := "1"
	if n := t.Precision - t.Scale; n != 0 {
		limit = "10^" + strconv.Itoa(int(n))
	}
	return stateErr(stateNumericRange, "numeric field overflow; a field with precision %d, scale %d must round to an absolute value less than %s",
		t.Precision, t.Scale, limit)
}

// floatTypeName is the name Postgres gives a floating-point type in errors.
func floatTypeName(k Kind) string {
	if k == KindFloat32 {
		return "real"
	}
	return "double precision"
}

// parseFloatText reads s as a DOUBLE PRECISION, or a REAL for k
// KindFloat32, as Postgres reads it with C's strtod (strtof): optional
// spaces and a sign, then decimal digits with an optional point and
// exponent ('1.5e-3'), 0x and hexadecimal digits with an optional point and
// binary exponent ('0x1F', '0x1.8p1'), 'inf' or 'infinity', or 'nan' (also
// 'nan(…)' with letters, digits and underscores), in any case, then
// optional spaces; no underscores. A value past the type's range, or that
// isn't 0 but rounds to it, is `"1e400" is out of range for type double
// precision`, naming the number read (even if junk follows it, as in
// Postgres).
func parseFloatText(s string, k Kind) (float64, error) {
	typ := floatTypeName(k)
	invalid := func() (float64, error) { return 0, invalidText(typ, s) }
	i := 0
	for i < len(s) && isCSpace(s[i]) {
		i++
	}
	start := i
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	var f float64
	nonzero := false // a digit other than 0 was read
	switch {
	case hasPrefixFold(s[i:], "infinity"):
		f, i = math.Inf(1), i+8
	case hasPrefixFold(s[i:], "inf"):
		f, i = math.Inf(1), i+3
	case hasPrefixFold(s[i:], "nan"):
		f, i = math.NaN(), i+3
		if i < len(s) && s[i] == '(' {
			j := i + 1
			for j < len(s) && (isDigit(s[j]) || s[j] == '_' || s[j]|0x20 >= 'a' && s[j]|0x20 <= 'z') {
				j++
			}
			if j < len(s) && s[j] == ')' {
				i = j + 1
			}
		}
	default:
		// The longest number strtod reads: its mantissa, and an exponent if
		// one is complete.
		hex := len(s) > i+1 && s[i] == '0' && s[i+1]|0x20 == 'x'
		isMant := isDigit
		if hex {
			isMant = func(c byte) bool { return digitValue(c, 16) >= 0 }
		}
		m := i
		if hex {
			m += 2
		}
		n, dot := 0, false
		for m < len(s) && (isMant(s[m]) || s[m] == '.' && !dot) {
			if s[m] == '.' {
				dot = true
			} else {
				n++
				nonzero = nonzero || s[m] != '0'
			}
			m++
		}
		if n == 0 {
			if !hex {
				return invalid()
			}
			// "0x" without hexadecimal digits is the number 0, then junk.
			m, hex = i+1, false
		}
		mant := s[i:m]
		if e := m; e < len(s) && (hex && s[e]|0x20 == 'p' || !hex && s[e]|0x20 == 'e') {
			e++
			if e < len(s) && (s[e] == '+' || s[e] == '-') {
				e++
			}
			if e < len(s) && isDigit(s[e]) {
				for e < len(s) && isDigit(s[e]) {
					e++
				}
				m = e
			}
		}
		num := s[i:m]
		if hex && len(num) == len(mant) {
			num += "p0" // Go needs a binary exponent
		}
		bits := 64
		if k == KindFloat32 {
			bits = 32
		}
		var err error
		switch f, err = strconv.ParseFloat(num, bits); {
		case err != nil && !errors.Is(err, strconv.ErrRange):
			return invalid()
		case err != nil, nonzero && f == 0:
			return 0, stateErr(stateNumericRange, `"%s" is out of range for type %s`, s[start:m], typ)
		}
		i = m
	}
	if trimCSpace(s[i:]) != "" {
		return invalid()
	}
	if neg {
		f = -f
	}
	return f, nil
}

// parseBoolText reads s as a BOOLEAN, as Postgres's boolin does: without
// its leading and trailing spaces, true is a prefix of true or yes, on, or
// 1, and false a prefix of false or no, of or off, or 0, in any case ('t',
// 'Ye', 'OFF'); 'o' isn't one.
func parseBoolText(s string) (bool, error) {
	v := strings.ToLower(trimCSpace(s))
	if v != "" {
		switch {
		case strings.HasPrefix("true", v), strings.HasPrefix("yes", v), v == "on", v == "1":
			return true, nil
		case strings.HasPrefix("false", v), strings.HasPrefix("no", v), v == "of", v == "off", v == "0":
			return false, nil
		}
	}
	return false, invalidText("boolean", s)
}

// dateTimeTextError is Postgres's error for text in that doesn't read as a
// value of type typ ("date", "time", "timestamp" or "timestamp with time
// zone"), as DateTimeParseError (datetime.c) reports it, given the SQLSTATE
// of what is wrong: a field out of range (25:00, February 30) is
// `date/time field value out of range: "2024-02-30"` (22008), an offset
// past 15:59:59 `time zone displacement out of range: "…"` (22009), and
// anything else `invalid input syntax for type date: "x"` (22007).
func dateTimeTextError(state, typ, in string) error {
	switch state {
	case stateDatetimeRange:
		return stateErr(state, `date/time field value out of range: "%s"`, in)
	case stateTZDisplacement:
		return stateErr(state, `time zone displacement out of range: "%s"`, in)
	}
	return stateErr(stateDatetimeFormat, `invalid input syntax for type %s: "%s"`, typ, in)
}
