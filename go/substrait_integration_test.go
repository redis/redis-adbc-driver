// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	sp "github.com/adbc-drivers/redis/go/internal/substrait/gen/substrait"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func subCollect(t testing.TB, r array.RecordReader) ([][]Value, *arrow.Schema) {
	t.Helper()
	defer r.Release()
	var rows [][]Value
	for r.Next() {
		rec := r.RecordBatch()
		for i := 0; i < int(rec.NumRows()); i++ {
			row := make([]Value, rec.NumCols())
			for j := range row {
				v, err := valueAt(rec.Column(j), i)
				if err != nil {
					t.Fatal(err)
				}
				row[j] = v
			}
			rows = append(rows, row)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return rows, r.Schema()
}
func TestSubstraitIntegrationExact(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`DROP TABLE IF EXISTS it_sub_exact`)
	t.Cleanup(func() { h.exec(`DROP TABLE IF EXISTS it_sub_exact`) })
	h.exec(`CREATE TABLE it_sub_exact(id BIGINT NOT NULL, value TEXT, d DECIMAL(38,9), ts TIMESTAMP(9), tz TIMESTAMPTZ(9), bytes BYTEA, flag BOOLEAN)`)
	h.exec(`INSERT INTO it_sub_exact VALUES (9007199254740991,'quote'' dot.x',12345678901234567890123456789.123456789,'1969-12-31 23:59:59.999999999','1969-12-31 23:59:59.999999999+00',X'00ff01',TRUE),(9007199254740992,NULL,-12345678901234567890123456789.123456789,NULL,NULL,NULL,NULL),(9007199254740993,'last',NULL,'1970-01-01 00:00:00.000000001','1970-01-01 00:00:00.000000001+00',X'ff00',FALSE)`)
	p := subTestPlan(t, "it_sub_exact", []string{"id", "value", "d", "ts", "tz", "bytes", "flag"}, subI64, subStr, `{"decimal":{"precision":38,"scale":9,"nullability":"NULLABILITY_NULLABLE"}}`, `{"precisionTimestamp":{"precision":9,"nullability":"NULLABILITY_NULLABLE"}}`, `{"precisionTimestampTz":{"precision":9,"nullability":"NULLABILITY_NULLABLE"}}`, `{"binary":{"nullability":"NULLABILITY_NULLABLE"}}`, `{"bool":{"nullability":"NULLABILITY_NULLABLE"}}`)
	for _, materialized := range []bool{false, true} {
		for _, filter := range []bool{false, true} {
			t.Run(fmt.Sprintf("materialized=%t/filter=%t", materialized, filter), func(t *testing.T) {
				q := subTestPlan(t, "it_sub_exact", []string{"id", "value", "d", "ts", "tz", "bytes", "flag"}, subI64, subStr, `{"decimal":{"precision":38,"scale":9,"nullability":"NULLABILITY_NULLABLE"}}`, `{"precisionTimestamp":{"precision":9,"nullability":"NULLABILITY_NULLABLE"}}`, `{"precisionTimestampTz":{"precision":9,"nullability":"NULLABILITY_NULLABLE"}}`, `{"binary":{"nullability":"NULLABILITY_NULLABLE"}}`, `{"bool":{"nullability":"NULLABILITY_NULLABLE"}}`)
				sql := `SELECT id,value,d,ts,tz,bytes,flag FROM it_sub_exact`
				if filter {
					x := subFunctionPlan(q, "gte:any_any", subComparison, []*sp.Expression{subRef(0), subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_I64{I64: 9007199254740992}})}, subBoolType(false))
					root := q.Relations[0].GetRoot()
					root.Input = &sp.Rel{RelType: &sp.Rel_Filter{Filter: &sp.FilterRel{Input: root.Input, Condition: x}}}
					sql += ` WHERE id>=9007199254740992`
				}
				st, err := h.conn.NewStatement(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close(h.ctx)
				if materialized {
					if err := st.SetOption(h.ctx, OptionStringStreamResults, "false"); err != nil {
						t.Fatal(err)
					}
				}
				bytes := subBytes(t, q)
				if err := st.SetSubstraitPlan(h.ctx, bytes); err != nil {
					t.Fatal(err)
				}
				clear(bytes)
				if err := st.Prepare(h.ctx); err != nil {
					t.Fatal(err)
				}
				schema, err := st.(*statementImpl).ExecuteSchema(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if schema.Field(0).Nullable {
					t.Fatal("lost required id")
				}
				r, _, err := st.ExecuteQuery(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				rows, gotSchema := subCollect(t, r)
				if !gotSchema.Equal(schema) {
					t.Fatal(gotSchema, schema)
				}
				wantIDs := []int64{9007199254740991, 9007199254740992, 9007199254740993}
				if filter {
					wantIDs = wantIDs[1:]
				}
				if len(rows) != len(wantIDs) {
					t.Fatal(len(rows))
				}
				slices.SortFunc(rows, func(a, b []Value) int {
					if a[0].I < b[0].I {
						return -1
					}
					if a[0].I > b[0].I {
						return 1
					}
					return 0
				})
				for i, row := range rows {
					id := wantIDs[i]
					if row[0].I != id {
						t.Fatal(row[0], id)
					}
					switch id {
					case 9007199254740991:
						if row[1].S != "quote' dot.x" || formatDecimal(row[2].D, 9) != "12345678901234567890123456789.123456789" || row[3].I != -1 || row[4].I != -1 || row[5].S != "\x00\xff\x01" || row[6].I != 1 {
							t.Fatal(row)
						}
					case 9007199254740992:
						for j := 1; j < len(row); j++ {
							if j == 2 {
								if formatDecimal(row[j].D, 9) != "-12345678901234567890123456789.123456789" {
									t.Fatal(row[j])
								}
							} else if !row[j].Null {
								t.Fatal(row[j])
							}
						}
					case 9007199254740993:
						if row[1].S != "last" || !row[2].Null || row[3].I != 1 || row[4].I != 1 || row[5].S != "\xff\x00" || row[6].I != 0 {
							t.Fatal(row)
						}
					}
				}
				// Compare complete results against SQL too; independent values above prevent shared bugs.
				sqlSt, _ := h.conn.NewStatement(h.ctx)
				defer sqlSt.Close(h.ctx)
				if err := sqlSt.SetSqlQuery(h.ctx, sql); err != nil {
					t.Fatal(err)
				}
				sqlReader, _, err := sqlSt.ExecuteQuery(h.ctx)
				if err != nil {
					t.Fatal(err)
				}
				sqlRows, sqlSchema := subCollect(t, sqlReader)
				if len(sqlRows) != len(rows) {
					t.Fatal(len(sqlRows), len(rows))
				}
				slices.SortFunc(sqlRows, func(a, b []Value) int {
					if a[0].I < b[0].I {
						return -1
					}
					if a[0].I > b[0].I {
						return 1
					}
					return 0
				})
				for i, row := range rows {
					for j, v := range row {
						want := sqlRows[i][j]
						if !arrow.TypeEqual(schema.Field(j).Type, sqlSchema.Field(j).Type) || v.Null != want.Null {
							t.Fatal(i, j, v, want)
						}
						if !v.Null {
							cmp, ok := compareValues(v, want)
							if !ok || cmp != 0 {
								t.Fatal(i, j, v, want)
							}
						}
					}
				}
			})
		}
	}
	st, _ := h.conn.NewStatement(h.ctx)
	defer st.Close(h.ctx)
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
		t.Fatal(err)
	}
	h.exec(`ALTER TABLE it_sub_exact ADD COLUMN extra INT`)
	if _, err := st.(*statementImpl).ExecuteSchema(h.ctx); !isStatus(err, adbc.StatusInvalidArgument) {
		t.Fatal(err)
	}
}
func TestSubstraitIntegrationStateAndOwnership(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`DROP TABLE IF EXISTS it_substrait`)
	t.Cleanup(func() { h.exec(`DROP TABLE IF EXISTS it_substrait`) })
	h.exec(`CREATE TABLE it_substrait(id BIGINT NOT NULL,value TEXT)`)
	h.exec(`INSERT INTO it_substrait SELECT g::bigint, CASE WHEN g%3=0 THEN NULL ELSE 'v'||g END FROM generate_series(1,12001) s(g)`)
	p := subTestPlan(t, "it_substrait", []string{"id", "value"}, subI64, subStr)
	subProject(p, []string{"dup", "dup", "n"}, []*sp.Expression{subLit(&sp.Expression_Literal{LiteralType: &sp.Expression_Literal_Null{Null: subBoolType(true)}})}, 1, 0, 2)
	st, _ := h.conn.NewStatement(h.ctx)
	defer st.Close(h.ctx)
	_ = st.SetOption(h.ctx, OptionIntStreamBatchRows, "127")
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSubstraitPlan(h.ctx, []byte{255}); err == nil {
		t.Fatal("accepted invalid setter")
	}
	params, err := st.GetParameterSchema(h.ctx)
	if err != nil || params.NumFields() != 0 {
		t.Fatal(params, err)
	}
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	st.(*statementImpl).conn.Alloc = mem
	defer mem.AssertSize(t, 0)
	rb := array.NewRecordBuilder(mem, arrow.NewSchema(nil, nil))
	batch := rb.NewRecordBatch()
	rb.Release()
	if err := st.Bind(h.ctx, batch); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
	batch.Release()
	bound := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.PrimitiveTypes.Int64}}, nil))
	bound.Field(0).(*array.Int64Builder).Append(1)
	rec := bound.NewRecordBatch()
	bound.Release()
	source, err := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
	rec.Release()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BindStream(h.ctx, source); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
	mem.AssertSize(t, 0)
	r, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	seen := map[int64]bool{}
	for r.Next() {
		rec := r.RecordBatch()
		for i := 0; i < int(rec.NumRows()); i++ {
			id := rec.Column(1).(*array.Int64).Value(i)
			if id < 1 || id > 12001 || seen[id] {
				t.Fatal(id)
			}
			seen[id] = true
			v := rec.Column(0)
			if v.IsNull(i) != (id%3 == 0) || !rec.Column(2).IsNull(i) {
				t.Fatal(id)
			}
			if !v.IsNull(i) && v.(*array.String).Value(i) != fmt.Sprintf("v%d", id) {
				t.Fatal(id)
			}
			count++
		}
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	r.Release()
	if count != 12001 {
		t.Fatal(count)
	}
	for _, reason := range []string{"release", "cancel", "close", "deadline"} {
		t.Run(reason, func(t *testing.T) {
			s, _ := h.conn.NewStatement(h.ctx)
			defer s.Close(h.ctx)
			_ = s.SetOption(h.ctx, OptionIntStreamBatchRows, "1")
			if err := s.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			r, _, err := s.ExecuteQuery(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Next() {
				t.Fatal(r.Err())
			}
			rec := r.RecordBatch()
			rec.Retain()
			defer rec.Release()
			switch reason {
			case "release":
				r.Release()
				return
			case "cancel":
				if err := s.(interface{ Cancel(context.Context) error }).Cancel(ctx); err != nil {
					t.Fatal(err)
				}
			case "close":
				if err := s.Close(ctx); err != nil {
					t.Fatal(err)
				}
			case "deadline":
				cancel()
			}
			if r.Next() || r.Err() == nil {
				t.Fatal("reader survived", reason)
			}
			r.Release()
			if rec.NumRows() != 1 {
				t.Fatal("released caller buffer")
			}
		})
	}
	if err := st.SetSqlQuery(h.ctx, `SELECT 7`); err != nil {
		t.Fatal(err)
	}
	r, _, err = st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := subCollect(t, r)
	if rows[0][0].I != 7 {
		t.Fatal(rows)
	}
}
func TestSubstraitIntegrationScanBudget(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`DROP TABLE IF EXISTS it_sub_budget`)
	t.Cleanup(func() { h.exec(`DROP TABLE IF EXISTS it_sub_budget`) })
	h.exec(`CREATE TABLE it_sub_budget(id BIGINT NOT NULL)`)
	h.exec(`INSERT INTO it_sub_budget SELECT g::bigint FROM generate_series(1,20) s(g)`)
	p, err := decodeSubstrait(h.ctx, subBytes(t, subTestPlan(t, "it_sub_budget", []string{"id"}, subI64)))
	if err != nil {
		t.Fatal(err)
	}
	statement, _ := h.conn.NewStatement(h.ctx)
	defer statement.Close(h.ctx)
	conn := statement.(*statementImpl).conn
	e := conn.executor()
	ctx := context.WithValue(h.ctx, subBudgetKey{}, &subWork{rows: subMaxRows - 19})
	plan, err := p.bind(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	plan.sel.Where = &Binary{Op: "<", L: &ColumnRef{Name: "id"}, R: &ColumnRef{Name: "id"}}
	src, err := newSubSource(ctx, e, plan)
	if err != nil {
		t.Fatal(err)
	}
	defer src.close(ctx)
	for {
		_, err = src.next(ctx)
		if err != nil {
			break
		}
	}
	if err == nil || !strings.Contains(err.Error(), "scan_rows") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(h.ctx, time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := p.bind(ctx, e); !isStatus(err, adbc.StatusTimeout) {
		t.Fatal(err)
	}
}

func TestSubstraitIntegrationBinding(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`DROP SCHEMA IF EXISTS "sub.space" CASCADE`)
	h.exec(`CREATE SCHEMA "sub.space"`)
	defer h.exec(`DROP SCHEMA IF EXISTS "sub.space" CASCADE`)
	h.exec(`CREATE TABLE "sub.space"."t' dot"("a.b" BIGINT NOT NULL)`)
	h.exec(`INSERT INTO "sub.space"."t' dot" VALUES (42)`)
	p := subTestPlan(t, "t' dot", []string{"a.b"}, subI64)
	p.Relations[0].GetRoot().Input.GetRead().GetNamedTable().Names = []string{"redis", "sub.space", "t' dot"}
	st, _ := h.conn.NewStatement(h.ctx)
	defer st.Close(h.ctx)
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := subCollect(t, r)
	if len(rows) != 1 || rows[0][0].I != 42 {
		t.Fatal(rows)
	}
	h.exec(`CREATE TEMP TABLE it_sub_temp(id BIGINT NOT NULL)`)
	h.exec(`INSERT INTO pg_temp.it_sub_temp VALUES (17)`)
	temp := subTestPlan(t, "it_sub_temp", []string{"id"}, subI64)
	temp.Relations[0].GetRoot().Input.GetRead().GetNamedTable().Names = []string{"pg_temp", "it_sub_temp"}
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, temp)); err != nil {
		t.Fatal(err)
	}
	r, _, err = st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ = subCollect(t, r)
	if len(rows) != 1 || rows[0][0].I != 17 {
		t.Fatal(rows)
	}
	temp.Relations[0].GetRoot().Input.GetRead().GetNamedTable().Names[0] = st.(*statementImpl).conn.store.tempSchema()
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, temp)); err == nil {
		t.Fatal("physical temporary schema accepted")
	}
	missing := subTestPlan(t, "sub_missing_table", []string{"id"}, subI64)
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, missing)); !isStatus(err, adbc.StatusNotFound) {
		t.Fatal(err)
	}
	h.exec(`CREATE VIEW "sub.space".v AS SELECT "a.b" FROM "sub.space"."t' dot"`)
	p.Relations[0].GetRoot().Input.GetRead().GetNamedTable().Names[2] = "v"
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, p)); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
	// No prior physical binding survives DROP/recreate, even when schema is identical.
	p.Relations[0].GetRoot().Input.GetRead().GetNamedTable().Names[2] = "t' dot"
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
		t.Fatal(err)
	}
	h.exec(`DROP VIEW "sub.space".v`)
	h.exec(`DROP TABLE "sub.space"."t' dot"`)
	h.exec(`CREATE TABLE "sub.space"."t' dot"("a.b" BIGINT NOT NULL)`)
	h.exec(`INSERT INTO "sub.space"."t' dot" VALUES (99)`)
	r, _, err = st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ = subCollect(t, r)
	if len(rows) != 1 || rows[0][0].I != 99 {
		t.Fatal(rows)
	}
}
func TestSubstraitIntegrationAdopted(t *testing.T) {
	f := newHashFixture(t, "it:substrait:adopted:")
	f.dropAdopted("it_sub_adopted")
	defer f.dropAdopted("it_sub_adopted")
	f.hset(f.prefix+"1", "n", "9007199254740993", "s", "MiX")
	f.hset(f.prefix+"2", "n", "-7", "s", "nul\x00byte")
	result := f.adopt(HashAdoptOptions{Table: "it_sub_adopted", Types: map[string]string{"n": "BIGINT", "s": "TEXT"}, OwnIndex: true, Apply: true})
	if !result.Applied {
		t.Fatal(result)
	}
	statement, _ := f.h.conn.NewStatement(f.h.ctx)
	defer statement.Close(f.h.ctx)
	// Build the discovery vector from the table's stored public schema; the
	// values themselves are asserted independently against application HASHes.
	impl := statement.(*statementImpl)
	meta, err := impl.conn.store.getTable(f.h.ctx, "public", "it_sub_adopted")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(meta.Columns))
	types := make([]string, len(meta.Columns))
	for i, c := range meta.Columns {
		names[i] = c.Name
		n := "NULLABILITY_REQUIRED"
		if c.Nullable {
			n = "NULLABILITY_NULLABLE"
		}
		kind := "i64"
		if c.Name == "s" {
			kind = "string"
		}
		types[i] = fmt.Sprintf(`{"%s":{"nullability":"%s"}}`, kind, n)
	}
	p := subTestPlan(t, "it_sub_adopted", names, types...)
	if err := statement.SetSubstraitPlan(f.h.ctx, subBytes(t, p)); err != nil {
		t.Fatal(err)
	}
	r, _, err := statement.ExecuteQuery(f.h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := subCollect(t, r)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	n, s := -1, -1
	for i, name := range names {
		if name == "n" {
			n = i
		}
		if name == "s" {
			s = i
		}
	}
	if n < 0 || s < 0 {
		t.Fatal(names)
	}
	got := map[int64]string{}
	for _, row := range rows {
		got[row[n].I] = row[s].S
	}
	if got[9007199254740993] != "MiX" || got[-7] != "nul\x00byte" {
		t.Fatal(got)
	}
}

func TestSubstraitIntegrationFloatAndEmpty(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`DROP TABLE IF EXISTS it_sub_float`)
	defer h.exec(`DROP TABLE IF EXISTS it_sub_float`)
	h.exec(`CREATE TABLE it_sub_float(id BIGINT NOT NULL, f REAL, d DOUBLE PRECISION)`)
	p := subTestPlan(t, "it_sub_float", []string{"id", "f", "d"}, subI64, `{"fp32":{"nullability":"NULLABILITY_NULLABLE"}}`, `{"fp64":{"nullability":"NULLABILITY_NULLABLE"}}`)
	statement, _ := h.conn.NewStatement(h.ctx)
	defer statement.Close(h.ctx)
	if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
		t.Fatal(err)
	}
	r, _, err := statement.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, schema := subCollect(t, r)
	if len(rows) != 0 || schema.Field(1).Type.ID() != arrow.FLOAT32 || schema.Field(2).Type.ID() != arrow.FLOAT64 {
		t.Fatal(rows, schema)
	}
	h.exec(`INSERT INTO it_sub_float VALUES (1,'NaN'::real,'NaN'::float8),(2,'Infinity'::real,'-Infinity'::float8),(3,'-0'::real,'-0'::float8),(4,NULL,NULL)`)
	r, _, err = statement.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ = subCollect(t, r)
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		switch row[0].I {
		case 1:
			if !math.IsNaN(row[1].F) || !math.IsNaN(row[2].F) {
				t.Fatal(row)
			}
		case 2:
			if !math.IsInf(row[1].F, 1) || !math.IsInf(row[2].F, -1) {
				t.Fatal(row)
			}
		case 3:
			if !math.Signbit(row[1].F) || !math.Signbit(row[2].F) {
				t.Fatal(row)
			}
		case 4:
			if !row[1].Null || !row[2].Null {
				t.Fatal(row)
			}
		}
	}
	comparison := subFunctionPlan(p, "equal:any_any", subComparison, []*sp.Expression{subRef(1), subRef(1)}, subBoolType(true))
	root := p.Relations[0].GetRoot()
	root.Input = &sp.Rel{RelType: &sp.Rel_Filter{Filter: &sp.FilterRel{Input: root.Input, Condition: comparison}}}
	if err := statement.SetSubstraitPlan(h.ctx, subBytes(t, p)); !isStatus(err, adbc.StatusNotImplemented) {
		t.Fatal(err)
	}
}

func TestSubstraitIntegrationTimestampPrecisions(t *testing.T) {
	h := newSQLHarness(t)
	h.exec(`DROP TABLE IF EXISTS it_sub_times`)
	defer h.exec(`DROP TABLE IF EXISTS it_sub_times`)
	h.exec(`CREATE TABLE it_sub_times(s TIMESTAMP(0),ms TIMESTAMP(3),us TIMESTAMP(6),ns TIMESTAMP(9),s_tz TIMESTAMPTZ(0),ms_tz TIMESTAMPTZ(3),us_tz TIMESTAMPTZ(6),ns_tz TIMESTAMPTZ(9))`)
	h.exec(`INSERT INTO it_sub_times VALUES ('1969-12-31 23:59:59','1969-12-31 23:59:59.999','1969-12-31 23:59:59.999999','1969-12-31 23:59:59.999999999','1969-12-31 23:59:59+00','1969-12-31 23:59:59.999+00','1969-12-31 23:59:59.999999+00','1969-12-31 23:59:59.999999999+00'),(NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)`)
	types := []string{}
	for _, kind := range []string{"precisionTimestamp", "precisionTimestampTz"} {
		for _, p := range []int{0, 3, 6, 9} {
			types = append(types, fmt.Sprintf(`{"%s":{"precision":%d,"nullability":"NULLABILITY_NULLABLE"}}`, kind, p))
		}
	}
	p := subTestPlan(t, "it_sub_times", []string{"s", "ms", "us", "ns", "s_tz", "ms_tz", "us_tz", "ns_tz"}, types...)
	st, _ := h.conn.NewStatement(h.ctx)
	defer st.Close(h.ctx)
	if err := st.SetSubstraitPlan(h.ctx, subBytes(t, p)); err != nil {
		t.Fatal(err)
	}
	r, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, schema := subCollect(t, r)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		for j, v := range row {
			typ := schema.Field(j).Type.(*arrow.TimestampType)
			if typ.Unit != arrow.TimeUnit(j%4) || (typ.TimeZone == "UTC") != (j >= 4) {
				t.Fatal(typ)
			}
			if !v.Null && v.I != -1 {
				t.Fatal(v)
			}
		}
	}
}
