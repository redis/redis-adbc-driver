// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"math"
	"strings"
	"testing"
)

func TestCompleteFlexOverBudgetCountAndByteBoundaries(t *testing.T) {
	for _, which := range []string{"count_at", "count_above", "bytes_at", "bytes_above"} {
		t.Run(which, func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			if strings.HasPrefix(which, "bytes") {
				d.meta.Schema = strings.Repeat("s", 512)
				id := snapshotID()
				d.meta.KeyPrefix = snapshotRows(id, d.meta.Schema, d.meta.SnapshotAccessID)
				d.meta.IndexName = snapshotIndexPrefix(id) + "working"
			}
			n := int64(snapshotFlexCandidateCap)
			if which == "count_above" {
				n++
			}
			if strings.HasPrefix(which, "bytes") {
				n = int64(snapshotFlexCandidateBytes / (len(d.meta.prefix()) + 19))
				if n+1 > int64(snapshotFlexCandidateCap) {
					t.Fatal("byte boundary fixture must remain below the installed count cap")
				}
				if which == "bytes_above" {
					n++
				}
			}
			d.meta.SnapshotRows = &n
			seg := d.meta.snapshotSegments()[0]
			h.segments = map[string]snapshotSegment{seg.Index: seg}
			h.mutate = func(c goredis.Cmder) error {
				if c.Name() == "ft.search" {
					if c.Args()[6].(int) != 0 {
						t.Error("proof fetched candidates")
					}
					c.(onceCmd).SetVal([]any{n})
				}
				return nil
			}
			over, err := d.store.completeFlexOverBudget(ctx, d.meta, "@value:{kept}")
			if err != nil || over != strings.HasSuffix(which, "above") || len(h.searches) != 1 {
				t.Fatalf("over=%v err=%v searches=%d", over, err, len(h.searches))
			}
		})
	}
}

func TestCompleteFlexOverBudgetFailureProofs(t *testing.T) {
	for _, kind := range []string{"warning", "too_many", "negative", "keys", "ambiguous", "index", "native_limit"} {
		t.Run(kind, func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			h.mutate = func(c goredis.Cmder) error {
				if kind == "index" && c.Name() == "ft.info" {
					seg := d.meta.snapshotSegments()[0]
					seg.Prefix = "foreign:"
					c.(onceCmd).SetVal(pageReadyInfo(seg))
				}
				if c.Name() != "ft.search" {
					return nil
				}
				switch kind {
				case "warning":
					c.(onceCmd).SetVal(map[string]any{"total_results": int64(1), "results": []any{}, "warning": "timeout"})
				case "too_many":
					c.(onceCmd).SetVal([]any{int64(4)})
				case "negative":
					c.(onceCmd).SetVal([]any{int64(-1)})
				case "keys":
					c.(onceCmd).SetVal([]any{int64(1), d.meta.prefix() + "1"})
				case "ambiguous":
					return errors.Join(completeLimitError("LIMIT exceeds maximum of 0"), errors.New("reply lost"))
				case "native_limit":
					return completeLimitError("LIMIT exceeds maximum of 0")
				}
				return nil
			}
			over, err := d.store.completeFlexOverBudget(ctx, d.meta, "@value:{kept}")
			if kind == "native_limit" {
				if over || err != nil {
					t.Fatalf("capability refusal is not an overbudget proof: %v %v", over, err)
				}
			} else if over || err == nil {
				t.Fatalf("invalid proof converted to directory admission: %v %v", over, err)
			}
		})
	}
}

func TestCompleteFlexOverBudgetChecksLaterSources(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	n := int64(snapshotFlexCandidateCap + 1)
	d.meta.SnapshotRows = &n
	old := d.meta.snapshotSegments()[0]
	h.segments[old.Index] = old
	id := snapshotID()
	newer := snapshotSegment{Prefix: snapshotRows(id, d.meta.Schema, d.meta.SnapshotAccessID), Index: snapshotIndexPrefix(id) + "working", Rows: 1}
	d.meta.SnapshotOverlay = true
	d.meta.SnapshotSegments = []snapshotSegment{old}
	d.meta.KeyPrefix = newer.Prefix
	d.meta.IndexName = newer.Index
	one := int64(1)
	d.meta.SnapshotRows = &one
	h.segments[newer.Index] = newer
	h.mutate = func(c goredis.Cmder) error {
		if c.Name() == "ft.search" {
			if fmt.Sprint(c.Args()[1]) == old.Index {
				c.(onceCmd).SetVal([]any{n})
			} else {
				return errors.New("later source count reply lost")
			}
		}
		return nil
	}
	over, err := d.store.completeFlexOverBudget(ctx, d.meta, "@value:{kept}")
	if over || err == nil || !strings.Contains(err.Error(), "later source") || len(h.searches) != 2 {
		t.Fatalf("early source budget skipped later proof: over%v error%v searches%d", over, err, len(h.searches))
	}
}

func TestCompleteSnapshotBudgetSaturatesWithoutOverflow(t *testing.T) {
	total, bytes, over := completeSnapshotBudget("p:", math.MaxInt64, 0, 0)
	if !over || total <= snapshotFlexCandidateCap || bytes <= snapshotFlexCandidateBytes {
		t.Fatal("count overflow not saturated")
	}
	total, bytes, over = completeSnapshotBudget("p:", math.MaxInt64, total, bytes)
	if !over || total <= snapshotFlexCandidateCap || bytes <= snapshotFlexCandidateBytes {
		t.Fatal("continued proof overflowed budget state")
	}
}
