// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Keep this review fixture independent of the single-segment reply adapter's
// directory implementation. Native HASH and lease replies remain unchanged.
type cohortSegmentReviewReplies struct {
	*scalarCohortReplies
	directories map[string][]string
	fetched     map[string]int
}

func (h *cohortSegmentReviewReplies) reply(cmd goredis.Cmder) error {
	if cmd.Name() == "zrange" {
		h.reads++
		ids, ok := h.directories[fmt.Sprint(cmd.Args()[1])]
		if !ok {
			return fmt.Errorf("unexpected review source directory")
		}
		start, _ := strconv.Atoi(fmt.Sprint(cmd.Args()[2]))
		end, _ := strconv.Atoi(fmt.Sprint(cmd.Args()[3]))
		start = min(start, len(ids))
		end = min(end+1, len(ids))
		cmd.(*goredis.StringSliceCmd).SetVal(append([]string(nil), ids[start:end]...))
		return nil
	}
	if cmd.Name() == "hmget" {
		h.fetched[fmt.Sprint(cmd.Args()[1])]++
	}
	return h.scalarCohortReplies.reply(cmd)
}
func (h *cohortSegmentReviewReplies) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		err := h.reply(cmd)
		if err != nil {
			cmd.SetErr(err)
		}
		return err
	}
}
func (h *cohortSegmentReviewReplies) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.reply(cmd); err != nil {
				cmd.SetErr(err)
				return err
			}
		}
		return nil
	}
}

func TestScalarCohortReviewNewerVersionsAndTombstones(t *testing.T) {
	for _, kind := range []string{"visible_winners", "missing_newer_identity"} {
		t.Run(kind, func(t *testing.T) {
			e, ctx, base, m := cohortFixture(t, 1)
			old := m.snapshotSegments()[0]
			id := snapshotID()
			count := int64(2)
			newer := snapshotSegment{Prefix: snapshotRows(id, m.Schema, m.SnapshotAccessID), Index: snapshotIndexPrefix(id) + "working", Rows: count}
			m.SnapshotSegments = []snapshotSegment{old}
			m.KeyPrefix = newer.Prefix
			m.IndexName = newer.Index
			m.SnapshotRows = &count
			// Malformed shadowed versions must never become lane input. A malformed
			// metric in a winning tombstone also cannot be decoded as a live fact.
			base.rows[old.Prefix+"1"]["qty"] = "broken-old-value"
			base.rows[old.Prefix+"1"]["old_good"] = "broken-old-metric"
			base.rows[newer.Prefix+"1"] = aggRow{rowIDField: "1", "qty": "4", "old_good": "20.00", "bad": "5.00"}
			base.rows[newer.Prefix+"2"] = aggRow{rowIDField: "2", snapshotDeletedField: "1", "qty": "broken-deleted-value", "old_good": "broken-deleted-metric"}
			if kind == "missing_newer_identity" {
				delete(base.rows[newer.Prefix+"1"], rowIDField)
			}
			replies := &cohortSegmentReviewReplies{scalarCohortReplies: base, directories: map[string][]string{}, fetched: map[string]int{}}
			for _, seg := range []snapshotSegment{old, newer} {
				var ids []string
				for key := range base.rows {
					if strings.HasPrefix(key, seg.Prefix) {
						n, _ := strconv.ParseInt(strings.TrimPrefix(key, seg.Prefix), 10, 64)
						ids = append(ids, fmt.Sprintf("%020d", n))
					}
				}
				sort.Strings(ids)
				replies.directories[snapshotMembers(snapshotDataID(seg.Prefix))] = ids
			}
			client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
			t.Cleanup(func() { _ = client.Close() })
			client.AddHook(replies)
			e.store.client = client
			p := cohortPlan(t, e, ctx, cohortParsedCase)
			c := oneCohort(t, e)
			if c.started || replies.reads != 0 {
				t.Fatal("registration read multisegment source")
			}
			rows, err := e.runSelect(ctx, p, nil)
			if kind == "missing_newer_identity" {
				if err == nil || len(e.cache.sub) != 0 {
					t.Fatalf("invalid newer winner was hidden or cached: %v %v", rows, err)
				}
				return
			}
			if err != nil || len(rows) != 1 || rows[0][0].Text() != "13.7500000000000000" {
				t.Fatalf("wrong visible AVG: %v %v", rows, err)
			}
			if c.scans != 1 || replies.reads != 2 || replies.fetches != 3 {
				t.Fatalf("not one two-source exact scan: %d/%d/%d", c.scans, replies.reads, replies.fetches)
			}
			if replies.fetched[old.Prefix+"1"] != 0 || replies.fetched[old.Prefix+"2"] != 0 || replies.fetched[newer.Prefix+"1"] != 1 || replies.fetched[newer.Prefix+"2"] != 1 || replies.fetched[old.Prefix+"3"] != 1 {
				t.Fatalf("wrong winning HASH selection: %v", replies.fetched)
			}
			for _, lane := range c.lanes {
				if lane.fn.Name == "COUNT" && lane.acc.count != 2 {
					t.Fatalf("COUNT included replaced/deleted row: %d", lane.acc.count)
				}
				if lane.input == "bad" && (lane.err != nil || lane.finalized) {
					t.Fatalf("shadowed/deleted malformed inputs affected unused AVG: %v", lane.err)
				}
			}
		})
	}
}
