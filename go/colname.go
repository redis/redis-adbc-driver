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

// The names Postgres gives output columns, which a bare name in ORDER BY
// and DISTINCT ON matches before it matches an input column.
//
// An output column is named by its alias, or by FigureColname
// (parse_target.c) from the expression: a column reference by the column,
// a function call by the function, CAST(x AS t) and x::t by x if that has a
// name and by t's type name (int4, varchar) if not, CASE by its ELSE in the
// same way (or "case"), a scalar subquery by its column, EXISTS by
// "exists", a row by "row", a field (r).f1 by the field, and anything else
// (operators, literals) "?column?". The
// driver's own name for an unaliased expression is its text, so these are
// kept apart (planItem.pgName).

import (
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// figureColname returns the name Postgres gives the output column of the
// unaliased select-list expression e.
func figureColname(e Expr) string {
	if name, _ := figureName(e); name != "" {
		return name
	}
	return "?column?"
}

// figureName returns figureColname's name for e and how good a name it is,
// as in Postgres: 2 for a column's or a function's name, 1 for a type's name
// or "case" (which an enclosing cast or CASE replaces with its own), 0 for
// no name.
//
// e may be bound, and bound again (a cached statement is planned again):
// binding keeps a column's name as written, and moves an aggregate of an
// enclosing query there, leaving COALESCE(ref) (hoistAggregate).
func figureName(e Expr) (string, int) {
	switch x := e.(type) {
	case *ColumnRef:
		return x.writtenName(), 2
	case *Func:
		return funcColname(x)
	case *WindowFunc:
		return funcColname(x.Func)
	case *Cast:
		if name, s := figureName(x.X); s == 2 {
			return name, 2
		}
		if x.typeName != "" {
			return x.typeName, 1
		}
		return typeColname(x.T), 1
	case *Case:
		if name, s := figureName(x.Else); s == 2 {
			return name, 2
		}
		return "case", 1
	case *Subquery:
		switch x.Kind {
		case SubqueryExists:
			return "exists", 2
		case SubqueryScalar:
			// The subquery's column, also when Postgres has no name for it
			// ("?column?").
			if x.plan != nil && len(x.plan.items) > 0 {
				return x.plan.items[0].pgName, 2
			}
		}
	case *RowExpr:
		return "row", 2
	case *FieldSelect:
		return x.Name, 2 // (r).f1
	case *Literal:
		// A typed literal (DATE '…', INTERVAL '1 day'), which Postgres reads
		// as a cast of the string.
		switch x.V.T.Kind {
		case KindDate, KindTime, KindTimeTZ, KindTimestamp, KindInterval:
			if !x.V.Null {
				return typeColname(x.V.T), 1
			}
		}
	}
	return "", 0
}

// funcColname is figureName for a call: the function's name, in lower case.
// The parser also makes calls of operators and of special forms, which
// Postgres names as it parses them.
func funcColname(f *Func) (string, int) {
	if f.named {
		return strings.ToLower(f.Name), 2
	}
	switch f.Name {
	case "LIKE", "ILIKE", "SIMILAR TO", "~", "~*", "->", "->>", "#>", "#>>", "MOD", "__IS_JSON":
		// Operators: x LIKE p, x ~ p, j -> k, a % b, x IS JSON.
		return "", 0
	case "DATE_PART":
		return "extract", 2 // EXTRACT(field FROM x)
	case "__INTERVAL":
		return "interval", 1 // INTERVAL '1' DAY: a cast of the string
	case "__JSON", "__JSONB":
		// CAST(x AS JSON), x::jsonb
		if name, s := figureName(f.Args[0]); s == 2 {
			return name, 2
		}
		return strings.ToLower(f.Name[2:]), 1
	case "__JSON_OBJECT", "__JSON_ARRAY":
		return strings.ToLower(f.Name[2:]), 2
	case "COALESCE":
		// An aggregate of an enclosing query (hoistAggregate).
		if len(f.Args) == 1 {
			if r, ok := f.Args[0].(*ColumnRef); ok && r.outerAgg != nil {
				return funcColname(r.outerAgg.fn)
			}
		}
	}
	// x AT TIME ZONE z (timezone), TRIM(LEADING FROM s) (ltrim),
	// SUBSTRING(s FROM n), CURRENT_DATE, …
	return strings.ToLower(f.Name), 2
}

// typeColname is the name Postgres gives type t (its pg_type name), which
// names an unaliased cast to it: int4 for INTEGER, float8 for DOUBLE
// PRECISION, bpchar for CHAR(n). A string type without a length is text;
// the parser notes VARCHAR (castTypeName).
func typeColname(t ColType) string {
	switch t.Kind {
	case KindBool:
		return "bool"
	case KindInt16:
		return "int2"
	case KindInt32:
		return "int4"
	case KindInt64:
		return "int8"
	case KindFloat32:
		return "float4"
	case KindFloat64:
		return "float8"
	case KindDecimal:
		return "numeric"
	case KindString:
		switch {
		case t.Fixed:
			return "bpchar"
		case t.Length > 0:
			return "varchar"
		}
		return "text"
	case KindBinary:
		return "bytea"
	case KindDate:
		return "date"
	case KindTime:
		return "time"
	case KindTimeTZ:
		return "timetz"
	case KindTimestamp:
		if t.TZ != "" {
			return "timestamptz"
		}
		return "timestamp"
	case KindInterval:
		return "interval"
	}
	return "unknown"
}

// castTypeName is the name Postgres gives the type spec names, t: as
// typeColname, except that VARCHAR without a length is varchar, not text.
func castTypeName(spec sqlTypeSpec, t ColType) string {
	switch spec.Name {
	case "VARCHAR", "CHARACTER VARYING", "NVARCHAR":
		return "varchar"
	}
	return typeColname(t)
}

// writtenName is the column name of c as written (binding sets Name to the
// column's canonical name).
func (c *ColumnRef) writtenName() string {
	if c.written != "" {
		return c.written
	}
	return c.Name
}

// findOutputColumn returns the output column a bare name in clause (ORDER
// BY or DISTINCT ON) names, or nil if there is none, as Postgres finds it
// (findTargetlistEntrySQL92): the columns of that name must all be the same
// expression (SELECT a, a … ORDER BY a), otherwise the name is ambiguous
// (SELECT a AS b, b … ORDER BY b).
func findOutputColumn(items []planItem, name, clause string) (*planItem, error) {
	var match *planItem
	for i := range items {
		it := &items[i]
		if !strings.EqualFold(it.pgName, name) {
			continue
		}
		if match == nil {
			match = it
		} else if !exprEqual(match.expr, it.expr) {
			return nil, errorf(adbc.StatusInvalidArgument, "%s %q is ambiguous", clause, name)
		}
	}
	return match, nil
}
