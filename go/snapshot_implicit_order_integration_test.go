// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Reject reducers on a separate read client, without changing native indexes,
// published rows, the server cap, or the fixture writer's client.
type snapshotImplicitReadHook struct {
	snapshotHook
	aggregates  atomic.Int64
	directories atomic.Int64
}

func (h *snapshotImplicitReadHook) inspect(c goredis.Cmder) error {
	if c.Name() == "ft.aggregate" {
		h.aggregates.Add(1)
		err := errors.New("SEARCH_LIMIT_OVER LIMIT exceeds maximum of 137")
		c.SetErr(err)
		return err
	}
	if c.Name() == "zrange" && len(c.Args()) > 1 && strings.HasPrefix(fmt.Sprint(c.Args()[1]), snapshotPrefix+"members:") {
		h.directories.Add(1)
	}
	return nil
}
func (h *snapshotImplicitReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		if err := h.inspect(c); err != nil {
			return err
		}
		return next(ctx, c)
	}
}
func (h *snapshotImplicitReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cs []goredis.Cmder) error {
		for _, c := range cs {
			if err := h.inspect(c); err != nil {
				return err
			}
		}
		return next(ctx, cs)
	}
}

func TestSnapshotImplicitOrderUsesExactVisibilityWithoutReducers(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,exact BIGINT,tag VARCHAR,payload VARCHAR,spare VARCHAR)")
	h.exec("INSERT INTO t SELECT i,9007199254740993,CASE WHEN i%2=0 THEN 'match' ELSE 'miss' END,'row-'||i,NULL FROM generate_series(1,300) AS g(i)")
	h.exec("UPDATE t SET tag='miss',exact=9007199254740995 WHERE id=2")
	h.exec("DELETE FROM t WHERE id=4")
	h.exec("INSERT INTO t VALUES(301,NULL,'match','later',NULL)")
	for _, tc := range []struct {
		name, where string
		match       func(int) bool
	}{
		{"all", "", func(int) bool { return true }},
		{"indexed", " WHERE tag='match'", func(i int) bool { return i == 301 || i%2 == 0 && i != 2 }},
		{"residual", " WHERE id%3=0", func(i int) bool { return i%3 == 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := snapshotTestStore(t, raw)
			guard := &snapshotImplicitReadHook{}
			s.client.AddHook(guard)
			result := snapshotExecuteTest(t, s, h.ctx, "SELECT id,exact,payload,spare FROM t"+tc.where)
			var want []string
			for i := 1; i <= 301; i++ {
				if i == 4 || !tc.match(i) {
					continue
				}
				exact, payload := "9007199254740993", fmt.Sprintf("row-%d", i)
				if i == 2 {
					exact = "9007199254740995"
				}
				if i == 301 {
					exact, payload = "NULL", "later"
				}
				want = append(want, fmt.Sprintf("%d|%s|%s|NULL", i, exact, payload))
			}
			if got := snapshotResultStrings(result.rows); !slices.Equal(got, want) {
				t.Fatal("implicit-order visibility or exact values differ", len(got), len(want))
			}
			if guard.aggregates.Load() != 0 {
				t.Fatal("implicit row-ID order opened redundant Search reducers")
			}
			if tc.name != "indexed" && guard.directories.Load() == 0 {
				t.Fatal("unfiltered/residual scan did not use exact directories")
			}
		})
	}
}
