// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestQuotedIndexAliasCollisionsAndPersistence(t *testing.T) {
	m := &tableMeta{
		Columns: []columnMeta{
			{Name: "Customer ID", Type: typeInt64},
			{Name: "another", Field: "__adbc_index_0"},
			{Name: "other space", SearchAlias: "__adbc_index_1"},
		},
		RetiredFields: []string{"__adbc_index_2"},
	}
	bindSearchAlias(m, 0)
	if m.Columns[0].SearchAlias != "__adbc_index_3" {
		t.Fatal("alias collided with a physical field, alias or retired field", m.Columns[0])
	}
	m.Columns[0].Field, m.Columns[0].Name = "Customer ID", "Renamed ID"
	bindSearchAlias(m, 0)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var restored tableMeta
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	c := restored.Columns[0]
	if c.field() != "Customer ID" || c.Name != "Renamed ID" || c.SearchAlias != "__adbc_index_3" {
		t.Fatal("rename or metadata round trip lost the binding", c)
	}
}

func TestQuotedIndexUnsupportedForms(t *testing.T) {
	for _, sql := range []string{
		`CREATE UNIQUE INDEX ix ON t ("Customer ID")`,
		`CREATE INDEX ix ON t (("Customer ID" + 1))`,
		`CREATE INDEX ix ON t ("Customer ID") WHERE "Customer ID" > 0`,
	} {
		_, err := ParseScript(sql)
		checkState(t, sql, err, adbc.StatusNotImplemented, "0A000", "indexes are not supported")
	}
}
