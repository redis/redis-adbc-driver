# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

A snowflake dimension branch with several tables first tries `snowflakeJoinRows` on its own join plan. Its largest table then streams through the existing parallel readers, and its other tables become that sub-shape's dimensions. The same admission rules, caps, residual checks and eager fallback as the top-level snowflake apply, including the 65,536-row minimum fact. A branch the sub-shape does not admit, such as Q5's 10,000-row supplier⋈nation⋈region, keeps the eager `runJoin`.

The branch's output rows are the same merged rows as the eager join's. They are compacted, keyed and capped by the outer shape exactly as before. Only row order inside a bucket can differ, and buckets are order-independent. The recursion applies on the RAM profile too, because its sub-shape reads are no slower there.

A ranged exact-directory scan (each parallel reader chunk) previously spent up to four sequential round trips per 256-row chunk: a `ZLEXCOUNT` seek, the first `ZRANGE` page, the HASH fetch pipeline, and usually a further page that returned only the next chunk's first ID. The seek is now deferred to the source's first page. That page is `ZRANGE key [bound + BYLEX LIMIT 0 page`, pipelined with the same `ZLEXCOUNT`, so later pages still continue by rank and the end-of-directory row-count check is unchanged. A page whose last ID reaches the range's upper bound ends that source, because every later member is outside the range. Results, visibility merging and the page bound (`cap`) are unchanged.

With `adbc.redis.flex=true`, `query_workers` and `query_worker_budget` left unset default to a fixed 32, the driver maximum, applied through the explicit-budget path when the first connection opens. Explicit values, including `auto`, are unchanged, and RAM databases keep the automatic CPU-sized start. Flex readers mostly wait on flash reads, and on SF1 Flex 32 workers read 32k rows/s against 24k for 16.

Acceptance:
- **Recursion:** `TestSnowflakeLargeBranchStreamsRecursively` shows the Q5 orders branch admitted as its own snowflake with orders as its fact, returning the eager join's rows, and the eager join kept below the minimum fact size.
- **Directory round trips:** `TestStarChunkRangeSeekReadsOneDirectoryPage` reads full, short, two-page and tail ranges with one `ZLEXCOUNT` and the minimum number of `ZRANGE` pages. It fails on the previous reader, which reads an extra page for a full range.
- **Flex worker defaults:** `TestFlexQueryWorkerDefaults`.
- **No regressions:** the existing snowflake differential, star, join-common and Q19 tests pass.
- **SF1 (not met):** Q3/Q5/Q7/Q8/Q9 fully fetched and validated within the 60-second deadline on the SF1 Flex database with a 4-CPU / 8 GiB Linux client. The subset in Flex mode is not SF1 Flex evidence: its tables were created on the RAM profile, so they never use Flex's bounded candidate path. Measured SF1 Q9 took 266 s with 32 workers; see [validation](validation.md#sf1-flex).
