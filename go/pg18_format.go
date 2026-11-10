// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

func pgFormat(args []Value, z tzZone) (Value, error) {
	if args[0].Null {
		return nullValue(typeString), nil
	}
	format := args[0].textIn(z)
	next := 1
	var out strings.Builder
	arg := func(pos int) (Value, error) {
		if pos < 1 || pos >= len(args) {
			return Value{}, stateErr(stateInvalidParameter, "too few arguments for format()")
		}
		next = pos + 1
		return args[pos], nil
	}
	for i := 0; i < len(format); {
		if format[i] != '%' {
			out.WriteByte(format[i])
			i++
			continue
		}
		i++
		if i < len(format) && format[i] == '%' {
			out.WriteByte('%')
			i++
			continue
		}
		readNum := func() (int, error) {
			start := i
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
			if start == i {
				return 0, nil
			}
			n, err := strconv.ParseInt(format[start:i], 10, 32)
			return int(n), err
		}
		start := i
		n, err := readNum()
		if err != nil {
			return Value{}, stateErr(stateNumericRange, "number is out of range")
		}
		pos := 0
		if i < len(format) && format[i] == '$' {
			if n == 0 {
				return Value{}, stateErr(stateInvalidParameter, "format specifies argument 0")
			}
			pos = n
			i++
		} else {
			i = start
		}
		left := false
		if i < len(format) && format[i] == '-' {
			left = true
			i++
		}
		width := 0
		if i < len(format) && format[i] == '*' {
			i++
			p, err := readNum()
			if err != nil {
				return Value{}, err
			}
			if i < len(format) && format[i] == '$' {
				i++
			} else {
				if p != 0 {
					return Value{}, stateErr(stateInvalidParameter, "width argument position must be followed by $")
				}
				p = next
			}
			v, err := arg(p)
			if err != nil {
				return Value{}, err
			}
			if !v.Null {
				w, err := intArg("FORMAT", "width", v)
				if err != nil {
					return Value{}, err
				}
				if w < math.MinInt32 || w > math.MaxInt32 {
					return Value{}, stateErr(stateNumericRange, "integer out of range")
				}
				if w < 0 {
					left = true
					w = -w
				}
				if w > maxStringLen {
					return Value{}, stateErr(stateNumericRange, "format width too large")
				}
				width = int(w)
			}
		} else {
			width, err = readNum()
			if err != nil {
				return Value{}, stateErr(stateNumericRange, "number is out of range")
			}
		}
		if i >= len(format) {
			return Value{}, stateErr(stateInvalidParameter, "unterminated format() type specifier")
		}
		kind := format[i]
		i++
		if kind != 's' && kind != 'I' && kind != 'L' {
			return Value{}, stateErr(stateInvalidParameter, "unrecognized format() type specifier %q", string(kind))
		}
		if pos == 0 {
			pos = next
		}
		v, err := arg(pos)
		if err != nil {
			return Value{}, err
		}
		if !v.Null && v.T.Kind == KindBool {
			if v.I == 0 {
				v = stringValue("f")
			} else {
				v = stringValue("t")
			}
		}
		s := ""
		switch kind {
		case 's':
			if !v.Null {
				s = v.textIn(z)
			}
		case 'I':
			if v.Null {
				return Value{}, stateErr(stateNullNotAllowed, "null values cannot be formatted as an SQL identifier")
			}
			s = pgQuoteIdent(v.textIn(z))
		case 'L':
			if v.Null {
				s = "NULL"
			} else {
				q, err := pg18Scalar(&Func{Name: "QUOTE_LITERAL"}, []Value{v}, z)
				if err != nil {
					return Value{}, err
				}
				s = q.S
			}
		}
		padding := max(width-utf8.RuneCountInString(s), 0)
		if padding > maxStringLen || out.Len()+len(s)+padding > maxStringLen {
			return Value{}, stateErr(stateNumericRange, "format result too large")
		}
		if !left {
			out.WriteString(strings.Repeat(" ", padding))
		}
		out.WriteString(s)
		if left {
			out.WriteString(strings.Repeat(" ", padding))
		}
	}
	if out.Len() > maxStringLen {
		return Value{}, fmt.Errorf("format result too large")
	}
	return stringValue(out.String()), nil
}

func (p *parser) parseMakeInterval() (Expr, error) {
	f := &Func{Name: "MAKE_INTERVAL", named: true, Args: make([]Expr, 7)}
	for i := range f.Args {
		f.Args[i] = &Literal{V: intValue(typeInt32, 0)}
	}
	positions := map[string]int{"years": 0, "months": 1, "weeks": 2, "days": 3, "hours": 4, "mins": 5, "secs": 6}
	used := map[int]bool{}
	pos := 0
	named := false
	if !p.isOp(")") {
		for {
			slot := pos
			if p.peek().kind == tokIdent && (p.peekAt(1).text == "=>" || p.peekAt(1).text == ":=") {
				name := p.next().text
				p.pos++
				var ok bool
				slot, ok = positions[strings.ToLower(name)]
				if !ok {
					return nil, stateErr(stateUndefinedFunction, "function make_interval has no argument named %q", name)
				}
				named = true
			} else if named {
				return nil, syntaxErr("positional argument cannot follow named argument")
			}
			if slot >= 7 {
				return nil, stateErr(stateUndefinedFunction, "too many arguments for make_interval")
			}
			if used[slot] {
				return nil, syntaxErr("argument name used more than once")
			}
			used[slot] = true
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			f.Args[slot] = x
			pos++
			if !p.acceptOp(",") {
				break
			}
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	return f, nil
}
