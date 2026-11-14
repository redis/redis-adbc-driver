// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestStatementRepeatedSQLKeepsSyntaxAndClearsBinding(t *testing.T) {
	ctx := context.Background()
	s := &statementImpl{}
	const sql = "SELECT $1 + 1"
	if err := s.SetSqlQuery(ctx, sql); err != nil {
		t.Fatal(err)
	}
	first, err := s.parse()
	if err != nil {
		t.Fatal(err)
	}
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	b := array.NewInt64Builder(mem)
	b.Append(41)
	a := b.NewArray()
	b.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{a}, 1)
	a.Release()
	if err := s.Bind(ctx, rec); err != nil {
		t.Fatal(err)
	}
	rec.Release()
	s.ingest.TableName = "previous_ingest"
	s.partitionExecution = &incrementalPartitions{}
	if err := s.SetSqlQuery(ctx, sql); err != nil {
		t.Fatal(err)
	}
	second, err := s.parse()
	if err != nil {
		t.Fatal(err)
	}
	if &first[0] != &second[0] {
		t.Fatal("identical SQL was reparsed")
	}
	if s.params != nil || s.partitionExecution != nil || s.ingest.TableName != "" {
		t.Fatal("syntax reuse retained execution state")
	}
	if err := s.SetSqlQuery(ctx, "SELECT 2"); err != nil {
		t.Fatal(err)
	}
	if s.parsed != nil {
		t.Fatal("changed SQL retained old syntax")
	}
	if _, err := s.parse(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSqlQuery(ctx, "SELECT ("); err != nil {
		t.Fatal(err)
	}
	if _, err := s.parse(); err == nil {
		t.Fatal("invalid replacement SQL used old syntax")
	}
}

func BenchmarkStatementRepeatedSQLParse(b *testing.B) {
	for _, same := range []bool{false, true} {
		name := "changing"
		if same {
			name = "identical"
		}
		b.Run(name, func(b *testing.B) {
			s := &statementImpl{}
			ctx := context.Background()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sql := "SELECT id,grp,amount,label FROM bench.managed WHERE __rowid=$1"
				if !same && i%2 == 0 {
					sql += " "
				}
				if err := s.SetSqlQuery(ctx, sql); err != nil {
					b.Fatal(err)
				}
				if _, err := s.parse(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestStatementRepeatedSQLRebindsCatalog(t *testing.T) {
	h, _, _ := newSnapshotHarness(t)
	h.exec("CREATE TABLE repeated_sql(v BIGINT)")
	h.exec("INSERT INTO repeated_sql VALUES(42)")
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	read := func(want arrow.Type, value string) {
		t.Helper()
		if err := st.SetSqlQuery(h.ctx, "SELECT * FROM repeated_sql"); err != nil {
			t.Fatal(err)
		}
		if err := st.Prepare(h.ctx); err != nil {
			t.Fatal(err)
		}
		r, _, err := st.ExecuteQuery(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Release()
		if r.Schema().Field(0).Type.ID() != want {
			t.Fatalf("stale prepared schema: %s", r.Schema())
		}
		rows := int64(0)
		for r.Next() {
			rec := r.RecordBatch()
			rows += rec.NumRows()
			if rec.NumRows() != 1 || rec.Column(0).ValueStr(0) != value {
				t.Fatalf("stale prepared row: %v", rec)
			}
		}
		if r.Err() != nil || rows != 1 {
			t.Fatal("prepared fetch", rows, r.Err())
		}
	}
	read(arrow.INT64, "42")
	h.exec("UPDATE repeated_sql SET v=43")
	read(arrow.INT64, "43")
	h.exec("ALTER TABLE repeated_sql ALTER COLUMN v TYPE TEXT")
	read(arrow.STRING, "43")
}
