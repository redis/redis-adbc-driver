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

// Local-time literals in stored definitions.
//
// A view, a column DEFAULT and a CHECK are stored as SQL text and parsed
// again each time they are used, by the session that uses them. Text
// without an offset that is read as a timestamp with time zone, or as a
// time with time zone, is a local time in the session time zone, so such a
// literal would be read in the zone of whichever session reads the view or
// writes the row. Postgres reads it once: it converts the literal to an
// instant (or a time with an offset) when the definition is created (the
// constant is folded), and stores that.
//
// The driver does the same to the text it stores: each such literal is
// replaced by its instant, written with the offset of the creating
// session's zone ('2024-01-01 00:00' in New York becomes '2024-01-01
// 00:00:00-05', which reads the same in every zone; a time with time zone
// '10:00' becomes '10:00:00-04' in July). A literal is such a one, as in
// Postgres, if it is
//
//   - cast to a timestamp with time zone or a time with time zone:
//     TIMESTAMPTZ '…', '…'::timestamptz, CAST('…' AS TIMESTAMPTZ), TIMETZ
//     '…', '…'::timetz (also TRY_CAST and its fallback);
//   - or untyped and read as one because of what it meets: compared with a
//     timestamp with time zone or a time with time zone (=, <, IS DISTINCT
//     FROM, IN, BETWEEN, a CASE operand), a result of a CASE or an argument
//     of COALESCE, GREATEST or LEAST whose result is one (see unknown.go),
//     or of NULLIF with one, an operand of - with one (tstz -
//     '2024-01-01'), the operand of AT TIME ZONE and AT LOCAL, and a
//     column's DEFAULT itself when the column is one.
//
// Everything else keeps reading the session time zone when it runs, as in
// Postgres: casts of columns and expressions (ts::date, CAST(d AS
// TIMESTAMPTZ), TIMESTAMP '…'::timestamptz), CURRENT_DATE, NOW(),
// LOCALTIMESTAMP, text output and the field functions. Literals with an
// offset or a zone name, and text that doesn't read as a timestamp, are
// kept as written. Definitions stored by earlier versions keep their text,
// and so read their literals in the session time zone.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
)

// litSpan is where a string literal is written in the source: bytes
// [start, end). cast is set if the literal is cast to a timestamp with
// time zone.
type litSpan struct {
	lit        *Literal
	start, end int
	cast       bool
}

// noteLiteral returns the string literal token t as a literal, and records
// where it is written when p.lits is set.
func (p *parser) noteLiteral(t token) *Literal {
	lit := &Literal{V: stringValue(t.text), Unknown: true}
	if p.lits != nil {
		*p.lits = append(*p.lits, litSpan{lit: lit, start: t.pos, end: t.end})
	}
	return lit
}

// noteCast returns c, and marks its operand (and TRY_CAST's fallback) if
// it is a string literal cast to a timestamp with time zone.
func (p *parser) noteCast(c *Cast) *Cast {
	if p.lits == nil || !readsLocalTime(c.T) {
		return c
	}
	for _, x := range []Expr{c.X, c.OnError} {
		if lit, ok := x.(*Literal); ok {
			for i := range *p.lits {
				if (*p.lits)[i].lit == lit {
					(*p.lits)[i].cast = true
				}
			}
		}
	}
	return c
}

// parseViewText parses the stored text of a view, and returns where its
// string literals are.
func parseViewText(text string) (*SelectStmt, []litSpan, error) {
	toks, err := lex(text)
	if err != nil {
		return nil, nil, err
	}
	var lits []litSpan
	p := &parser{src: text, toks: toks, lits: &lits}
	st, err := p.parseSelect()
	if err != nil {
		return nil, nil, err
	}
	sel, ok := st.(*SelectStmt)
	if !ok || p.peek().kind != tokEOF {
		return nil, nil, syntaxErr("a view is a single SELECT")
	}
	return sel, lits, nil
}

// parseExprLits is parseExprText, which also returns where the string
// literals are.
func parseExprLits(text string) (Expr, []litSpan, error) {
	toks, err := lex(text)
	if err != nil {
		return nil, nil, err
	}
	var lits []litSpan
	p := &parser{src: text, toks: toks, lits: &lits}
	x, err := p.parseExpr()
	if err != nil {
		return nil, nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, nil, syntaxErr("unexpected %q", p.peek().text)
	}
	return x, lits, nil
}

// localTimeLiterals adds to out the untyped string literals of x (outside
// its subqueries) that are read as a timestamp with time zone because of
// what they meet (see the top of this file). typeOf gives the type of an
// expression of x, the zero type if it isn't known.
func localTimeLiterals(x Expr, typeOf func(Expr) ColType, out map[*Literal]bool) {
	text := func(x Expr) (*Literal, bool) {
		lit, ok := x.(*Literal)
		return lit, ok && !lit.V.Null && lit.V.T.Kind == KindString
	}
	// meets adds x if it is one read as other's type.
	meets := func(x, other Expr) {
		if lit, ok := text(x); ok {
			if _, both := text(other); !both && readsLocalTime(typeOf(other)) {
				out[lit] = true
			}
		}
	}
	// resultsOf adds the literals that x's value is one of (the results of
	// a CASE, the arguments of COALESCE, …), if they are read as its type.
	resultsOf := func(x Expr) {
		parts, _ := matchedParts(x)
		if parts == nil || !readsLocalTime(typeOf(x)) {
			return
		}
		for _, p := range parts {
			if lit, ok := text(p); ok {
				out[lit] = true
			}
		}
	}
	walkExpr(x, func(n Expr) {
		switch v := n.(type) {
		case *Binary:
			if isComparison(v.Op) || isDistinctOp(v.Op) {
				meets(v.L, v.R)
				meets(v.R, v.L)
			}
			if o, ok := operatorLiteral(v); ok && o.other != nil {
				// tstz - '2024-01-01': an operand read as the other's type.
				if t, ok, err := o.typeNext(typeOf(o.other)); err == nil && ok && readsLocalTime(t) {
					out[o.lit] = true
				}
			}
		case *Case:
			if v.Operand != nil {
				for _, w := range v.Whens {
					meets(w.When, v.Operand)
					meets(v.Operand, w.When)
				}
			}
			resultsOf(v)
		case *Func:
			d, ok := callee(v)
			switch {
			case !ok || len(v.Args) == 0:
			case v.Name == "NULLIF":
				if len(v.Args) == 2 {
					meets(v.Args[0], v.Args[1])
					meets(v.Args[1], v.Args[0])
				}
			case d.impl == implCoalesce || d.impl == implConditional || d.impl == implIIF:
				// COALESCE, GREATEST, LEAST, IIF: the arguments are read as
				// the result's type.
				resultsOf(v)
			case v.Name == "TIMEZONE":
				// x AT TIME ZONE z and x AT LOCAL read text x as a timestamp
				// with time zone.
				if lit, ok := text(v.Args[len(v.Args)-1]); ok {
					out[lit] = true
				}
			}
		}
	})
}

// noteLocalTimes records the literals of expr, just bound in the innermost
// scope, that localTimeLiterals finds (while a view is created, see
// runCreateView, or a CHECK, see foldCheck).
func (e *executor) noteLocalTimes(expr Expr) {
	types := e.scopeTypes()
	localTimeLiterals(expr, func(x Expr) ColType {
		t, err := inferType(x, types, e.paramTypes)
		if err != nil {
			return ColType{}
		}
		return t
	}, e.localLits)
}

// foldLocalTimes returns text, whose string literals are at lits, with each
// one that is cast to a timestamp with time zone, or is in untyped,
// replaced by its instant read in z (foldedLocalTime).
func foldLocalTimes(text string, lits []litSpan, untyped map[*Literal]bool, z tzZone) string {
	lits = slices.Clone(lits)
	slices.SortFunc(lits, func(a, b litSpan) int { return a.start - b.start })
	var b strings.Builder
	last := 0
	for _, l := range lits {
		if !l.cast && !untyped[l.lit] || l.start < last {
			continue
		}
		s, ok := foldedLocalTime(l.lit.V.S, z)
		if !ok {
			continue
		}
		b.WriteString(text[last:l.start])
		b.WriteString("'" + s + "'")
		last = l.end
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// foldedLocalTime returns timestamp text without an offset or a zone, s,
// read as a timestamp with time zone in z, as text with the offset of z at
// that instant, which reads as the same instant in every zone: in
// America/Los_Angeles, '2024-01-01 00:00' is '2024-01-01 00:00:00-08', and
// '2024-03-10 02:30', which the spring-forward transition skips, is
// '2024-03-10 03:30:00-07'. Where z's offset has seconds (local mean time
// before 1883 in America/New_York), the text is in UTC, since timestamp
// text can't have them. The fraction keeps its digits' unit. Time with time
// zone text without a date or a zone is read as one in z, as text with its
// offset (foldedTimeTZ). ok is false for text with an offset or a zone, or
// that isn't a timestamp or a time.
func foldedLocalTime(s string, z tzZone) (string, bool) {
	if _, _, _, hasTZ, err := parseTimestamp(s); err != nil || hasTZ {
		if err != nil {
			return foldedTimeTZ(s, z)
		}
		return "", false
	}
	v, err := timestampLiteralIn(s, true, z)
	if err != nil {
		return "", false
	}
	off := v.offsetIn(z)
	if off%60 != 0 {
		off = 0
	}
	sec, nanos := splitUnits(v.I, v.T.Unit)
	frac := fractionText(nanos)
	if v.T.Unit == arrow.Nanosecond {
		frac = fmt.Sprintf(".%09d", nanos) // more than 6 digits, as written
	}
	tm := time.Unix(sec+int64(off), 0).UTC()
	date, bc := dateText(tm)
	out := fmt.Sprintf("%s %02d:%02d:%02d%s%s", date, tm.Hour(), tm.Minute(), tm.Second(), frac, formatOffset(off, false))
	if bc {
		out += " BC"
	}
	if w, err := timestampLiteral(out, true); err != nil || w != v {
		return "", false
	}
	return out, true
}

// foldedTimeTZ returns time with time zone text without a zone, s, read in
// z (with z's offset on the current date), as text with that offset: in
// America/New_York in July, '10:00' is '10:00:00-04'. ok is false for text
// with a zone, or that isn't a time with time zone.
func foldedTimeTZ(s string, z tzZone) (string, bool) {
	if !timeTZLocal(s) {
		return "", false
	}
	v, err := parseTimeTZ(s, z)
	if err != nil {
		return "", false
	}
	return timeTZText(v), true
}

// foldView plans the body of a new view with plan, and returns its text
// (as parsed, parsed) to store, read in the session time zone. The body
// plan gets is parsed again from text, so that its literals can be found
// in the text.
func (e *executor) foldView(text string, parsed *SelectStmt, plan func(*SelectStmt) error) (string, error) {
	sel, lits, err := parseViewText(text)
	if err != nil {
		return text, plan(parsed) // not expected: the text parsed before
	}
	saved := e.localLits
	e.localLits = map[*Literal]bool{}
	err = plan(sel)
	untyped := e.localLits
	e.localLits = saved
	if err != nil {
		return "", err
	}
	return foldLocalTimes(text, lits, untyped, e.zone()), nil
}

// foldCheck returns the text of a new CHECK constraint of meta, which has
// been checked, to store, read in the session time zone.
func (e *executor) foldCheck(ctx context.Context, meta *tableMeta, text string) string {
	x, lits, err := parseExprLits(text)
	if err != nil {
		return text // not expected: the text parsed before
	}
	saved := e.localLits
	e.localLits = map[*Literal]bool{}
	err = e.bindCheck(ctx, meta, x)
	untyped := e.localLits
	e.localLits = saved
	if err != nil {
		return text // not expected: the constraint was bound before
	}
	return foldLocalTimes(text, lits, untyped, e.zone())
}

// foldDefault returns the text of a new DEFAULT of a column of type t,
// which has been checked, to store, read in the session time zone.
func (e *executor) foldDefault(text string, t ColType) string {
	x, lits, err := parseExprLits(text)
	if err != nil {
		return text // not expected: the text parsed before
	}
	untyped := map[*Literal]bool{}
	if lit, ok := x.(*Literal); ok && lit.V.T.Kind == KindString && readsLocalTime(t) {
		untyped[lit] = true // read as the column's type
	}
	localTimeLiterals(x, func(x Expr) ColType {
		t, err := inferType(x, nil, nil)
		if err != nil {
			return ColType{}
		}
		return t
	}, untyped)
	return foldLocalTimes(text, lits, untyped, e.zone())
}
