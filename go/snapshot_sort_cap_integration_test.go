// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	goredis "github.com/redis/go-redis/v9"
)

type snapshotSortCapHook struct {
	snapshotHook
	probes, refusedData int
	data                int
	native              bool
}

func (h *snapshotSortCapHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		if c.Name() == "ft.aggregate" {
			a := c.Args()
			probe := len(a) > 4 && fmt.Sprint(a[3]) == "FILTER" && fmt.Sprint(a[4]) == "0"
			if probe {
				h.probes++
			} else {
				h.data++
			}
			if h.native {
				return next(ctx, c)
			}
			for i := 3; i < len(a); i++ {
				at := 0
				switch strings.ToUpper(fmt.Sprint(a[i])) {
				case "MAX":
					at = i + 1
				case "LIMIT":
					at = i + 2
				}
				if at > 0 && at < len(a) {
					n, _ := strconv.ParseInt(fmt.Sprint(a[at]), 10, 64)
					if n > 137 {
						if !probe {
							h.refusedData++
						}
						err := controlScriptNativeError("SEARCH_LIMIT_OVER LIMIT exceeds maximum of 137")
						c.SetErr(err)
						return err
					}
				}
			}
		}
		return next(ctx, c)
	}
}

func TestSnapshotExplicitOrderHiddenAggregateCap(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER NOT NULL,exact BIGINT,payload VARCHAR,spare VARCHAR)")
	h.exec("INSERT INTO t SELECT i,9007199254740993+i%3,'row-'||i,NULL FROM generate_series(1,300) AS g(i)")
	t.Run("native empty probe", func(t *testing.T) {
		s := snapshotTestStore(t, raw)
		s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
		guard := &snapshotSortCapHook{native: true}
		s.client.AddHook(guard)
		res := snapshotExecuteTest(t, s, h.ctx, "SELECT id FROM t ORDER BY id DESC LIMIT 10")
		if got := snapshotResultStrings(res.rows); !slices.Equal(got, []string{"300", "299", "298", "297", "296", "295", "294", "293", "292", "291"}) {
			t.Fatal(got)
		}
		if guard.probes != 1 || guard.data == 0 {
			t.Fatalf("probes=%d data=%d", guard.probes, guard.data)
		}
	})
	t.Run("single baseline", func(t *testing.T) {
		s := snapshotTestStore(t, raw)
		s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
		if os.Getenv("REDIS_TEST_NATIVE_HIDDEN_CAP") != "137" {
			s.client.AddHook(&snapshotSortCapHook{})
		}
		res := snapshotExecuteTest(t, s, h.ctx, "SELECT id FROM t ORDER BY id DESC")
		var want []string
		for i := 300; i > 0; i-- {
			want = append(want, strconv.Itoa(i))
		}
		if got := snapshotResultStrings(res.rows); !slices.Equal(got, want) {
			t.Fatal("baseline sort differs", len(got), len(want))
		}
	})
	h.exec("UPDATE t SET exact=9007199254740999,payload='new' WHERE id=2")
	h.exec("DELETE FROM t WHERE id=4")
	h.exec("CREATE SEQUENCE calls CACHE 1")
	for _, tc := range []struct {
		name, clause  string
		match         func(int) bool
		less          func(int, int) bool
		offset, limit int
	}{
		{"unbounded", "ORDER BY id DESC", func(int) bool { return true }, func(a, b int) bool { return a > b }, 0, -1},
		{"limited", "ORDER BY id DESC LIMIT 200 OFFSET 5", func(int) bool { return true }, func(a, b int) bool { return a > b }, 5, 200},
		{"small bounded", "ORDER BY id DESC LIMIT 10 OFFSET 5", func(int) bool { return true }, func(a, b int) bool { return a > b }, 5, 10},
		{"residual", "WHERE id%3=0 ORDER BY id DESC LIMIT 70 OFFSET 2", func(i int) bool { return i%3 == 0 }, func(a, b int) bool { return a > b }, 2, 70},
		{"rounded ties", "ORDER BY exact DESC,id ASC LIMIT 10", func(int) bool { return true }, func(a, b int) bool {
			av, bv := int64(9007199254740993+a%3), int64(9007199254740993+b%3)
			if a == 2 {
				av = 9007199254740999
			}
			if b == 2 {
				bv = 9007199254740999
			}
			if av != bv {
				return av > bv
			}
			return a < b
		}, 0, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := snapshotTestStore(t, raw)
			// Model a named reader's unavailable CONFIG without changing the
			// published rows/indexes or the writer's connection.
			s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
			guard := &snapshotSortCapHook{}
			if os.Getenv("REDIS_TEST_NATIVE_HIDDEN_CAP") != "137" {
				s.client.AddHook(guard)
			}
			res := snapshotExecuteTest(t, s, h.ctx, "SELECT id,exact,payload,spare FROM t "+tc.clause)
			var ids []int
			for i := 1; i <= 300; i++ {
				if i != 4 && tc.match(i) {
					ids = append(ids, i)
				}
			}
			slices.SortFunc(ids, func(a, b int) int {
				if tc.less(a, b) {
					return -1
				}
				if tc.less(b, a) {
					return 1
				}
				return 0
			})
			ids = ids[tc.offset:]
			if tc.limit >= 0 {
				ids = ids[:min(tc.limit, len(ids))]
			}
			var want []string
			for _, i := range ids {
				exact, payload := int64(9007199254740993+i%3), fmt.Sprintf("row-%d", i)
				if i == 2 {
					exact, payload = 9007199254740999, "new"
				}
				want = append(want, fmt.Sprintf("%d|%d|%s|NULL", i, exact, payload))
			}
			if got := snapshotResultStrings(res.rows); !slices.Equal(got, want) {
				t.Fatalf("ordered exact/visible rows differ: got=%d want=%d", len(got), len(want))
			}
			if os.Getenv("REDIS_TEST_NATIVE_HIDDEN_CAP") != "137" && (guard.probes != 1 || guard.refusedData != 0) {
				t.Fatalf("probes=%d refused data queries=%d", guard.probes, guard.refusedData)
			}
			if tc.name == "small bounded" && os.Getenv("REDIS_TEST_NATIVE_HIDDEN_CAP") != "137" && guard.data == 0 {
				t.Fatal("small bounded order lost its index path")
			}
		})
	}
	t.Run("volatile predicate once", func(t *testing.T) {
		s := snapshotTestStore(t, raw)
		s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
		if os.Getenv("REDIS_TEST_NATIVE_HIDDEN_CAP") != "137" {
			s.client.AddHook(&snapshotSortCapHook{})
		}
		res := snapshotExecuteTest(t, s, h.ctx, "SELECT id FROM t WHERE nextval('calls')>0 ORDER BY id DESC LIMIT 10")
		if got := snapshotResultStrings(res.rows); !slices.Equal(got, []string{"300", "299", "298", "297", "296", "295", "294", "293", "292", "291"}) {
			t.Fatal(got)
		}
		h.expectRows("SELECT nextval('calls')", "300") // 299 visible rows, evaluated once each.
	})
	t.Run("required stream refuses before volatility", func(t *testing.T) {
		h.exec("ALTER SEQUENCE calls RESTART WITH 1")
		s := snapshotTestStore(t, raw)
		s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
		if os.Getenv("REDIS_TEST_NATIVE_HIDDEN_CAP") != "137" {
			s.client.AddHook(&snapshotSortCapHook{})
		}
		e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
		ps, err := ParseScript("SELECT id FROM t WHERE nextval('calls')>0 ORDER BY id DESC")
		if err != nil {
			t.Fatal(err)
		}
		res, err := e.execute(h.ctx, ps[0], nil, nil, &streamOpts{required: true, batchRows: 10, mem: memory.NewGoAllocator()})
		if res.stream != nil {
			res.stream.Release()
		}
		var ae adbc.Error
		if !errors.As(err, &ae) || ae.Code != adbc.StatusNotImplemented || !strings.Contains(ae.Msg, "stream_required") {
			t.Fatal(err)
		}
		h.expectRows("SELECT nextval('calls')", "1")
	})
}

type snapshotSortCapErrorHook struct {
	snapshotHook
	err          error
	cancel       context.CancelFunc
	probes, data int
	noRetry      bool
}

func (h *snapshotSortCapErrorHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, c goredis.Cmder) error {
		if c.Name() == "ft.aggregate" {
			a := c.Args()
			if len(a) > 4 && fmt.Sprint(a[3]) == "FILTER" && fmt.Sprint(a[4]) == "0" {
				h.probes++
				marker, ok := c.(interface{ NoRetry() bool })
				h.noRetry = ok && marker.NoRetry()
				if h.cancel != nil {
					h.cancel()
				}
				c.SetErr(h.err)
				return h.err
			}
			h.data++
		}
		return next(ctx, c)
	}
}

func TestSnapshotSortCapacityUnknownStopsBeforeSQL(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER NOT NULL)")
	h.exec("INSERT INTO t SELECT i FROM generate_series(1,300) AS g(i)")
	h.exec("CREATE SEQUENCE calls CACHE 1")
	native := controlScriptNativeError("SEARCH_LIMIT_OVER LIMIT exceeds maximum of 137")
	for _, tc := range []struct {
		name   string
		err    error
		cancel bool
	}{
		{"plain", errors.New(native.Error()), false},
		{"wrapped", fmt.Errorf("transport: %w", native), false},
		{"joined", errors.Join(native, errors.New("lost reply")), false},
		{"EOF", io.EOF, false},
		{"permission", controlScriptNativeError("NOPERM this user has no permissions to run the 'ft.aggregate' command"), false},
		{"canceled refusal", native, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := snapshotTestStore(t, raw)
			s.searchCapsOnce.Do(func() { s.searchCaps = [2]int64{1_000_000, 1 << 31} })
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			guard := &snapshotSortCapErrorHook{err: tc.err}
			if tc.cancel {
				guard.cancel = cancel
			}
			s.client.AddHook(guard)
			e := &executor{store: s, sess: newSession("public"), schema: "public", pushdown: "exact"}
			ps, err := ParseScript("SELECT id FROM t WHERE nextval('calls')>0 ORDER BY id DESC")
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.execute(ctx, ps[0], nil, nil, nil)
			if err == nil {
				t.Fatal("unknown probe outcome executed SQL")
			}
			if guard.probes != 1 || guard.data != 0 || !guard.noRetry {
				t.Fatalf("probes=%d data=%d noRetry=%v", guard.probes, guard.data, guard.noRetry)
			}
		})
	}
	h.expectRows("SELECT nextval('calls')", "1")
}
