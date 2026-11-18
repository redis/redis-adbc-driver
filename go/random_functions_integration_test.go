// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func randomOne(t *testing.T, h *sqlHarness, q string) float64 {
	t.Helper()
	st := h.stmtWith(q, nil)
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	if !reader.Next() || reader.RecordBatch().NumRows() != 1 {
		t.Fatal("missing random row", reader.Err())
	}
	col, ok := reader.RecordBatch().Column(0).(*array.Float64)
	if !ok {
		t.Fatal("random Arrow type", reader.Schema())
	}
	value := col.Value(0)
	if reader.Next() || reader.Err() != nil {
		t.Fatal("random trailing result", reader.Err())
	}
	return value
}
func seedSQL(t *testing.T, h *sqlHarness, seed string) {
	t.Helper()
	schema := h.expectRows("SELECT setseed("+seed+")", "")
	expectTypes(t, schema, arrow.BinaryTypes.String)
}

func TestSQLSeededRandomSession(t *testing.T) {
	h, other := newSQLHarness(t), newSQLHarness(t)
	seedSQL(t, h, ".5")
	seedSQL(t, other, ".5")
	if v := randomOne(t, h, "SELECT random()"); v != .9851677175347999 {
		t.Fatal(v)
	}
	if v := randomOne(t, other, "SELECT random()"); v != .9851677175347999 {
		t.Fatal("connection stream leaked", v)
	}
	seedSQL(t, h, "-.5")
	control := randomEnv()
	randomEval(t, control, "setseed(.5)")
	randomEval(t, control, "random()")
	if v := randomOne(t, other, "SELECT random()"); v != randomEval(t, control, "random()").F {
		t.Fatal("reseeding another connection changed stream")
	}
	seedSQL(t, h, ".5")
	h.expectRows("SELECT setseed(-.5) WHERE FALSE")
	h.expectRows("SELECT random_normal() WHERE FALSE")
	h.expectRows("SELECT setseed(NULL)", "NULL")
	h.expectRows("SELECT CASE WHEN FALSE THEN setseed(-.5) ELSE NULL END", "NULL")
	if v := randomOne(t, h, "SELECT random()"); v != .9851677175347999 {
		t.Fatal("empty/lazy/NULL call changed stream", v)
	}
	for _, seed := range []string{"2", "'NaN'", "'Infinity'", "'-Infinity'"} {
		seedSQL(t, h, ".5")
		st := h.stmtWith("SELECT setseed("+seed+")", nil)
		r, _, err := st.ExecuteQuery(h.ctx)
		if r != nil {
			r.Release()
		}
		if sqlState(err) != stateInvalidParameter {
			t.Fatalf("seed %s:%v", seed, err)
		}
		if v := randomOne(t, h, "SELECT random()"); v != .9851677175347999 {
			t.Fatal("failed seed changed state", v)
		}
	}
	// Session PRNG effects survive rollback and failed statements, like sequences;
	// the SQL mutation itself still publishes atomically with no replay.
	h.dropTables("it_random_failure")
	h.exec("CREATE TABLE it_random_failure (v DOUBLE PRECISION CHECK(v<0))")
	seedSQL(t, h, ".5")
	st := h.stmtWith("INSERT INTO it_random_failure VALUES (random())", nil)
	if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateCheck {
		t.Fatalf("failed write:%v", err)
	}
	h.expectRows("SELECT * FROM it_random_failure")
	control = randomEnv()
	randomEval(t, control, "setseed(.5)")
	randomEval(t, control, "random()")
	if v := randomOne(t, h, "SELECT random()"); v != randomEval(t, control, "random()").F {
		t.Fatal("failed write rewound or replayed PRNG", v)
	}
	h.exec("BEGIN")
	seedSQL(t, h, ".5")
	h.exec("ROLLBACK")
	if v := randomOne(t, h, "SELECT random()"); v != .9851677175347999 {
		t.Fatal("rollback rewound seed", v)
	}
	for _, q := range []string{"SELECT setseed(NULL::text) IS NULL WHERE FALSE", "SELECT random_normal('bad') WHERE FALSE"} {
		_, err := h.executeSchema(q)
		want := stateUndefinedFunction
		if q == "SELECT random_normal('bad') WHERE FALSE" {
			want = stateInvalidText
		}
		if sqlState(err) != want {
			t.Fatalf("%s:%v", q, err)
		}
	}
	for _, q := range []string{"CREATE TABLE it_random_void AS SELECT setseed(.5) AS v", "CREATE VIEW it_random_void AS SELECT setseed(.5) AS v"} {
		st := h.stmtWith(q, nil)
		if _, err := st.ExecuteUpdate(h.ctx); sqlState(err) != stateTableDefinition {
			t.Fatalf("void persistence:%v", err)
		}
	}
}

func TestSQLRandomNormalBinding(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT random_normal($1,$2)", nil)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		mean, stddev     float64
		null, empty, bad bool
	}{
		{name: "normal", mean: 2, stddev: 3}, {name: "negative", mean: 2, stddev: -3},
		{name: "null", null: true}, {name: "wrong-type", bad: true}, {name: "empty", empty: true},
		{name: "reuse", mean: 2, stddev: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ := arrow.DataType(arrow.PrimitiveTypes.Float64)
			if tc.bad {
				typ = arrow.BinaryTypes.String
			}
			b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: typ, Nullable: true}, {Name: "", Type: arrow.PrimitiveTypes.Float64, Nullable: true}}, nil))
			defer b.Release()
			if !tc.empty {
				if tc.null {
					b.Field(0).AppendNull()
				} else if tc.bad {
					b.Field(0).(*array.StringBuilder).Append("2")
				} else {
					b.Field(0).(*array.Float64Builder).Append(tc.mean)
				}
				b.Field(1).(*array.Float64Builder).Append(tc.stddev)
			}
			record := b.NewRecordBatch()
			defer record.Release()
			seedSQL(t, h, ".5")
			if err := st.Bind(h.ctx, record); err != nil {
				t.Fatal(err)
			}
			reader, _, err := st.ExecuteQuery(h.ctx)
			if tc.bad {
				if reader != nil {
					reader.Release()
				}
				if sqlState(err) != stateUndefinedFunction {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Release()
			expectTypes(t, reader.Schema(), arrow.PrimitiveTypes.Float64)
			n := int64(0)
			for reader.Next() {
				r := reader.RecordBatch()
				n += r.NumRows()
				for i := 0; i < int(r.NumRows()); i++ {
					v := r.Column(0).(*array.Float64)
					if tc.null {
						if !v.IsNull(i) {
							t.Fatal("expected NULL")
						}
					} else {
						control := randomEnv()
						randomEval(t, control, "setseed(.5)")
						want, err := control.exec.sess.random.draw(true, tc.mean, tc.stddev)
						if err != nil || math.Abs(v.Value(i)-want) > 1e-14 {
							t.Fatalf("bound normal %.17g want %.17g %v", v.Value(i), want, err)
						}
					}
				}
			}
			if reader.Err() != nil {
				t.Fatal(reader.Err())
			}
			if tc.empty && n != 0 || !tc.empty && n != 1 {
				t.Fatal("rows", n)
			}
			// Empty/NULL arrays never evaluate the volatile call.
			if tc.empty || tc.null {
				if v := randomOne(t, h, "SELECT random()"); v != .9851677175347999 {
					t.Fatal("empty/NULL binding consumed PRNG", v)
				}
			}
		})
	}
	types, err := h.paramTypes("SELECT setseed($1),random_normal($2,$3)")
	if err != nil || len(types) != 3 {
		t.Fatal(types, err)
	}
	for _, typ := range types {
		if typ != arrow.PrimitiveTypes.Float64.String() {
			t.Fatal(types)
		}
	}
}

func TestSQLSetseedBinding(t *testing.T) {
	h := newSQLHarness(t)
	st := h.stmtWith("SELECT setseed($1)", nil)
	if err := st.Prepare(h.ctx); err != nil {
		t.Fatal(err)
	}
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: "", Type: arrow.PrimitiveTypes.Float64, Nullable: true}}, nil))
	defer b.Release()
	values := b.Field(0).(*array.Float64Builder)
	values.Append(.5)
	values.AppendNull()
	values.Append(-.5)
	record := b.NewRecordBatch()
	defer record.Release()
	if err := st.Bind(h.ctx, record); err != nil {
		t.Fatal(err)
	}
	reader, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	expectTypes(t, reader.Schema(), arrow.BinaryTypes.String)
	rows := 0
	for reader.Next() {
		col := reader.RecordBatch().Column(0).(*array.String)
		for i := 0; i < col.Len(); i++ {
			if rows == 1 {
				if !col.IsNull(i) {
					t.Fatal("NULL seed must return NULL void")
				}
			} else if col.IsNull(i) || col.Value(i) != "" {
				t.Fatal("seed must return non-NULL empty void")
			}
			rows++
		}
	}
	if rows != 3 || reader.Err() != nil {
		t.Fatal("seed binding rows", rows, reader.Err())
	}
	original := record.Column(0).(*array.Float64)
	if original.Value(0) != .5 || !original.IsNull(1) || original.Value(2) != -.5 {
		t.Fatal("seed binding changed caller input")
	}
	if v := randomOne(t, h, "SELECT random()"); v != .6005582198295805 {
		t.Fatal("last bound seed not used", v)
	}
	// A typed refusal on the same prepared statement does not replace its state.
	values.Append(2)
	bad := b.NewRecordBatch()
	defer bad.Release()
	if err := st.Bind(h.ctx, bad); err != nil {
		t.Fatal(err)
	}
	refused, _, err := st.ExecuteQuery(h.ctx)
	if refused != nil {
		refused.Release()
	}
	if sqlState(err) != stateInvalidParameter {
		t.Fatal("bound seed refusal", err)
	}
	control := randomEnv()
	randomEval(t, control, "setseed(-.5)")
	randomEval(t, control, "random()")
	if got, want := randomOne(t, h, "SELECT random()"), randomEval(t, control, "random()").F; got != want {
		t.Fatal("bound seed refusal changed state", got, want)
	}
}

func TestSQLRandomVolatileCase(t *testing.T) {
	h := newSQLHarness(t)
	for _, q := range []string{
		"SELECT CASE WHEN TRUE THEN random() ELSE 0 END",
		"SELECT CASE WHEN FALSE THEN 0 ELSE random() END",
	} {
		seedSQL(t, h, ".5")
		if _, err := h.executeSchema(q); err != nil {
			t.Fatal(err)
		}
		if got := randomOne(t, h, q); got != .9851677175347999 {
			t.Fatalf("CASE planning consumed draw:%.17g", got)
		}
		control := randomEnv()
		randomEval(t, control, "setseed(.5)")
		randomEval(t, control, "random()")
		if got, want := randomOne(t, h, "SELECT random()"), randomEval(t, control, "random()").F; got != want {
			t.Fatal("CASE continuation", got, want)
		}
	}
	seedSQL(t, h, ".5")
	if _, err := h.executeSchema("SELECT CASE WHEN TRUE THEN setseed(-.5) ELSE NULL END"); err != nil {
		t.Fatal(err)
	}
	if got := randomOne(t, h, "SELECT random()"); got != .9851677175347999 {
		t.Fatal("schema inspection seeded stream", got)
	}
	h.expectRows("SELECT CASE WHEN TRUE THEN setseed(-.5) ELSE NULL END", "")
	if got := randomOne(t, h, "SELECT random()"); got != .6005582198295805 {
		t.Fatal("selected seed not executed at runtime", got)
	}
}
