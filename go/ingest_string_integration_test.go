// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestIngestStringArenaMultiRecordValues(t *testing.T) {
	for _, unmanaged := range []bool{false, true} {
		t.Run(fmt.Sprintf("unmanaged_%t", unmanaged), func(t *testing.T) {
			h, raw, _ := newSnapshotHarnessMode(t, false, false)
			keyName := "source_key"
			if unmanaged {
				keyName = "__key"
				if err := raw.HSet(h.ctx, "app:seed", "n", "0", "note", "seed").Err(); err != nil {
					t.Fatal(err)
				}
				if err := raw.Do(h.ctx, "FT.CREATE", "app_idx", "ON", "HASH", "PREFIX", 1, "app:", "SCHEMA", "n", "NUMERIC").Err(); err != nil {
					t.Fatal(err)
				}
				if err := (&store{client: raw}).awaitIndexReady(h.ctx, "app_idx"); err != nil {
					t.Fatal(err)
				}
				_, err := AdoptHashes(h.ctx, unmanagedArrowDatabaseOptions(), HashAdoptOptions{Mode: "unmanaged", Prefix: "app:", Table: "t", Types: map[string]string{"n": "BIGINT", "note": "VARCHAR"}, Apply: true})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				h.exec("CREATE TABLE t(source_key VARCHAR,n BIGINT,note VARCHAR, omitted VARCHAR DEFAULT 'fallback')")
			}
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			schema := arrow.NewSchema([]arrow.Field{{Name: keyName, Type: arrow.BinaryTypes.String}, {Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "note", Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
			var records []arrow.RecordBatch
			var expected []string
			next := int64(1)
			for _, n := range []int{4096, 3, 4097, 1} {
				builder := array.NewRecordBuilder(mem, schema)
				for r := 0; r < n; r++ {
					key := fmt.Sprintf("app:row%d", next)
					builder.Field(0).(*array.StringBuilder).Append(key)
					value := int64(9007199254740993) + next
					builder.Field(1).(*array.Int64Builder).Append(value)
					note := fmt.Sprintf("é:\x00%d", next)
					if next%7 == 0 {
						builder.Field(2).(*array.StringBuilder).AppendNull()
						note = "NULL"
					} else {
						builder.Field(2).(*array.StringBuilder).Append(note)
					}
					expected = append(expected, fmt.Sprintf("%s|%d|%s", key, value, note))
					next++
				}
				records = append(records, builder.NewRecordBatch())
				builder.Release()
			}
			reader, err := array.NewRecordReader(schema, records)
			for _, record := range records {
				record.Release()
			}
			if err != nil {
				t.Fatal(err)
			}
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				reader.Release()
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: "t", adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeAppend} {
				if err := st.SetOption(h.ctx, k, v); err != nil {
					reader.Release()
					t.Fatal(err)
				}
			}
			if err := st.BindStream(h.ctx, reader); err != nil {
				t.Fatal(err)
			}
			written, err := st.ExecuteUpdate(h.ctx)
			if err != nil || written != int64(len(expected)) {
				t.Fatalf("rows%d error%v", written, err)
			}
			// Check every value after all Arrow records have been consumed. SQL
			// ordering follows numeric n, avoiding lexical source-key ordering.
			query := fmt.Sprintf("SELECT %s,n,note FROM t WHERE n>0 ORDER BY n", keyName)
			rows, _ := h.query(query)
			if len(rows) != len(expected) {
				t.Fatalf("got%d rows want%d", len(rows), len(expected))
			}
			for i := range rows {
				if rows[i] != expected[i] {
					t.Fatalf("row%d got%q want%q", i, rows[i], expected[i])
				}
			}
			if !unmanaged {
				h.expectRows("SELECT COUNT(*) FROM t WHERE omitted='fallback'", fmt.Sprint(len(expected)))
			}
		})
	}
}
