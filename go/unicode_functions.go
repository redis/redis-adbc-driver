// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func unicodeFunction(name string) bool { return name == "NORMALIZE" || name == "UNISTR" }

// Text signatures resolve before strict NULL handling, even in unused branches.
// Value errors are evaluated separately so lazy CASE/COALESCE keep their meaning.
func unicodeCallError(f *Func, args []ColType) error {
	if !unicodeFunction(f.Name) {
		return nil
	}
	for _, t := range args {
		if t.Kind != KindNull && (t.Kind != KindString || t.Alias != "") {
			return noSuchFunction(f.Name, false, args)
		}
	}
	return nil
}

func pgUnicode(f *Func, args []Value) (Value, error) {
	if err := unicodeCallError(f, argTypes(args)); err != nil {
		return Value{}, err
	}
	for _, v := range args {
		if v.Null {
			return nullValue(typeString), nil
		}
	}
	text := func(v Value) string {
		if v.T.isChar() {
			return v.Text()
		}
		return v.S
	}
	s := text(args[0])
	if !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return Value{}, stateErr("22021", "invalid byte sequence for encoding UTF8")
	}
	if f.Name == "UNISTR" {
		out, err := pgUnistr(s)
		return stringValue(out), err
	}
	form := norm.NFC
	if len(args) == 2 {
		switch strings.ToUpper(text(args[1])) {
		case "NFC":
			form = norm.NFC
		case "NFD":
			form = norm.NFD
		case "NFKC":
			form = norm.NFKC
		case "NFKD":
			form = norm.NFKD
		default:
			return Value{}, stateErr(stateInvalidParameter, "invalid normalization form: %s", text(args[1]))
		}
	}
	return stringValue(pgNormalize(s, form)), nil
}

// PostgreSQL 18 uses Unicode 16. These are the Unicode 17 additions with
// nonzero combining class or decomposition, from DerivedAge/UnicodeData 17.0.0.
// Unicode data copyright 2025 Unicode, Inc.; see unicode-LICENSE.txt.
// In Unicode 16 they are inert starters, so do not let the Go 1.27 tables
// decompose, reorder or compose across them. Normalization stability preserves
// properties of earlier assigned characters. See the validation provenance.
func unicode17Normalization(r rune) bool {
	return r >= 0x1acf && r <= 0x1add || r >= 0x1ae0 && r <= 0x1aeb ||
		r == 0xa7f1 || r >= 0x10efa && r <= 0x10efb || r == 0x1e6e3 || r == 0x1e6e6 ||
		r >= 0x1e6ee && r <= 0x1e6ef || r == 0x1e6f5
}

func pgCombiningClass(r rune) uint8 {
	if unicode17Normalization(r) {
		return 0
	}
	return norm.NFD.PropertiesString(string(r)).CCC()
}

// Normalize without x/text's stream-safe insertion of CGJ every 30 nonstarters.
// Decompose individual scalars, stably order each nonstarter run, then compose
// unblocked pairs. Stable sorting avoids quadratic insertion sorting on long
// adversarial combining runs. Pair normalization has
// at most two scalars and cannot reach the stream-safe limit.
func pgNormalize(s string, form norm.Form) string {
	decomposition := norm.NFD
	if form == norm.NFKC || form == norm.NFKD {
		decomposition = norm.NFKD
	}
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode17Normalization(r) {
			runes = append(runes, r)
		} else {
			runes = append(runes, []rune(decomposition.String(string(r)))...)
		}
	}
	// Cache classes: comparison does not allocate or repeat Unicode table lookups.
	type mark struct {
		r rune
		c uint8
	}
	marks := make([]mark, len(runes))
	for i, r := range runes {
		marks[i] = mark{r, pgCombiningClass(r)}
	}
	for start := 0; start < len(marks); {
		if marks[start].c == 0 {
			start++
			continue
		}
		end := start + 1
		for end < len(marks) && marks[end].c != 0 {
			end++
		}
		sort.SliceStable(marks[start:end], func(i, j int) bool { return marks[start+i].c < marks[start+j].c })
		start = end
	}
	out := runes[:0]
	starter, lastClass := -1, uint8(0)
	for _, m := range marks {
		if (form == norm.NFC || form == norm.NFKC) && starter >= 0 && (lastClass == 0 || lastClass < m.c) && !unicode17Normalization(out[starter]) && !unicode17Normalization(m.r) {
			pair := norm.NFC.String(string([]rune{out[starter], m.r}))
			r, n := utf8.DecodeRuneInString(pair)
			if n == len(pair) {
				out[starter] = r
				continue
			}
		}
		if m.c == 0 {
			starter = len(out)
		}
		out = append(out, m.r)
		lastClass = m.c
	}
	return string(out)
}

// UNISTR is a single left-to-right decode. Produced backslashes are never
// rescanned, and surrogate halves must be adjacent escape sequences.
func pgUnistr(s string) (string, error) {
	var out strings.Builder
	out.Grow(len(s))
	var high rune
	badPair := func() (string, error) { return "", stateErr(stateSyntax, "invalid Unicode surrogate pair") }
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			if high != 0 {
				return badPair()
			}
			out.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '\\' {
			if high != 0 {
				return badPair()
			}
			out.WriteByte('\\')
			i += 2
			continue
		}
		offset, digits := 1, 4
		if i+1 < len(s) {
			switch s[i+1] {
			case 'u':
				offset, digits = 2, 4
			case '+':
				offset, digits = 2, 6
			case 'U':
				offset, digits = 2, 8
			}
		}
		if len(s)-i < offset+digits {
			return "", stateErr(stateSyntax, "invalid Unicode escape")
		}
		var code uint32
		for j := 0; j < digits; j++ {
			c := s[i+offset+j]
			var v byte
			switch {
			case c >= '0' && c <= '9':
				v = c - '0'
			case c >= 'a' && c <= 'f':
				v = c - 'a' + 10
			case c >= 'A' && c <= 'F':
				v = c - 'A' + 10
			default:
				return "", stateErr(stateSyntax, "invalid Unicode escape")
			}
			code = code<<4 | uint32(v)
		}
		if code == 0 || code > utf8.MaxRune {
			return "", stateErr(stateInvalidParameter, "invalid Unicode code point: %04X", code)
		}
		r := rune(code)
		if high != 0 {
			if r < 0xdc00 || r > 0xdfff {
				return badPair()
			}
			r = 0x10000 + (high-0xd800)*0x400 + (r - 0xdc00)
			high = 0
		} else if r >= 0xdc00 && r <= 0xdfff {
			return badPair()
		}
		if r >= 0xd800 && r <= 0xdbff {
			high = r
		} else {
			out.WriteRune(r)
		}
		i += offset + digits
	}
	if high != 0 {
		return badPair()
	}
	return out.String(), nil
}
