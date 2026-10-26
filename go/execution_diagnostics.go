// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// liveTimer includes operations still in flight. Summed worker seconds may
// exceed wall time; Redis round-trip time includes pool wait and decoding,
// and must never be presented as measured Redis server CPU time.
type liveTimer struct {
	mu     sync.Mutex
	last   time.Time
	nanos  int64
	active int64
}

func (t *liveTimer) update(now time.Time) {
	if t.active > 0 {
		t.nanos += int64(now.Sub(t.last)) * t.active
	}
	t.last = now
}

func (t *liveTimer) start() func() {
	t.mu.Lock()
	t.update(time.Now())
	t.active++
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		t.update(time.Now())
		t.active--
		t.mu.Unlock()
	}
}

// add records a completed duration measured outside an active interval.
func (t *liveTimer) add(elapsed time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.update(time.Now())
	t.nanos += int64(elapsed)
}

func (t *liveTimer) seconds() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.update(time.Now())
	return float64(t.nanos) / 1e9
}

type diagnosticsContextKey struct{}

type executionDiagnostics struct {
	started                                               time.Time
	phase                                                 atomic.Pointer[string]
	plan                                                  atomic.Pointer[json.RawMessage]
	processed, matched, readBytes, writtenBytes, commands atomic.Int64
	redisWait, socketRead                                 liveTimer
	metrics                                               *partitionMetrics
	grant                                                 *atomic.Int64
	allocatedBefore                                       uint64
}

func (d *executionDiagnostics) setPhase(phase string) {
	if d != nil {
		d.phase.Store(&phase)
	}
}

func (d *executionDiagnostics) setPlan(plan any) {
	if d == nil {
		return
	}
	b, err := json.Marshal(plan)
	if err == nil {
		raw := json.RawMessage(b)
		d.plan.Store(&raw)
	}
}

func (d *executionDiagnostics) snapshot(status string) map[string]any {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	phase := "planning"
	if p := d.phase.Load(); p != nil {
		phase = *p
	}
	var plan any
	if p := d.plan.Load(); p != nil {
		plan = *p
	}
	grant := int64(0)
	if d.grant != nil {
		grant = d.grant.Load()
	}
	return map[string]any{
		"version": 1, "status": status, "phase": phase, "sampled_at": time.Now().UTC(),
		"execute_elapsed_seconds": time.Since(d.started).Seconds(), "worker_grant": grant,
		"plan": plan, "processed_rows": d.processed.Load(), "matched_rows": d.matched.Load(),
		"aggregate_metrics": json.RawMessage(d.metrics.json()),
		"redis_commands":    d.commands.Load(), "redis_round_trip_worker_seconds": d.redisWait.seconds(),
		"socket_read_worker_seconds": d.socketRead.seconds(),
		"connection_read_bytes":      d.readBytes.Load(), "connection_written_bytes": d.writtenBytes.Load(),
		"io_scope":                               "connection RESP bytes during execute, including concurrent connection maintenance; excludes TLS framing",
		"redis_server_profile":                   nil,
		"process_allocated_bytes_during_execute": mem.TotalAlloc - d.allocatedBefore,
		"process_heap_bytes":                     mem.HeapAlloc,
	}
}

// The sampler reads only atomics/immutable plan snapshots. It never calls the
// busy ADBC connection. Atomic replacement leaves the last complete sample
// usable after SIGKILL. File failures are optional diagnostic failures only.
func (e *executor) startDiagnostics(path string, cfg *clientConfig) func(error) {
	if path == "" {
		return func(error) {}
	}
	var active *atomic.Pointer[executionDiagnostics]
	if cfg != nil {
		active = &cfg.diagnostics
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	d := &executionDiagnostics{started: time.Now(), metrics: e.aggregateMetrics, grant: e.workerGrant, allocatedBefore: mem.TotalAlloc}
	e.diagnostics = d
	if active != nil {
		active.Store(d)
	}
	write := func(status string) {
		b, err := json.Marshal(d.snapshot(status))
		if err != nil {
			return
		}
		f, err := os.CreateTemp(filepath.Dir(path), ".redis-adbc-diagnostics-*")
		if err != nil {
			return
		}
		defer os.Remove(f.Name())
		_, err = f.Write(append(b, '\n'))
		closeErr := f.Close()
		if err == nil && closeErr == nil {
			_ = os.Rename(f.Name(), path)
		}
	}
	write("running")
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				write("running")
			case <-stop:
				return
			}
		}
	}()
	return func(err error) {
		close(stop)
		<-done
		status := "execute_returned"
		if err != nil {
			status = "execute_failed"
		}
		write(status)
		if active != nil {
			active.CompareAndSwap(d, nil)
		}
	}
}

func (e *executor) recordSelectPlan(plan *selectPlan, sc selectScan) {
	if e.diagnostics == nil {
		return
	}
	columns := make([]map[string]any, 0)
	for _, c := range plan.meta.Columns {
		if sc.req.need[c.Name] {
			columns = append(columns, map[string]any{"name": c.Name, "field": c.field(), "indexed": c.Indexed,
				"sorts_exactly": c.sortsExactly(), "loads_exactly": loadsExactly(c),
				"tag_values": c.TagValues, "tags_checked": c.TagsChecked, "missing_through": c.MissingThrough})
		}
	}
	sort := make([]map[string]any, 0)
	for _, key := range sc.req.sortBy {
		sort = append(sort, map[string]any{"field": key.field, "descending": key.desc})
	}
	e.diagnostics.setPlan(map[string]any{"kind": "select_scan", "hash_scan": sc.req.hashScan, "index_sort": sc.indexSort,
		"search_result_cap": sc.caps[0], "aggregate_result_cap": sc.caps[1],
		"search_query": sc.req.where.query, "residual": sc.req.where.residual, "pushed_limit": sc.req.limit,
		"sql_limit": plan.sel.Limit, "sql_offset": plan.sel.Offset, "late_projection": sc.req.late != nil,
		"tie_sort": sc.req.ties != nil, "columns": columns, "sort": sort})
	e.diagnostics.setPhase("select_scan")
}

type diagnosticsHook struct {
	active *atomic.Pointer[executionDiagnostics]
}

func (h diagnosticsHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := next(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &diagnosticConn{Conn: conn, active: h.active}, nil
	}
}

func (h diagnosticsHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if d, _ := ctx.Value(diagnosticsContextKey{}).(*executionDiagnostics); d != nil {
			d.commands.Add(1)
			defer d.redisWait.start()()
		}
		return next(ctx, cmd)
	}
}

func (h diagnosticsHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		if d, _ := ctx.Value(diagnosticsContextKey{}).(*executionDiagnostics); d != nil {
			d.commands.Add(int64(len(cmds)))
			defer d.redisWait.start()()
		}
		return next(ctx, cmds)
	}
}

type diagnosticConn struct {
	net.Conn
	active *atomic.Pointer[executionDiagnostics]
}

func (c *diagnosticConn) Read(b []byte) (int, error) {
	d := c.active.Load()
	if d != nil {
		defer d.socketRead.start()()
	}
	n, err := c.Conn.Read(b)
	if d != nil {
		d.readBytes.Add(int64(n))
	}
	return n, err
}

func (c *diagnosticConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if d := c.active.Load(); d != nil {
		d.writtenBytes.Add(int64(n))
	}
	return n, err
}
