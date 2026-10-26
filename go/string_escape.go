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
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

func invalidUTF8() error { return stateErr("22021", `invalid byte sequence for encoding "UTF8"`) }

// lexEscapeString reads E'...', including escaped quotes, byte escapes and
// Unicode escapes. The final byte sequence must be valid non-NUL UTF-8.
func lexEscapeString(s string) (string, int, error) {
	var b strings.Builder
	for i := 1; i < len(s); {
		c := s[i]
		i++
		if c == '\'' {
			if i < len(s) && s[i] == '\'' {
				b.WriteByte(c)
				i++
				continue
			}
			if !utf8.ValidString(b.String()) || strings.ContainsRune(b.String(), 0) {
				return "", 0, invalidUTF8()
			}
			return b.String(), i, nil
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i == len(s) {
			break
		}
		c = s[i]
		i++
		switch c {
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u', 'U':
			n := 4
			if c == 'U' {
				n = 8
			}
			if i+n > len(s) {
				return "", 0, syntaxErr("invalid Unicode escape")
			}
			v, err := strconv.ParseUint(s[i:i+n], 16, 32)
			if err != nil {
				return "", 0, syntaxErr("invalid Unicode escape")
			}
			i += n
			cp := rune(v)
			if cp >= 0xD800 && cp <= 0xDBFF {
				if i+6 > len(s) || s[i:i+2] != `\u` {
					return "", 0, syntaxErr("invalid Unicode surrogate pair")
				}
				lo, err := strconv.ParseUint(s[i+2:i+6], 16, 16)
				if err != nil || lo < 0xDC00 || lo > 0xDFFF {
					return "", 0, syntaxErr("invalid Unicode surrogate pair")
				}
				cp = utf16.DecodeRune(cp, rune(lo))
				i += 6
			}
			if !utf8.ValidRune(cp) || cp == 0 {
				return "", 0, invalidUTF8()
			}
			b.WriteRune(cp)
		default:
			base, maxDigits := 0, 0
			if c >= '0' && c <= '7' {
				base, maxDigits = 8, 3
				i--
			}
			if c == 'x' {
				base, maxDigits = 16, 2
			}
			if base == 0 {
				b.WriteByte(c)
				continue
			}
			start := i
			for i < len(s) && i-start < maxDigits {
				d := s[i]
				ok := d >= '0' && d <= '7'
				if base == 16 {
					ok = d >= '0' && d <= '9' || d >= 'a' && d <= 'f' || d >= 'A' && d <= 'F'
				}
				if !ok {
					break
				}
				i++
			}
			if i == start {
				b.WriteByte(c)
				continue
			}
			n, _ := strconv.ParseUint(s[start:i], base, 16)
			b.WriteByte(byte(n))
		}
	}
	return "", 0, syntaxErr("unterminated quoted string")
}

// lexUnicodeString consumes U& strings/identifiers and an optional UESCAPE.
func lexUnicodeString(s string, quote byte) (string, int, error) {
	raw, n, err := lexQuoted(s[2:], quote)
	if err != nil {
		return "", 0, err
	}
	n += 2
	escape := '\\'
	tail := n
	for tail < len(s) && unicode.IsSpace(rune(s[tail])) {
		tail++
	}
	if tail+7 <= len(s) && strings.EqualFold(s[tail:tail+7], "UESCAPE") && (tail+7 == len(s) || !isIdentStart(s[tail+7:])) {
		tail += 7
		for tail < len(s) && unicode.IsSpace(rune(s[tail])) {
			tail++
		}
		if tail == len(s) || s[tail] != '\'' {
			return "", 0, syntaxErr("UESCAPE must be a single character")
		}
		esc, size, err := lexQuoted(s[tail:], '\'')
		if err != nil {
			return "", 0, err
		}
		runes := []rune(esc)
		if len(runes) != 1 || strings.ContainsRune("0123456789abcdefABCDEF+'\"", runes[0]) || unicode.IsSpace(runes[0]) {
			return "", 0, syntaxErr("invalid Unicode escape character")
		}
		escape = runes[0]
		n = tail + size
	}
	var b strings.Builder
	rs := []rune(raw)
	var high rune
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == escape {
			i++
			if i == len(rs) {
				return "", 0, syntaxErr("invalid Unicode escape")
			}
			if rs[i] == escape {
				c = escape
			} else {
				digits := 4
				if rs[i] == '+' {
					digits = 6
					i++
				}
				if i+digits > len(rs) {
					return "", 0, syntaxErr("invalid Unicode escape")
				}
				v, err := strconv.ParseUint(string(rs[i:i+digits]), 16, 32)
				if err != nil {
					return "", 0, syntaxErr("invalid Unicode escape")
				}
				c = rune(v)
				i += digits - 1
			}
		}
		if high != 0 {
			if c < 0xDC00 || c > 0xDFFF {
				return "", 0, syntaxErr("invalid Unicode surrogate pair")
			}
			c = utf16.DecodeRune(high, c)
			high = 0
		} else if c >= 0xD800 && c <= 0xDBFF {
			high = c
			continue
		}
		if !utf8.ValidRune(c) || c == 0 {
			return "", 0, invalidUTF8()
		}
		b.WriteRune(c)
	}
	if high != 0 {
		return "", 0, syntaxErr("invalid Unicode surrogate pair")
	}
	return b.String(), n, nil
}

func foldIdentifier(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
