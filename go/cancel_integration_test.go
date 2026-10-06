// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// cancelLag is how soon after its context is cancelled a statement must
// fail. Each query below takes several seconds uncancelled.
const cancelLag = time.Second

// runCancelled runs sql (with update, as ExecuteUpdate) with a context that
// arm cancels: arm gets the cancel function and returns one that disarms it
// once the statement has returned. It returns the error and how long after
// the cancel the statement returned.
func (h *sqlHarness) runCancelled(sql string, update bool, arm func(cancel func()) (disarm func())) (time.Duration, error) {
	h.t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, sql); err != nil {
		h.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	cancelledAt := make(chan time.Time, 1)
	disarm := arm(sync.OnceFunc(func() {
		cancelledAt <- time.Now()
		cancel()
	}))
	if update {
		_, err = st.ExecuteUpdate(ctx)
	} else {
		var rdr array.RecordReader
		if rdr, _, err = st.ExecuteQuery(ctx); err == nil {
			rdr.Release()
		}
	}
	end := time.Now()
	disarm()
	select {
	case at := <-cancelledAt:
		return max(end.Sub(at), 0), err
	default:
		return 0, err // returned before the cancel
	}
}

// cancelAfter cancels a statement once d has passed.
func cancelAfter(d time.Duration) func(cancel func()) func() {
	return func(cancel func()) func() {
		timer := time.AfterFunc(d, cancel)
		return func() { timer.Stop() }
	}
}

// expectCancelled checks that a statement run by runCancelled failed with
// CANCELLED within cancelLag of the cancel.
func expectCancelled(t *testing.T, lag time.Duration, err error) {
	t.Helper()
	if !isStatus(err, adbc.StatusCancelled) || sqlState(err) != stateQueryCanceled {
		t.Fatalf("got %v, want CANCELLED (57014)", err)
	}
	if lag > cancelLag {
		t.Errorf("failed %v after the cancel, want under %v: %v", lag, cancelLag, err)
	}
}

// TestSQLCancel cancels statements part-way through the work the driver
// does in memory, which checks the context in its row loops (cancel.go):
// each fails with CANCELLED within cancelLag of the cancel. Most of the
// queries read no table, so nothing but those checks could stop them.
func TestSQLCancel(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("cancel_small", "cancel_ctas")
	t.Cleanup(func() { h.dropTables("cancel_small", "cancel_ctas") })
	h.exec("create table cancel_small as select g as id from generate_series(1, 50) as s(g)")

	const delay = 100 * time.Millisecond
	for _, c := range []struct {
		name   string
		sql    string
		update bool
	}{
		{name: "where", sql: "select g from generate_series(1, 1000000) as s(g) where length(repeat(cast(g as varchar), 1000)) < 0"},
		{name: "aggregate", sql: "select sum(length(repeat(cast(g as varchar), 2000))) from generate_series(1, 500000) as s(g)"},
		// 15,000 rows in 97 groups: 2.3M pairs.
		{name: "hash join", sql: "with j as (select g, g % 97 as k from generate_series(1, 15000) as s(g)) select count(*) from j as a join j as b on a.k = b.k where a.g + b.g > 0"},
		{name: "nested loop join", sql: "select count(*) from generate_series(1, 3000) as a(g) join generate_series(1, 3000) as b(h) on a.g + b.h < 0"},
		{name: "lateral", sql: "select count(*) from generate_series(1, 2500) as a(g) cross join lateral generate_series(1, a.g) as b(h) where a.g + b.h < 0"},
		// Each row's frame is aggregated afresh: O(n²).
		{name: "window exclude", sql: "select max(c) from (select sum(g) over (order by g rows between unbounded preceding and unbounded following exclude current row) as c from generate_series(1, 15000) as s(g)) as t"},
		// A growing frame's STRING_AGG joins all its rows for each row.
		{name: "window frame", sql: "select max(length(x)) from (select string_agg(cast(g as varchar), ',') over (order by g) as x from generate_series(1, 20000) as s(g)) as t"},
		{name: "order by", sql: "select g from generate_series(1, 500000) as s(g) order by length(repeat(cast(g as varchar), 1000)) limit 1"},
		{name: "union", sql: "select count(*) from (select repeat(cast(g as varchar), 500) as x from generate_series(1, 200000) as s(g) union select repeat(cast(g as varchar), 500) from generate_series(1, 200000) as s(g)) as u"},
		{name: "recursive", sql: "with recursive r(n) as (select g from generate_series(1, 500) as s(g) union all select n + 1 from r where n < 2200) select count(*) from r"},
		{name: "correlated subquery", sql: "select sum((select count(*) from generate_series(1, 3000) as b(h) where b.h < a.g)) from generate_series(1, 3000) as a(g)"},
		{name: "update", update: true, sql: "update cancel_small set id = id + (select count(*) from generate_series(1, 200000) as b(h) where b.h < cancel_small.id)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			lag, err := h.runCancelled(c.sql, c.update, cancelAfter(delay))
			expectCancelled(t, lag, err)
		})
	}

	// Cancelled once it has written its first pipeline of rows, which a
	// fixed delay can miss on a fast machine: its next command fails
	// (go-redis), and the rows written stay.
	t.Run("create table as", func(t *testing.T) {
		const rows = 300000
		written := 0
		lag, err := h.runCancelled(fmt.Sprintf("create table cancel_ctas as select g from generate_series(1, %d) as s(g)", rows), true, func(cancel func()) func() {
			testHookRowsWritten = func(meta *tableMeta, n int) {
				if meta.Name == "cancel_ctas" && written == 0 {
					written = n
					cancel()
				}
			}
			return func() { testHookRowsWritten = nil }
		})
		expectCancelled(t, lag, err)
		if written >= rows {
			t.Fatalf("cancelled after %d of %d rows were written, want part-way through", written, rows)
		}
		if got, _ := h.query("select count(*) from cancel_ctas"); len(got) != 1 || got[0] != strconv.Itoa(written) {
			t.Errorf("rows: got %v, want %d", got, written)
		}
	})

	// go-redis fails a command with a cancelled context at once.
	t.Run("redis command", func(t *testing.T) {
		ctx, cancel := context.WithCancel(h.ctx)
		cancel()
		for _, sql := range []string{"select * from cancel_small", "insert into cancel_small values (0)"} {
			st, err := h.conn.NewStatement(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetSqlQuery(h.ctx, sql); err != nil {
				t.Fatal(err)
			}
			if _, err := st.ExecuteUpdate(ctx); !isStatus(err, adbc.StatusCancelled) || sqlState(err) != stateQueryCanceled {
				t.Errorf("%s: got %v, want CANCELLED (57014)", sql, err)
			}
			st.Close(h.ctx)
		}
		if got, _ := h.query("select count(*) from cancel_small"); len(got) != 1 || got[0] != "50" {
			t.Errorf("rows: got %v, want 50", got)
		}
	})
}
