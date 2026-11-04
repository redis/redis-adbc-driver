# Remaining partition coverage

Current snapshot branch contract, 2026-11-04. See [live reads](partitioned-reads.md),
[retained reads](retained-partition-results.md) and the [open parity gates](sdlc/changes/snapshot-parity/plan.md).
The published 0.0.19 macOS ARM64 wheel’s native live/retained TLS checks are
[historical platform-specific evidence](sdlc/changes/210-212/validation.md), not
validation of the current snapshot implementation or every release asset.
Ordinary SQL has its own supported subset and remains autocommit-only.

| Operation/source | Live partitions | Retained partitions | Ordinary SQL |
|---|---|---|---|
| Managed permanent table, row-local projection/filter | Restricted deterministic allowlist | Bounded evaluator | Supported |
| SUBSTR, LEFT, RIGHT | Rejected | Supported | Supported |
| Pattern/regex SUBSTRING | Rejected | Rejected | Supported forms |
| ORDER BY, LIMIT/OFFSET | Rejected | Once over complete captured input | Once per execution |
| DISTINCT, grouping, aggregates, windows | Rejected | Once at coordinator, within budgets | Supported subset |
| Joins, sets, nonrecursive CTEs, permanent views/subqueries | Rejected | Bounded supported subset | Supported subset |
| Managed copies of adopted HASHes and foreign/application indexes | Native managed partition path | Captured managed copy | Managed copy with explicit refresh |
| Legacy storage and direct live external layouts | Rejected | Rejected by current catalog bootstrap | Rejected; no migration |
| Temporary source | Rejected | Producer-owned temporary tables/views | Connection-owned lifetime |
| Recursive CTEs, generators, lateral/table functions | Rejected | Rejected | Supported subset |
| Volatile/side-effect functions | Rejected | Rejected | Supported subset |
| Atomic source snapshot under concurrent DML/DDL | One producer generation for the live set | One pinned managed generation during capture | One managed generation per statement |
| Retry after source changes | Same pinned generation until expiry/release | Replays immutable committed output until expiry/revocation | Fresh execution |

The retained expansion selected for
[#211](https://github.com/redis/redis-adbc-driver/issues/211) added
positional string slicing: SUBSTR, LEFT and RIGHT. These operations select from
an already bounded value; they do not generate unbounded output or execute
regex patterns. Existing scalar input, work, allocation and IPC limits remain.
Tests compare complete Arrow values/types with ordinary execution, including
NULL, Unicode, negative positions/lengths and extreme integer bounds. Global
operators still run once before chunking, never once per reader.

## Source-consistent capture

Managed statement snapshots are always enabled. Live sets retain one producer
generation, retained coordinators evaluate one pinned generation, and ordinary
statements each acquire their own generation. Concurrent SQL DML/DDL does not
change a pinned input. Readiness checks and before/after counts do not establish
this guarantee; immutable data/catalog generations and their reference protocol do.

Application-owned HASHes and indexes remain external. SQL reads a private managed
copy, and explicit refresh publishes a new copy for future statements. External
writers do not participate in the driver’s snapshot protocol; a refresh does not
promise an atomic capture of arbitrary concurrently changing application data.
Completed copies and retained output preserve the captured exact values.

Retained capture admits temporary tables/views owned by the producing connection.
The coordinator copies its temporary catalog identity; unowned temporary schemas
are rejected. Independent readers access only committed namespace-authorized
output. Closing the producer cancels active work; completed output remains
replayable after the producer’s temporary objects are removed. Existing deadlines,
budgets, quota, expiry and revocation apply.

Full feature/platform and current-protocol Cluster recovery checks, process/input
resumption and comparative/sustained workload measurements remain open in the
parity plan. Targeted snapshot and replay checks do not establish complete
packaged-release or performance parity.
