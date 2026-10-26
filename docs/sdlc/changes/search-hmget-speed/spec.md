# Experiment contract

Review: [PR #419](https://github.com/alberttwong/redis-adbc-driver/pull/419).

2026-10-21, base `4b0fdb5`. See [intent](intent.md) and [plan](plan.md).

Screen isolated variants from the pinned source, then verify a guarded
production candidate separately. RESP3 remains the protocol default. Use Search
discovery of actual keys and row IDs, followed by bounded
pipelined HMGET of required HASH fields. Retain row-ID existence checks, field
null markers, decoding, readiness checks, cancellation and cursor cleanup.
Select the production path only when at least 64 needed columns comprise more
than half the table, the old strategy would LOAD-all, and managed scans have no
residual predicates,
late materialization, tie sorting, non-row-ID sort or limit. Preserve the implicit
ascending row-ID sort and its aliases. Compare existing map merge
with direct adoption of fetched maps and cursor/pipeline sizing.

Use identical explicit RESP3, fixture, SQL and complete typed Arrow IPC. Time
both public APIs with four partition workers/eight descriptors; validate every
value and multiplicity outside timing. Record interleaved warm-up/measured runs,
CPU, first batch, RSS and server output bytes. Narrow output is a control.
Require repeatable elapsed wins and standalone/Cluster,
sparse/deleted/concurrent-row and lifecycle evidence. Discovery+fetch adds a
race window; it does not provide snapshot isolation.

Final implementation: `dbdafa2f`; focused test strengthening and link helper:
`51baf9d0`. Exact production binary source hashes match the candidate commit.
The 4,096-row batches improve elapsed time and CPU on both APIs, with higher
peak memory. Replies fall ~55%, but requests rise, so total bidirectional traffic
is ~2% higher. The selection rule is a measured heuristic, not a universal speed
promise. See [report](../../../../benchmarks/search-hmget/2026-10-21/README.md).
