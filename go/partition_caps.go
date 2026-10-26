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
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

const maxPartitionSubranges = 1 << 20

// Unlike ordinary paginationCaps, never assume a hidden setting's default.
// Reprobe on every range; do not use a stale per-connection capability cache.
// CONFIG is optional: explicit caller lower bounds support restricted readers.
func (s *store) partitionScanCap(ctx context.Context, index string, configured int64) (int64, error) {
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	// A configured cap is a deployment contract, not a hint to raise a limit.
	if configured > 0 {
		return configured, nil
	}
	username := ""
	switch c := s.redis(ctx).(type) {
	case *goredis.Client:
		username = c.Options().Username
	case *goredis.ClusterClient:
		username = c.Options().Username
	}
	if username == "" || username == "default" {
		node, err := s.search(ctx, index)
		if err == nil {
			reply, err := node.Do(ctx, "CONFIG", "GET", "search-max-aggregate-results").Result()
			if err == nil {
				config, ok := redisReplyFields(reply)
				if ok {
					raw, ok := config["search-max-aggregate-results"].(string)
					if ok {
						n, err := strconv.ParseInt(raw, 10, 64)
						if err == nil && n > 0 {
							return min(n, int64(1<<31)), nil
						}
					}
				}
			}
		}
	}
	if err := cancelled(ctx); err != nil {
		return 0, err
	}
	return 0, errorf(adbc.StatusNotImplemented, "partition aggregate cap is unavailable or nonpositive; configure adbc.redis.partition_scan_cap with a guaranteed positive lower bound")
}

// A range width bounds result slots only after initial Search indexing has
// completed: backfill has emitted duplicate documents on supported servers.
// Check every OSS Cluster primary, not only the query coordinator. Software's
// proxy reports the distributed index through the same FT.INFO contract.
func (s *store) checkRangeReadiness(ctx context.Context, index string) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	check := func(ctx context.Context, node searchConn) error {
		reply, err := node.Do(ctx, "FT.INFO", index).Result()
		if err != nil {
			if ce := cancelled(ctx); ce != nil {
				return ce
			}
			if inconsistentIndexState(err) {
				return errorf(adbc.StatusInvalidState, "Search index schemas are still converging; retry after indexing completes")
			}
			if strings.HasPrefix(err.Error(), "NOPERM") {
				return errorf(adbc.StatusUnauthorized, "bounded Search reads require FT.INFO permission to verify index readiness")
			}
			return wrapRedis(err, "checking bounded scan index readiness failed")
		}
		return checkRangeReadinessReply(reply)
	}
	if cc, ok := s.redis(ctx).(*goredis.ClusterClient); ok {
		return cc.ForEachMaster(ctx, func(ctx context.Context, node *goredis.Client) error {
			return check(ctx, searchConn{node})
		})
	}
	return check(ctx, searchConn{s.redis(ctx)})
}

func checkRangeReadinessReply(reply any) error {
	fields, ok := redisReplyFields(reply)
	indexing, present := fields["indexing"]
	active, flagErr := strconv.ParseFloat(fmt.Sprint(indexing), 64)
	if !ok || !present || flagErr != nil || math.IsNaN(active) || math.IsInf(active, 0) || active < 0 {
		return errorf(adbc.StatusNotImplemented, "Search index readiness is unavailable (indexing=%v); bounded scan completeness cannot be verified", indexing)
	}
	// Cluster responses may aggregate positive indexing flags, and percentage
	// can be NaN while a backfill is starting. A known building flag is enough
	// to reject transiently; only a ready flag requires complete progress data.
	if active > 0 {
		return errorf(adbc.StatusInvalidState, "Search index is still indexing; retry the bounded read after indexing completes")
	}
	percent, percentPresent := fields["percent_indexed"]
	progress, err := strconv.ParseFloat(fmt.Sprint(percent), 64)
	if !percentPresent || err != nil || math.IsNaN(progress) || math.IsInf(progress, 0) {
		return errorf(adbc.StatusNotImplemented, "Search index readiness is unavailable (indexing=%v, percent_indexed=%v); bounded scan completeness cannot be verified", indexing, percent)
	}
	if progress < 1 {
		return errorf(adbc.StatusInvalidState, "Search index is still indexing; retry the bounded read after indexing completes")
	}
	return nil
}

// With unique integer row IDs, a range of width cap can match at most cap
// rows. Disjoint ranges avoid cap truncation, including residual/direct-key
// predicates, without buffering whole partitions or replaying cursor pages.
type rowIDRanges struct {
	exec                    *executor
	req                     scanRequest
	params                  []Value
	lo, hi, cap, configured int64
	opened                  int64
}

func (r *rowIDRanges) open(ctx context.Context) (*scanIter, error) {
	if err := cancelled(ctx); err != nil {
		return nil, err
	}
	if r.lo > r.hi {
		return nil, io.EOF
	}
	// Check before opening another cursor so a released/replaced prefix is
	// reported as a stale read rather than an unrelated missing-index error.
	if r.opened > 0 {
		if err := r.exec.store.checkKeys(ctx, r.req.meta, false); err != nil {
			return nil, err
		}
	}
	if err := r.exec.store.checkRangeReadiness(ctx, r.req.meta.index()); err != nil {
		return nil, err
	}
	cap, err := r.exec.store.partitionScanCap(ctx, r.req.meta.index(), r.configured)
	if err != nil {
		return nil, err
	}
	r.cap = min(r.cap, cap, maxScanRangeRows)
	if r.opened+(r.hi-r.lo)/r.cap+1 > maxPartitionSubranges {
		return nil, errorf(adbc.StatusNotImplemented, "partition requires too many cap-bounded ranges")
	}
	hi := min(r.hi, r.lo+r.cap-1)
	req := r.req
	rangeQuery := fmt.Sprintf("@%s:[%d %d]", rowIDField, r.lo, hi)
	if req.where.query != "" && req.where.query != "*" {
		rangeQuery = "(" + req.where.query + ") " + rangeQuery
	}
	req.where.query = rangeQuery
	it, err := r.exec.openScan(ctx, req, r.params)
	if err != nil {
		return nil, err
	}
	if err := r.exec.store.checkRangeReadiness(ctx, r.req.meta.index()); err != nil {
		it.close(ctx)
		return nil, err
	}
	r.lo = hi + 1
	r.opened++
	it.rowIDRanges = r
	return it, nil
}
