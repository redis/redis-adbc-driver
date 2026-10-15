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

func TestSQLPostgresCatalogCompatibility(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP TABLE IF EXISTS it_pgcat")
	t.Cleanup(func() { h.exec("DROP TABLE IF EXISTS it_pgcat") })
	h.exec("CREATE TABLE it_pgcat (id INTEGER, name VARCHAR(20))")

	h.expectRows("SELECT nspname FROM pg_catalog.pg_namespace WHERE nspname = 'public'", "public")
	h.expectRows("SELECT relname FROM pg_class WHERE relname = 'it_pgcat'", "it_pgcat")
	h.expectRows("SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename = 'it_pgcat'", "it_pgcat")
	h.expectRows("SELECT 'it_pgcat'::regclass", "it_pgcat")
	h.expectRows("SELECT obj_description('it_pgcat'::regclass)", "NULL")
	h.expectRows("SELECT format_type(23, NULL)", "integer")

	h.expectRows(`SELECT column_name, data_type, udt_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'it_pgcat' ORDER BY ordinal_position`,
		"id|integer|int4", "name|character varying|varchar")
	h.expectRows(`SELECT table_catalog, is_identity, is_generated, collation_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'it_pgcat' ORDER BY ordinal_position`,
		"redis|NO|NEVER|NULL", "redis|NO|NEVER|NULL")
}
