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
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

// paginationCaps reads the server limits once per connection. Managed
// services and restricted ACL users can deny CONFIG; use documented defaults
// there, avoiding invalid arguments for the standard server configuration.
func (s *store) paginationCaps(ctx context.Context, index string) [2]int64 {
	s.searchCapsOnce.Do(func() {
		s.searchCaps = [2]int64{1_000_000, 1 << 31}
		// Named ACL users need no CONFIG permission for ordinary queries.
		// Do not probe administrative commands and pollute their ACL log.
		username := ""
		switch c := s.redis(ctx).(type) {
		case *goredis.Client:
			username = c.Options().Username
		case *goredis.ClusterClient:
			username = c.Options().Username
		}
		if username != "" && username != "default" {
			return
		}
		node, err := s.search(ctx, index)
		if err != nil {
			return
		}
		reply, err := node.Do(ctx, "CONFIG", "GET", "search-max-search-results", "search-max-aggregate-results").Result()
		if err != nil {
			return
		}
		config, ok := redisReplyFields(reply)
		if !ok {
			return
		}
		for i, name := range []string{"search-max-search-results", "search-max-aggregate-results"} {
			raw, ok := config[name]
			if !ok {
				continue
			}
			text, ok := raw.(string)
			if !ok {
				continue
			}
			n, err := strconv.ParseInt(text, 10, 64)
			if err == nil && n >= 0 {
				s.searchCaps[i] = n
				if i == 1 {
					s.searchAggregateCapKnown = true
				}
			}
		}
	})
	return s.searchCaps
}

// snapshotSortCap checks an unavailable aggregate cap without executing the SQL
// predicate or opening a data cursor. FILTER 0 keeps this generated capability
// query empty. A native argument refusal supplies the actual bound; all other
// errors stop planning. In particular, transport failures never trigger a
// materializing fallback or a replay of the user's query.
func (s *store) snapshotSortCap(ctx context.Context, index string, caps [2]int64) ([2]int64, error) {
	if s.searchAggregateCapKnown {
		return caps, nil
	}
	if err := cancelled(ctx); err != nil {
		return caps, err
	}
	node, err := s.search(ctx, index)
	if err != nil {
		return caps, err
	}
	reply, err := once(ctx, node.c, "FT.AGGREGATE", index, "*", "FILTER", "0", "LIMIT", 0, maxAggLimit,
		"TIMEOUT", 0, "WITHCURSOR", "COUNT", 1, "DIALECT", 2).Result()
	if err != nil {
		if ce := cancelled(ctx); ce != nil {
			return caps, ce
		}
		if cap, ok := snapshotAggregateLimitRefusal(err); ok {
			caps[1] = cap
			return caps, nil
		}
		return caps, wrapRedis(err, "checking snapshot sort capacity failed")
	}
	// Parse and close even an unexpected cursor before validating emptiness.
	c := &aggCursor{store: s, node: node, index: index}
	rows, cursor, err := parseCursorReplyLists(reply, nil)
	c.cursor = cursor
	c.close(ctx)
	if err != nil {
		return caps, err
	}
	if len(rows) != 0 || cursor != 0 || snapshotReplyWarning(reply) {
		return caps, errorf(adbc.StatusIO, "snapshot sort capacity probe returned unexpected rows, cursor or warning")
	}
	return caps, nil
}

func snapshotAggregateLimitRefusal(err error) (int64, bool) {
	native, ok := err.(interface {
		error
		RedisError()
	})
	if !ok {
		return 0, false
	}
	const prefix = "SEARCH_LIMIT_OVER LIMIT exceeds maximum of "
	message := native.Error()
	if !strings.HasPrefix(message, prefix) {
		return 0, false
	}
	raw := strings.TrimPrefix(message, prefix)
	n, parseErr := strconv.ParseInt(raw, 10, 64)
	return n, parseErr == nil && n >= 0 && n < maxAggLimit && strconv.FormatInt(n, 10) == raw
}
