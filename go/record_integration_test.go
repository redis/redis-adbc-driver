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

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// setupRecords creates it_rec_t (indexed) and it_rec_n (the same rows, not
// indexed):
//
//	id | a    | b
//	 1 | 1    | x
//	 2 | 1    | NULL
//	 3 | NULL | y
//	 4 | NULL | NULL
//	 5 | 2    | a
//	 6 | 1    | x
func (h *sqlHarness) setupRecords() {
	h.t.Helper()
	h.dropTables("it_rec_t", "it_rec_n", "it_rec_c")
	rows := "(1, 1, 'x'), (2, 1, NULL), (3, NULL, 'y'), (4, NULL, NULL), (5, 2, 'a'), (6, 1, 'x')"
	h.exec("CREATE TABLE it_rec_t (id INTEGER, a INTEGER, b VARCHAR)")
	h.exec("INSERT INTO it_rec_t VALUES " + rows)
	h.exec("CREATE TABLE it_rec_n (id INTEGER, a INTEGER NOINDEX, b VARCHAR NOINDEX)")
	h.exec("INSERT INTO it_rec_n VALUES " + rows)
}

// Rows as values over a table, indexed or not, with the results Postgres 16
// gives: sorting by a record (NULL fields last ascending, first
// descending), DISTINCT and GROUP BY on records (NULL fields equal),
// windows, record comparisons in WHERE, and fields.
func TestSQLRecordValues(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRecords()
	for _, tbl := range []string{"it_rec_t", "it_rec_n"} {
		h.expectRows("SELECT id, (a, b)::text FROM "+tbl+" ORDER BY (a, b), id",
			"1|(1,x)", "6|(1,x)", "2|(1,)", "5|(2,a)", "3|(,y)", "4|(,)")
		h.expectRows("SELECT id, (a, b)::text FROM "+tbl+" ORDER BY (a, b) DESC, id",
			"4|(,)", "3|(,y)", "5|(2,a)", "2|(1,)", "1|(1,x)", "6|(1,x)")
		h.expectRows("SELECT id FROM "+tbl+" ORDER BY ROW(a, b) DESC LIMIT 3", "4", "3", "5")
		h.expectRows("SELECT id FROM "+tbl+" ORDER BY ROW(a, b) NULLS FIRST, id LIMIT 3", "1", "6", "2")
		// s.r: a bare r is the output column r::text, as in Postgres.
		h.expectRows("SELECT r::text FROM (SELECT DISTINCT (a, b) AS r FROM "+tbl+") s ORDER BY s.r",
			"(1,x)", "(1,)", "(2,a)", "(,y)", "(,)")
		h.expectRows("SELECT ROW(a, b)::text, count(*) FROM "+tbl+" GROUP BY ROW(a, b) ORDER BY ROW(a, b) DESC",
			"(,)|1", "(,y)|1", "(2,a)|1", "(1,)|1", "(1,x)|2")
		// GROUP BY (a, b) groups by a and b (the index can do it).
		h.expectRows("SELECT (a, b)::text, count(*) FROM "+tbl+" GROUP BY (a, b) ORDER BY (a, b)",
			"(1,x)|2", "(1,)|1", "(2,a)|1", "(,y)|1", "(,)|1")
		h.expectRows("SELECT (r).f1, (r).f2, count(*) FROM (SELECT ROW(a, b) AS r FROM "+tbl+") s GROUP BY r ORDER BY r",
			"1|x|2", "1|NULL|1", "2|a|1", "NULL|y|1", "NULL|NULL|1")
		h.expectRows("SELECT count(DISTINCT (a, b)) FROM "+tbl, "5")
		h.expectRows("SELECT id, row_number() OVER (PARTITION BY (a, b) ORDER BY id), "+
			"row_number() OVER (ORDER BY (a, b) DESC, id) FROM "+tbl+" ORDER BY id",
			"1|1|5", "2|1|4", "3|1|2", "4|1|1", "5|1|3", "6|2|6")
		// A record compared with a record: NULL fields are equal. A row
		// compared with a row: NULL items make it NULL.
		h.expectRows("SELECT id FROM (SELECT id, (a, b) AS r FROM "+tbl+") s WHERE r = (1, NULL) ORDER BY id", "2")
		h.expectRows("SELECT id FROM " + tbl + " WHERE (a, b) = (1, NULL) ORDER BY id")
		h.expectRows("SELECT id FROM "+tbl+" WHERE ((a, b), id) > ((1, NULL), 2) ORDER BY id", "3", "4", "5")
		h.expectRows("SELECT id FROM "+tbl+" WHERE ((a, b), id) IN (((1, NULL), 2), ((NULL, 'y'), 3)) ORDER BY id", "2", "3")
		h.expectRows("SELECT id FROM "+tbl+" WHERE (a, b) IS NULL", "4")
		h.expectRows("SELECT count(*) FROM "+tbl+" WHERE ((a, b), 1) IS NOT NULL", "6")
		h.expectRows("SELECT id, r IS NULL, r IS NOT NULL FROM (SELECT id, (a, b) AS r FROM "+tbl+") s ORDER BY id",
			"1|false|true", "2|false|false", "3|false|false", "4|true|false", "5|false|true", "6|false|true")
		h.expectRows("SELECT x.id, y.id FROM (SELECT id, (a, b) AS r FROM "+tbl+") x JOIN (SELECT id, ROW(a, b) AS r FROM "+tbl+") y "+
			"ON x.r = y.r AND x.id < y.id ORDER BY 1, 2", "1|6")
		h.expectRows("SELECT to_json((id, b)) FROM "+tbl+" WHERE id IN (1, 4) ORDER BY id", `{"f1":1,"f2":"x"}`, `{"f1":4,"f2":null}`)
	}
}

// A record column is an Arrow struct (f1 … fn, nullable, with the fields'
// types), named "row" for a row constructor, also from ExecuteSchema and
// for a NULL record.
func TestSQLRecordArrow(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRecords()
	const want = "struct<f1: int32 nullable, f2: utf8 nullable>"
	schema := h.expectRows("SELECT (a, b), CASE WHEN id > 1 THEN ROW(id, b) END AS c FROM it_rec_t WHERE id IN (1, 2) ORDER BY id",
		`{"f1":1,"f2":"x"}|NULL`, `{"f1":1,"f2":null}|{"f1":2,"f2":null}`)
	if f := schema.Field(0); f.Name != "row" || f.Type.String() != want || !f.Nullable {
		t.Errorf("SELECT (a, b): %s", f)
	}
	if f := schema.Field(1); f.Type.String() != want {
		t.Errorf("CASE … ROW(id, b): %s", f)
	}
	s, err := h.executeSchema("SELECT (a, b) AS r, ((a, b), id) FROM it_rec_t")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.String(); got != "schema:\n  fields: 2\n    - r: type="+want+", nullable\n"+
		"    - row: type=struct<f1: "+want+" nullable, f2: int32 nullable>, nullable" {
		t.Errorf("ExecuteSchema: %s", got)
	}
}

// Records can't be stored, with Postgres's errors; nothing is created.
func TestSQLRecordStorage(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRecords()
	h.expectExecErrorText("CREATE TABLE it_rec_c AS SELECT id, (a, b) FROM it_rec_t", `column "row" has pseudo-type record`)
	h.expectExecErrorText("CREATE TABLE it_rec_c AS SELECT ROW(1) AS r", `column "r" has pseudo-type record`)
	h.expectExecErrorText("CREATE TABLE it_rec_c (r RECORD)", `column "r" has pseudo-type record`)
	h.expectExecErrorText("ALTER TABLE it_rec_t ADD COLUMN r RECORD", `column "r" has pseudo-type record`)
	h.expectExecErrorText("UPDATE it_rec_t SET b = (a, b)", `column "b" is of type varchar but expression is of type record`)
	h.expectErrorText("SELECT * FROM it_rec_c", `table "public"."it_rec_c" does not exist`)
	h.expectRows("SELECT count(*) FROM it_rec_t WHERE b IS NOT NULL", "4")
	// A record can't be a parameter or ingested: Arrow structs aren't
	// accepted.
	mem := memory.DefaultAllocator
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, "SELECT ($1, $2)::text, ROW($1)"); err != nil {
		t.Fatal(err)
	}
	ib := array.NewInt64Builder(mem)
	ib.Append(7)
	sb := array.NewStringBuilder(mem)
	sb.Append("a b")
	ia, sa := ib.NewArray(), sb.NewArray()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "1", Type: arrow.PrimitiveTypes.Int64}, {Name: "2", Type: arrow.BinaryTypes.String}}, nil),
		[]arrow.Array{ia, sa}, 1)
	if err := st.Bind(h.ctx, rec); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rdr.Next()
	out := rdr.RecordBatch()
	if got := out.Column(0).ValueStr(0) + "|" + out.Column(1).DataType().String(); got != `(7,"a b")|struct<f1: int64 nullable>` {
		t.Errorf("SELECT ($1, $2)::text, ROW($1) = %q", got)
	}
	rdr.Release()
	rec.Release()
	stb := array.NewStructBuilder(mem, arrow.StructOf(arrow.Field{Name: "f1", Type: arrow.PrimitiveTypes.Int64, Nullable: true}))
	stb.Append(true)
	stb.FieldBuilder(0).(*array.Int64Builder).Append(1)
	structs := stb.NewArray()
	srec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "1", Type: structs.DataType(), Nullable: true}}, nil), []arrow.Array{structs}, 1)
	defer srec.Release()
	if err := st.SetSqlQuery(h.ctx, "SELECT $1"); err != nil {
		t.Fatal(err)
	}
	if err := st.Bind(h.ctx, srec); err != nil {
		t.Fatal(err)
	}
	if rdr, _, err := st.ExecuteQuery(h.ctx); err == nil {
		rdr.Release()
		t.Error("a struct parameter was accepted")
	}
}

// The errors of records have Postgres's SQLSTATEs (psql's \set VERBOSITY
// verbose on Postgres 16), and the ADBC statuses they give.
func TestSQLRecordErrorStates(t *testing.T) {
	h := newSQLHarness(t)
	h.setupRecords()
	const (
		arg    = adbc.StatusInvalidArgument
		absent = adbc.StatusNotFound
		notImp = adbc.StatusNotImplemented
	)
	for _, c := range []struct {
		sql   string
		code  adbc.Status
		state string
		msg   string
	}{
		{"SELECT upper((a, b)) FROM it_rec_t", absent, "42883", "function upper(record) does not exist"},
		{"SELECT (1, 2) + 1", absent, "42883", "operator does not exist: record + bigint"},
		{"SELECT -(1, 2)", absent, "42883", "operator does not exist: - record"},
		{"SELECT (a, b) = 1 FROM it_rec_t", absent, "42883", "operator does not exist: record = bigint"},
		{"SELECT id FROM it_rec_t WHERE (a, b)", arg, "42804", "argument of WHERE must be type boolean, not type record"},
		{"SELECT COALESCE((1, 2), 1)", arg, "42804", "COALESCE types record and bigint cannot be matched"},
		{"SELECT ((1, 2), 3) = ((1, 2, 3), 3)", arg, "42804", "cannot compare record types with different numbers of columns"},
		{"SELECT CAST((a, b) AS INTEGER) FROM it_rec_t", arg, "42846", "cannot cast type record to integer"},
		{"SELECT '(1,2)'::record", notImp, "0A000", "input of anonymous composite types is not implemented"},
		{"SELECT (ROW(1, 2)).f3", absent, "42703", `could not identify column "f3" in record data type`},
		{"SELECT (1).f1", arg, "42809", "column notation .f1 applied to type bigint, which is not a composite type"},
		{"SELECT (1, 2) = (1, 2, 3)", arg, "42601", "unequal number of entries in row expressions"},
		{"SELECT ROW() = ROW()", notImp, "0A000", "cannot compare rows of zero length"},
		{"SELECT (SELECT a, b FROM it_rec_t LIMIT 1) = (1, 'x')", arg, "42601", "subquery must return only one column"},
		{"SELECT (a, b) IN (SELECT a FROM it_rec_t) FROM it_rec_t", arg, "42601", "subquery has too few columns"},
		{"CREATE TABLE it_rec_c AS SELECT (a, b) FROM it_rec_t", arg, "42P16", `column "row" has pseudo-type record`},
		{"CREATE TABLE it_rec_c (r RECORD)", arg, "42P16", `column "r" has pseudo-type record`},
		{"UPDATE it_rec_t SET a = (1, 2)", arg, "42804", `column "a" is of type integer but expression is of type record`},
	} {
		checkState(t, c.sql, h.stmtErr(c.sql, nil), c.code, c.state, c.msg)
	}
}
