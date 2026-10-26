// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const compactIntegerTupleLimit = 4096

// Complete input tuples are compacted, never per-partition DISTINCT counts.
// The established external merge still owns global equality and weighted sums.
type compactIntegerTuple struct {
	values [4]int64
	nulls  uint8
}

func compactIntegerTupleShape(meta *tableMeta, p *tupleAggregatePlan, wp wherePlan) bool {
	if !p.server || !managedRowIDTable(meta) || len(meta.PendingCleanup) != 0 || len(meta.Columns) < 64 || len(p.cols) == 0 || len(p.cols) > 4 || len(p.distinct) == 0 || len(p.argExprs) != 0 || wp.query != "*" || wp.residual != nil || wp.keys != nil || wp.none {
		return false
	}
	for _, x := range p.groupExprs {
		if _, ok := x.(*ColumnRef); !ok {
			return false
		}
	}
	inexact := false
	for _, c := range p.cols {
		if !c.Type.Kind.isInteger() {
			return false
		}
		inexact = inexact || !loadsExactly(c)
	}
	return inexact
}

// Three bounded PING samples per primary reject this command-heavy source on
// slower links. This is a conservative planner heuristic, not a latency SLA;
// failed/slow probes retain the established server source without retrying SQL.
func (e *executor) compactIntegerTupleRTT(ctx context.Context) (bool, time.Duration) {
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	var slowest atomic.Int64
	probe := func(client *goredis.Client) error {
		// WithTimeout shares the owned pool but resets hooks. These read-only PINGs
		// are planner probes, outside query command diagnostics; server INFO includes
		// them. Never Close the clone or mutate the real statement client options.
		client = client.WithTimeout(100 * time.Millisecond)
		elapsed, err := sampleCompactTupleRTT(probeCtx, func(ctx context.Context) error { return client.Ping(ctx).Err() })
		for old := slowest.Load(); int64(elapsed) > old; old = slowest.Load() {
			if slowest.CompareAndSwap(old, int64(elapsed)) {
				break
			}
		}
		return err
	}

	var err error
	if cluster, ok := e.store.client.(*goredis.ClusterClient); ok {
		err = cluster.ForEachMaster(probeCtx, func(_ context.Context, c *goredis.Client) error { return probe(c) })
	} else if client, ok := e.store.client.(*goredis.Client); ok {
		err = probe(client)
	} else {
		return false, 0
	}
	return err == nil, time.Duration(slowest.Load())
}

func sampleCompactTupleRTT(ctx context.Context, ping func(context.Context) error) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Exclude first connection/TLS setup from the steady-state samples.
	if err := ping(ctx); err != nil {
		return 0, err
	}
	var slowest time.Duration
	for range 3 {
		if err := ctx.Err(); err != nil {
			return slowest, err
		}
		started := time.Now()
		err := ping(ctx)
		slowest = max(slowest, time.Since(started))
		if err != nil {
			return slowest, err
		}
		if slowest >= 2*time.Millisecond {
			return slowest, fmt.Errorf("tuple HASH source latency threshold exceeded")
		}
	}
	return slowest, nil
}

func (e *executor) compactIntegerTuplePartition(ctx context.Context, plan *selectPlan, p *tupleAggregatePlan, part wherePlan, params []Value, emit func([]Value, int64) error) error {
	need := map[string]bool{}
	aggs := make([]*Func, len(p.cols))
	for i, c := range p.cols {
		need[c.Name] = true
		aggs[i] = &Func{Name: "MIN", Args: []Expr{&ColumnRef{Name: c.Name}}}
	}
	counts := make(map[compactIntegerTuple]int64, compactIntegerTupleLimit)
	values := make([]Value, len(p.cols))
	flush := func() error {
		for key, count := range counts {
			if err := ctx.Err(); err != nil {
				return cancelled(ctx)
			}
			for i, c := range p.cols {
				values[i] = intValue(c.Type, key.values[i])
				values[i].Null = key.nulls&(1<<i) != 0
			}
			if e.aggregateMetrics != nil {
				e.aggregateMetrics.tuples.Add(1)
				for i, v := range values {
					if !v.Null {
						e.aggregateMetrics.valueBytes.Add(int64(len(p.cols[i].field()) + len(encodeStored(v))))
					}
				}
			}
			if err := emit(values, count); err != nil {
				return err
			}
		}
		clear(counts)
		return nil
	}
	local := *e
	stats := &scanTelemetry{}
	local.telemetry = stats
	started := time.Now()
	defer func() {
		if e.aggregateMetrics != nil {
			e.aggregateMetrics.fetch.add(time.Duration(stats.fetch.Load()))
			e.aggregateMetrics.compactNanos.Add(int64(time.Since(started)) - stats.fetch.Load())
		}
		if e.telemetry != nil {
			e.telemetry.fetch.Add(stats.fetch.Load())
		}
	}()
	if e.aggregateMetrics != nil {
		e.aggregateMetrics.rowScans.Add(1)
	}
	handled, err := local.scanScalarAggregateValues(ctx, scanRequest{meta: plan.meta, where: part, need: need, authoritative: true}, params, aggs, func(vals []Value) error {
		var key compactIntegerTuple
		for i, v := range vals {
			if v.Null {
				key.nulls |= 1 << i
			} else {
				key.values[i] = v.I
			}
		}
		if counts[key] == 0 && len(counts) == compactIntegerTupleLimit {
			if err := flush(); err != nil {
				return err
			}
		}
		counts[key]++
		return nil
	})
	if err != nil {
		return err
	}
	if !handled {
		return invalidArg(fmt.Errorf("integer tuple scan unavailable"))
	}
	return flush()
}
