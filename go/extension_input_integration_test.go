// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/extensions"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestExtensionStorageReplay(t *testing.T) {
	h := newSQLHarness(t)
	for _, name := range []string{"arrow.uuid", "arrow.json"} {
		t.Run(name, func(t *testing.T) {
			const table = "it_extension_policy_replay"
			h.exec("DROP TABLE IF EXISTS " + table)
			defer h.exec("DROP TABLE IF EXISTS " + table)
			var a, want arrow.Array
			if name == "arrow.uuid" {
				b := array.NewFixedSizeBinaryBuilder(memory.DefaultAllocator, &arrow.FixedSizeBinaryType{ByteWidth: 16})
				b.Append([]byte("0123456789abcdef"))
				b.AppendNull()
				storage := b.NewArray()
				b.Release()
				a = array.NewExtensionArrayWithStorage(extensions.NewUUIDType(), storage)
				storage.Release()
				b2 := array.NewBinaryBuilder(memory.DefaultAllocator, arrow.BinaryTypes.Binary)
				b2.Append([]byte("0123456789abcdef"))
				b2.AppendNull()
				want = b2.NewArray()
				b2.Release()
			} else {
				storage := inventoryJSON(t, memory.DefaultAllocator, arrow.BinaryTypes.String, `["{\"x\":[1,null]}",null]`)
				dt, err := extensions.NewJSONType(arrow.BinaryTypes.String)
				if err != nil {
					t.Fatal(err)
				}
				a = array.NewExtensionArrayWithStorage(dt, storage)
				want = storage
			}
			rec := inventoryRecord(a)
			a.Release()
			defer rec.Release()
			expected := inventoryRecord(want)
			want.Release()
			defer expected.Release()
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			for k, v := range map[string]string{OptionStringExtensionPolicy: "storage", adbc.OptionKeyIngestTargetTable: table} {
				if err := st.SetOption(h.ctx, k, v); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.Bind(h.ctx, rec); err != nil {
				t.Fatal(err)
			}
			if _, err := st.ExecuteUpdate(h.ctx); err != nil {
				t.Fatal(err)
			}
			worker := newSQLHarness(t)
			for _, mode := range []string{"live", "retained"} {
				publisher := h.conn
				reader := worker.conn
				if mode == "retained" {
					storage := retainedFixture(t, h.rawClient(), defaultRetainedLimits())
					publisher = retainedAPIConnection(t, storage.namespace, false)
					reader = retainedAPIConnection(t, storage.namespace, true)
				}
				plan, err := publisher.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				for k, v := range map[string]string{OptionStringPartitionResultMode: mode, OptionIntPartitionCount: "1"} {
					if err := plan.SetOption(h.ctx, k, v); err != nil {
						t.Fatal(err)
					}
				}
				if err := plan.SetSqlQuery(h.ctx, "SELECT v FROM "+table); err != nil {
					t.Fatal(err)
				}
				schema, parts, _, err := plan.ExecutePartitions(h.ctx)
				plan.Close(h.ctx)
				if err != nil || parts.NumPartitions != 1 || !schema.Equal(expected.Schema()) {
					t.Fatal(schema, parts.NumPartitions, err)
				}
				if mode == "retained" {
					h.exec("TRUNCATE TABLE " + table)
				}
				r, err := reader.ReadPartition(h.ctx, parts.PartitionIDs[0])
				if err != nil {
					t.Fatal(err)
				}
				assertInventoryUnorderedReader(t, r, expected)
				r.Release()
			}
		})
	}
}

func TestExtensionPartitionBindingPolicy(t *testing.T) {
	for _, policy := range []string{"reject", "storage"} {
		h := newSQLHarness(t)
		const table = "it_extension_unknown_partition"
		h.exec("DROP TABLE IF EXISTS " + table)
		h.exec("CREATE TABLE " + table + " (v VARCHAR)")
		h.exec("INSERT INTO " + table + " VALUES ('opaque')")
		defer h.exec("DROP TABLE IF EXISTS " + table)
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		if err := st.SetOption(h.ctx, OptionStringExtensionPolicy, policy); err != nil {
			t.Fatal(err)
		}
		a := inventoryJSON(t, memory.DefaultAllocator, arrow.BinaryTypes.String, `["opaque"]`)
		defer a.Release()
		schema := arrow.NewSchema([]arrow.Field{{Name: "x", Type: a.DataType(), Metadata: arrow.NewMetadata([]string{"ARROW:extension:name"}, []string{"custom.unknown"})}}, nil)
		rec := array.NewRecordBatch(schema, []arrow.Array{a}, 1)
		defer rec.Release()
		if err := st.Bind(h.ctx, rec); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSqlQuery(h.ctx, "SELECT v FROM "+table+" WHERE v=?"); err != nil {
			t.Fatal(err)
		}
		_, parts, _, err := st.ExecutePartitions(h.ctx)
		if err == nil || parts.NumPartitions != 0 {
			t.Fatal("partition binding dropped unknown extension metadata", policy, err)
		}
	}
}
