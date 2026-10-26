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

// Integration tests for local-time literals in views, DEFAULTs and CHECKs
// (creationzone.go): each is created in a New York session and used from
// UTC and Kolkata sessions, and reads its literals in New York, as Postgres
// 16 does (the expected values are pg16's); what the definition converts
// when it runs reads the session's zone. Definitions stored by earlier
// versions keep reading their literals in the session time zone.

import (
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// newZoneHarness is a harness on another connection, in time zone zone.
func newZoneHarness(t *testing.T, zone string) *sqlHarness {
	h := newSQLHarness(t)
	h.exec(`SET TIME ZONE '` + zone + `'`)
	return h
}

// patchMeta replaces old with new in the metadata stored under key, as an
// earlier version would have written it.
func (h *sqlHarness) patchMeta(raw goredis.UniversalClient, key, old, new string) {
	h.t.Helper()
	s, err := raw.Get(h.ctx, key).Result()
	if err != nil {
		h.t.Fatal(err)
	}
	if !strings.Contains(s, old) {
		h.t.Fatalf("metadata %s has no %q: %s", key, old, s)
	}
	if err := raw.Set(h.ctx, key, strings.ReplaceAll(s, old, new), 0).Err(); err != nil {
		h.t.Fatal(err)
	}
}

func (h *sqlHarness) setupCreationZone() {
	h.t.Helper()
	h.dropViews("it_cz_after", "it_cz_cols")
	h.dropTables("it_cz")
	h.exec(`SET TIME ZONE 'America/New_York'`)
	h.exec(`CREATE TABLE it_cz (id INTEGER, tz TIMESTAMPTZ, ts TIMESTAMP)`)
	h.exec(`INSERT INTO it_cz VALUES (1, '2024-01-01 04:00:00+00', '2024-01-01 04:00'),
		(2, '2024-01-01 05:00:00+00', '2024-01-01 05:00'), (3, '2024-01-01 06:00:00+00', '2024-01-01 06:00'),
		(4, NULL, NULL)`)
}

// A view reads its local-time literals in the zone it was created in, also
// in the WHERE pushed down to the index; what it converts when it runs
// (text output, a TIMESTAMP cast to a timestamp with time zone) reads the
// zone of the session that queries it.
func TestCreationZoneView(t *testing.T) {
	h := newSQLHarness(t)
	h.setupCreationZone()
	// Midnight in New York is 05:00 UTC.
	h.exec(`CREATE VIEW it_cz_after AS SELECT id FROM it_cz WHERE tz >= '2024-01-01 00:00'`)
	h.exec(`CREATE VIEW it_cz_cols AS SELECT id, TIMESTAMPTZ '2024-01-01 00:00' AS typed,
		'2024-03-10 02:30'::timestamptz AS gap, tz = '2024-01-01 00:00' AS eq,
		tz IN ('2024-01-01 00:00', '2024-01-01 01:00') AS in_list,
		tz BETWEEN '2023-12-31 23:30' AND '2024-01-01 00:30' AS btw,
		CASE tz WHEN '2024-01-01 01:00' THEN 'one' ELSE 'other' END AS case_op,
		COALESCE(tz, '2024-01-01 00:00') AS coalesced, '2024-01-01 00:00' AT TIME ZONE 'UTC' AS at_utc,
		CAST(tz AS TEXT) AS shown, CAST(ts AS TIMESTAMPTZ) AS naive
		FROM it_cz`)
	// As Postgres's pg_get_viewdef, with the instant's offset in New York.
	h.expectRows(`SELECT view_definition FROM information_schema.views WHERE table_name = 'it_cz_after'`,
		`SELECT id FROM it_cz WHERE tz >= '2024-01-01 00:00:00-05'`)

	cols := `SELECT id, CAST(typed AS TEXT), CAST(gap AS TEXT), eq, in_list, btw, case_op, CAST(coalesced AS TEXT),
		CAST(at_utc AS TEXT), shown, CAST(naive AS TEXT) FROM it_cz_cols ORDER BY id`
	utc := newZoneHarness(t, "UTC")
	utc.expectRows(`SELECT id FROM it_cz_after ORDER BY id`, "2", "3")
	utc.expectRows(`SELECT id FROM it_cz_after WHERE id > 2`, "3")
	utc.expectRows(cols,
		"1|2024-01-01 05:00:00+00|2024-03-10 07:30:00+00|false|false|false|other|2024-01-01 04:00:00+00|2024-01-01 05:00:00|2024-01-01 04:00:00+00|2024-01-01 04:00:00+00",
		"2|2024-01-01 05:00:00+00|2024-03-10 07:30:00+00|true|true|true|other|2024-01-01 05:00:00+00|2024-01-01 05:00:00|2024-01-01 05:00:00+00|2024-01-01 05:00:00+00",
		"3|2024-01-01 05:00:00+00|2024-03-10 07:30:00+00|false|true|false|one|2024-01-01 06:00:00+00|2024-01-01 05:00:00|2024-01-01 06:00:00+00|2024-01-01 06:00:00+00",
		"4|2024-01-01 05:00:00+00|2024-03-10 07:30:00+00|NULL|NULL|NULL|other|2024-01-01 05:00:00+00|2024-01-01 05:00:00|NULL|NULL")
	kolkata := newZoneHarness(t, "Asia/Kolkata")
	kolkata.expectRows(`SELECT id FROM it_cz_after ORDER BY id`, "2", "3")
	kolkata.expectRows(cols,
		"1|2024-01-01 10:30:00+05:30|2024-03-10 13:00:00+05:30|false|false|false|other|2024-01-01 09:30:00+05:30|2024-01-01 05:00:00|2024-01-01 09:30:00+05:30|2024-01-01 04:00:00+05:30",
		"2|2024-01-01 10:30:00+05:30|2024-03-10 13:00:00+05:30|true|true|true|other|2024-01-01 10:30:00+05:30|2024-01-01 05:00:00|2024-01-01 10:30:00+05:30|2024-01-01 05:00:00+05:30",
		"3|2024-01-01 10:30:00+05:30|2024-03-10 13:00:00+05:30|false|true|false|one|2024-01-01 11:30:00+05:30|2024-01-01 05:00:00|2024-01-01 11:30:00+05:30|2024-01-01 06:00:00+05:30",
		"4|2024-01-01 10:30:00+05:30|2024-03-10 13:00:00+05:30|NULL|NULL|NULL|other|2024-01-01 10:30:00+05:30|2024-01-01 05:00:00|NULL|NULL")

	// CREATE OR REPLACE VIEW reads them in its own session's zone.
	utc.exec(`CREATE OR REPLACE VIEW it_cz_after AS SELECT id FROM it_cz WHERE tz >= '2024-01-01 00:00'`)
	h.expectRows(`SELECT id FROM it_cz_after ORDER BY id`, "1", "2", "3")

	// A view stored by an earlier version, with the literal as written,
	// reads it in the session time zone, as it did.
	raw := h.rawClient()
	h.exec(`CREATE OR REPLACE VIEW it_cz_after AS SELECT id FROM it_cz WHERE tz >= '2024-01-01 00:00'`)
	h.patchMeta(raw, viewKey(defaultSchema, "it_cz_after"), "2024-01-01 00:00:00-05", "2024-01-01 00:00")
	h.expectRows(`SELECT id FROM it_cz_after ORDER BY id`, "2", "3")
	utc.expectRows(`SELECT id FROM it_cz_after ORDER BY id`, "1", "2", "3")
}

// A DEFAULT reads its local-time literals in the zone it was defined in
// (CREATE TABLE, ADD COLUMN, ALTER COLUMN … SET DEFAULT); the conversions to
// the column's type read the zone of the session that writes the row.
func TestCreationZoneDefault(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_cz_def", "it_cz_def2")
	h.exec(`SET TIME ZONE 'America/New_York'`)
	h.exec(`CREATE TABLE it_cz_def (id INTEGER, untyped TIMESTAMPTZ DEFAULT '2024-01-01 00:00',
		typed TIMESTAMPTZ DEFAULT TIMESTAMPTZ '2024-03-10 02:30',
		naive TIMESTAMP DEFAULT '2024-01-01 00:00'::timestamptz, from_date TIMESTAMPTZ DEFAULT DATE '2024-01-01',
		from_naive TIMESTAMPTZ DEFAULT TIMESTAMP '2024-01-01 00:00')`)
	h.exec(`ALTER TABLE it_cz_def ADD COLUMN added TIMESTAMPTZ DEFAULT '2024-07-01 12:00'`)
	h.exec(`CREATE TABLE it_cz_def2 (id INTEGER)`)
	h.exec(`INSERT INTO it_cz_def2 VALUES (1)`)
	h.exec(`ALTER TABLE it_cz_def2 ADD COLUMN later TIMESTAMPTZ`)
	h.exec(`ALTER TABLE it_cz_def2 ALTER COLUMN later SET DEFAULT '2024-07-01 12:00'`)
	h.expectRows(`SELECT column_name, column_default FROM information_schema.columns
		WHERE table_name = 'it_cz_def' ORDER BY ordinal_position`,
		"id|NULL", "untyped|'2024-01-01 00:00:00-05'", "typed|TIMESTAMPTZ '2024-03-10 03:30:00-04'",
		"naive|'2024-01-01 00:00:00-05'::timestamptz", "from_date|DATE '2024-01-01'", "from_naive|TIMESTAMP '2024-01-01 00:00'",
		"added|'2024-07-01 12:00:00-04'")

	utc := newZoneHarness(t, "UTC")
	utc.exec(`INSERT INTO it_cz_def (id) VALUES (1)`)
	utc.exec(`INSERT INTO it_cz_def2 (id) VALUES (2)`)
	kolkata := newZoneHarness(t, "Asia/Kolkata")
	kolkata.exec(`INSERT INTO it_cz_def (id) VALUES (2)`)
	kolkata.exec(`INSERT INTO it_cz_def2 (id) VALUES (3)`)
	utc.expectRows(`SELECT id, CAST(untyped AS TEXT), CAST(typed AS TEXT), CAST(naive AS TEXT), CAST(from_date AS TEXT),
		CAST(from_naive AS TEXT), CAST(added AS TEXT) FROM it_cz_def ORDER BY id`,
		"1|2024-01-01 05:00:00+00|2024-03-10 07:30:00+00|2024-01-01 05:00:00|2024-01-01 00:00:00+00|2024-01-01 00:00:00+00|2024-07-01 16:00:00+00",
		"2|2024-01-01 05:00:00+00|2024-03-10 07:30:00+00|2024-01-01 10:30:00|2023-12-31 18:30:00+00|2023-12-31 18:30:00+00|2024-07-01 16:00:00+00")
	utc.expectRows(`SELECT id, CAST(later AS TEXT) FROM it_cz_def2 ORDER BY id`,
		"1|NULL", "2|2024-07-01 16:00:00+00", "3|2024-07-01 16:00:00+00")

	// A default stored by an earlier version, with the literal as written,
	// reads it in the session time zone, as it did.
	raw := h.rawClient()
	h.patchMeta(raw, metaKey(defaultSchema, "it_cz_def2"), "2024-07-01 12:00:00-04", "2024-07-01 12:00")
	utc.exec(`INSERT INTO it_cz_def2 (id) VALUES (4)`)
	utc.expectRows(`SELECT CAST(later AS TEXT) FROM it_cz_def2 WHERE id = 4`, "2024-07-01 12:00:00+00")
}

// A CHECK reads its local-time literals in the zone it was defined in
// (CREATE TABLE, ADD CONSTRAINT); the conversions of the row's values read
// the zone of the session that writes it.
func TestCreationZoneCheck(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_cz_chk")
	raw := h.rawClient()
	h.exec(`SET TIME ZONE 'America/New_York'`)
	h.exec(`CREATE TABLE it_cz_chk (id INTEGER, tz TIMESTAMPTZ CHECK (tz >= '2024-01-01 00:00'), ts TIMESTAMP,
		CONSTRAINT naive_after CHECK (ts >= TIMESTAMPTZ '2024-01-01 00:00'))`)
	h.exec(`ALTER TABLE it_cz_chk ADD CONSTRAINT before_2025 CHECK (tz < '2025-01-01 00:00')`)
	// As Postgres's pg_get_constraintdef, with the instants' offset in New
	// York.
	h.expectChecks(raw, "it_cz_chk", "it_cz_chk_tz_check: tz >= '2024-01-01 00:00:00-05'",
		"naive_after: ts >= TIMESTAMPTZ '2024-01-01 00:00:00-05'", "before_2025: tz < '2025-01-01 00:00:00-05'")

	utc := newZoneHarness(t, "UTC")
	utc.expectError(`INSERT INTO it_cz_chk (id, tz) VALUES (1, '2024-01-01 04:59:59+00')`,
		`violates check constraint "it_cz_chk_tz_check"`)
	utc.exec(`INSERT INTO it_cz_chk (id, tz) VALUES (2, '2024-01-01 05:00:00+00')`)
	utc.expectError(`INSERT INTO it_cz_chk (id, tz) VALUES (3, '2025-01-01 05:00:00+00')`,
		`violates check constraint "before_2025"`)
	utc.exec(`INSERT INTO it_cz_chk (id, tz) VALUES (4, '2025-01-01 04:59:59+00')`)
	utc.expectError(`INSERT INTO it_cz_chk (id, ts) VALUES (5, '2024-01-01 04:00')`,
		`violates check constraint "naive_after"`)
	utc.exec(`INSERT INTO it_cz_chk (id, ts) VALUES (6, '2024-01-01 05:00')`)
	kolkata := newZoneHarness(t, "Asia/Kolkata")
	kolkata.expectError(`INSERT INTO it_cz_chk (id, ts) VALUES (7, '2024-01-01 10:00')`,
		`violates check constraint "naive_after"`)
	kolkata.exec(`INSERT INTO it_cz_chk (id, ts) VALUES (8, '2024-01-01 10:30')`)
	kolkata.expectError(`INSERT INTO it_cz_chk (id, tz) VALUES (9, '2024-01-01 10:00')`,
		`violates check constraint "it_cz_chk_tz_check"`)
	kolkata.expectError(`UPDATE it_cz_chk SET tz = '2024-01-01 10:29' WHERE id = 2`,
		`violates check constraint "it_cz_chk_tz_check"`)
	h.expectRows(`SELECT id FROM it_cz_chk ORDER BY id`, "2", "4", "6", "8")

	// A CHECK stored by an earlier version, with the literal as written,
	// reads it in the session time zone, as it did.
	h.patchMeta(raw, metaKey(defaultSchema, "it_cz_chk"), "2024-01-01 00:00:00-05", "2024-01-01 00:00")
	utc.exec(`INSERT INTO it_cz_chk (id, tz) VALUES (10, '2024-01-01 04:00:00+00')`)
	utc.expectError(`INSERT INTO it_cz_chk (id, tz) VALUES (11, '2023-12-31 23:59:59+00')`,
		`violates check constraint "it_cz_chk_tz_check"`)
}
