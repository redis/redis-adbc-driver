// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func cudaTestPlan() *selectPlan {
	return &selectPlan{
		meta:  &tableMeta{Columns: []columnMeta{{Name: "x", Type: typeInt64, Nullable: true}}},
		sel:   &SelectStmt{Where: &Binary{Op: ">=", L: &ColumnRef{Name: "x"}, R: &Literal{V: intValue(typeInt64, 2)}}},
		items: []planItem{{expr: &ColumnRef{Name: "x"}, typ: typeInt64}},
	}
}
func TestCUDAEligibilityAndOptions(t *testing.T) {
	for _, op := range []string{"=", "!=", "<>", "<", "<=", ">", ">="} {
		p := cudaTestPlan()
		p.sel.Where.(*Binary).Op = op
		if _, err := compileCUDAPredicate(p); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	for _, change := range []func(*selectPlan){
		func(p *selectPlan) { p.meta.Columns[0].Type = typeFloat64 },
		func(p *selectPlan) { p.sel.Where = nil },
		func(p *selectPlan) { p.sel.Where.(*Binary).Op = "AND" },
		func(p *selectPlan) { p.sel.Where.(*Binary).R = &Literal{V: nullValue(typeInt64)} },
		func(p *selectPlan) { p.items[0].expr = &Func{Name: "RANDOM"} },
		func(p *selectPlan) { p.distinct = true },
		func(p *selectPlan) { p.meta.isMem = true },
	} {
		p := cudaTestPlan()
		change(p)
		if _, err := compileCUDAPredicate(p); err == nil {
			t.Fatal("unsafe CUDA shape accepted")
		}
	}
	s := &statementImpl{conn: &connectionImpl{}}
	if err := s.SetOption(context.Background(), OptionStringExecutionBackend, "auto"); err == nil {
		t.Fatal("implicit fallback mode accepted")
	}
	if err := s.SetOptionInt(context.Background(), OptionIntCUDADevice, -1); err == nil {
		t.Fatal("negative device accepted")
	}
	if err := s.SetOption(context.Background(), OptionStringExecutionBackend, "cuda"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteUpdate(context.Background()); err == nil {
		t.Fatal("CUDA update accepted")
	}
}

type testCUDAFilter struct {
	calls  int
	cancel context.CancelFunc
	fail   bool
}

func (f *testCUDAFilter) Close() {}
func (f *testCUDAFilter) Run(values []int64, valid []byte, rhs int64, op int) ([]byte, error) {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	if f.fail {
		return nil, fmt.Errorf("injected device failure")
	}
	mask := make([]byte, len(values))
	for i, v := range values {
		if valid[i] != 0 && v >= rhs {
			mask[i] = 1
		}
	}
	return mask, nil
}
func TestCUDABatchNullsOrderAndCancellation(t *testing.T) {
	p := cudaTestPlan()
	fake := &testCUDAFilter{}
	metrics := &gpuMetrics{}
	it := &scanIter{meta: p.meta, env: &evalEnv{ctx: context.Background()}, gpu: &cudaPredicate{column: "x", rhs: 2, op: 5, filter: fake, metrics: metrics}}
	rows := []aggRow{{"x": "1"}, {}, {"x": "9007199254740993"}, nil, {"x": fmt.Sprint(math.MaxInt64)}}
	keys := []string{"a", "null", "large", "gone", "max"}
	got, values, err := it.keepCUDA(keys, rows)
	if err != nil || !reflect.DeepEqual(got, []string{"large", "max"}) || values[0]["x"].I != 9007199254740993 {
		t.Fatalf("%v %v %v", got, values, err)
	}
	if metrics.launches.Load() != 1 || metrics.rows.Load() != 4 {
		t.Fatalf("wrong GPU admission metrics: %s", metrics.json())
	}
	ctx, cancel := context.WithCancel(context.Background())
	fake.cancel = cancel
	it.env.ctx = ctx
	if _, _, err := it.keepCUDA(keys, rows); err == nil {
		t.Fatal("cancelled kernel result accepted")
	}
	it.env.ctx = context.Background()
	fake.cancel = nil
	fake.fail = true
	if _, _, err := it.keepCUDA(keys, rows); err == nil {
		t.Fatal("CUDA failure silently fell back to CPU")
	}
}
