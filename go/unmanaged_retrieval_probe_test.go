// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const unmanagedProjectionProbeLua = `
if redis.call('TYPE',KEYS[1]).ok~='hash' then return false end
return redis.call('HMGET',KEYS[1],unpack(ARGV))
`

// A matched retrieval-only experiment, not native ADBC/SQL or a public option.
// The fixture and every returned field are independently validated. Lua remains
// test-only until complete lifecycle and ACL compatibility checks are in place.
func TestUnmanagedRetrievalProbe(t *testing.T) {
	if os.Getenv("REDIS_UNMANAGED_RETRIEVAL_PROBE") != "true" {
		t.Skip("opt-in owned Software projection retrieval experiment")
	}
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	const prefix = "unmanaged_retrieval_probe:"
	digest := goredis.NewScript(unmanagedProjectionProbeLua).Hash()
	for _, wide := range []bool{false, true} {
		rows, width := 100000, 5
		if wide {
			rows, width = 10000, 106
		}
		for start := 1; start <= rows; start += 1000 {
			p := raw.Pipeline()
			for id := start; id < min(start+1000, rows+1); id++ {
				values := map[string]any{"id": id, "grp": id % 16, "amount": 3 * id, "label": fmt.Sprintf("row-%08d", id), "bench_ordinal": id}
				if wide {
					for c := 5; c < width; c++ {
						values["pad"+strconv.Itoa(c)] = "payload-012345678901234567890123456789"
					}
				}
				p.HSet(h.ctx, prefix+strconv.Itoa(id), values)
			}
			if _, err := p.Exec(h.ctx); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := raw.Eval(h.ctx, unmanagedProjectionProbeLua, []string{prefix + "1"}, "grp", "amount").Result(); err != nil {
			t.Fatal(err)
		}
		for _, requested := range []int{10000, rows} {
			if wide && requested != 10000 {
				continue
			}
			keys := make([]string, requested)
			for i := range keys {
				keys[i] = prefix + strconv.Itoa(i+1)
			}
			for repeat := -1; repeat < 3; repeat++ {
				modes := []string{"hgetall", "lua_hmget"}
				if repeat%2 == 1 {
					modes[0], modes[1] = modes[1], modes[0]
				}
				for _, mode := range modes {
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					started := time.Now()
					var groups [16]int64
					var total int64
					for start := 0; start < len(keys); start += 1000 {
						part := keys[start:min(start+1000, len(keys))]
						p := raw.Pipeline()
						maps := make([]*goredis.MapStringStringCmd, 0, len(part))
						slices := make([]*goredis.SliceCmd, 0, len(part))
						for _, key := range part {
							if mode == "hgetall" {
								maps = append(maps, p.HGetAll(h.ctx, key))
							} else {
								cmd := goredis.NewSliceCmd(h.ctx, "EVALSHA", digest, 1, key, "grp", "amount")
								slices = append(slices, cmd)
								if err := p.Process(h.ctx, cmd); err != nil {
									t.Fatal(err)
								}
							}
						}
						if _, err := p.Exec(h.ctx); err != nil {
							t.Fatal(err)
						}
						for row := range part {
							id := start + row + 1
							var g, a string
							if mode == "hgetall" {
								m, err := maps[row].Result()
								if err != nil || len(m) != width {
									t.Fatal("incomplete full hash", err, len(m), width)
								}
								g, a = m["grp"], m["amount"]
							} else {
								v, err := slices[row].Result()
								if err != nil || len(v) != 2 {
									t.Fatal("incomplete projected hash", err, v)
								}
								var ok bool
								g, ok = v[0].(string)
								if !ok {
									t.Fatal(v)
								}
								a, ok = v[1].(string)
								if !ok {
									t.Fatal(v)
								}
							}
							if g != strconv.Itoa(id%16) || a != strconv.Itoa(id*3) {
								t.Fatal("projection changed exact value", id, g, a)
							}
							groups[id%16]++
							total += int64(id * 3)
						}
					}
					elapsed := time.Since(started)
					runtime.ReadMemStats(&after)
					if total != 3*int64(requested)*int64(requested+1)/2 {
						t.Fatal("incomplete aggregate", total)
					}
					for g, n := range groups {
						first := g
						if first == 0 {
							first = 16
						}
						if n != int64((requested-first)/16+1) {
							t.Fatal("incomplete groups", g, n)
						}
					}
					t.Logf("RETRIEVAL_PROBE width=%d rows=%d mode=%s repeat=%d seconds=%.9f allocated_bytes=%d allocations=%d validated=true gomaxprocs=%d", width, requested, mode, repeat, elapsed.Seconds(), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs, runtime.GOMAXPROCS(0))
				}
			}
			if wide {
				break
			}
		}
		keys := keysForUnmanagedProbe(prefix, rows)
		for start := 0; start < len(keys); start += 1000 {
			if _, err := raw.Del(h.ctx, keys[start:min(start+1000, len(keys))]...).Result(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func keysForUnmanagedProbe(prefix string, rows int) []string {
	keys := make([]string, rows)
	for i := range keys {
		keys[i] = prefix + strconv.Itoa(i+1)
	}
	return keys
}
