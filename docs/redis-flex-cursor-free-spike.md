# Spike: Redis Flex reads without aggregate cursors

Date: 2026-11-05

Status: Investigation proposed; no implementation or new live benchmarks.

## Question

Can the driver replace aggregate cursors for Redis Flex, and what would that
cost in query performance, Redis memory, and client memory?

This complements the [plain NUMERIC spike](redis-flex-numeric-spike.md).
Cursor availability and sortable-field availability are separate capabilities;
measure their effects separately.

## Initial finding

Aggregate cursors are a dependency of the current implementation, not a
fundamental requirement for SQL queries. A replacement can issue independent,
bounded requests and track scan progress in the driver.

Redis Software's current [Flex documentation](https://redis.io/docs/latest/operate/rs/flex/search/)
excludes `WITHCURSOR`, `FT.CURSOR READ`, and `FT.CURSOR DEL`. Ordinary aggregate
requests must be probed separately on the target deployment. The historical
[compatibility report](redis-flex-compatibility.md) tested an older build;
its results do not establish capabilities of newer Software or Cloud releases.

## Current dependencies

- [`aggregate.go`](../go/aggregate.go): `aggRequest.args` always appends
  `WITHCURSOR`; batch reads and cleanup issue `FT.CURSOR READ` and `DEL`.
- [`query.go`](../go/query.go): row scanning feeds projection, filtering,
  ordering, and client aggregation. Some paths expect insertion order by row ID.
- [`tags.go`](../go/tags.go): string-level checks also use aggregate cursors.
- [`store.go`](../go/store.go): Search routing and retry behavior account for
  cursor commands. Independent range reads require their own retry contract.

Removing the cursor clause alone is unsafe: ordinary responses have result
limits and a different reply shape, and do not provide continuation state.

## Design options

| Approach | Benefit | Cost or constraint |
|---|---|---|
| Keyset paging by row ID | Continue after the last returned ID; no growing offset | Requires exact numeric ordering and efficient execution without SORTABLE |
| Disjoint row-ID ranges | Bound each query's input and response; ranges can run independently | Requires completeness detection, adaptive splitting, and numeric boundary validation |
| Offset paging with LIMIT | Simple prototype | Repeats query work; large offsets can be costly; changes can shift page boundaries |
| SCAN plus HASH reads | Avoids Search paging and works without a driver row-ID field | Iterates the keyspace, requires duplicate handling and cluster coverage, and loses Search filtering benefits |

`SCAN` uses a keyspace cursor; it is an alternative to an aggregate cursor,
not a completely cursorless protocol. Its `COUNT` is a hint rather than a hard
batch bound. See the [SCAN contract](https://redis.io/docs/latest/commands/scan/).

## Preferred experiment: bounded row-ID ranges

Start with driver-created tables that have a numeric `__rowid`:

1. Capture an upper ID boundary and partition the eligible ID domain into
   disjoint ranges.
2. Combine each range predicate with the original indexed filter and read
   rows through ordinary Search or aggregate requests supported by the server.
3. Request a bounded response with a reliable overflow signal. Split a range
   when it cannot be read completely; do not accept a truncated response as
   complete. Handle timeouts, warnings, and server result caps explicitly.
4. Feed complete batches through existing decoding and client execution.
   Preserve required insertion order through per-range sorting and ordered
   delivery, or use another verified ordering strategy.
5. Bound concurrent requests, queued batches, retries, and temporary storage.

An upper ID boundary bounds the domain; it does not establish snapshot
isolation. Updates, deletes, delayed index visibility, and inserts allocated
before that boundary can still affect results. Define the consistency contract
and compare it with the existing reader instead of assuming cursor equivalence.

Validate numeric range precision for large integer IDs. If IDs cannot be
distinguished by Search numeric comparisons, splitting must not loop or omit
rows; require a safe fallback or an explicit supported-domain limit.

Adopted tables may lack `__rowid`. Treat them as a separate design decision:
validate a suitable unique immutable key, implement a keyspace scan fallback,
or explicitly restrict the first Flex implementation.

## Expected performance and memory effects

These are hypotheses to measure, not benchmark results.

| Resource | Aggregate cursor | Independent bounded requests |
|---|---|---|
| Execution overhead | Continues server execution | Repeats planning and query setup; may repeat index work |
| Network latency | Batch reads against retained state | More independent requests; adaptive splitting can add requests |
| Redis memory between batches | Retains cursor/execution state | No retained aggregate cursor; normal index and per-request memory remain |
| Redis peak memory | Depends on query operators and cursor state | Depends on operators, range size, and concurrent requests; not necessarily lower |
| Client scan memory | Batch and downstream operator state | Can remain bounded by batch bytes, prefetch, and concurrency |
| Parallel throughput | Depends on existing cursor execution | Independent ranges permit concurrency, at greater CPU and memory cost |

Removing `SORTABLE` separately saves sortable-value storage, but may increase
HASH reads and flash access. That saving must not be attributed to removing
cursors. Neither row-count batching nor a small LIMIT alone guarantees a
strict byte bound for wide rows or small server execution memory.

## Sorting and aggregation

Replacing cursors does not automatically require client-side execution of
every operation. Preserve server operations that the deployment supports.
The [aggregate API](https://redis.io/docs/latest/commands/ft.aggregate/)
defines ordinary requests independently of `WITHCURSOR`.

| Query shape | Candidate execution | Main cost |
|---|---|---|
| Filter and stream | Read bounded matching batches | Request overhead and document reads; bounded scan buffers |
| ORDER BY with LIMIT K | Client top-K if server ordering is unavailable | Roughly K candidates plus batch state, but potentially all matching rows transferred |
| Full ORDER BY | Client sort with external spill when needed | Memory grows with rows and width without spill; spill adds disk I/O |
| COUNT, SUM, MIN, MAX | One server aggregate where valid, or merge range partials | Small output; plain numeric loading may still require many server document reads |
| AVG | Merge partial sums and non-null counts where semantically valid | Small merge state; preserve NULL and numeric semantics |
| GROUP BY | Merge supported partial aggregates or aggregate input in the driver | State grows with group cardinality unless spilled; partial output can also be large |
| Exact DISTINCT aggregates | Dedicated exact strategy | Distinct counts cannot be added across overlapping value sets |

Bound grouped partial responses too: a small input request limit must not
silently truncate groups. Floating-point order of operations, overflow, NULLs,
and non-decomposable reducers require correctness checks before partial merging.

## Validation and benchmark plan

1. Probe field loading, numeric ranges and ordering, ordinary aggregate replies,
   result caps, timeout reporting, and cursor rejection on the target build.
2. Compare cursor and range readers on the same server supporting both paths
   to isolate paging overhead. Separately compare NUMERIC and NUMERIC SORTABLE,
   then run the proposed reader on Flex and disclose deployment differences.
3. Exercise selective filters, broad scans, sparse IDs, wide rows, top-K,
   full sorting, scalar aggregates, and low/high-cardinality grouping. Vary
   range sizes and concurrency; include warm and cold data runs.
4. Measure latency percentiles, throughput, request counts, transferred bytes,
   Redis CPU and peak memory, client CPU and peak memory, spill bytes, and
   document/flash reads where observable. Record retained cursor state in the
   baseline separately from index storage and transient execution memory.
5. Check complete results under server caps, boundary IDs, retries, cancellation,
   and failures. For an unchanged dataset, require no missing or duplicate rows.
   Test concurrent writes separately against the declared consistency contract.
6. Audit all cursor users, including tag checks and aggregate fallback paths.
   Verify that cancellation stops outstanding requests and cleans temporary
   files without requiring aggregate-cursor cleanup.

## Exit criteria and recommendation

Produce a bounded reader prototype with verified completeness, ordering,
numeric boundaries, and cancellation; measured resource costs; a documented
consistency contract; and a decision for adopted tables. Record result limits
and any initial feature restrictions explicitly.

Investigate disjoint row-ID ranges first for driver-created Flex tables.
Retain the cursor reader for deployments supporting it and select readers
through capability checks. Prefer server-side scalar and mergeable aggregate
execution when validated. Cursor-free reading alone does not resolve the
remaining Flex schema and command compatibility gaps.
