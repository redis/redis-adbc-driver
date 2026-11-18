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

func TestSQLDefaultErrorTiming(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_default_timing", "it_default_timing_add")
	h.exec(`CREATE TABLE it_default_timing (id INTEGER, bad INTEGER DEFAULT 1/0, text_bad TEXT DEFAULT unistr('\xyz'))`)
	h.exec("INSERT INTO it_default_timing VALUES(1,2,'ok'),(2,NULL,NULL)")
	h.expectRows("SELECT * FROM it_default_timing ORDER BY id", "1|2|ok", "2|NULL|NULL")
	for _, tc := range []struct{ sql, state string }{
		{"INSERT INTO it_default_timing(id,text_bad) VALUES(3,'ok')", stateDivisionByZero},
		{"INSERT INTO it_default_timing(id,bad) VALUES(3,3)", stateSyntax},
		{"INSERT INTO it_default_timing(id,text_bad) SELECT 3,'ok' WHERE FALSE", stateDivisionByZero},
		{"INSERT INTO it_default_timing DEFAULT VALUES", stateDivisionByZero},
		{"INSERT INTO it_default_timing VALUES(3,3,'ok'),(4,DEFAULT,'ok')", stateDivisionByZero},
	} {
		st := h.stmtWith(tc.sql, nil)
		if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != tc.state {
			t.Errorf("%s: %v, want %s", tc.sql, err, tc.state)
		}
		h.expectRows("SELECT count(*) FROM it_default_timing", "2")
	}
	h.exec("INSERT INTO it_default_timing SELECT 3,3,'ok' WHERE FALSE")
	h.exec("ALTER TABLE it_default_timing ALTER bad SET DEFAULT ('bad'::text)::integer")
	h.exec("ALTER TABLE it_default_timing ALTER bad TYPE BIGINT")
	h.exec("INSERT INTO it_default_timing VALUES(3,4,'ok')")
	st := h.stmtWith("INSERT INTO it_default_timing(id,text_bad) VALUES(4,'ok')", nil)
	if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateInvalidText {
		t.Fatalf("ALTER retained default: %v", err)
	}
	h.exec("ALTER TABLE it_default_timing ALTER bad DROP DEFAULT")
	h.exec("ALTER TABLE it_default_timing ALTER text_bad SET DEFAULT 'valid'")
	h.exec("INSERT INTO it_default_timing(id) VALUES(4)")
	h.expectRows("SELECT * FROM it_default_timing ORDER BY id", "1|2|ok", "2|NULL|NULL", "3|4|ok", "4|NULL|valid")
	h.exec("CREATE TABLE it_default_timing_add(id INTEGER)")
	st = h.stmtWith("ALTER TABLE it_default_timing_add ADD bad INTEGER DEFAULT 1/0", nil)
	if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateDivisionByZero {
		t.Fatalf("empty ADD materialization: %v", err)
	}
	h.expectRows("SELECT count(*) FROM information_schema.columns WHERE table_name='it_default_timing_add' AND column_name='bad'", "0")
}

func TestSQLDefaultDeferredTypmods(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_default_typmods")
	h.exec("CREATE TABLE it_default_typmods(id INTEGER, s SMALLINT DEFAULT 100000, v VARCHAR(2) DEFAULT 'long', n NUMERIC(2,0) DEFAULT '1000')")
	for _, tc := range []struct{ sql, state string }{
		{"INSERT INTO it_default_typmods(id,v,n) VALUES(1,'ok',1)", stateNumericRange},
		{"INSERT INTO it_default_typmods(id,s,n) VALUES(1,1,1)", stateStringTooLong},
		{"INSERT INTO it_default_typmods(id,s,v) VALUES(1,1,'ok')", stateNumericRange},
		{"INSERT INTO it_default_typmods(id,v,n) SELECT 1,'ok',1 WHERE FALSE", stateNumericRange},
		{"INSERT INTO it_default_typmods(id,s,n) SELECT 1,1,1 WHERE FALSE", stateStringTooLong},
	} {
		st := h.stmtWith(tc.sql, nil)
		if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != tc.state {
			t.Errorf("%s: %v, want %s", tc.sql, err, tc.state)
		}
	}
	h.expectRows("SELECT * FROM it_default_typmods")
	h.exec("INSERT INTO it_default_typmods VALUES(1,1,'ok',1),(2,NULL,NULL,NULL)")
	h.expectRows("SELECT * FROM it_default_typmods ORDER BY id", "1|1|ok|1", "2|NULL|NULL|NULL")
}

func TestSQLDefaultBoundAndIngest(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_default_bound")
	h.exec("CREATE TABLE it_default_bound(id INTEGER,b INTEGER DEFAULT 1/0)")
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: arrow.PrimitiveTypes.Int32}, {Name: "", Type: arrow.PrimitiveTypes.Int32, Nullable: true}}, nil))
	defer b.Release()
	b.Field(0).(*array.Int32Builder).AppendValues([]int32{1, 2}, nil)
	b.Field(1).(*array.Int32Builder).AppendValues([]int32{1, 0}, []bool{true, false})
	rec := b.NewRecordBatch()
	defer rec.Release()
	st := h.stmtWith("INSERT INTO it_default_bound VALUES($1,$2)", rec)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := st.ExecuteUpdate(h.ctx); err != nil || n != 2 {
		t.Fatalf("explicit bindings: %d %v", n, err)
	}
	ids := array.NewInt32Builder(memory.DefaultAllocator)
	defer ids.Release()
	ids.AppendValues([]int32{3, 4}, nil)
	col := ids.NewArray()
	defer col.Release()
	one := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "", Type: arrow.PrimitiveTypes.Int32}}, nil), []arrow.Array{col}, 2)
	defer one.Release()
	bad := h.stmtWith("INSERT INTO it_default_bound(id) VALUES($1)", one)
	if err := bad.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := bad.ExecuteUpdate(h.ctx); sqlState(err) != stateDivisionByZero {
		t.Fatalf("omitted binding default: %v", err)
	}
	if err := h.ingest("it_default_bound", adbc.OptionValueIngestModeAppend, false, 5, 6); sqlState(err) != stateDivisionByZero {
		t.Fatalf("omitted Arrow default: %v", err)
	}
	h.expectRows("SELECT * FROM it_default_bound ORDER BY id", "1|1", "2|NULL")
	h.exec("ALTER TABLE it_default_bound ALTER b SET DEFAULT 5")
	if err := bad.Bind(h.ctx, one); err != nil {
		t.Fatal(err)
	}
	if n, err := bad.ExecuteUpdate(h.ctx); err != nil || n != 2 {
		t.Fatalf("reuse after refused default: %d %v", n, err)
	}
	if err := h.ingest("it_default_bound", adbc.OptionValueIngestModeAppend, false, 5, 6); err != nil {
		t.Fatal(err)
	}
	h.expectRows("SELECT * FROM it_default_bound ORDER BY id", "1|1", "2|NULL", "3|5", "4|5", "5|5", "6|5")
	if rec.Column(1).ValueStr(0) != "1" || !rec.Column(1).IsNull(1) {
		t.Fatal("bound input changed")
	}
}

func TestSQLDefaultSideEffects(t *testing.T) {
	h := newSQLHarness(t)
	h.exec("DROP SEQUENCE IF EXISTS it_default_seq")
	t.Cleanup(func() { h.exec("DROP SEQUENCE IF EXISTS it_default_seq") })
	h.dropTables("it_default_effect", "it_default_sequence")
	h.exec("SELECT set_config('application_name','base',false)")
	h.exec("CREATE TABLE it_default_effect(id INTEGER,s TEXT DEFAULT set_config('application_name',current_setting('application_name')||'x',false))")
	h.expectRows("SELECT current_setting('application_name')", "base")
	h.exec("INSERT INTO it_default_effect VALUES(0,'explicit')")
	h.expectRows("SELECT current_setting('application_name')", "base")
	h.exec("ALTER TABLE it_default_effect ALTER s SET DEFAULT set_config('application_name',current_setting('application_name')||'x',false)")
	h.expectRows("SELECT current_setting('application_name')", "base")
	h.exec("INSERT INTO it_default_effect(id) SELECT 1 WHERE FALSE")
	h.expectRows("SELECT current_setting('application_name')", "base")
	h.exec("INSERT INTO it_default_effect(id) VALUES(1),(2),(3)")
	h.expectRows("SELECT s FROM it_default_effect ORDER BY id", "explicit", "basex", "basexx", "basexxx")
	h.exec("CREATE SEQUENCE it_default_seq START WITH 10")
	h.exec("CREATE TABLE it_default_sequence(id BIGINT DEFAULT CASE WHEN TRUE THEN nextval('it_default_seq') ELSE 0 END,bad INTEGER DEFAULT 1/0)")
	st := h.stmtWith("INSERT INTO it_default_sequence DEFAULT VALUES", nil)
	if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateDivisionByZero {
		t.Fatal(err)
	}
	h.expectRows("SELECT nextval('it_default_seq')", "10")
	h.exec("ALTER TABLE it_default_sequence ALTER bad SET DEFAULT 1")
	h.exec("INSERT INTO it_default_sequence DEFAULT VALUES")
	h.expectRows("SELECT * FROM it_default_sequence", "11|1")
	h.exec("ALTER TABLE it_default_sequence ALTER bad TYPE BIGINT")
	h.expectRows("SELECT nextval('it_default_seq')", "12")
	h.exec("INSERT INTO it_default_sequence(bad) SELECT 1 WHERE FALSE")
	h.exec("INSERT INTO it_default_sequence(bad) VALUES(1),(2)")
	h.expectRows("SELECT id FROM it_default_sequence ORDER BY id", "11", "13", "14")
	h.exec("ALTER TABLE it_default_sequence ALTER id SET DEFAULT CASE WHEN FALSE THEN 0 ELSE nextval('it_default_seq') END")
	h.exec("INSERT INTO it_default_sequence(bad) SELECT 1 WHERE FALSE")
	h.expectRows("SELECT nextval('it_default_seq')", "15")

}
