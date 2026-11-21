// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"testing"
)

func TestSQLDefaultRestrictedGrammar(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_default_grammar", "it_default_refused")
	h.exec("CREATE TABLE it_default_grammar(id integer DEFAULT 1 NOT NULL, b boolean DEFAULT (1 IS NULL), CHECK(id>0))")
	for _, sql := range []string{
		"CREATE TABLE it_default_refused(b boolean DEFAULT 1 IS NULL)",
		"CREATE TABLE it_default_refused(b boolean DEFAULT NULL::integer IS NORMALIZED)",
		"ALTER TABLE it_default_grammar ADD bad boolean DEFAULT 1 IS NULL",
		"ALTER TABLE it_default_grammar ADD bad boolean DEFAULT true AND false",
	} {
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = st.SetSqlQuery(h.ctx, sql)
		if err == nil {
			_, err = st.ExecuteUpdate(h.ctx)
		}
		_ = st.Close(h.ctx)
		if sqlState(err) != stateSyntax {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	h.expectRows("SELECT count(*) FROM information_schema.tables WHERE table_name='it_default_refused'", "0")
	h.expectRows("SELECT count(*) FROM information_schema.columns WHERE table_name='it_default_grammar' AND column_name='bad'", "0")
	h.exec("INSERT INTO it_default_grammar DEFAULT VALUES")
	h.expectRows("SELECT * FROM it_default_grammar", "1|false")
	h.exec("ALTER TABLE it_default_grammar ALTER b SET DEFAULT 1 IS NULL")
	h.exec("INSERT INTO it_default_grammar DEFAULT VALUES")
	h.expectRows("SELECT count(*) FROM it_default_grammar WHERE b=false", "2")
	h.expectRows("SELECT column_default FROM information_schema.columns WHERE table_name='it_default_grammar' AND column_name='b'", "1 IS NULL")
}

func TestSQLAlterTypeRetainedDefault(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_retained_default")
	h.exec("CREATE TABLE it_retained_default(id bigint,v numeric(4,2) DEFAULT '1.50')")
	b := array.NewInt64Builder(memory.DefaultAllocator)
	defer b.Release()
	b.Append(4)
	col := b.NewArray()
	defer col.Release()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{col}, 1)
	defer rec.Release()
	st := h.stmtWith("INSERT INTO it_retained_default(id) VALUES($1)", rec)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.exec("INSERT INTO it_retained_default VALUES(0,1.49)")
	h.exec("ALTER TABLE it_retained_default ALTER v TYPE integer")
	h.exec("INSERT INTO it_retained_default(id) VALUES(1)")
	h.exec("INSERT INTO it_retained_default VALUES(2,7),(3,NULL)")
	if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 1 {
		t.Fatalf("prepared: %d %v", n, err)
	}
	if err := h.ingest("it_retained_default", adbc.OptionValueIngestModeAppend, false, 5); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT * FROM it_retained_default ORDER BY id", "0|1", "1|2", "2|7", "3|NULL", "4|2", "5|2")
	h.expectRows("SELECT column_default FROM information_schema.columns WHERE table_name='it_retained_default' AND column_name='v'", "('1.50')::numeric")
	h.exec("ALTER TABLE it_retained_default ALTER v TYPE bigint")
	h.exec("INSERT INTO it_retained_default(id) VALUES(6)")
	h.expectRows("SELECT v FROM it_retained_default WHERE id=6", "2")
	// USING cannot make an incompatible retained default assignable, and a
	// refusal must preserve both stored rows and the previous default.
	bad := h.stmtWith("ALTER TABLE it_retained_default ALTER v TYPE boolean USING v>0", nil)
	if _, err := bad.ExecuteUpdate(h.ctx); sqlState(err) != stateDatatypeMismatch {
		t.Fatal(err)
	}
	h.exec("INSERT INTO it_retained_default(id) VALUES(7)")
	h.expectRows("SELECT v FROM it_retained_default WHERE id=7", "2")
}
