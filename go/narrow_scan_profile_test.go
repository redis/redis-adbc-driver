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

// Opt-in scan profile for #211. This uses a synthetic 105-column table,
// not the ClickBench data. It measures both strategies on identical rows.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type scanProfileConn struct {
	net.Conn
	bytes *atomic.Int64
}

func (c scanProfileConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.bytes.Add(int64(n))
	return n, err
}

type scanProfileResult struct {
	Strategy                             string
	Rows, Pages                          int
	ReplyBytes, AllocBytes, Allocs       uint64
	TotalMS, CursorMS, FetchMS, DecodeMS float64
	Checksum                             int64
}

func TestNarrowScanProfile(t *testing.T) {
	if os.Getenv("REDIS_SCAN_PROFILE") == "" {
		t.Skip("set REDIS_SCAN_PROFILE=1 for the 105-column scan profile")
	}
	n := 100000
	if s := os.Getenv("REDIS_SCAN_PROFILE_ROWS"); s != "" {
		var err error
		n, err = strconv.Atoi(s)
		if err != nil || n < 1 {
			t.Fatal("REDIS_SCAN_PROFILE_ROWS must be positive")
		}
	}
	h := newSQLHarness(t)
	table := os.Getenv("REDIS_SCAN_PROFILE_TABLE")
	synthetic := table == ""
	if synthetic {
		table = "it_scan_profile"
		h.dropTables(table)
	}
	columns := []string{"userid BIGINT"}
	for i := 1; i < 105; i++ {
		columns = append(columns, fmt.Sprintf("c%d VARCHAR", i))
	}
	if synthetic {
		h.exec("CREATE TABLE " + table + " (" + strings.Join(columns, ",") + ")")
	}
	admin := h.rawClient()
	meta, err := (&store{client: admin}).getTable(h.ctx, defaultSchema, table)
	if err != nil {
		t.Fatal(err)
	}
	userColumn := ""
	for _, c := range meta.Columns {
		if strings.EqualFold(c.Name, "userid") {
			userColumn = c.Name
		}
	}
	if userColumn == "" {
		t.Fatal("profile table has no UserID column")
	}
	for start := 0; synthetic && start < n; start += pipelineChunk {
		pipe := admin.Pipeline()
		for i := start; i < min(start+pipelineChunk, n); i++ {
			fields := []any{rowIDField, i + 1, "userid", strconv.FormatInt(9007199254740992+int64(i%1000), 10)}
			for j := 1; j < 105; j++ {
				fields = append(fields, fmt.Sprintf("c%d", j), "payload-0123456789-abcdefghijklmn")
			}
			pipe.HSet(h.ctx, meta.prefix()+strconv.Itoa(i+1), fields...)
		}
		if _, err := pipe.Exec(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Indexing is asynchronous on some servers; wait until all rows match.
	waitFor(t, "profile rows indexed", func() bool {
		c, err := (&store{client: admin}).countMatches(h.ctx, meta.index(), "*")
		return err == nil && c == int64(n)
	})
	opts, err := goredis.ParseURL(os.Getenv("REDIS_URI"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Protocol = 2
	opts.ReadTimeout = 5 * time.Minute
	var wire atomic.Int64
	opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return scanProfileConn{Conn: conn, bytes: &wire}, nil
	}
	client := goredis.NewClient(opts)
	defer client.Close()
	if err := client.Ping(h.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	st := &store{client: client}
	e := &executor{store: st}
	req := scanRequest{meta: meta, where: wherePlan{query: "*"}, need: map[string]bool{userColumn: true}}
	profile := func(strategy string) scanProfileResult {
		runtime.GC()
		wire.Store(0)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		result := scanProfileResult{Strategy: strategy}
		begin := time.Now()
		cursorStart := time.Now()
		var it *scanIter
		var err error
		if strategy == "load_all" {
			it, _, _ = e.newScanIter(h.ctx, req, nil)
			it.cur, err = st.openAggregate(h.ctx, &aggRequest{index: meta.index(), query: "*", load: []string{"__key"}, loadAll: true, width: len(meta.Columns) + 2})
		} else {
			it, err = e.openScan(h.ctx, req, nil)
			if err == nil && len(it.fetch) == 0 {
				t.Fatal("profile did not select narrow exact fetch")
			}
		}
		result.CursorMS += float64(time.Since(cursorStart).Microseconds()) / 1000
		if err != nil {
			t.Fatal(err)
		}
		defer it.close(h.ctx)
		for {
			cursorStart = time.Now()
			page, err := it.cur.next(h.ctx)
			result.CursorMS += float64(time.Since(cursorStart).Microseconds()) / 1000
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			result.Pages++
			keys := make([]string, len(page))
			for i, r := range page {
				keys[i] = r["__key"]
			}
			fetchStart := time.Now()
			if len(it.fetch) > 0 {
				rows, err := st.fetchRows(h.ctx, keys, it.fetch)
				if err != nil {
					t.Fatal(err)
				}
				for i, r := range rows {
					if r == nil {
						page[i] = nil
						continue
					}
					for k, v := range r {
						page[i][k] = v
					}
				}
			}
			result.FetchMS += float64(time.Since(fetchStart).Microseconds()) / 1000
			decodeStart := time.Now()
			_, vals, err := it.keep(keys, page)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range vals {
				result.Checksum += v[userColumn].I
			}
			result.Rows += len(vals)
			result.DecodeMS += float64(time.Since(decodeStart).Microseconds()) / 1000
		}
		result.TotalMS = float64(time.Since(begin).Microseconds()) / 1000
		runtime.ReadMemStats(&after)
		result.ReplyBytes = uint64(wire.Load())
		result.AllocBytes, result.Allocs = after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs
		if result.Rows != n {
			t.Fatalf("got %d rows, want %d", result.Rows, n)
		}
		return result
	}
	var checksum int64
	for trial := 0; trial < 4; trial++ {
		order := []string{"load_all", "narrow"}
		if trial%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		for _, strategy := range order {
			r := profile(strategy)
			if trial == 0 && strategy == "load_all" {
				checksum = r.Checksum
			}
			if r.Checksum != checksum {
				t.Fatalf("checksum mismatch: %+v", r)
			}
			b, _ := json.Marshal(r)
			t.Logf("trial=%d %s", trial, b)
		}
	}
}
