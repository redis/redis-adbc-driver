// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"reflect"
	"slices"
	"testing"
)

// #654: a snowflake whose retained branch rows exceed the cap spills the
// dimension that crossed it to partitions, partitions the fact by the same
// key, and joins partition by partition. Whatever the cap and partition
// count, it returns the eager join's rows (as a multiset: the streamed order
// is unspecified), and it removes its files.
func TestSnowflakeBranchSpill(t *testing.T) {
	var dirs []string
	testHookJoinSpillDir = func(dir string) { dirs = append(dirs, dir) }
	oldCap, oldParts := snowflakeBranchRows, joinSpillPartitions
	t.Cleanup(func() { testHookJoinSpillDir, snowflakeBranchRows, joinSpillPartitions = nil, oldCap, oldParts })
	for _, name := range []string{"q3", "q5", "q7", "q9", "rows"} {
		t.Run(name, func(t *testing.T) {
			snowflakeBranchRows = oldCap
			want, _ := snowflakeRun(t, snowflakeQueries[name], math.MaxInt64)
			slices.Sort(want)
			spilled := 0
			for _, parts := range []int{1, 4, 16} {
				for _, limit := range []int{8, 16, 32, 64, 96, 128, 200} {
					snowflakeBranchRows, joinSpillPartitions = limit, parts
					before := joinSpillStats.joined.Load()
					got, _ := snowflakeRun(t, snowflakeQueries[name], 1)
					slices.Sort(got)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("cap %d, %d partitions: got %v\nwant %v", limit, parts, got, want)
					}
					if joinSpillStats.joined.Load() != before {
						spilled++
					}
					for _, dir := range dirs {
						if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
							t.Fatalf("cap %d: spill directory %s remains (%v)", limit, dir, err)
						}
					}
					dirs = nil
				}
			}
			if spilled == 0 {
				t.Fatal("no cap joined a spilled dimension")
			}
			t.Logf("%d of 21 cap/partition settings joined a spilled dimension", spilled)
		})
	}
}

// A spilled partition that would itself exceed the cap with the dimensions in
// memory falls back to the eager join before any fact row is read, as an
// over-cap snowflake did before; so does testNoJoinSpill.
func TestSnowflakeBranchSpillFallsBackBeforeFacts(t *testing.T) {
	oldCap, oldParts := snowflakeBranchRows, joinSpillPartitions
	t.Cleanup(func() { snowflakeBranchRows, joinSpillPartitions, testNoJoinSpill = oldCap, oldParts, false })
	for _, c := range []struct {
		what    string
		parts   int
		noSpill bool
	}{{"one partition", 1, false}, {"spill disabled", 64, true}} {
		t.Run(c.what, func(t *testing.T) {
			snowflakeBranchRows, joinSpillPartitions, testNoJoinSpill = 3, c.parts, c.noSpill
			e, p, jp := snowflakeTestShape(t, snowflakeQueries["q5"])
			visited := false
			handled, err := e.streamJoinPages(context.Background(), jp, map[string]bool{"nation.n_name": true}, p.sel.Where, nil, true, func([]map[string]Value) error {
				visited = true
				return nil
			})
			if err != nil || handled || visited {
				t.Fatalf("handled=%v visited=%v err=%v", handled, visited, err)
			}
		})
	}
}
