// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"strings"
	"testing"
)

func TestManagedHashInspectionPartialSample(t *testing.T) {
	for _, qualifying := range []bool{false, true} {
		t.Run(map[bool]string{false: "nested_only", true: "one_qualifying_row"}[qualifying], func(t *testing.T) {
			// No Redis client: the inspection's sample must suffice before a
			// catalog exists, without another scan or initialization.
			c := &hashConn{db: &databaseImpl{schema: defaultSchema}, st: &store{}}
			r := &HashReport{Prefix: "app:", Schema: defaultSchema, Table: "copied", Server: HashServer{Search: true},
				Columns: []HashColumn{{Name: "_key", Type: "VARCHAR"}, {Name: "v", Field: "v", Type: "VARCHAR"}}}
			rows := []hashRow{{key: "app:1:nested", fields: []string{"v"}, values: []string{"not an integer"}}}
			if qualifying {
				rows = append(rows, hashRow{key: "app:1", fields: []string{"v"}, values: []string{"10"}})
			}
			stats := keyStats{keys: 3, rows: 2, nested: 1, nestedEx: "app:1:nested"}
			cl, err := c.snapshotInspectionChecklist(context.Background(), r, rows, stats, nil, HashInspectOptions{Prefix: r.Prefix})
			if err != nil {
				t.Fatal(err)
			}
			if cl.Ready != qualifying {
				t.Fatal("sample readiness", cl)
			}
			expectItem(t, cl, CheckWarn, "sampled qualifying HASHes")
			if qualifying {
				if !strings.HasSuffix(cl.Command, " -apply") || r.Columns[1].InPlaceType != "BIGINT" {
					t.Fatal(cl, r.Columns)
				}
			} else {
				expectItem(t, cl, CheckBlocker, "inspection sample has no fields")
				if cl.Command != "" {
					t.Fatal("unverified sample offered an Apply command")
				}
			}
			if r.Columns[0].InPlaceType != "" || r.Columns[1].Type != "VARCHAR" {
				t.Fatal("inspection changed copy columns", r.Columns)
			}
		})
	}
}
