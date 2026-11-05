// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import "testing"

func TestPrivilegeStatements(t *testing.T) {
	for _, sql := range []string{
		`grant select on t to analyst`, `revoke select on t from analyst`,
		`GRANT SELECT ON TABLE public.t TO "Mixed Role"`,
		`REVOKE SELECT ON "Odd Schema"."Table" FROM analyst;`,
	} {
		t.Run(sql, func(t *testing.T) {
			stmts, err := ParseScript(sql)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := stmts[0].Stmt.(*PrivilegeStmt); !ok {
				t.Fatalf("got %T", stmts[0].Stmt)
			}
		})
	}
	for _, sql := range []string{
		`GRANT`, `GRANT SELECT ON t`, `GRANT SELECT ON t TO`,
		`GRANT SELECT ON t TO reader,`, `GRANT SELECT ON t TO reader garbage`,
		`GRANT SELECT ON t TO reader WITH GRANT OPTION`, `REVOKE GRANT OPTION FOR SELECT ON t FROM reader`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA public TO reader`, `GRANT SELECT (id) ON t TO reader`,
		`GRANT SELECT ON DATABASE db TO reader`, `GRANT reader TO writer`,
		`GRANT USAGE ON t TO reader`, `GRANT DELETE ON SCHEMA public TO reader`,
		`GRANT SELECT ON t TO PUBLIC`, `GRANT SELECT ON t TO CURRENT_USER`,
		`GRANT SELECT ON t, u TO reader`, `GRANT SELECT ON t TO reader, writer`,
	} {
		if _, err := ParseScript(sql); err == nil {
			t.Errorf("accepted %s", sql)
		}
	}
}

func TestPrivilegeViewsEmptyWithoutManagement(t *testing.T) {
	for _, name := range []string{"role_table_grants", "table_privileges"} {
		if got := memQuery(`SELECT count(*) FROM information_schema.` + name); got != "0" {
			t.Errorf("%s: %s", name, got)
		}
	}
}

func TestRBACACLRules(t *testing.T) {
	for _, prefix := range []string{"public:t:", "public:t~2:", "safe%20schema:t:"} {
		s := rbacACLString([]string{prefix})
		got, err := rbacPrefixes(s)
		if err != nil || len(got) != 1 || got[0] != prefix {
			t.Fatalf("%s: %v %v", s, got, err)
		}
	}
	for _, s := range []string{
		rbacBaseACL + " +@all", rbacBaseACL + " %R~public:foo*:*", rbacBaseACL + " %R~adbc:*",
		rbacBaseACL + " %R~public:[abc]:*", rbacBaseACL + " %R~public:t:* %R~public:t:*",
	} {
		if _, err := rbacPrefixes(s); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
}
