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
	"context"
	"fmt"
	"strings"
	"testing"
)

// Which types ALTER COLUMN … TYPE converts to without USING, as on
// postgres:16: for each pair, an empty one-column table altered from the
// row's type to the column's (Y: it worked).
func TestAlterTypeAssignable(t *testing.T) {
	types := []string{"boolean", "smallint", "integer", "bigint", "real", "double precision", "numeric(10,2)", "numeric",
		"text", "varchar(5)", "char(3)", "bytea", "date", "time", "time(2)", "timestamp", "timestamp(2)", "timestamptz", "interval",
		"timetz", "timetz(2)"}
	pg := map[string]string{
		"boolean":          "Y . . . . . . . Y Y Y . . . . . . . . . .",
		"smallint":         ". Y Y Y Y Y Y Y Y Y Y . . . . . . . . . .",
		"integer":          ". Y Y Y Y Y Y Y Y Y Y . . . . . . . . . .",
		"bigint":           ". Y Y Y Y Y Y Y Y Y Y . . . . . . . . . .",
		"real":             ". Y Y Y Y Y Y Y Y Y Y . . . . . . . . . .",
		"double precision": ". Y Y Y Y Y Y Y Y Y Y . . . . . . . . . .",
		"numeric(10,2)":    ". Y Y Y Y Y Y Y Y Y Y . . . . . . . . . .",
		"numeric":          ". Y Y Y Y Y Y Y Y Y Y . . . . . . . . . .",
		"text":             ". . . . . . . . Y Y Y . . . . . . . . . .",
		"varchar(5)":       ". . . . . . . . Y Y Y . . . . . . . . . .",
		"char(3)":          ". . . . . . . . Y Y Y . . . . . . . . . .",
		"bytea":            ". . . . . . . . Y Y Y Y . . . . . . . . .",
		"date":             ". . . . . . . . Y Y Y . Y . . Y Y Y . . .",
		"time":             ". . . . . . . . Y Y Y . . Y Y . . . Y Y Y",
		"time(2)":          ". . . . . . . . Y Y Y . . Y Y . . . Y Y Y",
		"timestamp":        ". . . . . . . . Y Y Y . Y Y Y Y Y Y . . .",
		"timestamp(2)":     ". . . . . . . . Y Y Y . Y Y Y Y Y Y . . .",
		"timestamptz":      ". . . . . . . . Y Y Y . Y Y Y Y Y Y . Y Y",
		"interval":         ". . . . . . . . Y Y Y . . Y Y . . . Y . .",
		"timetz":           ". . . . . . . . Y Y Y . . Y Y . . . . Y Y",
		"timetz(2)":        ". . . . . . . . Y Y Y . . Y Y . . . . Y Y",
	}
	parse := func(s string) ColType {
		ct, err := parseTypeText(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		return ct
	}
	for _, from := range types {
		var got []string
		for _, to := range types {
			if assignable(parse(from), parse(to)) {
				got = append(got, "Y")
			} else {
				got = append(got, ".")
			}
		}
		if g := strings.Join(got, " "); g != pg[from] {
			t.Errorf("from %s:\n got: %s\nwant: %s", from, g, pg[from])
		}
	}
}

// Postgres's type names in ALTER COLUMN … TYPE's errors.
func TestAlterTypeNames(t *testing.T) {
	for _, c := range []struct{ typ, name, typmod string }{
		{"timestamp(3)", "timestamp without time zone", "timestamp(3) without time zone"},
		{"timestamptz", "timestamp with time zone", "timestamp with time zone"},
		{"timestamp(0) with time zone", "timestamp with time zone", "timestamp(0) with time zone"},
		{"time(2)", "time without time zone", "time(2) without time zone"},
		{"time", "time without time zone", "time without time zone"},
		{"timetz", "time with time zone", "time with time zone"},
		{"time(3) with time zone", "time with time zone", "time(3) with time zone"},
		{"numeric(5,1)", "numeric", "numeric(5,1)"},
		{"varchar(5)", "character varying", "character varying(5)"},
		{"char(4)", "character", "character(4)"},
		{"text", "text", "text"},
		{"bytea", "bytea", "bytea"},
		{"bool", "boolean", "boolean"},
		{"int2", "smallint", "smallint"},
		{"int", "integer", "integer"},
		{"int8", "bigint", "bigint"},
		{"float4", "real", "real"},
		{"float8", "double precision", "double precision"},
		{"date", "date", "date"},
		{"interval", "interval", "interval"},
	} {
		ct, err := parseTypeText(c.typ)
		if err != nil {
			t.Fatal(err)
		}
		if got := pgTypeName(ct, false); got != c.name {
			t.Errorf("%s: %q, want %q", c.typ, got, c.name)
		}
		if got := pgTypeName(ct, true); got != c.typmod {
			t.Errorf("%s with typmod: %q, want %q", c.typ, got, c.typmod)
		}
	}
}

// applyAlter's ALTER COLUMN … TYPE: the metadata it leaves, whether the
// rows are rewritten, and Postgres's errors.
func TestApplyAlterType(t *testing.T) {
	table := func() *tableMeta {
		return &tableMeta{Schema: "public", Name: "t", Columns: []columnMeta{
			{Name: "id", Type: typeInt32, Nullable: true, Indexed: true},
			{Name: "a", Type: typeInt32, Nullable: true, Indexed: true},
			{Name: "v", Type: typeInt32, Nullable: false, Indexed: true, Default: "4"},
			{Name: "s", Type: ColType{Kind: KindString, Length: 5}, Nullable: true, Indexed: true, Default: "'x'"},
			{Name: "b", Type: typeBinary, Nullable: true},
		}, Checks: []checkMeta{{Name: "c", Expr: "v > 0"}, {Name: "len", Expr: "length(s) < 5"}}}
	}
	describe := func(m *tableMeta) string {
		var out []string
		for _, c := range m.Columns {
			s := c.Name + " " + strings.ToLower(c.Type.SQLName())
			if !c.Nullable {
				s += " not null"
			}
			if c.Default != "" {
				s += " default " + c.Default
			}
			if c.Indexed {
				s += " indexed"
			}
			out = append(out, s)
		}
		for _, k := range m.Checks {
			out = append(out, fmt.Sprintf("%s: %s", k.Name, k.Expr))
		}
		return strings.Join(out, "; ")
	}
	const base = "id integer indexed; a integer indexed; v integer not null default 4 indexed; s varchar(5) default 'x' indexed; b varbinary; c: v > 0; len: length(s) < 5"
	views := &alterInputs{views: map[string]string{"a": `rule _RETURN on view w depends on column "a"`}}
	e := &executor{cache: newExecCache()}
	for _, c := range []struct {
		sql, want, rewrite, rechecked string
		in                            *alterInputs
	}{
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE bigint`, rewrite: "true",
			want: strings.Replace(base, "a integer", "a bigint", 1)},
		{sql: `ALTER TABLE t ALTER COLUMN a SET DATA TYPE smallint`, rewrite: "true",
			want: strings.Replace(base, "a integer", "a smallint", 1)},
		// The values stay as they are: only the metadata changes.
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE varchar(10)`, rewrite: "false", rechecked: "len",
			want: strings.Replace(base, "varchar(5)", "varchar(10)", 1)},
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE text`, rewrite: "false", rechecked: "len",
			want: strings.Replace(base, "varchar(5)", "varchar", 1)},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE integer`, rewrite: "false", want: base},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE integer USING a + 1`, rewrite: "true", want: base},
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE varchar(3)`, rewrite: "true", rechecked: "len",
			want: strings.Replace(base, "varchar(5)", "varchar(3)", 1)},
		// The default converts (an integer to text, a string literal of the
		// column's type to a longer string), and the CHECKs bind again.
		{sql: `ALTER TABLE t ALTER COLUMN v TYPE text`, rewrite: "true", rechecked: "c",
			want: strings.Replace(base, "v integer", "v varchar", 1)},
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE char(5)`, rewrite: "true", rechecked: "len",
			want: strings.Replace(base, "varchar(5)", "char(5)", 1)},
		// A column the index can't hold, and one that becomes indexable.
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE bytea USING NULL`, rewrite: "true",
			want: strings.Replace(base, "a integer indexed", "a varbinary", 1)},
		{sql: `ALTER TABLE t ALTER COLUMN b TYPE text`, rewrite: "true",
			want: strings.Replace(base, "b varbinary", "b varchar indexed", 1)},
		// USING reads the table before the statement: a column it drops, and
		// the old value of another one it alters.
		{sql: `ALTER TABLE t DROP COLUMN a, ALTER COLUMN id TYPE bigint USING a`, rewrite: "true",
			want: strings.Replace(strings.Replace(base, "a integer indexed; ", "", 1), "id integer", "id bigint", 1)},
		{sql: `ALTER TABLE t ALTER COLUMN id TYPE text USING a::text, ALTER COLUMN a TYPE text USING id::text`, rewrite: "true",
			want: strings.Replace(strings.Replace(base, "id integer", "id varchar", 1), "a integer", "a varchar", 1)},
		// A first ALTER COLUMN … TYPE that doesn't change the type is
		// replaced by a second.
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE integer USING id, ALTER COLUMN a TYPE bigint`, rewrite: "true",
			want: strings.Replace(base, "a integer", "a bigint", 1)},
		// With other actions, in Postgres's order.
		{sql: `ALTER TABLE t ALTER COLUMN v DROP DEFAULT, ALTER COLUMN v TYPE boolean USING v > 0, ALTER COLUMN v SET DEFAULT true, DROP CONSTRAINT c`,
			rewrite: "true", want: "id integer indexed; a integer indexed; v boolean not null default true indexed; s varchar(5) default 'x' indexed; b varbinary; len: length(s) < 5"},
		{sql: `ALTER TABLE t ADD COLUMN n int DEFAULT 3, ALTER COLUMN a TYPE bigint, ADD CONSTRAINT k CHECK (a < 10)`, rewrite: "true",
			want: "id integer indexed; a bigint indexed; v integer not null default 4 indexed; s varchar(5) default 'x' indexed; b varbinary; n integer default 3 indexed; c: v > 0; len: length(s) < 5; k: a < 10"},
		// Values are converted when the rows are rewritten.
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE smallint USING 100000`, rewrite: "true",
			want: strings.Replace(base, "a integer", "a smallint", 1)},

		// Errors, as Postgres's.
		{sql: `ALTER TABLE t ALTER COLUMN nope TYPE integer`, want: `column "nope" of relation "t" does not exist`},
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE integer`,
			want: `column "s" cannot be cast automatically to type integer; you might need to specify "USING s::integer"`},
		{sql: `ALTER TABLE t ALTER COLUMN v TYPE boolean`,
			want: `column "v" cannot be cast automatically to type boolean; you might need to specify "USING v::boolean"`},
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE timestamp(3)`,
			want: `column "s" cannot be cast automatically to type timestamp without time zone; you might need to specify "USING s::timestamp(3) without time zone"`},
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE integer USING s`,
			want: `result of USING clause for column "s" cannot be cast automatically to type integer; you might need to add an explicit cast`},
		{sql: `ALTER TABLE t ALTER COLUMN v TYPE boolean USING v > 0`, want: `default for column "v" cannot be cast automatically to type boolean`},
		// The driver's length() takes an integer (Postgres's doesn't).
		{sql: `ALTER TABLE t ALTER COLUMN s DROP DEFAULT, ALTER COLUMN s TYPE integer USING length(s)`, rewrite: "true", rechecked: "len",
			want: strings.Replace(base, "s varchar(5) default 'x'", "s integer", 1)},
		{sql: `ALTER TABLE t ALTER COLUMN s TYPE integer USING length(s)`, want: `default for column "s" cannot be cast automatically to type integer`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE bigint, ALTER COLUMN a TYPE text`, want: `cannot alter type of column "a" twice`},
		{sql: `ALTER TABLE t DROP COLUMN a, ALTER COLUMN a TYPE text`, want: `column "a" of relation "t" does not exist`},
		{sql: `ALTER TABLE t ADD COLUMN z int, ALTER COLUMN z TYPE text`, want: `column "z" of relation "t" does not exist`},
		{sql: `ALTER TABLE t ALTER COLUMN id TYPE int USING z, ADD COLUMN z int`, want: `column "z" does not exist in table "t"`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE int USING (SELECT 1)`, want: `cannot use subquery in transform expression`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE int USING count(*)`, want: `aggregate functions are not allowed in transform expressions`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE int USING row_number() OVER ()`, want: `window functions are not allowed in transform expressions`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE int USING generate_series(1, 2)`, want: `set-returning functions are not allowed in transform expressions`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE int USING $1`, want: `there is no parameter $1`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE bigint`, in: views, want: `cannot alter type of a column used by a view or rule; rule _RETURN on view w depends on column "a"`},
		{sql: `ALTER TABLE t ALTER COLUMN a TYPE integer`, in: views, want: `cannot alter type of a column used by a view or rule; rule _RETURN on view w depends on column "a"`},
		{sql: `ALTER TABLE t ALTER COLUMN id TYPE bigint`, in: views, rewrite: "true", want: strings.Replace(base, "id integer", "id bigint", 1)},
	} {
		m := table()
		res, err := e.applyAlter(context.Background(), m, parseAlter(t, c.sql).Cmds, c.in)
		got := ""
		if err != nil {
			got = errText(err)
		} else {
			got = describe(m)
			if c.rewrite != "" && fmt.Sprint(res.rewrite) != c.rewrite {
				t.Errorf("%s: rewrite %v, want %s", c.sql, res.rewrite, c.rewrite)
			}
			var names []string
			for _, k := range res.rechecked {
				names = append(names, k.Name)
			}
			if n := strings.Join(names, " "); n != c.rechecked {
				t.Errorf("%s: CHECKs bound again %q, want %q", c.sql, n, c.rechecked)
			}
		}
		if got != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.sql, got, c.want)
		}
	}

	// A CHECK that no longer binds for the new type fails the statement, as
	// in Postgres.
	m := &tableMeta{Schema: "public", Name: "t", Columns: []columnMeta{{Name: "f", Type: typeBool, Nullable: true}},
		Checks: []checkMeta{{Name: "t_f_check", Expr: "f"}}}
	_, err := e.applyAlter(context.Background(), m, parseAlter(t, `ALTER TABLE t ALTER COLUMN f TYPE integer USING f::integer`).Cmds, nil)
	if want := "argument of CHECK must be type boolean, not type integer"; err == nil || errText(err) != want {
		t.Errorf("got %v, want %q", err, want)
	}
}
