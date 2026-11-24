// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotPageReplies struct {
	snapshotHook
	segments map[string]snapshotSegment
	rows     map[string]aggRow
	matching map[string]bool
	searches [][]any
	mutate   func(goredis.Cmder) error
}

func pageReadyInfo(seg snapshotSegment) map[string]any {
	return map[string]any{"index_definition": map[string]any{"key_type": "HASH", "prefixes": []any{seg.Prefix}}, "num_docs": seg.Rows, "indexing": 0, "percent_indexed": 1, "hash_indexing_failures": 0}
}
func (h *snapshotPageReplies) reply(cmd goredis.Cmder) error {
	switch cmd.Name() {
	case "fcall":
		cmd.(onceCmd).SetVal(int64(1))
	case "hmget":
		row := h.rows[fmt.Sprint(cmd.Args()[1])]
		vals := make([]any, len(cmd.Args())-2)
		for i, field := range cmd.Args()[2:] {
			if v, ok := row[fmt.Sprint(field)]; ok {
				vals[i] = v
			}
		}
		cmd.(*goredis.SliceCmd).SetVal(vals)
	case "ft.info":
		cmd.(onceCmd).SetVal(pageReadyInfo(h.segments[fmt.Sprint(cmd.Args()[1])]))
	case "zcard":
		key := fmt.Sprint(cmd.Args()[1])
		for _, seg := range h.segments {
			if key == snapshotMembers(snapshotDataID(seg.Prefix)) {
				cmd.(*goredis.IntCmd).SetVal(seg.Rows)
				break
			}
		}
	case "zmscore":
		values := make([]any, len(cmd.Args())-2)
		dir := fmt.Sprint(cmd.Args()[1])
		for _, seg := range h.segments {
			if dir == snapshotMembers(snapshotDataID(seg.Prefix)) {
				for i, id := range cmd.Args()[2:] {
					n, _ := strconv.ParseInt(fmt.Sprint(id), 10, 64)
					if h.rows[seg.Prefix+fmt.Sprint(n)] != nil {
						values[i] = float64(0)
					}
				}
			}
		}
		cmd.(*goredis.Cmd).SetVal(values)
	case "hget":
		row := h.rows[fmt.Sprint(cmd.Args()[1])]
		v, ok := row[fmt.Sprint(cmd.Args()[2])]
		if !ok {
			cmd.SetErr(goredis.Nil)
			return goredis.Nil
		}
		cmd.(*goredis.StringCmd).SetVal(v)
	case "ft.search":
		args := cmd.Args()
		h.searches = append(h.searches, append([]any(nil), args...))
		if fmt.Sprint(args[3]) != "INKEYS" {
			limit := args[6].(int)
			var keys []string
			seg := h.segments[fmt.Sprint(args[1])]
			for key, match := range h.matching {
				if match && strings.HasPrefix(key, seg.Prefix) {
					keys = append(keys, key)
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(keys)))
			result := []any{int64(len(keys))}
			for _, key := range keys[:min(limit, len(keys))] {
				result = append(result, key)
			}
			cmd.(onceCmd).SetVal(result)
			break
		}
		n := args[4].(int)
		result := []any{int64(0)}
		// Deliberately reverse Search order; directory order must win.
		for i := n - 1; i >= 0; i-- {
			key := fmt.Sprint(args[5+i])
			if h.matching[key] {
				result = append(result, key)
			}
		}
		result[0] = int64(len(result) - 1)
		cmd.(onceCmd).SetVal(result)
	default:
		return fmt.Errorf("unexpected page-filter command %s", cmd.Name())
	}
	if h.mutate != nil {
		return h.mutate(cmd)
	}
	return nil
}
func (h *snapshotPageReplies) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		err := h.reply(cmd)
		if err != nil {
			cmd.SetErr(err)
		}
		return err
	}
}
func (h *snapshotPageReplies) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		var errs []error
		for _, cmd := range cmds {
			if err := h.reply(cmd); err != nil {
				cmd.SetErr(err)
				errs = append(errs, err)
			}
		}
		if len(errs) == 1 {
			return errs[0]
		}
		return errors.Join(errs...)
	}
}
func snapshotPageFixture(t *testing.T) (*snapshotDeltaScan, context.Context, *snapshotPageReplies) {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1"})
	t.Cleanup(func() { _ = client.Close() })
	s := &store{client: client, snapshots: true, flex: true}
	access := snapshotID()
	id := snapshotID()
	n := int64(3)
	seg := snapshotSegment{Prefix: snapshotRows(id, defaultSchema, access), Index: snapshotIndexPrefix(id) + "working", Rows: n}
	m := &tableMeta{Schema: defaultSchema, SnapshotAccessID: access, KeyPrefix: seg.Prefix, IndexName: seg.Index, SnapshotRows: &n, PlainIndex: true}
	ctx := context.WithValue(context.Background(), snapshotContextKey{}, &snapshotScope{store: s, pin: &snapshotPin{store: s}})
	h := &snapshotPageReplies{segments: map[string]snapshotSegment{seg.Index: seg}, rows: map[string]aggRow{}, matching: map[string]bool{}}
	client.AddHook(h)
	d := newSnapshotDeltaScan(s, m, []string{"value"})
	d.sources[0].ids = []string{fmt.Sprintf("%020d", 1), fmt.Sprintf("%020d", 2), fmt.Sprintf("%020d", 3)}
	d.sources[0].done = true
	for i := 1; i <= 3; i++ {
		h.rows[seg.Prefix+fmt.Sprint(i)] = aggRow{rowIDField: fmt.Sprint(i), "value": "kept"}
	}
	d.pageFilter = newSnapshotPageFilter(ctx, s, m, "@value:{kept}")
	if d.pageFilter == nil {
		t.Fatal("eligible published Flex source rejected")
	}
	return d, ctx, h
}
func TestSnapshotPageWinnerRefillAndOrder(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	d.pageRows = 1
	h.matching[d.meta.prefix()+"2"] = true
	logical, physical, err := d.nextKeys(ctx)
	if err != nil || !reflect.DeepEqual(logical, []string{d.meta.prefix() + "2"}) || !reflect.DeepEqual(logical, physical) {
		t.Fatalf("refill=%v,%v,%v", logical, physical, err)
	}
	if len(h.searches) != 2 {
		t.Fatalf("empty first page stopped early: %v", h.searches)
	}
	if _, _, err = d.nextKeys(ctx); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
	d, ctx, h = snapshotPageFixture(t)
	for key := range h.rows {
		h.matching[key] = true
	}
	logical, _, err = d.nextKeys(ctx)
	if err != nil || !reflect.DeepEqual(logical, []string{d.meta.prefix() + "1", d.meta.prefix() + "2", d.meta.prefix() + "3"}) {
		t.Fatalf("Search reordered winners: %v %v", logical, err)
	}
	args := h.searches[0]
	if fmt.Sprint(args[3]) != "INKEYS" || args[4] != 3 || strings.Contains(fmt.Sprint(args), "SORTBY") || strings.Contains(fmt.Sprint(args), "CURSOR") {
		t.Fatalf("not bounded plain Search: %v", args)
	}
}
func TestSnapshotPageFilteredNewerWinnerShadowsOlder(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprint(deleted), func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			old := d.sources[0].segment
			id := snapshotID()
			newer := snapshotSegment{Prefix: snapshotRows(id, defaultSchema, d.meta.SnapshotAccessID), Index: snapshotIndexPrefix(id) + "working", Rows: 1}
			h.segments[newer.Index] = newer
			d.meta.SnapshotOverlay = true
			d.meta.SnapshotSegments = []snapshotSegment{old}
			d.meta.KeyPrefix = newer.Prefix
			d.meta.IndexName = newer.Index
			n := int64(1)
			d.meta.SnapshotRows = &n
			d.sources = append(d.sources, snapshotDeltaSource{segment: newer, ids: []string{fmt.Sprintf("%020d", 1)}, done: true})
			row := aggRow{rowIDField: "1"}
			if deleted {
				row[snapshotDeletedField] = "1"
			}
			h.rows[newer.Prefix+"1"] = row
			h.matching[old.Prefix+"1"] = true
			d.pageFilter = newSnapshotPageFilter(ctx, d.store, d.meta, "@value:{kept}")
			if d.pageFilter == nil {
				t.Fatal("overlay rejected")
			}
			if logical, _, err := d.nextKeys(ctx); err != io.EOF || len(logical) != 0 {
				t.Fatalf("older match leaked: %v %v", logical, err)
			}
		})
	}
}
func TestSnapshotPageRejectsMissingNonmatch(t *testing.T) {
	for _, bad := range []aggRow{nil, {rowIDField: "999"}} {
		d, ctx, h := snapshotPageFixture(t)
		h.rows[d.meta.prefix()+"1"] = bad
		if _, _, err := d.nextKeys(ctx); err == nil {
			t.Fatal("corrupt nonmatch silently filtered")
		}
		if len(h.searches) != 0 {
			t.Fatal("Search preceded authoritative row checks")
		}
	}
}
func TestSnapshotPageMembershipStrict(t *testing.T) {
	cases := map[string]any{
		"truncated": []any{int64(2), "a"}, "outside": []any{int64(1), "outside"}, "duplicate": []any{int64(2), "a", "a"},
		"negative": []any{int64(-1)}, "warning": map[string]any{"total_results": int64(0), "results": []any{}, "warnings": []any{"Timeout limit was reached"}},
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := snapshotPageMembership(reply, []string{"a", "b"}); err == nil {
				t.Fatal("accepted incomplete/invalid reply")
			}
		})
	}
	if got, err := snapshotPageMembership([]any{int64(2), "b", "a"}, []string{"a", "b"}); err != nil || len(got) != 2 {
		t.Fatalf("valid membership: %v %v", got, err)
	}
}
func TestSnapshotPageReadinessStrict(t *testing.T) {
	seg := snapshotSegment{Prefix: "owned:", Rows: 3}
	for _, field := range []string{"num_docs", "hash_indexing_failures", "indexing", "percent_indexed", "prefix", "key_type", "filter"} {
		t.Run(field, func(t *testing.T) {
			info := pageReadyInfo(seg)
			switch field {
			case "prefix":
				info["index_definition"].(map[string]any)["prefixes"] = []any{"other:"}
			case "key_type":
				info["index_definition"].(map[string]any)["key_type"] = "JSON"
			case "filter":
				info["index_definition"].(map[string]any)["filter"] = "@n>0"
			default:
				delete(info, field)
			}
			if err := checkSnapshotPageIndex(info, seg); err == nil {
				t.Fatal("accepted invalid proof")
			}
		})
	}
	if err := checkSnapshotPageIndex(pageReadyInfo(seg), seg); err != nil {
		t.Fatal(err)
	}
}
func TestSnapshotPageEligibilityFallback(t *testing.T) {
	for _, kind := range []string{"star", "private", "unmanaged", "live", "not_plain", "no_pin", "ram", "wrong_prefix", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			d, ctx, _ := snapshotPageFixture(t)
			query := "@value:{kept}"
			switch kind {
			case "star":
				query = "*"
			case "private":
				snapshotOf(ctx).write = true
				snapshotOf(ctx).owned = map[string]bool{snapshotDataID(d.meta.prefix()): true}
			case "unmanaged":
				d.meta.Unmanaged = true
			case "live":
				d.meta.Live = true
			case "not_plain":
				d.meta.PlainIndex = false
			case "no_pin":
				snapshotOf(ctx).pin = nil
			case "ram":
				d.store.flex = false
			case "wrong_prefix":
				d.meta.KeyPrefix = "other:"
			case "cleanup":
				d.meta.PendingCleanup = []string{"old"}
			}
			if newSnapshotPageFilter(ctx, d.store, d.meta, query) != nil {
				t.Fatal("unsafe source did not retain directory fallback")
			}
		})
	}
}
func TestSnapshotPageFailsClosedOnTransportAndCancellation(t *testing.T) {
	for _, failure := range []string{"transport", "permission", "partial", "rebuilding", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			d, ctx, h := snapshotPageFixture(t)
			h.mutate = func(cmd goredis.Cmder) error {
				if cmd.Name() != "ft.search" {
					return nil
				}
				switch failure {
				case "transport":
					return errors.New("reply lost")
				case "permission":
					return errors.New("NOPERM denied")
				case "partial":
					cmd.(onceCmd).SetVal([]any{int64(2)})
				case "rebuilding":
					h.mutate = func(cmd goredis.Cmder) error {
						if cmd.Name() == "ft.info" {
							v := pageReadyInfo(d.sources[0].segment)
							v["indexing"] = 1
							cmd.(onceCmd).SetVal(v)
						}
						return nil
					}
				}
				return nil
			}
			if failure == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, _, err := d.nextKeys(ctx); err == nil {
				t.Fatal("failure silently fell back or omitted rows")
			}
		})
	}
}

func TestSnapshotPageControlCompositeError(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	h.mutate = func(cmd goredis.Cmder) error {
		if cmd.Name() == "hmget" {
			return errors.Join(goredis.Nil, errors.New("control reply lost"))
		}
		return nil
	}
	if _, _, err := d.nextKeys(ctx); err == nil {
		t.Fatal("composed control reply loss was ignored")
	}
	if len(h.searches) != 0 {
		t.Fatal("unproven controls reached Search")
	}
}

func TestSnapshotPageChunksAreBounded(t *testing.T) {
	d, ctx, h := snapshotPageFixture(t)
	d.pageRows = snapshotDeltaPage + 1
	d.sources[0].ids = nil
	for i := 1; i <= d.pageRows; i++ {
		id := fmt.Sprint(i)
		key := d.meta.prefix() + id
		d.sources[0].ids = append(d.sources[0].ids, fmt.Sprintf("%020d", i))
		h.rows[key] = aggRow{rowIDField: id}
		h.matching[key] = true
	}
	logical, _, err := d.nextKeys(ctx)
	if err != nil || len(logical) != snapshotDeltaPage+1 {
		t.Fatalf("chunked result len=%d err=%v", len(logical), err)
	}
	if len(h.searches) != 2 || h.searches[0][4] != snapshotDeltaPage || h.searches[1][4] != 1 {
		t.Fatalf("unbounded keys: %v", h.searches)
	}
}

func TestSnapshotPageOpenScanPreservesResidualAndPhysicalFields(t *testing.T) {
	for _, name := range []string{"renamed", "alias.renamed"} {
		t.Run(name, func(t *testing.T) {
			d, ctx, _ := snapshotPageFixture(t)
			d.meta.Columns = []columnMeta{{Name: name, Field: "original_field", Type: typeInt64, Indexed: true, PlainIndex: true}}
			predicate := &Binary{Op: ">", L: &ColumnRef{Name: name}, R: &Literal{V: Value{T: typeInt64, I: 7}}}
			e := &executor{store: d.store}
			wp, err := e.planWhere(ctx, predicate, d.meta, nil)
			if err != nil || wp.query == "*" || !strings.Contains(wp.query, "original_field") {
				t.Fatalf("physical source query=%q %v", wp.query, err)
			}
			it, err := e.openScan(ctx, scanRequest{meta: d.meta, where: wp}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if it.residual != predicate || it.candidates == nil || it.candidates.query != wp.query {
				t.Fatal("full source predicate or bounded filter was lost")
			}
			if !reflect.DeepEqual(it.fetch, []string{"original_field"}) {
				t.Fatalf("logical name used for HASH projection: %v", it.fetch)
			}
			// Flex overlay planning stays conservative. This change does not enable
			// the RAM numeric-range candidate scanner or bypass private readiness.
			d.meta.SnapshotOverlay = true
			overlay, err := e.planWhere(ctx, predicate, d.meta, nil)
			if err != nil || overlay.query != "*" || overlay.residual != predicate || e.snapshotIndexable(ctx, d.meta) {
				t.Fatalf("overlay guard changed: %+v %v", overlay, err)
			}
		})
	}
}
