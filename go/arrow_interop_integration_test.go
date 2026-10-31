// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestArrowNormalizedStorage(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_arrow_normalized"
	defer h.dropTables(table)
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "u", Type: arrow.PrimitiveTypes.Uint64, Nullable: true},
		{Name: "lists", Type: arrow.LargeListOf(arrow.ListOf(arrow.PrimitiveTypes.Int64)), Nullable: true},
		{Name: "record", Type: arrow.StructOf(arrow.Field{Name: "text", Type: arrow.BinaryTypes.String, Nullable: true}, arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Int64, Nullable: true}), Nullable: true},
	}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Uint64Builder).AppendValues([]uint64{math.MaxUint64, 1 << 63, 0}, []bool{true, true, false})
	if err := rb.Field(1).UnmarshalJSON([]byte(`[[[1,null],[],null,[2]],null,[]]`)); err != nil {
		t.Fatal(err)
	}
	if err := rb.Field(2).UnmarshalJSON([]byte(`[{"text":null,"n":null},null,{"text":"hello","n":42}]`)); err != nil {
		t.Fatal(err)
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	for _, mode := range []string{adbc.OptionValueIngestModeCreate, adbc.OptionValueIngestModeAppend} {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
			if err := st.SetOption(h.ctx, k, v); err != nil {
				t.Fatal(err)
			}
		}
		if mode == adbc.OptionValueIngestModeCreate {
			err = st.Bind(h.ctx, rec)
		} else {
			reader, _ := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
			err = st.BindStream(h.ctx, reader)
		}
		if err != nil {
			t.Fatal(err)
		}
		n, err := st.ExecuteUpdate(h.ctx)
		st.Close(h.ctx)
		if err != nil || n != 3 {
			t.Fatal(n, err)
		}
	}
	h.expectRows("SELECT u FROM "+table+" WHERE u > 9223372036854775807 ORDER BY u", "9223372036854775808", "9223372036854775808", "18446744073709551615", "18446744073709551615")
	h.expectRows("SELECT (record).text,(record).n FROM "+table+" WHERE u IS NULL", "hello|42", "hello|42")
	q := "SELECT u,lists,record FROM " + table
	materialized, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamResults: "false"})
	expected, schemaOut := unionValues(t, materialized)
	materialized.Release()
	stream, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "1"})
	actual, gotSchema := unionValues(t, stream)
	stream.Release()
	if !slices.Equal(actual, expected) || !schemaOut.Equal(gotSchema) {
		t.Fatal(expected, actual, schemaOut, gotSchema)
	}
	// Portable partitions retain the canonical nested/decimal schema and values.
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, q); err != nil {
		t.Fatal(err)
	}
	partSchema, parts, _, err := st.ExecutePartitions(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var partitionRows []string
	for _, token := range parts.PartitionIDs {
		r, err := h.conn.ReadPartition(h.ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		rows, schema := unionValues(t, r)
		r.Release()
		if !schema.Equal(partSchema) || !schema.Equal(schemaOut) {
			t.Fatal(schema, partSchema, schemaOut)
		}
		partitionRows = append(partitionRows, rows...)
	}
	want := slices.Clone(expected)
	slices.Sort(want)
	slices.Sort(partitionRows)
	if !slices.Equal(want, partitionRows) {
		t.Fatal(want, partitionRows)
	}
	tableSchema, err := h.conn.GetTableSchema(h.ctx, nil, nil, table)
	if err != nil || !tableSchema.Equal(schemaOut) {
		t.Fatal(tableSchema, schemaOut, err)
	}
}

func TestArrowResultLayouts(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_arrow_layouts"
	h.setupStreamTable(table, 37)
	defer h.dropTables(table)
	for _, layout := range []string{"large_string", "string_view", "dictionary", "fixed_size_binary:3"} {
		t.Run(layout, func(t *testing.T) {
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close(h.ctx)
			q := "SELECT label AS v FROM " + table
			name := "large_string"
			if strings.HasPrefix(layout, "fixed_size") {
				q = "SELECT CAST('a' AS VARBINARY) AS v FROM " + table
				name = "fixed_size_binary:1"
			} else {
				name = layout
			}
			if err := st.SetSqlQuery(h.ctx, q); err != nil {
				t.Fatal(err)
			}
			if err := st.SetOption(h.ctx, OptionStringResultLayouts, fmt.Sprintf(`{"v":%q}`, name)); err != nil {
				t.Fatal(err)
			}
			if err := st.SetOption(h.ctx, OptionIntStreamBatchRows, "3"); err != nil {
				t.Fatal(err)
			}
			schema, err := st.(adbc.StatementExecuteSchemaWithContext).ExecuteSchema(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			rdr, _, err := st.ExecuteQuery(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !schema.Equal(rdr.Schema()) {
				t.Fatal(schema, rdr.Schema())
			}
			count := int64(0)
			for rdr.Next() {
				count += rdr.RecordBatch().NumRows()
				for i := 0; i < int(rdr.RecordBatch().NumRows()); i++ {
					if _, err := valueAt(rdr.RecordBatch().Column(0), i); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = rdr.Err()
			rdr.Release()
			if err != nil || count != 37 {
				t.Fatal(count, err)
			}
			path := filepath.Join(t.TempDir(), "out.arrow")
			if err := st.SetOption(h.ctx, OptionStringIPCOutput, path); err != nil {
				t.Fatal(err)
			}
			n, err := st.ExecuteUpdate(h.ctx)
			if err != nil || n != 37 {
				t.Fatal(n, err)
			}
			input, err := openIPCInput(h.ctx, path, "stream", memory.DefaultAllocator)
			if err != nil {
				t.Fatal(err)
			}
			if !schema.Equal(input.Schema()) {
				t.Fatal(schema, input.Schema())
			}
			count = 0
			for input.Next() {
				count += input.RecordBatch().NumRows()
			}
			err = input.Err()
			input.Release()
			if err != nil || count != 37 {
				t.Fatal(count, err)
			}
			if err := st.SetOption(h.ctx, OptionStringIPCOutput, ""); err != nil {
				t.Fatal(err)
			}
			_, _, _, err = st.ExecutePartitions(h.ctx)
			if err == nil {
				t.Fatal("partition output silently ignored result layouts")
			}
		})
	}
}

func TestKeySourceStreaming(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_arrow_key_stream"
	h.setupStreamTable(table, 31)
	defer h.dropTables(table)
	for _, q := range []string{
		"SELECT id,label FROM " + table + " WHERE __rowid IN (3,1,2,5,9,6)",
		"(SELECT id,label FROM " + table + " WHERE __rowid IN (3,1,2,5,9,6) LIMIT 3 OFFSET 1) UNION ALL SELECT id,label FROM " + table + " LIMIT 9 OFFSET 2",
	} {
		m, _ := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionStringStreamResults: "false"})
		expected, schema := unionValues(t, m)
		m.Release()
		r, n := h.openStream(h.ctx, h.conn, q, nil, map[string]string{OptionIntStreamBatchRows: "1"})
		actual, gotSchema := unionValues(t, r)
		r.Release()
		if n != -1 || !slices.Equal(actual, expected) || !schema.Equal(gotSchema) {
			t.Fatal(n, expected, actual, schema, gotSchema)
		}
	}
}
