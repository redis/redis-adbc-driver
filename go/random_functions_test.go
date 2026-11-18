// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"math"
	"sync"
	"testing"
)

func randomEnv() *evalEnv { return &evalEnv{exec: &executor{sess: newSession(defaultSchema)}} }
func randomEval(t *testing.T, e *evalEnv, sql string) Value {
	t.Helper()
	x := parseTestExpr(t, sql)
	if _, err := inferType(x, nil, nil); err != nil {
		t.Fatal(err)
	}
	v, err := e.eval(x)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Independent pinned PostgreSQL18.6 vectors interleave uniform and normal
// draws. Go math and the server's C library may differ by a few normal ULP.
func TestSQLRandomOracle(t *testing.T) {
	for _, tc := range []struct {
		seed float64
		want [5]float64
	}{
		{-1, [5]float64{.725656831544149, -.35902660892224186, .6445944697870825, 4.714368904082312, .8728075690357591}},
		{-.5, [5]float64{.6005582198295805, -1.6897897290450024, .1004490862746068, 9.4049030548748, .29592813263223916}},
		{0, [5]float64{.8702553105818676, .9192851066335249, .530027136563384, 1.1640221346358284, .2836209520202677}},
		{.1, [5]float64{.7345507212519062, 1.3506561847404195, .931589426258701, 3.726432955109713, .8578615329841426}},
		{.5, [5]float64{.9851677175347999, -1.3596653474301394, .16356291958601088, 4.920635352202876, .1404566845227775}},
		{1, [5]float64{.3978842227698167, -1.0718449159612309, .4108009373061563, 2.6442364797835043, .061724163496955864}},
	} {
		e := randomEnv()
		if err := e.exec.sess.random.setSeed(tc.seed); err != nil {
			t.Fatal(err)
		}
		for i, q := range []string{"random()", "random_normal()", "random()", "random_normal(2,3)", "random()"} {
			v := randomEval(t, e, q)
			want := tc.want[i]
			if i%2 == 0 {
				if v.F != want {
					t.Errorf("seed%g %s:%.17g want%.17g", tc.seed, q, v.F, want)
				}
			} else if math.Abs(v.F-want) > 2e-14*math.Max(1, math.Abs(want)) {
				t.Errorf("seed%g %s:%.17g want%.17g", tc.seed, q, v.F, want)
			}
		}
	}
}

func TestSQLRandomSessionAndLazy(t *testing.T) {
	a, b := randomEnv(), randomEnv()
	randomEval(t, a, "setseed(.5)")
	randomEval(t, b, "setseed(.5)")
	first := randomEval(t, a, "random()").F
	if got := randomEval(t, b, "random()").F; got != first {
		t.Fatal("sessions did not seed independently")
	}
	randomEval(t, a, "setseed(-.5)")
	// Compare b's untouched continuation with an independently reseeded control.
	control := randomEnv()
	randomEval(t, control, "setseed(.5)")
	randomEval(t, control, "random()")
	if got, want := randomEval(t, b, "random()").F, randomEval(t, control, "random()").F; got != want {
		t.Fatal("another session changed stream")
	}
	randomEval(t, a, "setseed(.5)")
	if v := randomEval(t, a, "setseed(NULL)"); !v.Null || v.T != typeVoid {
		t.Fatal("NULL seed", v)
	}
	randomEval(t, a, "CASE WHEN FALSE THEN setseed(-.5) ELSE NULL END")
	if got := randomEval(t, a, "random()").F; got != first {
		t.Fatal("NULL or lazy seed changed stream")
	}
	randomEval(t, a, "setseed(.5)")
	if v := randomEval(t, a, "random_normal(NULL,1)"); !v.Null || v.T != typeFloat64 {
		t.Fatal("NULL normal", v)
	}
	if got := randomEval(t, a, "random()").F; got != first {
		t.Fatal("NULL normal consumed stream")
	}
	for _, q := range []string{"setseed(2)", "setseed('NaN')", "setseed('Infinity')", "setseed('-Infinity')"} {
		randomEval(t, a, "setseed(.5)")
		_, err := a.eval(parseTestExpr(t, q))
		if sqlState(err) != stateInvalidParameter {
			t.Errorf("%s:%v", q, err)
		}
		if got := randomEval(t, a, "random()").F; got != first {
			t.Fatal("failed seed changed stream")
		}
	}
}

func TestSQLRandomTypesAndBounds(t *testing.T) {
	runScalarCases(t, []scalarCase{{"pg_typeof(setseed(.5))", "void", "VARCHAR"}, {"setseed(.5)", "", "VOID"}, {"setseed(NULL) IS NULL", "true", "BOOLEAN"}, {"setseed(.5) IS NULL", "false", "BOOLEAN"}, {"random_normal(4,0)", "4", "DOUBLE PRECISION"}, {"random_normal(stddev=>0,mean=>4)", "4", "DOUBLE PRECISION"}, {"pg_typeof(coalesce(setseed(NULL),'x'))", "void", "VARCHAR"}})
	for _, tc := range []struct{ q, state string }{
		{"setseed(NULL::text)", stateUndefinedFunction}, {"random_normal(NULL::text)", stateUndefinedFunction},
		{"setseed(true)", stateUndefinedFunction}, {"random_normal(1,false)", stateUndefinedFunction},
		{"setseed()", stateUndefinedFunction}, {"random_normal(1,2,3)", stateUndefinedFunction},
		{"setseed('bad')", stateInvalidText}, {"random_normal('bad')", stateInvalidText},
	} {
		for _, wrap := range []string{tc.q, tc.q + " IS NULL"} {
			stmts, err := ParseScript("SELECT " + wrap)
			if err != nil {
				if sqlState(err) != tc.state {
					t.Errorf("%s:%v want%s", wrap, err, tc.state)
				}
				continue
			}
			x := stmts[0].Stmt.(*SelectStmt).Items[0].Expr
			err = checkUnknowns(x, func() map[string]ColType { return nil }, nil)
			if err == nil {
				_, err = inferType(x, nil, nil)
			}
			if sqlState(err) != tc.state {
				t.Errorf("%s:%v want%s", wrap, err, tc.state)
			}
		}
	}
	e := randomEnv()
	randomEval(t, e, "setseed(.5)")
	for _, q := range []string{"random_normal('NaN',1)", "random_normal(0,'NaN')"} {
		if v := randomEval(t, e, q); !math.IsNaN(v.F) {
			t.Fatal(q, v)
		}
	}
	if v := randomEval(t, e, "random_normal('Infinity',0)"); !math.IsInf(v.F, 1) {
		t.Fatal(v)
	}
}

func TestSQLRandomConcurrentSession(t *testing.T) {
	var r sqlRandom
	if err := r.setSeed(.5); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 200; j++ {
				v, err := r.draw(false, 0, 1)
				if err != nil || v < 0 || v >= 1 {
					t.Errorf("uniform %g %v", v, err)
				}
			}
		})
	}
	wg.Wait()
}
