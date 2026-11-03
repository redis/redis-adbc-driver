// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Report a late failure after the server really created the private index.
// The application index and published copy must remain independently usable.
type managedImportIndexFailure struct {
	snapshotHook
	fired   atomic.Bool
	creates atomic.Int64
	index   string
}

func (h *managedImportIndexFailure) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		args := cmd.Args()
		if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "FT.CREATE") {
			h.creates.Add(1)
		}
		if err == nil && len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "FT.CREATE") && h.fired.CompareAndSwap(false, true) {
			h.index = fmt.Sprint(args[1])
			return errors.New("injected late private index failure")
		}
		return err
	}
}

func TestAdoptIndexFailure(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("refresh_%t", refresh), func(t *testing.T) {
			f := newHashFixture(t, "it:ad:fail:")
			const table = "it_ad_fail"
			f.h.dropTables(table)
			t.Cleanup(func() { f.h.dropTables(table) })
			for i := 1; i <= 5; i++ {
				f.hset(fmt.Sprintf("%s%d", f.prefix, i), "n", i)
			}
			const sourceIndex = "it_ad_failure_source"
			f.appIndex(sourceIndex, "SCHEMA", "n", "NUMERIC")
			o := HashAdoptOptions{Prefix: f.prefix, Table: table, Apply: true, Index: sourceIndex}
			if refresh {
				if !f.adopt(o).Applied {
					t.Fatal("initial import was not published")
				}
				f.hset(f.prefix+"2", "n", "20")
			}
			root := f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val()
			c, err := dialHashes(f.h.ctx, f.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.close(f.h.ctx)
			hook := &managedImportIndexFailure{}
			c.client.AddHook(hook)
			a := &adopter{c: c, o: o, progress: func(string) {}, res: &HashAdoption{Report: &HashReport{Prefix: f.prefix}}}
			result, err := a.snapshotAdopt(c.context(f.h.ctx))
			if !hook.fired.Load() || hook.creates.Load() != 1 || hook.index == "" || err == nil || !strings.Contains(err.Error(), "injected late private index failure") {
				t.Fatalf("late index failure not exercised: %v", err)
			}
			if result == nil || result.Applied || !result.ManagedCopy || len(result.Report.Checks) != 1 || !result.Report.Checks[0].Ready {
				t.Fatalf("late failure lost its completed import plan: %+v", result)
			}
			if root != f.raw.HGet(f.h.ctx, snapshotCatalogKey, "root").Val() {
				t.Fatal("failed import published")
			}
			if refresh {
				f.h.expectRows("SELECT SUM(n),COUNT(__key) FROM "+table, "15|5")
			} else {
				f.h.expectRows("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='"+table+"'", "0")
			}
			c.st.reapSnapshotBudget(f.h.ctx, 2*time.Second)
			if err := f.raw.Do(f.h.ctx, "FT.INFO", hook.index).Err(); err == nil || !isUnknownIndex(err) {
				t.Fatalf("failed private index not reclaimed: %v", err)
			}
			if err := f.raw.Do(f.h.ctx, "FT.INFO", sourceIndex).Err(); err != nil {
				t.Fatal("application index changed", err)
			}
			sum := "15|5"
			if refresh {
				sum = "33|5"
			}
			if f.h.prefixKeyCount(f.raw, f.prefix) != 5 {
				t.Fatal("failure changed source HASH coverage")
			}
			for i := 1; i <= 5; i++ {
				want := fmt.Sprint(i)
				if refresh && i == 2 {
					want = "20"
				}
				fields := f.raw.HGetAll(f.h.ctx, fmt.Sprintf("%s%d", f.prefix, i)).Val()
				if len(fields) != 1 || fields["n"] != want {
					t.Fatal("failure changed an application HASH")
				}
			}
			if !f.adopt(o).Applied {
				t.Fatal("fresh explicit import did not publish")
			}
			f.h.expectRows("SELECT SUM(n),COUNT(__key) FROM "+table, sum)
		})
	}
}
