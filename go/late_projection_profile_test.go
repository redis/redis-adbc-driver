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

// Opt-in stage profile on a preloaded fixture; native runner validates results.
import (
	"context"
	"encoding/json"
	"errors"
	goredis "github.com/redis/go-redis/v9"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type lateStageMetrics struct {
	cursorRows, pages, hmgetRows int
	cursor, fetch                time.Duration
}

func (m *lateStageMetrics) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (m *lateStageMetrics) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		if cmd.Name() == "ft.aggregate" || (cmd.Name() == "ft.cursor" && strings.EqualFold(cmd.Args()[1].(string), "READ")) {
			m.cursor += time.Since(start)
			if err == nil {
				if reply, ok := cmd.(interface{ Val() any }).Val().([]any); ok && len(reply) == 2 {
					if rows, ok := aggregateReplyRows(reply[0]); ok {
						m.cursorRows += len(rows)
						m.pages++
					}
				}
			}
		}
		return err
	}
}
func (m *lateStageMetrics) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		for _, cmd := range cmds {
			if cmd.Name() == "hmget" {
				m.hmgetRows++
			}
		}
		m.fetch += time.Since(start)
		return err
	}
}
func TestLateProjectionProfile(t *testing.T) {
	sql := os.Getenv("REDIS_LATE_PROFILE_QUERY")
	if sql == "" {
		t.Skip("set REDIS_LATE_PROFILE_QUERY on preloaded table")
	}
	h := newSQLHarness(t)
	client := h.rawClient()
	m := &lateStageMetrics{}
	client.AddHook(m)
	e := &executor{store: &store{client: client}, schema: defaultSchema, cache: newExecCache(), now: time.Now().UTC()}
	parsed, err := ParseScript(sql)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := e.planSelect(h.ctx, parsed[0].Stmt.(*SelectStmt), nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := e.prepareScan(h.ctx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	it, err := e.openScan(h.ctx, sc.req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.close(h.ctx)
	// Profile first ten qualifying rows, without OFFSET, in both versions.
	count := 0
	for count < 10 {
		_, rows, err := it.next(h.ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count += min(len(rows), 10-count)
	}
	elapsed := time.Since(start)
	b, _ := json.Marshal(map[string]any{"query": sql, "seconds": elapsed.Seconds(), "cursor_seconds": m.cursor.Seconds(), "hmget_seconds": m.fetch.Seconds(), "client_and_other_seconds": (elapsed - m.cursor - m.fetch).Seconds(), "cursor_rows": m.cursorRows, "cursor_pages": m.pages, "hmget_rows": m.hmgetRows, "result_rows": count})
	t.Logf("PROFILE %s", b)
}
