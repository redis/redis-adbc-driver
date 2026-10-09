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
	"errors"
	"fmt"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// Only Process is used by indexAttributes; embedding the interface keeps
// these tests independent of a live server.
type indexInfoClient struct {
	goredis.UniversalClient
	process func(context.Context, goredis.Cmder) error
}

func (c *indexInfoClient) Process(ctx context.Context, cmd goredis.Cmder) error {
	return c.process(ctx, cmd)
}

func TestIndexAttributesRetry(t *testing.T) {
	const inconsistent = "SEARCH_VALUE_BAD Inconsistent index state"
	for _, tc := range []struct {
		name   string
		err    string
		fail   int
		cancel bool
		calls  int
		want   string
	}{
		{name: "success", calls: 1},
		{name: "schema converges", err: inconsistent, fail: 2, calls: 3},
		{name: "bounded", err: inconsistent, fail: 100, calls: 20, want: inconsistent},
		{name: "permission denied", err: "NOPERM cannot run FT.INFO", fail: 100, calls: 1, want: "NOPERM"},
		{name: "missing index", err: "Unknown index name", fail: 100, calls: 1, want: "Unknown index"},
		{name: "network failure", err: "connection reset by peer", fail: 100, calls: 1, want: "connection reset"},
		{name: "other search error", err: "SEARCH_VALUE_BAD other error", fail: 100, calls: 1, want: "other error"},
		{name: "canceled", err: inconsistent, fail: 100, cancel: true, calls: 1, want: "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client := &indexInfoClient{process: func(ctx context.Context, cmd goredis.Cmder) error {
				calls++
				if got := fmt.Sprint(cmd.Args()); got != "[FT.INFO idx:test]" {
					t.Fatalf("unexpected command: %s", got)
				}
				if calls <= tc.fail {
					err := errors.New(tc.err)
					cmd.SetErr(err)
					if tc.cancel {
						cancel()
					}
					return err
				}
				cmd.(onceCmd).SetVal([]any{"attributes", []any{
					[]any{"identifier", "id", "attribute", "id", "type", "NUMERIC"},
				}})
				return nil
			}}
			attrs, err := (&store{client: client}).indexAttributes(ctx, "idx:test")
			if tc.want == "" {
				if err != nil || len(attrs) != 1 || !attrs["id"] {
					t.Fatalf("attributes = %v, error = %v", attrs, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if calls != tc.calls {
				t.Fatalf("%d calls, want %d", calls, tc.calls)
			}
		})
	}
}
