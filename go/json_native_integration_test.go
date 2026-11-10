// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"testing"
)

func TestSQLNativeJSONRoundTrips(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_json_native", "it_json_native_copy")
	t.Cleanup(func() { h.dropTables("it_json_native", "it_json_native_copy") })
	h.exec("CREATE TABLE it_json_native(id INTEGER, raw JSON, doc JSONB)")
	h.exec(`INSERT INTO it_json_native VALUES(1,' {"a":1, "a":2,"n":9007199254740993.50} ', '{"z":1,"a":2,"z":3}'),(2,'null',NULL)`)
	h.expectRows("SELECT raw,doc FROM it_json_native ORDER BY id", ` {"a":1, "a":2,"n":9007199254740993.50} |{"a": 2, "z": 3}`, "null|NULL")
	h.expectRows("SELECT raw IS NULL,doc IS NULL,JSON_TYPEOF(raw),PG_TYPEOF(raw),PG_TYPEOF(doc) FROM it_json_native WHERE id=2", "false|true|null|json|jsonb")
	h.expectRows("SELECT column_name,data_type,udt_name FROM information_schema.columns WHERE table_name='it_json_native' ORDER BY ordinal_position", "id|integer|int4", "raw|json|json", "doc|jsonb|jsonb")
	h.expectError(`INSERT INTO it_json_native VALUES(3,'{"broken":', '{}')`, "invalid input syntax")
	h.expectRows("SELECT COUNT(*) FROM it_json_native", "2")
	h.exec(`UPDATE it_json_native SET doc=jsonb_set(doc,'{new}','null') WHERE id=1`)
	h.expectRows(`SELECT doc ? 'new',doc->'new',doc->>'new',json_build_object('raw',raw) FROM it_json_native WHERE id=1`, `true|null|NULL|{"raw" :  {"a":1, "a":2,"n":9007199254740993.50} }`)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err = st.SetSqlQuery(h.ctx, "SELECT raw,doc FROM it_json_native ORDER BY id"); err != nil {
		t.Fatal(err)
	}
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	for i, f := range reader.Schema().Fields() {
		name, _ := f.Metadata.GetValue("ARROW:extension:name")
		if name != "arrow.json" {
			t.Fatalf("field %d missing JSON extension: %v", i, f)
		}
	}
	if !reader.Next() {
		t.Fatal(reader.Err())
	}
	rec := reader.RecordBatch()
	rec.Retain()
	defer rec.Release()
	if err = h.ingestImportRecord("it_json_native_copy", adbc.OptionValueIngestModeCreate, rec); err != nil {
		t.Fatal(err)
	}
	if reader.Next() || reader.Err() != nil {
		t.Fatal("incomplete JSON result fetch", reader.Err())
	}
	h.expectRows("SELECT raw,doc FROM it_json_native_copy", ` {"a":1, "a":2,"n":9007199254740993.50} |{"a": 2, "z": 3, "new": null}`, "null|NULL")
}

func TestSQLNativeJSONRejectsInvalidArrow(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_json_native_invalid"
	h.dropTables(table)
	t.Cleanup(func() { h.dropTables(table) })
	h.exec("CREATE TABLE " + table + "(v JSONB)")
	b := array.NewStringBuilder(memory.DefaultAllocator)
	defer b.Release()
	b.Append(`{"invalid":`)
	a := b.NewArray()
	defer a.Release()
	schema := arrow.NewSchema([]arrow.Field{arrowField("v", typeJSONB, true)}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
	defer rec.Release()
	if err := h.ingestImportRecord(table, adbc.OptionValueIngestModeAppend, rec); err == nil {
		t.Fatal("invalid JSON Arrow batch succeeded")
	}
	h.expectRows("SELECT COUNT(*) FROM "+table, "0")
}

func TestSQLNativeJSONParameters(t *testing.T) {
	h := newSQLHarness(t)
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err = st.SetSqlQuery(h.ctx, `SELECT ? - 'a', ? ? 'a'`); err != nil {
		t.Fatal(err)
	}
	b := array.NewStringBuilder(memory.DefaultAllocator)
	b.Append(`{"b":2,"a":1}`)
	a := b.NewArray()
	b.Release()
	defer a.Release()
	schema := arrow.NewSchema([]arrow.Field{arrowField("p1", typeJSONB, true), arrowField("p2", typeJSONB, true)}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{a, a}, 1)
	defer rec.Release()
	if err = st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	if !r.Next() {
		t.Fatal(r.Err())
	}
	row := r.RecordBatch()
	if row.Column(0).(*array.String).Value(0) != `{"b": 2}` || !row.Column(1).(*array.Boolean).Value(0) {
		t.Fatal(row)
	}
	if r.Next() || r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestSQLNativeJSONArrays(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_json_array_native", "it_json_array_copy")
	t.Cleanup(func() { h.dropTables("it_json_array_native", "it_json_array_copy") })
	h.exec("CREATE TABLE it_json_array_native(docs JSONB[])")
	h.exec(`INSERT INTO it_json_array_native VALUES(ARRAY['{"a":1}'::jsonb,NULL::jsonb])`)
	h.expectRows(`SELECT docs[1]->>'a', docs[2] IS NULL FROM it_json_array_native`, "1|true")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err = st.SetSqlQuery(h.ctx, "SELECT docs FROM it_json_array_native"); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	item := r.Schema().Field(0).Type.(*arrow.ListType).ElemField()
	if marker, _ := item.Metadata.GetValue("ARROW:extension:name"); marker != "arrow.json" {
		t.Fatal("JSON array lost child identity", item)
	}
	if !r.Next() {
		t.Fatal(r.Err())
	}
	rec := r.RecordBatch()
	rec.Retain()
	defer rec.Release()
	if err = h.ingestImportRecord("it_json_array_copy", adbc.OptionValueIngestModeCreate, rec); err != nil {
		t.Fatal(err)
	}
	if r.Next() || r.Err() != nil {
		t.Fatal(r.Err())
	}
	h.expectRows(`SELECT docs[1]->>'a',docs[2] IS NULL FROM it_json_array_copy`, "1|true")
}
