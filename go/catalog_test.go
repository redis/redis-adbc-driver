// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import "testing"

func TestCatalogSchemaIdentity(t *testing.T) {
	seen := map[string]bool{}
	for _, cat := range []string{"redis", "analytics", "a:b", "quoted catalog", "é"} {
		for _, schema := range []string{"public", "a:b", "analytics.public", "custom space"} {
			stored := catalogSchema(cat, schema)
			if seen[stored] {
				t.Fatalf("identity collision for %q %q", cat, schema)
			}
			seen[stored] = true
			if gotCat, gotSchema := splitCatalogSchema(stored); gotCat != cat || gotSchema != schema {
				t.Fatalf("round trip %q %q -> %q %q", cat, schema, gotCat, gotSchema)
			}
			if cat == catalogName && stored != schema {
				t.Fatal("default catalog layout changed")
			}
			if cat != catalogName && validateLogicalSchema(stored) == nil {
				t.Fatal("internal identity accepted as a logical schema")
			}
		}
	}
	if schemaCatalog("public") != "redis" || logicalSchema("public") != "public" {
		t.Fatal("legacy identity changed")
	}
}
