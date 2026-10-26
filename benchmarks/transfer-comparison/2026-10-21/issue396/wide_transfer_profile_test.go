// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

// Opt-in investigation for #396. Requires the independently loaded, immutable
// 100K ClickBench transferbench.hits fixture. This is a stage experiment, not a
// replacement execution path: forced HMGET is deliberately kept out of production.
import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

func TestWideTransferProfile(t *testing.T) {
	dir := os.Getenv("REDIS_WIDE_PROFILE_DIR")
	if dir == "" {
		t.Skip("set REDIS_WIDE_PROFILE_DIR on preloaded transferbench.hits")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	opts, err := goredis.ParseURL(os.Getenv("REDIS_URI"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Protocol = 2
	opts.ReadTimeout = 5 * time.Minute
	var wire atomic.Int64
	opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return scanProfileConn{Conn: c, bytes: &wire}, nil
	}
	client := goredis.NewClient(opts)
	defer client.Close()
	st := &store{client: client}
	meta, err := st.getTable(ctx, "transferbench", "hits")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseScript("SELECT * FROM transferbench.hits")
	if err != nil {
		t.Fatal(err)
	}
	type metrics struct {
		Strategy                                                         string
		Trial, Rows, Pages                                               int
		ReplyBytes, AllocBytes, Allocs                                   uint64
		TotalMS, CursorMS, FetchMS, DecodeMS, ProjectMS, ArrowMS, IPC_MS float64
	}
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	for trial := 0; trial < 4; trial++ {
		order := []string{"load_all", "discover_hmget"}
		if trial%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		for _, strategy := range order {
			if only := os.Getenv("REDIS_WIDE_PROFILE_STRATEGY"); only != "" && only != strategy {
				continue
			}
			pprof.Do(ctx, pprof.Labels("strategy", strategy), func(ctx context.Context) {
				e := &executor{store: st, schema: defaultSchema, cache: newExecCache(), now: time.Now().UTC()}
				plan, err := e.planSelect(ctx, parsed[0].Stmt.(*SelectStmt), nil)
				if err != nil {
					t.Fatal(err)
				}
				cols := plan.columns()
				need := map[string]bool{}
				for _, c := range meta.Columns {
					need[c.Name] = true
				}
				req := scanRequest{meta: meta, where: wherePlan{query: "*"}, need: need}
				it, fields, _ := e.newScanIter(ctx, req, nil)
				ar := &aggRequest{index: meta.index(), query: "*", rowFilter: meta.rowFilter(), load: []string{"__key"}, loadAll: true, width: len(meta.Columns) + 2}
				if strategy == "discover_hmget" {
					ar.load = []string{"__key", rowIDField}
					ar.loadAll = false
					it.store = st
					it.fetch = fields
				}
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				wire.Store(0)
				m := metrics{Strategy: strategy, Trial: trial}
				begin := time.Now()
				start := time.Now()
				it.cur, err = st.openAggregate(ctx, ar)
				if err != nil {
					t.Fatal(err)
				}
				m.CursorMS += ms(time.Since(start))
				defer it.close(ctx)
				f, err := os.Create(filepath.Join(dir, strategy+".arrow"))
				if err != nil {
					t.Fatal(err)
				}
				w := ipc.NewWriter(f, ipc.WithSchema(resultSchema(cols)))
				env := e.newEnv(ctx, meta.types(), nil)
				for {
					start = time.Now()
					page, err := it.cur.next(ctx)
					m.CursorMS += ms(time.Since(start))
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					m.Pages++
					keys := make([]string, len(page))
					for i, r := range page {
						keys[i] = r["__key"]
					}
					if len(it.fetch) > 0 {
						start = time.Now()
						fetched, err := st.fetchRows(ctx, keys, it.fetch)
						if err != nil {
							t.Fatal(err)
						}
						for i, row := range fetched {
							if row == nil {
								page[i] = nil
								continue
							}
							for k, v := range row {
								page[i][k] = v
							}
						}
						m.FetchMS += ms(time.Since(start))
					}
					start = time.Now()
					_, vals, err := it.keep(keys, page)
					if err != nil {
						t.Fatal(err)
					}
					m.DecodeMS += ms(time.Since(start))
					start = time.Now()
					rows := make([][]Value, 0, len(vals))
					for _, v := range vals {
						env.row = v
						row, err := evalItems(env, plan.items)
						if err != nil {
							t.Fatal(err)
						}
						if err := fitRow(row, cols, e.zone()); err != nil {
							t.Fatal(err)
						}
						rows = append(rows, row)
					}
					m.ProjectMS += ms(time.Since(start))
					m.Rows += len(rows)
					start = time.Now()
					rec, err := buildRecord(memory.DefaultAllocator, cols, rows)
					if err != nil {
						t.Fatal(err)
					}
					m.ArrowMS += ms(time.Since(start))
					start = time.Now()
					if err := w.Write(rec); err != nil {
						t.Fatal(err)
					}
					rec.Release()
					m.IPC_MS += ms(time.Since(start))
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				m.TotalMS = ms(time.Since(begin))
				runtime.ReadMemStats(&after)
				m.ReplyBytes = uint64(wire.Load())
				m.AllocBytes = after.TotalAlloc - before.TotalAlloc
				m.Allocs = after.Mallocs - before.Mallocs
				if m.Rows != 100000 {
					t.Fatalf("got %d rows", m.Rows)
				}
				b, _ := json.Marshal(m)
				t.Log(string(b))
			})
		}
	}
}
