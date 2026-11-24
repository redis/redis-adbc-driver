// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"errors"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestFlexCompleteCandidatesUsePlainCompleteSearch(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	for key := range h.rows {
		h.matching[key] = true
	}
	c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", []string{"value"}, nil)
	if err != nil || c == nil {
		t.Fatalf("open: %v %v", c, err)
	}
	logical, physical, err := c.nextKeys(ctx)
	want := []string{d.meta.prefix() + "1", d.meta.prefix() + "2", d.meta.prefix() + "3"}
	if err != nil || !reflect.DeepEqual(logical, want) || !reflect.DeepEqual(physical, want) {
		t.Fatalf("directory ordered result: %v %v %v", logical, physical, err)
	}
	if _, _, err := c.nextKeys(ctx); err != io.EOF {
		t.Fatalf("not finite: %v", err)
	}
	if len(h.searches) != 2 || h.searches[0][6] != 0 || h.searches[1][6] != 3 {
		t.Fatalf("not count0/complete3: %v", h.searches)
	}
	for _, args := range h.searches {
		text := fmt.Sprint(args)
		for _, bad := range []string{"SORTBY", "CURSOR", "INKEYS", "__rowid"} {
			if strings.Contains(text, bad) {
				t.Fatalf("unsupported command shape: %v", args)
			}
		}
	}
}

func TestFlexCompleteCandidatesCapFallsBackBeforeFetch(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	n := int64(snapshotFlexCandidateCap + 1)
	d.meta.SnapshotRows = &n
	seg := d.meta.snapshotSegments()[0]
	h.segments[seg.Index] = seg
	h.mutate = func(cmd goredis.Cmder) error {
		if cmd.Name() == "ft.search" {
			cmd.(onceCmd).SetVal([]any{n})
		}
		return nil
	}
	c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, nil)
	if err != nil || c != nil || len(h.searches) != 1 {
		t.Fatalf("cap fallback: %v %v %v", c, err, h.searches)
	}
	e := &executor{store: d.store}
	it, err := e.openScan(ctx, scanRequest{meta: d.meta, where: wherePlan{query: "@value:{kept}"}}, nil)
	if err != nil || it.delta == nil || it.delta.pageFilter != nil || it.candidates != nil {
		t.Fatalf("not original directory fallback: %+v %v", it, err)
	}
}

func TestFlexCompleteCandidatesRejectBadOwnMembership(t *testing.T) {
	for _, score := range []any{nil, float64(1), "garbage"} {
		t.Run(fmt.Sprint(score), func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			h.matching[d.meta.prefix()+"1"] = true
			h.mutate = func(cmd goredis.Cmder) error {
				if cmd.Name() == "zmscore" {
					cmd.(*goredis.Cmd).SetVal([]any{score})
				}
				return nil
			}
			if c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, nil); err == nil || c != nil {
				t.Fatal("Search falsely defined own-source membership")
			}
		})
	}
}

func TestFlexCompleteCandidatesExactLargeIDsAndBounds(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	h.rows = map[string]aggRow{}
	h.matching = map[string]bool{}
	for _, id := range []string{"2", "9007199254740993", "9223372036854775807"} {
		key := d.meta.prefix() + id
		h.rows[key] = aggRow{rowIDField: id}
		h.matching[key] = true
	}
	c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, &rowIDRangeScan{lo: 9007199254740993, hi: 9223372036854775807})
	if err != nil {
		t.Fatal(err)
	}
	logical, _, err := c.nextKeys(ctx)
	if err != nil || !reflect.DeepEqual(logical, []string{d.meta.prefix() + "9007199254740993", d.meta.prefix() + "9223372036854775807"}) {
		t.Fatalf("rounded or unbounded: %v %v", logical, err)
	}
}

func TestFlexCompleteCandidateReplyIntegrity(t *testing.T) {
	seg := snapshotSegment{Prefix: "owned:"}
	for name, reply := range map[string]any{"truncated": []any{int64(2), "owned:1"}, "foreign": []any{int64(1), "foreign:1"}, "duplicate": []any{int64(2), "owned:1", "owned:1"}, "noncanonical": []any{int64(1), "owned:01"}, "overflow": []any{int64(1), "owned:9223372036854775808"}, "negative": []any{int64(1), "owned:-1"}, "warning": map[string]any{"total_results": int64(1), "results": []any{map[string]any{"id": "owned:1"}}, "warning": "timeout"}} {
		t.Run(name, func(t *testing.T) {
			n := int64(1)
			if name == "duplicate" || name == "truncated" {
				n = 2
			}
			if _, err := completeSnapshotIDs(reply, seg, n); err == nil {
				t.Fatal("accepted invalid complete reply")
			}
		})
	}
}

type completeLimitError string

func (e completeLimitError) Error() string { return string(e) }
func (completeLimitError) RedisError()     {}
func TestFlexCompleteCandidateLimitRefusalIsConservative(t *testing.T) {
	native := completeLimitError("LIMIT exceeds maximum of 1000")
	if !completeSnapshotLimit(native) || completeSnapshotLimit(fmt.Errorf("wrapped: %w", native)) || completeSnapshotLimit(errors.Join(native, errors.New("reply lost"))) || completeSnapshotLimit(errors.New(string(native))) {
		t.Fatal("unsafe native capability classification")
	}
}

func TestFlexCompleteCandidatesResolveNewerAndTombstone(t *testing.T) {
	for _, kind := range []string{"nonmatch", "tombstone", "match", "missing_newer", "bad_newer_score", "header_error"} {
		t.Run(kind, func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			old := d.meta.snapshotSegments()[0]
			id := snapshotID()
			newer := snapshotSegment{Prefix: snapshotRows(id, defaultSchema, d.meta.SnapshotAccessID), Index: snapshotIndexPrefix(id) + "working", Rows: 1}
			h.segments[newer.Index] = newer
			d.meta.SnapshotOverlay = true
			d.meta.SnapshotSegments = []snapshotSegment{old}
			d.meta.KeyPrefix = newer.Prefix
			d.meta.IndexName = newer.Index
			n := int64(1)
			d.meta.SnapshotRows = &n
			h.rows[newer.Prefix+"1"] = aggRow{rowIDField: "1", "value": "newer"}
			h.matching[old.Prefix+"1"] = true
			if kind == "match" {
				h.matching[newer.Prefix+"1"] = true
			}
			if kind == "tombstone" {
				h.rows[newer.Prefix+"1"][snapshotDeletedField] = "1"
				h.matching[newer.Prefix+"1"] = true
			}
			c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", []string{"value"}, nil)
			if err != nil || c == nil {
				t.Fatalf("open: %v %v", c, err)
			}
			if kind == "missing_newer" {
				delete(h.rows, newer.Prefix+"1")
			}
			if kind == "missing_newer" || kind == "bad_newer_score" {
				h.mutate = func(cmd goredis.Cmder) error {
					if cmd.Name() == "zmscore" && fmt.Sprint(cmd.Args()[1]) == snapshotMembers(snapshotDataID(newer.Prefix)) {
						score := float64(0)
						if kind == "bad_newer_score" {
							score = 1
						}
						cmd.(*goredis.Cmd).SetVal([]any{score})
					}
					return nil
				}
			}
			if kind == "header_error" {
				h.mutate = func(cmd goredis.Cmder) error {
					if cmd.Name() == "hget" {
						cmd.SetErr(errors.New("NOPERM shadowing header"))
					}
					return nil
				}
			}
			var got []string
			for {
				keys, _, nextErr := c.next(ctx)
				if nextErr == io.EOF {
					break
				}
				if nextErr != nil {
					if kind == "missing_newer" || kind == "bad_newer_score" || kind == "header_error" {
						return
					}
					t.Fatal(nextErr)
				}
				got = append(got, keys...)
			}
			if kind == "missing_newer" || kind == "bad_newer_score" || kind == "header_error" {
				t.Fatal("invalid newer winner hidden")
			}
			if kind == "match" {
				if !reflect.DeepEqual(got, []string{newer.Prefix + "1"}) {
					t.Fatalf("wrong newer winner: %v", got)
				}
			} else if len(got) != 0 {
				t.Fatalf("old candidate leaked through newer %s: %v", kind, got)
			}
		})
	}
}

func TestFlexCompleteCandidateTransportFailureDoesNotFallback(t *testing.T) {
	for _, phase := range []string{"count", "fetch", "own"} {
		t.Run(phase, func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			h.matching[d.meta.prefix()+"1"] = true
			h.mutate = func(cmd goredis.Cmder) error {
				if cmd.Name() == "ft.search" && ((phase == "count" && cmd.Args()[6] == 0) || (phase == "fetch" && cmd.Args()[6] != 0)) {
					return errors.Join(completeLimitError("LIMIT exceeds maximum of 1000"), errors.New("reply lost"))
				}
				if phase == "own" && cmd.Name() == "zmscore" {
					return errors.Join(goredis.Nil, errors.New("NOPERM or reply lost"))
				}
				return nil
			}
			if c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, nil); err == nil || c != nil {
				t.Fatal("ambiguous failure converted into fallback")
			}
		})
	}
}

func TestFlexCompleteCandidateDirectLimitFallback(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	h.matching[d.meta.prefix()+"1"] = true
	h.mutate = func(cmd goredis.Cmder) error {
		if cmd.Name() == "ft.search" && cmd.Args()[6] != 0 {
			return completeLimitError("LIMIT exceeds maximum of 0")
		}
		return nil
	}
	c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, nil)
	if err != nil || c != nil || len(h.searches) != 2 {
		t.Fatalf("confirmed native limit should retain directory fallback: %v %v", c, err)
	}
}

func TestFlexCompleteCandidateByteCapPrecedesFetch(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	d.meta.Schema = strings.Repeat("s", 512)
	id := snapshotID()
	d.meta.KeyPrefix = snapshotRows(id, d.meta.Schema, d.meta.SnapshotAccessID)
	d.meta.IndexName = snapshotIndexPrefix(id) + "working"
	n := int64(snapshotFlexCandidateCap)
	d.meta.SnapshotRows = &n
	seg := d.meta.snapshotSegments()[0]
	h.segments = map[string]snapshotSegment{seg.Index: seg}
	h.mutate = func(cmd goredis.Cmder) error {
		if cmd.Name() == "ft.search" {
			cmd.(onceCmd).SetVal([]any{n})
		}
		return nil
	}
	c, err := openFlexSnapshotCandidates(ctx, d.store, d.meta, "@value:{kept}", nil, nil)
	if err != nil || c != nil || len(h.searches) != 1 {
		t.Fatalf("key-byte cap should precede full reply: %v %v", c, err)
	}
}
