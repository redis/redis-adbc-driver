// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Earlier mutable adoption metadata and interrupted legacy reservations cannot
// be upgraded or resumed into the current immutable storage format.
func TestSnapshotImportRejectsLegacyStorage(t *testing.T) {
	for _, kind := range []string{"v0011_table", "unfinished_reservation"} {
		t.Run(kind, func(t *testing.T) {
			h, raw, _ := newSnapshotHarness(t)
			if err := h.conn.Close(h.ctx); err != nil {
				t.Fatal(err)
			}
			if err := raw.FlushDB(h.ctx).Err(); err != nil {
				t.Fatal(err)
			}
			const prefix = "legacy:application:"
			for i := 1; i <= 3; i++ {
				if err := raw.HSet(h.ctx, fmt.Sprintf("%s%d", prefix, i), "n", i, rowIDField, i).Err(); err != nil {
					t.Fatal(err)
				}
			}
			var legacyKeys []string
			if kind == "v0011_table" {
				key := metaKey("public", "legacy_copy")
				value := `{"schema":"public","name":"legacy_copy","key_prefix":"legacy:application:","adopted":true,"columns":[{"name":"n","type":{"Kind":2}}]}`
				if err := raw.Set(h.ctx, key, value, 0).Err(); err != nil {
					t.Fatal(err)
				}
				legacyKeys = []string{key}
			} else {
				index := adoptIndexName("public", "legacy_copy")
				record := `{"schema":"public","table":"legacy_copy","index":"` + index + `"}`
				pipe := raw.TxPipeline()
				pipe.SAdd(h.ctx, prefixesKey, prefix)
				pipe.SAdd(h.ctx, adoptedKey, prefix)
				pipe.SAdd(h.ctx, indexesKey, index)
				pipe.HSet(h.ctx, adoptingKey, prefix, record)
				if _, err := pipe.Exec(h.ctx); err != nil {
					t.Fatal(err)
				}
				legacyKeys = []string{prefixesKey, adoptedKey, indexesKey, adoptingKey}
			}
			before := map[string]string{}
			for _, key := range legacyKeys {
				value, err := raw.Dump(h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				before[key] = value
			}
			size := raw.DBSize(h.ctx).Val()
			options := map[string]string{adbc.OptionKeyURI: os.Getenv("REDIS_SNAPSHOT_URI")}
			// An explicit second attempt must reject again, without resuming old work.
			for attempt := 0; attempt < 2; attempt++ {
				result, err := AdoptHashes(h.ctx, options, HashAdoptOptions{Prefix: prefix, Table: "legacy_copy", Apply: true, OwnIndex: true})
				if err == nil || !strings.Contains(err.Error(), "existing driver metadata cannot be imported or upgraded") || result != nil && result.Applied {
					t.Fatalf("legacy import attempt %d: %+v, %v", attempt, result, err)
				}
			}
			db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(h.ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(h.ctx)
			if conn, err := db.Open(h.ctx); err == nil {
				conn.Close(h.ctx)
				t.Fatal("legacy storage opened without rejection")
			}
			if raw.Exists(h.ctx, snapshotCatalogKey).Val() != 0 || raw.DBSize(h.ctx).Val() != size {
				t.Fatal("legacy rejection initialized or removed storage")
			}
			for key, value := range before {
				got, err := raw.Dump(h.ctx, key).Result()
				if err != nil {
					t.Fatal(err)
				}
				if got != value {
					t.Fatal("rejected import changed legacy metadata")
				}
			}
			for i := 1; i <= 3; i++ {
				fields := raw.HGetAll(h.ctx, fmt.Sprintf("%s%d", prefix, i)).Val()
				if len(fields) != 2 || fields["n"] != fmt.Sprint(i) || fields[rowIDField] != fmt.Sprint(i) {
					t.Fatal("legacy rejection changed application HASH")
				}
			}
		})
	}
}
