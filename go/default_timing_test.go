// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"testing"
)

func TestDefaultDefinitionTiming(t *testing.T) {
	e := &executor{schema: defaultSchema, sess: newSession(defaultSchema)}
	for _, tc := range []struct{ definition, state string }{
		{"INTEGER DEFAULT 1/0", ""}, {`TEXT DEFAULT unistr('\xyz')`, ""},
		{"SMALLINT DEFAULT 100000", ""}, {"VARCHAR(2) DEFAULT 'long'", ""},
		{"NUMERIC(2,0) DEFAULT '1000'", ""},
		{"INTEGER DEFAULT ('bad'::text)::integer", ""},
		{"INTEGER DEFAULT 'bad'", stateInvalidText},
		{"INTEGER DEFAULT '1'::text", stateDatatypeMismatch},
		{"INTEGER DEFAULT CAST('bad' AS integer)", stateInvalidText},
		{"INTEGER DEFAULT CASE WHEN FALSE THEN 'bad'::integer ELSE 1 END", stateInvalidText},
		{"INTEGER DEFAULT CURRENT_TIMESTAMP", stateDatatypeMismatch},
		{"INTEGER DEFAULT coalesce(NULL,'1')", stateDatatypeMismatch},
		{"DATE DEFAULT true::date", stateCannotCoerce},
		{"INTEGER DEFAULT CASE WHEN FALSE THEN NULL::date::integer ELSE 1 END", stateCannotCoerce},
		{"INTEGER DEFAULT gcd(NULL,NULL)", stateAmbiguousFunction},
		{"UUID DEFAULT 1", stateDatatypeMismatch},
		{"UUID DEFAULT true", stateDatatypeMismatch},
		{"UUID DEFAULT 'bad'::text", stateDatatypeMismatch},
		{"UUID DEFAULT NULL::text", stateDatatypeMismatch},
		{"INTEGER[] DEFAULT ARRAY['x']", stateDatatypeMismatch},
		{"INTEGER[] DEFAULT ARRAY['1']", stateDatatypeMismatch},
		{"INTEGER[] DEFAULT NULL::text[]", stateDatatypeMismatch},
		{"INTEGER[] DEFAULT ARRAY[1::numeric]", ""},
		{"INTEGER[] DEFAULT ARRAY[DATE '2024-01-01']::integer[]", stateCannotCoerce},
		{"INTEGER[] DEFAULT ARRAY['bad']::integer[]", stateInvalidText},
		{"INTEGER[] DEFAULT ARRAY['1']::integer[]", ""},
		{"JSONB DEFAULT '{}'::json", ""}, {"JSON DEFAULT '{}'::jsonb", ""},
		{"JSONB DEFAULT NULL::json", ""},
		{"JSON DEFAULT 'bad'::json", stateInvalidText},
		{"JSONB DEFAULT 'bad'::jsonb", stateInvalidText},
		{"JSON DEFAULT ('bad'::text)::json", ""},
		{"JSON DEFAULT 1::json", stateCannotCoerce},
		{"INTERVAL DEFAULT INTERVAL 'bad' DAY", stateDatetimeFormat},
		{"INTERVAL DEFAULT INTERVAL 'bad' HOUR", stateDatetimeFormat},
		{"INTERVAL DEFAULT INTERVAL '1' DAY", ""},
	} {
		t.Run(tc.definition, func(t *testing.T) {
			stmts, err := ParseScript("CREATE TABLE test_default (i " + tc.definition + ")")
			if err != nil {
				t.Fatal(err)
			}
			def := stmts[0].Stmt.(*CreateTableStmt).Columns[0]
			_, err = e.checkDefault(context.Background(), def)
			if sqlState(err) != tc.state {
				t.Fatalf("%v, want SQLSTATE %q", err, tc.state)
			}
		})
	}
}

func TestDefaultPlanLeavesVolatileCaseDeferred(t *testing.T) {
	e := &executor{schema: defaultSchema, sess: newSession(defaultSchema)}
	for _, expr := range []string{
		"CASE WHEN TRUE THEN set_config('application_name','used',false) ELSE 'x' END",
		"CASE WHEN FALSE THEN 'x' ELSE set_config('application_name','used',false) END",
	} {
		env := e.newEnv(context.Background(), nil, nil)
		if _, err := env.eval(parseTestExpr(t, "set_config('application_name','before',false)")); err != nil {
			t.Fatal(err)
		}
		d := e.columnDefaults(context.Background(), &tableMeta{Columns: []columnMeta{{Name: "v", Type: typeString, Default: expr}}})
		if err := d.plan(0); err != nil {
			t.Fatal(err)
		}
		v, err := env.eval(parseTestExpr(t, "current_setting('application_name')"))
		if err != nil || v.S != "before" || d.done[0] {
			t.Fatalf("planning ran/cached volatile CASE %s: %v %v done=%v", expr, v, err, d.done[0])
		}
	}
}

func TestDefaultTypedNullMetadata(t *testing.T) {
	e := &executor{schema: defaultSchema, sess: newSession(defaultSchema)}
	for _, tc := range []struct {
		expr  string
		empty bool
	}{
		{"NULL", true}, {"CAST(NULL AS TEXT)", true}, {"NULL::integer", false}, {"NULL::uuid", false}, {"(NULL::integer)::text", false}, {"NULLIF(1,1)::text", false},
	} {
		stmts, err := ParseScript("CREATE TABLE default_null(s TEXT DEFAULT " + tc.expr + ")")
		if err != nil {
			t.Fatal(err)
		}
		text, err := e.checkDefault(context.Background(), stmts[0].Stmt.(*CreateTableStmt).Columns[0])
		if err != nil || (text == "") != tc.empty {
			t.Errorf("%s metadata: %q %v", tc.expr, text, err)
		}
	}
}
