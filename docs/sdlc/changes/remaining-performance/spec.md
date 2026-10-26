# Performance contracts

See [intent](intent.md), [plan](plan.md). Baseline 7a42b2d, 2026-10-21.

`adbc.redis.stream_batch_bytes` is an optional nonnegative int64 accounted-byte
ceiling alongside stream_batch_rows; zero keeps current behavior. Accounting is
256 bytes per projected Value plus twice string/binary payload, decimal magnitude
plus 32 bytes, and recursive array/record children. NULL slots and duplicates
count. No process-RSS bound is claimed. A single oversized row progresses;
cursor-page raw input and completed-record overshoot remain bounded by the
existing page size, not by this setting. Applies to both public APIs and generic
streamed expressions. Existing row ceiling, cancellation, LIMIT and fallback
semantics remain. No universal default or supported SQL exactness changes.

#411 comparisons must include controlled latency/bandwidth and identical results.
#406 needs current pinned plans/profiles and independent full-dataset validation
of Q2/Q4/Q9/Q10, preserving NULL/default/boundary/global DISTINCT semantics.

#410 implementation: [PR #426](https://github.com/alberttwong/redis-adbc-driver/pull/426).
