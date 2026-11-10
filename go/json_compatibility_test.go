// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"encoding/json"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

func TestJSONCompatibilityBatch(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`SELECT '{"a":1}'::jsonb ? 'a'`, "true"},
		{`SELECT '{"a":1,"b":2}'::jsonb - 'a'`, `{"b": 2}`},
		{`SELECT '{"a":{"b":1}}'::jsonb #- '{a,b}'`, `{"a": {}}`},
		{`SELECT * FROM json_each('{"a":1}')`, "a|1"},
		{`SELECT * FROM jsonb_array_elements('[1,2]')`, "1\n2"},
		{`SELECT jsonb_set('{"a":1}', '{a}', '2')`, `{"a": 2}`},
		{`SELECT json_object_keys('{"a":1}')`, "a"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			if got := memQuery(tc.sql); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJSONLogicalTypesAndMetadata(t *testing.T) {
	for _, typ := range []ColType{typeJSON, typeJSONB} {
		raw, err := json.Marshal(typ)
		if err != nil {
			t.Fatal(err)
		}
		var restored ColType
		if err = json.Unmarshal(raw, &restored); err != nil || restored != typ {
			t.Fatalf("metadata round trip: %s, %v", raw, err)
		}
		if indexable(typ) {
			t.Fatal("logical JSON must not use text Search index semantics")
		}
		field := arrowField("doc", typ, true)
		got, err := colTypeFromField(field)
		if err != nil || got != typ {
			t.Fatalf("Arrow round trip: %v, %v", got, err)
		}
		if _, err = coerceJSON(stringValue(`{"broken":`), typ); err == nil {
			t.Fatal("malformed JSON accepted")
		}
	}
	jsonValue, err := coerceJSON(stringValue(` {"a":1,"a":2} `), typeJSON)
	if err != nil || jsonValue.S != ` {"a":1,"a":2} ` {
		t.Fatal(jsonValue, err)
	}
	jsonbValue, err := coerceJSON(jsonValue, typeJSONB)
	if err != nil || jsonbValue.S != `{"a": 2}` {
		t.Fatal(jsonbValue, err)
	}
	if !coerceJSONNull(typeJSON).Null {
		t.Fatal("SQL NULL lost")
	}
	layouts := resultLayouts{"doc": "dictionary"}
	if _, err = layouts.schema(arrow.NewSchema([]arrow.Field{arrowField("doc", typeJSON, true)}, nil)); err == nil {
		t.Fatal("incompatible JSON dictionary extension accepted")
	}
}
func coerceJSONNull(typ ColType) Value { v, _ := coerceJSON(nullValue(typeString), typ); return v }

func TestJSONBEqualityAndGrouping(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`WITH t(v) AS (VALUES ('1'::jsonb),('1.0'::jsonb),('1e0'::jsonb)) SELECT COUNT(*) FROM t GROUP BY v`, "3"},
		{`SELECT '9007199254740993'::jsonb > '9007199254740992'::jsonb`, "true"},
		{`WITH a(v) AS (VALUES ('1.0'::jsonb)), b(v) AS (VALUES ('1e0'::jsonb)) SELECT COUNT(*) FROM a JOIN b USING(v)`, "1"},
	} {
		if got := memQuery(tc.sql); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.sql, got, tc.want)
		}
	}
}
