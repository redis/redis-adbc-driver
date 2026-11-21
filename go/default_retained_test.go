// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAlterTypeRetainedLiteral(t *testing.T) {
	e := &executor{schema: defaultSchema, sess: newSession(defaultSchema)}
	for _, tc := range []struct{ from, to, expr, want, state string }{
		{"numeric(4,2)", "integer", "'1.50'", "2", ""},
		{"numeric(4,2)", "integer", "'-1.50'", "-2", ""},
		{"numeric(4,2)", "integer", "'1.499'", "1", ""},
		{"numeric(4,2)", "numeric(6,3)", "'1.555'", "1.555", ""},
		{"integer", "numeric(6,3)", "1.5", "1.500", ""},
		{"varchar(2)", "text", "'long'", "long", ""},
		{"char(2)", "text", "'long'", "long", ""},
		{"boolean", "text", "'true'", "true", ""},
		{"uuid", "text", "'A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11'", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", ""},
		{"json", "jsonb", "'{\"a\":1}'", "{\"a\": 1}", ""},
		{"text", "integer", "'1'", "", stateDatatypeMismatch},
		{"numeric(4,2)", "boolean", "'1.50'", "", stateDatatypeMismatch},
		{"integer", "bigint", "('bad'::text)::integer", "", stateInvalidText},
	} {
		t.Run(tc.from+"/"+tc.to+"/"+tc.expr, func(t *testing.T) {
			from, err := parseTypeText(tc.from)
			if err != nil {
				t.Fatal(err)
			}
			to, err := parseTypeText(tc.to)
			if err != nil {
				t.Fatal(err)
			}
			text, err := e.alterTypeDefault(context.Background(), columnMeta{Name: "v", Type: from, Default: tc.expr}, to)
			if tc.state == stateDatatypeMismatch {
				if sqlState(err) != tc.state {
					t.Fatalf("%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// Exercise persisted metadata and both evaluation/planning paths.
			encoded, err := json.Marshal(columnMeta{Name: "v", Type: to, Default: text})
			if err != nil {
				t.Fatal(err)
			}
			var col columnMeta
			if err = json.Unmarshal(encoded, &col); err != nil {
				t.Fatal(err)
			}
			for _, plan := range []bool{false, true} {
				d := e.columnDefaults(context.Background(), &tableMeta{Columns: []columnMeta{col}})
				if plan {
					err = d.plan(0)
				} else {
					err = nil
				}
				var v Value
				if err == nil {
					v, err = d.value(0)
				}
				if sqlState(err) != tc.state {
					t.Fatalf("plan=%v: %v, want %s", plan, err, tc.state)
				}
				if err == nil && v.Text() != tc.want {
					t.Fatalf("plan=%v: got %q, want %q (%s)", plan, v.Text(), tc.want, text)
				}
			}
		})
	}
}
