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

import "testing"

func TestInformationSchemaColumnsPostgresNames(t *testing.T) {
	cols := infoSchemaColumns["columns"]
	row := columnRow("public", "t", 2, columnMeta{Name: "name", Type: ColType{Kind: KindString, Length: 20}, Nullable: true})
	if len(row) != len(cols) {
		t.Fatalf("columns row has %d values for %d columns", len(row), len(cols))
	}
	values := map[string]Value{}
	for i, c := range cols {
		values[c.Name] = row[i]
	}
	for name, want := range map[string]string{
		"table_catalog": "redis", "data_type": "character varying", "udt_catalog": "redis",
		"udt_schema": "pg_catalog", "udt_name": "varchar", "is_identity": "NO",
		"is_generated": "NEVER", "is_updatable": "YES", "dtd_identifier": "2",
	} {
		if got := values[name]; got.Null || got.Text() != want {
			t.Errorf("%s = %v, want %q", name, got, want)
		}
	}
	if got := values["character_maximum_length"]; got.Null || got.I != 20 {
		t.Errorf("character_maximum_length = %v", got)
	}
	if !values["collation_name"].Null {
		t.Errorf("collation_name = %v, want NULL", values["collation_name"])
	}
}

func TestPostgresTypeNames(t *testing.T) {
	for _, c := range []struct {
		t        ColType
		data, udt string
	}{
		{typeInt32, "integer", "int4"},
		{decimalType(10, 2), "numeric", "numeric"},
		{typeTimestamp, "timestamp without time zone", "timestamp"},
		{typeTimestampTZ, "timestamp with time zone", "timestamptz"},
		{typeTimeTZ, "time with time zone", "timetz"},
	} {
		data, udt := postgresTypeNames(c.t)
		if data != c.data || udt != c.udt {
			t.Errorf("%s: (%q, %q), want (%q, %q)", c.t.SQLName(), data, udt, c.data, c.udt)
		}
	}
}
