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

// parseAlter parses an ALTER TABLE statement.
func parseAlter(t *testing.T, sql string) *AlterTableStmt {
	t.Helper()
	stmts, err := ParseScript(sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return stmts[0].Stmt.(*AlterTableStmt)
}

// describeSteps names the steps of an ALTER TABLE in order.
func describeSteps(steps []alterStep) string {
	var out []string
	for _, s := range steps {
		c := s.cmd
		var d string
		switch {
		case s.checks:
			d = "checks of " + c.Def.Name
		case c.Action == AlterAddColumn:
			d = "add " + c.Def.Name
		case c.Action == AlterDropColumn:
			d = "drop " + c.Column
		case c.Action == AlterSetDefault:
			d = "set default " + c.Column
		case c.Action == AlterDropDefault:
			d = "drop default " + c.Column
		case c.Action == AlterSetNotNull:
			d = "set not null " + c.Column
		case c.Action == AlterDropNotNull:
			d = "drop not null " + c.Column
		case c.Action == AlterAddConstraint && c.Check != nil:
			d = "add check " + c.Check.Name
		case c.Action == AlterAddConstraint:
			d = "add other constraint"
		case c.Action == AlterDropConstraint:
			d = "drop constraint " + c.Constraint
		}
		out = append(out, d)
	}
	return strings.Join(out, ", ")
}

// The steps run in Postgres 16's passes: drops; ADD COLUMN; SET NOT NULL;
// then SET DEFAULT, the CHECKs of the ADD COLUMNs and those of ADD
// CONSTRAINT. Within a pass, in the order written.
func TestAlterOrder(t *testing.T) {
	for _, c := range []struct{ sql, want string }{
		{`ALTER TABLE t ADD COLUMN a int, DROP COLUMN a`, "drop a, add a"},
		{`ALTER TABLE t DROP COLUMN a, ADD COLUMN a text`, "drop a, add a"},
		{`ALTER TABLE t ADD COLUMN z int DEFAULT 9, ALTER COLUMN z DROP DEFAULT`, "drop default z, add z"},
		{`ALTER TABLE t ALTER COLUMN z SET DEFAULT 1, ADD COLUMN z int`, "add z, set default z"},
		{`ALTER TABLE t ADD CONSTRAINT c CHECK (v > 0), DROP CONSTRAINT c`, "drop constraint c, add check c"},
		{`ALTER TABLE t DROP CONSTRAINT c, ADD CONSTRAINT c CHECK (v > 1)`, "drop constraint c, add check c"},
		{`ALTER TABLE t ADD CONSTRAINT k CHECK (b > 0), ADD COLUMN b int`, "add b, add check k"},
		{`ALTER TABLE t ADD COLUMN b int NOT NULL DEFAULT 0, ALTER COLUMN b DROP NOT NULL`, "drop not null b, add b"},
		{`ALTER TABLE t ALTER COLUMN a SET NOT NULL, ADD COLUMN n int`, "add n, set not null a"},
		{`ALTER TABLE t ALTER COLUMN a SET NOT NULL, ALTER COLUMN a DROP NOT NULL`, "drop not null a, set not null a"},
		{`ALTER TABLE t ALTER COLUMN v SET DEFAULT 5, ALTER COLUMN v DROP DEFAULT`, "drop default v, set default v"},
		{`ALTER TABLE t ALTER COLUMN v SET DEFAULT 1, ALTER COLUMN v SET DEFAULT 2`, "set default v, set default v"},
		{`ALTER TABLE t ALTER COLUMN v SET DEFAULT 1, DROP COLUMN v`, "drop v, set default v"},
		// In the last pass: SET DEFAULT, then the column CHECKs, then ADD
		// CONSTRAINT's, whatever the order written.
		{`ALTER TABLE t ADD CHECK (v > 0), ADD COLUMN c int CHECK (v > 1), ALTER COLUMN v SET DEFAULT 1, ADD COLUMN d int CHECK (d > 0) CHECK (d < 9)`,
			"add c, add d, set default v, checks of c, checks of d, add check "},
		{`ALTER TABLE t ADD PRIMARY KEY (id), DROP COLUMN x, ADD UNIQUE (v), ADD COLUMN y int`,
			"drop x, add y, add other constraint, add other constraint"},
		// dbt's on_schema_change: added, then removed columns.
		{`alter table t add column "a" int, add column "b" text, drop column "c"`, "drop c, add a, add b"},
	} {
		if got := describeSteps(alterOrder(parseAlter(t, c.sql).Cmds)); got != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.sql, got, c.want)
		}
	}
}

// applyAlter, which reads nothing from Redis, gives Postgres's results and
// errors for the orders it runs the actions in.
func TestApplyAlterPasses(t *testing.T) {
	table := func() *tableMeta {
		return &tableMeta{Schema: "public", Name: "t", Columns: []columnMeta{
			{Name: "id", Type: typeInt32, Nullable: true},
			{Name: "a", Type: typeInt32, Nullable: true},
			{Name: "v", Type: typeInt32, Nullable: true, Default: "4"},
		}, Checks: []checkMeta{{Name: "c", Expr: "v > 0"}}}
	}
	// describe shows a table's columns (name, type, NOT NULL, default) and
	// constraints.
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
			out = append(out, s)
		}
		for _, k := range m.Checks {
			out = append(out, fmt.Sprintf("%s: %s", k.Name, k.Expr))
		}
		return strings.Join(out, "; ")
	}
	e := &executor{cache: newExecCache()}
	for _, c := range []struct{ sql, want, notNull string }{
		// Accepted, as Postgres accepts them.
		{`ALTER TABLE t DROP COLUMN a, ADD COLUMN a text`,
			"id integer; v integer default 4; a varchar; c: v > 0", ""},
		{`ALTER TABLE t ADD CONSTRAINT k CHECK (b > 0), ADD COLUMN b int`,
			"id integer; a integer; v integer default 4; b integer; c: v > 0; k: b > 0", ""},
		{`ALTER TABLE t DROP CONSTRAINT c, ADD CONSTRAINT c CHECK (v > 1)`,
			"id integer; a integer; v integer default 4; c: v > 1", ""},
		{`ALTER TABLE t ALTER COLUMN y SET DEFAULT 3, ADD COLUMN y int`,
			"id integer; a integer; v integer default 4; y integer default 3; c: v > 0", ""},
		{`ALTER TABLE t ALTER COLUMN v SET DEFAULT 5, ALTER COLUMN v DROP DEFAULT`,
			"id integer; a integer; v integer default 5; c: v > 0", ""},
		{`ALTER TABLE t ALTER COLUMN a SET NOT NULL, ALTER COLUMN a DROP NOT NULL, ADD COLUMN n int DEFAULT 1, ALTER COLUMN n SET NOT NULL`,
			"id integer; a integer not null; v integer default 4; n integer not null default 1; c: v > 0", "a n"},
		{`ALTER TABLE t ADD COLUMN w int NOT NULL, ADD COLUMN x int NOT NULL DEFAULT 0, ALTER COLUMN x SET NOT NULL`,
			"id integer; a integer; v integer default 4; w integer not null; x integer not null default 0; c: v > 0", "w"},
		{`ALTER TABLE t ADD COLUMN IF NOT EXISTS id int CHECK (id > 5), DROP COLUMN IF EXISTS z, ADD COLUMN z int`,
			"id integer; a integer; v integer default 4; z integer; c: v > 0", ""},
		// Unnamed CHECKs are named in that order: the column's first.
		{`ALTER TABLE t ADD CHECK (v > 0), ADD COLUMN b int CHECK (v > 1)`,
			"id integer; a integer; v integer default 4; b integer; c: v > 0; t_v_check: v > 1; t_v_check1: v > 0", ""},
		// The table's only columns dropped, others added, written either way.
		{`ALTER TABLE t ADD COLUMN b int, DROP COLUMN id, DROP COLUMN a, DROP COLUMN v`, "b integer", ""},
		// Errors, as Postgres's: the first in that order.
		{`ALTER TABLE t ADD COLUMN x int, DROP COLUMN x`, `column "x" of relation "t" does not exist`, ""},
		{`ALTER TABLE t ADD COLUMN z int DEFAULT 9, ALTER COLUMN z DROP DEFAULT`, `column "z" of relation "t" does not exist`, ""},
		{`ALTER TABLE t ADD COLUMN b int NOT NULL DEFAULT 0, ALTER COLUMN b DROP NOT NULL`, `column "b" of relation "t" does not exist`, ""},
		{`ALTER TABLE t ADD CONSTRAINT k CHECK (v > 0), DROP CONSTRAINT k`, `constraint "k" of relation "t" does not exist`, ""},
		{`ALTER TABLE t ALTER COLUMN a SET NOT NULL, DROP COLUMN a`, `column "a" of relation "t" does not exist`, ""},
		{`ALTER TABLE t ADD COLUMN id int, DROP COLUMN nope`, `column "nope" of relation "t" does not exist`, ""},
		{`ALTER TABLE t ADD COLUMN z int DEFAULT 1, ADD COLUMN z int`, `column "z" of relation "t" already exists`, ""},
		{`ALTER TABLE t ALTER COLUMN nope2 SET DEFAULT 1, ALTER COLUMN nope SET NOT NULL`, `column "nope" of relation "t" does not exist`, ""},
		{`ALTER TABLE t ADD CONSTRAINT k CHECK (nope > 0), ALTER COLUMN v SET DEFAULT 'bad'`, `column "v": invalid decimal "bad"`, ""},
		{`ALTER TABLE t ADD CONSTRAINT k CHECK (nope > 0), ADD COLUMN w int DEFAULT 'bad'`, `column "w": invalid decimal "bad"`, ""},
		{`ALTER TABLE t ADD CONSTRAINT k CHECK (nope2 > 0), ADD COLUMN w int CHECK (nope1 > 0)`, `column "nope1" does not exist in table "t"`, ""},
	} {
		m := table()
		res, err := e.applyAlter(context.Background(), m, parseAlter(t, c.sql).Cmds, nil)
		got := ""
		if err != nil {
			got = errText(err)
		} else {
			got = describe(m)
			var notNull []string
			for _, f := range res.notNull {
				notNull = append(notNull, f)
			}
			if n := strings.Join(notNull, " "); n != c.notNull {
				t.Errorf("%s\n columns to check for NULLs: %q, want %q", c.sql, n, c.notNull)
			}
		}
		if got != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.sql, got, c.want)
		}
	}
}
