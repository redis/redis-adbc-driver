// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestCatalogSchemaIdentity(t *testing.T) {
	zero := string([]byte{0})
	if validateCatalogName("bad"+zero) == nil || validateLogicalSchema("bad"+zero) == nil {
		t.Fatal("public catalog/schema accepted a zero byte")
	}
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
			if cat != catalogName && !strings.Contains(stored, zero) {
				t.Fatal("reserved identity fixture has no zero byte")
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

func TestNamedCatalogAdvancedExecutionRefusals(t *testing.T) {
	ctx := context.Background()
	requireUnsupported := func(label string, err error) {
		t.Helper()
		var ae adbc.Error
		if !errors.As(err, &ae) || ae.Code != adbc.StatusNotImplemented {
			t.Fatalf("%s: expected NotImplemented, got %v", label, err)
		}
	}
	named := catalogSchema("analytics", "public")
	_, _, _, _, err := newRetainedExecutor(ctx, &executor{schema: named}, nil, timeouts{}, defaultRetainedExecutionLimits())
	requireUnsupported("retained selected catalog", err)
	_, err = (&retainedExecution{}).table(ctx, named, "t")
	requireUnsupported("retained physical target", err)
	retained := &executor{schema: "public", store: &store{}, retained: &retainedExecution{}}
	_, err = retained.targetCatalog(ctx, "analytics")
	requireUnsupported("retained explicit target", err)
	requireUnsupported("retained pre-publication admission", retainedTestAdmission().vet(&SelectStmt{From: &TableName{Catalog: "analytics", Schema: "public", Name: "t"}}, 0))
	sharedStore := &store{sharedView: &sharedReadView{}}
	shared := &executor{schema: "public", store: sharedStore}
	_, err = shared.targetCatalog(ctx, "analytics")
	requireUnsupported("shared explicit target", err)
	conn := &connectionImpl{store: sharedStore, sess: newSession("public")}
	requireUnsupported("shared catalog selection", conn.SetCurrentCatalog(ctx, "analytics"))
	conn.sess.catalog = "analytics"
	_, err = conn.prepareReadSnapshotBinding(OptionStringReadSnapshotAction, "acquire")
	requireUnsupported("named shared acquisition", err)
	for _, test := range []struct {
		schema string
		source TableName
	}{{named, TableName{Name: "t"}}, {"public", TableName{Catalog: "analytics", Schema: "public", Name: "t"}}} {
		_, err := (&executor{schema: test.schema}).partitionPlan(ctx, &SelectStmt{From: &test.source}, 1, 2)
		requireUnsupported("deferred partition target", err)
	}
}
