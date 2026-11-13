// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type managedPerformanceCalls struct {
	sync.Mutex
	counts  map[string]int
	elapsed map[string]time.Duration
}

func (p *managedPerformanceCalls) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func managedPerformanceCategory(cmd goredis.Cmder) string {
	args := cmd.Args()
	name := cmd.Name()
	if name == "eval" && len(args) > 1 {
		switch args[1] {
		case snapshotWriteBatchLua:
			return "eval-row-batch"
		case snapshotIndexNotifyLua:
			return "eval-index-notify"
		case snapshotControl:
			return "eval-control"
		}
		return "eval-other"
	}
	if name == "evalsha" {
		return "evalsha"
	}
	return name
}
func (p *managedPerformanceCalls) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		begin := time.Now()
		err := next(ctx, cmd)
		category := managedPerformanceCategory(cmd)
		p.Lock()
		p.counts[category]++
		p.elapsed[category] += time.Since(begin)
		p.Unlock()
		return err
	}
}
func (p *managedPerformanceCalls) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		begin := time.Now()
		err := next(ctx, cmds)
		p.Lock()
		p.counts["pipelines"]++
		p.elapsed["pipelines"] += time.Since(begin)
		for _, cmd := range cmds {
			p.counts["p:"+managedPerformanceCategory(cmd)]++
		}
		p.Unlock()
		return err
	}
}
func (p *managedPerformanceCalls) reset() {
	p.Lock()
	defer p.Unlock()
	p.counts = map[string]int{}
	p.elapsed = map[string]time.Duration{}
}
func (p *managedPerformanceCalls) report(t *testing.T, label string, wall time.Duration) {
	p.Lock()
	defer p.Unlock()
	var keys []string
	for k := range p.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("managed profile %s wall=%s", label, wall)
	for _, k := range keys {
		t.Logf("  %s count=%d summed_client_time=%s", k, p.counts[k], p.elapsed[k])
	}
}
func TestManagedPerformanceProfile(t *testing.T) {
	if os.Getenv("REDIS_MANAGED_PERF") != "true" {
		t.Skip("set REDIS_MANAGED_PERF=true on owned Software fixture")
	}
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	defs := []string{"id BIGINT"}
	exprs := []string{"i"}
	for i := 1; i < 64; i++ {
		defs = append(defs, fmt.Sprintf("c%d BIGINT", i))
		exprs = append(exprs, fmt.Sprintf("i*100+%d", i))
	}
	h.exec("CREATE TABLE mp (" + strings.Join(defs, ",") + ")")
	calls := &managedPerformanceCalls{}
	calls.reset()
	ctx, _ := statisticsHookedTransport(t, h, calls)
	prior := h.ctx
	h.ctx = ctx
	defer func() { h.ctx = prior }()
	calls.reset()
	begin := time.Now()
	h.exec("INSERT INTO mp SELECT " + strings.Join(exprs, ",") + " FROM generate_series(1,4096) AS g(i)")
	calls.report(t, "insert-4096x64", time.Since(begin))
	for _, q := range []string{"SELECT * FROM mp", "SELECT id FROM mp", "SELECT * FROM mp WHERE id BETWEEN 100 AND 200"} {
		for trial := 0; trial < 4; trial++ {
			calls.reset()
			begin = time.Now()
			got, _ := h.query(q)
			calls.report(t, fmt.Sprintf("%s trial=%d", q, trial), time.Since(begin))
			want := map[string]bool{}
			lo, hi := 1, 4096
			if strings.Contains(q, "BETWEEN") {
				lo, hi = 100, 200
			}
			for id := lo; id <= hi; id++ {
				cells := []string{fmt.Sprint(id)}
				if strings.Contains(q, "SELECT *") {
					for c := 1; c < 64; c++ {
						cells = append(cells, fmt.Sprint(id*100+c))
					}
				}
				want[strings.Join(cells, "|")] = true
			}
			if len(got) != len(want) {
				t.Fatalf("rows=%d want %d", len(got), len(want))
			}
			for _, row := range got {
				if !want[row] {
					t.Fatalf("unexpected row %q", row)
				}
				delete(want, row)
			}
			if len(want) != 0 {
				t.Fatal("missing exact values")
			}
		}
	}
	// Add immutable segments without compaction, then validate the latest values.
	h.exec("UPDATE mp SET c1=999 WHERE id<=10")
	h.exec("DELETE FROM mp WHERE id=11")
	calls.reset()
	begin = time.Now()
	got, _ := h.query("SELECT id,c1 FROM mp")
	calls.report(t, "overlay-narrow", time.Since(begin))
	if len(got) != 4095 {
		t.Fatalf("rows=%d", len(got))
	}
	want := map[string]bool{}
	for i := 1; i <= 4096; i++ {
		if i == 11 {
			continue
		}
		v := i*100 + 1
		if i <= 10 {
			v = 999
		}
		want[fmt.Sprintf("%d|%d", i, v)] = true
	}
	for _, row := range got {
		if !want[row] {
			t.Fatalf("unexpected row %q", row)
		}
		delete(want, row)
	}
	if len(want) != 0 {
		t.Fatal("missing rows")
	}
}
