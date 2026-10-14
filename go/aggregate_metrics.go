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
	"encoding/json"
	"sync/atomic"
)

// Times are cumulative worker times, except merge time (wall time). Value
// bytes are logical returned field/key bytes, not RESP wire bytes or Redis
// CPU time. Failed and discarded partition attempts remain in these counts.
type partitionMetrics struct {
	used                                                     atomic.Bool
	partitions, retries, fallbacks, tuples, rows, valueBytes atomic.Int64
	redisNanos, partitionNanos, mergeNanos, spillBytes       atomic.Int64
	workers                                                  atomic.Int64
	rowScans                                                 atomic.Int64
	spillReadBytes                                           atomic.Int64
	spillRead, spillWrite, spillSort, fetch, merge           liveTimer
}

func (m *partitionMetrics) json() string {
	if m == nil || !m.used.Load() {
		return "{}"
	}
	b, _ := json.Marshal(map[string]any{
		"strategy": "partitioned_exact", "workers": m.workers.Load(),
		"partition_attempts": m.partitions.Load(), "group_limit_retries": m.retries.Load(), "row_fallbacks": m.fallbacks.Load(),
		"returned_tuples": m.tuples.Load(), "represented_rows": m.rows.Load(), "logical_value_bytes": m.valueBytes.Load(),
		"redis_reply_seconds": float64(m.redisNanos.Load()) / 1e9, "partition_worker_seconds": float64(m.partitionNanos.Load()) / 1e9,
		"merge_wall_seconds": float64(m.mergeNanos.Load()) / 1e9, "spill_written_bytes": m.spillBytes.Load(),
		"row_scan_partitions": m.rowScans.Load(), "spill_read_bytes": m.spillReadBytes.Load(),
		"spill_read_worker_seconds": m.spillRead.seconds(), "spill_write_worker_seconds": m.spillWrite.seconds(),
		"spill_sort_worker_seconds": m.spillSort.seconds(), "partition_fetch_worker_seconds": m.fetch.seconds(),
		"merge_elapsed_seconds": m.merge.seconds(),
	})
	return string(b)
}
