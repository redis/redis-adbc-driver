// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestUnmanagedLiveSourceWrites(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "adopt"
		if foreign {
			name = "foreign"
		}
		t.Run(name, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
			for _, key := range []string{"app:1", "app:2"} {
				if err := raw.HSet(h.ctx, key, "n", "10", "extra", "keep").Err(); err != nil {
					t.Fatal(err)
				}
			}
			if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
				t.Fatal(err)
			}
			source := &store{client: raw}
			if err := source.awaitIndexReady(h.ctx, "app_idx"); err != nil {
				t.Fatal(err)
			}
			table := "live"
			if foreign {
				setter := h.conn.(adbc.GetSetOptionsWithContext)
				if err := setter.SetOption(h.ctx, OptionStringForeignIndexesMode, "unmanaged"); err != nil {
					t.Fatal(err)
				}
				if err := setter.SetOption(h.ctx, OptionStringForeignIndexes, "true"); err != nil {
					t.Fatal(err)
				}
				table = "redis_foreign.app_idx"
			} else {
				result, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Table: table, Apply: true})
				if err != nil {
					t.Fatal(err)
				}
				if !result.Applied || result.ManagedCopy || !result.AppIndex {
					t.Fatalf("incorrect adoption result: %+v", result)
				}
			}
			h.expectRows("SELECT n FROM "+table+" ORDER BY __key", "10", "10")
			if err := raw.HSet(h.ctx, "app:1", "n", "20").Err(); err != nil {
				t.Fatal(err)
			}
			h.expectRows("SELECT n FROM "+table+" WHERE __key='app:1'", "20")
			root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
			h.exec("INSERT INTO " + table + " (__key,n) VALUES ('app:3',30)")
			h.exec("UPDATE " + table + " SET n=n+1 WHERE __key='app:1'")
			h.exec("DELETE FROM " + table + " WHERE __key='app:2'")
			if got := raw.HGet(h.ctx, "app:1", "n").Val(); got != "21" {
				t.Fatalf("UPDATE did not change source: %q", got)
			}
			if got := raw.HGet(h.ctx, "app:1", "extra").Val(); got != "keep" {
				t.Fatalf("UPDATE lost unrelated source field: %q", got)
			}
			if got := raw.HGet(h.ctx, "app:3", "n").Val(); got != "30" {
				t.Fatalf("INSERT did not change source: %q", got)
			}
			if n := raw.Exists(h.ctx, "app:2").Val(); n != 0 {
				t.Fatal("DELETE did not remove source")
			}
			if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
				t.Fatal("unmanaged source DML published a managed generation")
			}
			h.expectRows("SELECT n FROM "+table+" ORDER BY n", "21", "30")
			h.expectError("INSERT INTO "+table+" (n) VALUES (99)", "__key")
			h.expectError("INSERT INTO "+table+" (__key,n) VALUES ('other:1',99)", "prefix")
			h.expectError("ALTER TABLE "+table+" ADD COLUMN bad INTEGER", "unmanaged")
			h.expectError("TRUNCATE "+table, "unmanaged")
			h.expectError("CREATE INDEX bad ON "+table+"(n)", "application-owned")
			setter := h.conn.(adbc.GetSetOptionsWithContext)
			if err := setter.SetOption(h.ctx, OptionStringReadSnapshotAction, "acquire"); err != nil {
				t.Fatal(err)
			}
			h.expectError("SELECT n FROM "+table, "unmanaged")
			if err := setter.SetOption(h.ctx, OptionStringReadSnapshotAction, "release"); err != nil {
				t.Fatal(err)
			}
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			if err := st.SetOption(h.ctx, OptionStringTruncateAdopted, "true"); err != nil {
				t.Fatal(err)
			}
			if err := st.SetSqlQuery(h.ctx, "TRUNCATE "+table); err != nil {
				t.Fatal(err)
			}
			if _, err := st.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), "unmanaged") {
				t.Fatalf("TRUNCATE opt-in escaped unmanaged refusal: %v", err)
			}
			if err := st.SetOption(h.ctx, OptionStringOperationAction, "prepare"); err != nil {
				t.Fatal(err)
			}
			if err := st.SetSqlQuery(h.ctx, "UPDATE "+table+" SET n=999"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.ExecuteUpdate(h.ctx); err == nil || !strings.Contains(err.Error(), "unmanaged") {
				t.Fatalf("prepared write escaped unmanaged refusal: %v", err)
			}
			h.expectRows("SELECT n FROM "+table+" ORDER BY n", "21", "30")
			// A managed copy of the same source stays isolated from later source writes.
			if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "app:", Table: "frozen", Apply: true}); err != nil {
				t.Fatal(err)
			}
			h.exec("UPDATE " + table + " SET n=44 WHERE __key='app:1'")
			h.expectRows("SELECT n FROM frozen ORDER BY n", "21", "30")
			binaryKey := "app:\x00binary"
			if err := raw.HSet(h.ctx, binaryKey, "n", "55").Err(); err != nil {
				t.Fatal(err)
			}
			h.expectRows("SELECT __key FROM "+table+" WHERE n=55", binaryKey)
			h.exec("UPDATE " + table + " SET n=66 WHERE n=55")
			if got := raw.HGet(h.ctx, binaryKey, "n").Val(); got != "66" {
				t.Fatalf("binary source key was not updated: %q", got)
			}
			if _, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: "app:", Table: strings.TrimPrefix(table, "redis_foreign."), Schema: map[bool]string{true: foreignSchema, false: "public"}[foreign], Apply: true}); err == nil {
				t.Fatal("managed import silently converted unmanaged registration")
			}
			h.exec("DROP TABLE " + table)
			if _, err := SweepReleased(h.ctx, options, SweepOptions{Apply: true}); err != nil {
				t.Fatal(err)
			}
			if got := raw.HGet(h.ctx, "app:1", "n").Val(); got != "44" {
				t.Fatalf("DROP or sweep changed source: %q", got)
			}
			if err := raw.Do(h.ctx, "FT.INFO", "app_idx").Err(); err != nil {
				t.Fatal("DROP or sweep removed application index", err)
			}
		})
	}
}

func TestUnmanagedEncodingAndMissingIndexDrop(t *testing.T) {
	h, raw, _ := newSnapshotHarness(t)
	if err := raw.HSet(h.ctx, "app:1", "n", "10", "enabled", "true").Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.PExpire(h.ctx, "app:1", 5*time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
		t.Fatal(err)
	}
	options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
	o := HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Table: "live",
		Types: map[string]string{"enabled": "BOOLEAN"}, Encodings: map[string]string{"enabled": "text"}, Renames: map[string]string{"n": "number"}}
	root := raw.HGet(h.ctx, snapshotCatalogKey, "root").Val()
	plan, err := AdoptHashes(h.ctx, options, o)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Applied || !plan.Report.Checks[0].Ready || len(plan.Steps) != 1 || !strings.Contains(plan.Steps[0], "without copying") || !strings.Contains(plan.Report.Checks[0].Command, "-encoding enabled=text") || !strings.Contains(plan.Report.Checks[0].Command, "-mode unmanaged") {
		t.Fatalf("unmanaged planning lost its source mode/encoding contract: %+v", plan)
	}
	if root != raw.HGet(h.ctx, snapshotCatalogKey, "root").Val() {
		t.Fatal("unmanaged planning published a catalog generation")
	}
	o.Apply = true
	if _, err := AdoptHashes(h.ctx, options, o); err != nil {
		t.Fatal(err)
	}
	if err := raw.HSet(h.ctx, "app:1", "unmodeled", "keep").Err(); err != nil {
		t.Fatal(err)
	}
	h.exec("UPDATE live SET enabled=FALSE,number=11 WHERE __key='app:1'")
	if raw.HGet(h.ctx, "app:1", "enabled").Val() != "false" || raw.HGet(h.ctx, "app:1", "n").Val() != "11" || raw.HGet(h.ctx, "app:1", "unmodeled").Val() != "keep" {
		t.Fatal("SQL write changed source encoding, field mapping or unrelated fields")
	}
	if ttl := raw.PTTL(h.ctx, "app:1").Val(); ttl <= 0 || ttl > 5*time.Minute {
		t.Fatal("SQL write changed source expiry")
	}
	h.exec("INSERT INTO live (__key,number,enabled) VALUES ('app:2',12,TRUE)")
	if raw.HGet(h.ctx, "app:2", "enabled").Val() != "true" || raw.HGet(h.ctx, "app:2", "n").Val() != "12" {
		t.Fatal("INSERT did not retain registered field encodings")
	}
	h.expectRows("SELECT number,enabled FROM live ORDER BY number", "11|false", "12|true")
	if err := raw.Do(h.ctx, "FT.DROPINDEX", "app_idx").Err(); err != nil {
		t.Fatal(err)
	}
	h.expectError("SELECT number FROM live", "unmanaged source index")
	h.exec("DROP TABLE live")
	if raw.Exists(h.ctx, "app:1", "app:2").Val() != 2 {
		t.Fatal("unregistering after index removal changed source keys")
	}
}
