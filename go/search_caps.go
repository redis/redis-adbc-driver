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
		switch c := s.client.(type) {
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
		config, ok := redisReplyMap(reply)
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
			}
		}
	})
	return s.searchCaps
}
