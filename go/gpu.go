// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"

	"github.com/adbc-drivers/redis/go/internal/cuda"
	"github.com/apache/arrow-adbc/go/adbc"
)

const (
	OptionStringExecutionBackend = "adbc.redis.execution_backend"
	OptionIntCUDADevice          = "adbc.redis.cuda_device"
	OptionStringGPUMetrics       = "adbc.redis.gpu_metrics"
)

type gpuMetrics struct{ launches, rows atomic.Int64 }

func (m *gpuMetrics) json() string {
	if m == nil {
		return "{}"
	}
	b, _ := json.Marshal(map[string]any{"backend": "cuda", "kernel_launches": m.launches.Load(), "input_rows": m.rows.Load()})
	return string(b)
}

type gpuFilter interface {
	Run([]int64, []byte, int64, int) ([]byte, error)
	Close()
}
type cudaPredicate struct {
	column  string
	rhs     int64
	op      int
	filter  gpuFilter
	metrics *gpuMetrics
}

func compileCUDAPredicate(plan *selectPlan) (*cudaPredicate, error) {
	unsupported := func() (*cudaPredicate, error) {
		return nil, errorf(adbc.StatusNotImplemented, "CUDA supports one physical table, integer column/literal comparison, integer projections and COUNT/SUM/AVG/MIN/MAX with integer GROUP BY; other SQL must use execution_backend=cpu")
	}
	if plan == nil || plan.sel == nil || plan.meta == nil || plan.meta.isMem || plan.recursive != nil || plan.setop != nil || plan.grouping != nil || plan.distinct || plan.windowed() || plan.having != nil || len(plan.order) != 0 || plan.sel.Limit != nil || plan.sel.Offset != nil || plan.hasCorrelated() || len(plan.outerAggs) != 0 {
		return unsupported()
	}
	types := plan.meta.types()
	columnOK := func(x Expr) bool {
		c, ok := x.(*ColumnRef)
		return ok && c.Outer == 0 && types[c.Name].Kind.isInteger()
	}
	for _, item := range plan.items {
		if columnOK(item.expr) {
			continue
		}
		f, ok := item.expr.(*Func)
		if !ok || f.Distinct || f.Filter != nil || f.WithinGroup || len(f.OrderBy) != 0 {
			return unsupported()
		}
		switch f.Name {
		case "COUNT", "SUM", "AVG", "MIN", "MAX":
		default:
			return unsupported()
		}
		if f.Star {
			if f.Name != "COUNT" {
				return unsupported()
			}
		} else if len(f.Args) != 1 || !columnOK(f.Args[0]) {
			return unsupported()
		}
	}
	for _, group := range plan.sel.GroupBy {
		if !columnOK(group) {
			return unsupported()
		}
	}
	comparison, ok := plan.sel.Where.(*Binary)
	if !ok || !columnOK(comparison.L) {
		return unsupported()
	}
	literal, ok := comparison.R.(*Literal)
	if !ok || literal.Unknown || literal.V.Null || !literal.V.T.Kind.isInteger() {
		return unsupported()
	}
	operations := map[string]int{"=": 0, "!=": 1, "<>": 1, "<": 2, "<=": 3, ">": 4, ">=": 5}
	op, ok := operations[comparison.Op]
	if !ok {
		return unsupported()
	}
	return &cudaPredicate{column: comparison.L.(*ColumnRef).Name, rhs: literal.V.I, op: op}, nil
}

func (e *executor) beginCUDA(ctx context.Context, plan *selectPlan) (func(), error) {
	predicate, err := compileCUDAPredicate(plan)
	if err != nil {
		return nil, err
	}
	if err = cancelled(ctx); err != nil {
		return nil, err
	}
	predicate.filter, err = cuda.New(e.cudaDevice)
	if err != nil {
		return nil, errorf(adbc.StatusNotImplemented, "CUDA initialization failed: %v", err)
	}
	predicate.metrics = e.gpuMetrics
	e.gpu = predicate
	return func() { predicate.filter.Close(); e.gpu = nil }, nil
}

// keepCUDA preserves row order, NULL semantics and the existing exact CPU
// reducers. Only the predicate is evaluated on CUDA in this initial backend.
func (it *scanIter) keepCUDA(keys []string, fetched []aggRow) ([]string, []map[string]Value, error) {
	if it.projection != nil {
		return nil, nil, errorf(adbc.StatusInternal, "CUDA scan cannot use CPU stream projection")
	}
	rows := make([]map[string]Value, 0, len(fetched))
	indices := make([]int, 0, len(fetched))
	values := make([]int64, 0, len(fetched))
	valid := make([]byte, 0, len(fetched))
	for i, raw := range fetched {
		if raw == nil {
			continue
		}
		if err := cancelled(it.env.ctx); err != nil {
			return nil, nil, err
		}
		row, err := decodeRow(it.meta, raw, it.need)
		if err != nil {
			return nil, nil, err
		}
		v, ok := row[it.gpu.column]
		values = append(values, v.I)
		bit := byte(0)
		if ok && !v.Null {
			bit = 1
		}
		valid = append(valid, bit)
		rows = append(rows, row)
		indices = append(indices, i)
	}
	outKeys := make([]string, 0, len(rows))
	out := make([]map[string]Value, 0, len(rows))
	if it.ties != nil {
		it.raw, it.kept = fetched, it.kept[:0]
	}
	const batch = 65536
	for start := 0; start < len(rows); start += batch {
		if err := cancelled(it.env.ctx); err != nil {
			return nil, nil, err
		}
		end := min(start+batch, len(rows))
		mask, err := it.gpu.filter.Run(values[start:end], valid[start:end], it.gpu.rhs, it.gpu.op)
		if cancel := cancelled(it.env.ctx); cancel != nil {
			return nil, nil, cancel
		}
		if err != nil {
			return nil, nil, errorf(adbc.StatusIO, "CUDA predicate failed: %v", err)
		}
		it.gpu.metrics.launches.Add(1)
		it.gpu.metrics.rows.Add(int64(end - start))
		for j, match := range mask {
			if match != 0 {
				i := start + j
				outKeys = append(outKeys, keys[indices[i]])
				out = append(out, rows[i])
				if it.ties != nil {
					it.kept = append(it.kept, indices[i])
				}
			}
		}
	}
	if it.diagnostics != nil {
		it.diagnostics.matched.Add(int64(len(out)))
	}
	return outKeys, out, nil
}

func (s *statementImpl) setGPUOption(key, value string) error {
	switch key {
	case OptionStringExecutionBackend:
		if value != "cpu" && value != "cuda" {
			return errorf(adbc.StatusInvalidArgument, "execution_backend must be cpu or cuda")
		}
		s.executionBackend = value
	case OptionIntCUDADevice:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 65535 {
			return errorf(adbc.StatusInvalidArgument, "cuda_device must be 0 through 65535")
		}
		s.cudaDevice = n
	}
	s.partitionExecution = nil
	return nil
}
