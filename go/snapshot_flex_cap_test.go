// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"strings"
	"testing"
)

func TestFlexCompleteExpandedCandidateCapAdmitsBoundedSet(t *testing.T) {
	for _, n := range []int64{78999, snapshotFlexCandidateCap} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			d.meta.SnapshotRows = &n
			seg := d.meta.snapshotSegments()[0]
			h.segments[seg.Index] = seg
			if bytes := n * int64(len(seg.Prefix)+19); bytes > snapshotFlexCandidateBytes {
				t.Fatalf("fixture exceeds independent key budget: %d", bytes)
			}
			h.rows = map[string]aggRow{}
			h.matching = map[string]bool{}
			for i := int64(1); i <= n; i++ {
				id := fmt.Sprint(i)
				key := seg.Prefix + id
				h.rows[key] = aggRow{rowIDField: id}
				h.matching[key] = true
			}
			c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, nil)
			if err != nil || c == nil || len(c.sources) != 1 || int64(len(c.sources[0].ids)) != n {
				t.Fatalf("bounded complete set refused: candidates=%v error=%v", c != nil, err)
			}
		})
	}
}

func TestFlexCompleteCandidateByteBoundaryUnchanged(t *testing.T) {
	for _, above := range []bool{false, true} {
		t.Run(fmt.Sprint(above), func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			d.meta.Schema = strings.Repeat("s", 100)
			id := snapshotID()
			d.meta.KeyPrefix = snapshotRows(id, d.meta.Schema, d.meta.SnapshotAccessID)
			d.meta.IndexName = snapshotIndexPrefix(id) + "working"
			n := int64(snapshotFlexCandidateBytes / (len(d.meta.KeyPrefix) + 19))
			if above {
				n++
			}
			if n > snapshotFlexCandidateCap {
				t.Fatal("fixture must exercise byte cap first")
			}
			d.meta.SnapshotRows = &n
			seg := d.meta.snapshotSegments()[0]
			h.segments = map[string]snapshotSegment{seg.Index: seg}
			reachedFetch := errors.New("test reached complete fetch")
			h.mutate = func(cmd goredis.Cmder) error {
				if cmd.Name() == "ft.search" {
					if cmd.Args()[6].(int) > 0 {
						return reachedFetch
					}
					cmd.(onceCmd).SetVal([]any{n})
				}
				return nil
			}
			c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, nil)
			if above {
				if c != nil || err != nil || len(h.searches) != 1 {
					t.Fatalf("over-byte cap fetched: %v %v %d", c, err, len(h.searches))
				}
			} else if err == nil || !strings.Contains(err.Error(), reachedFetch.Error()) || len(h.searches) != 2 {
				t.Fatalf("within-byte boundary refused before fetch: %v %d", err, len(h.searches))
			}
		})
	}
}
