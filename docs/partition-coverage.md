# Remaining partition coverage

As of 2026-10-22, this table compares source capabilities. Published wheel
availability has not been verified; keep the source-build caveats in
[live reads](partitioned-reads.md) and [retained reads](retained-partition-results.md).
Ordinary SQL has its own supported subset and remains autocommit-only.

| Operation/source | Live partitions | Retained partitions | Ordinary SQL |
|---|---|---|---|
| Managed permanent table, row-local projection/filter | Restricted deterministic allowlist | Bounded evaluator | Supported |
| SUBSTR, LEFT, RIGHT | Rejected | Admitted in this change | Supported |
| Pattern/regex SUBSTRING | Rejected | Rejected | Supported forms |
| ORDER BY, LIMIT/OFFSET | Rejected | Once over complete captured input | Once per execution |
| DISTINCT, grouping, aggregates, windows | Rejected | Once at coordinator, within budgets | Supported subset |
| Joins, sets, nonrecursive CTEs, permanent views/subqueries | Rejected | Bounded supported subset | Supported subset |
| Adopted key-identified, foreign/application index, legacy layouts | Rejected | Supported layouts with ownership/membership checks | Supported layouts |
| Temporary source | Rejected | Producer-owned temporary tables/views | Connection-owned lifetime |
| Recursive CTEs, generators, lateral/table functions | Rejected | Rejected | Supported subset |
| Volatile/side-effect functions | Rejected | Rejected | Supported subset |
| Atomic source snapshot under concurrent DML/DDL | Unavailable | Unavailable | Unavailable |
| Retry after source changes | Rereads live data | Replays immutable committed output until expiry/revocation | Fresh execution |

The next independently useful expansion selected for
[#477](https://github.com/alberttwong/redis-adbc-driver/issues/477) is retained
positional string slicing: SUBSTR, LEFT and RIGHT. These operations select from
an already bounded value; they do not generate unbounded output or execute
regex patterns. Existing scalar input, work, allocation and IPC limits remain.
Tests compare complete Arrow values/types with ordinary execution, including
NULL, Unicode, negative positions/lengths and extreme integer bounds. Global
operators still run once before chunking, never once per reader.

## Source-consistent capture assessment

No opt-in atomic mode is implemented. A before/after row count, index-ready bit,
schema identity or checksum cannot detect an update changed and restored while
capture runs. A best-effort retry therefore cannot establish source consistency.
SCAN deduplication prevents repeated keys; it does not freeze their values.

Driver-level transaction and consistency feature development is outside the
active roadmap per maintainer direction (2026-10-22). Operators requiring
consistent capture must quiesce all source writers and DDL through publication.
This operational condition is not a driver-verified snapshot.

Retained capture admits temporary tables/views owned by the producing
connection. The coordinator copies the producer's temporary catalog identity;
unowned temporary schemas are rejected. Capture and evaluation finish before
publication; independent readers only access committed namespace-authorized
output. Closing the producer cancels active work, and detectable source identity
or membership changes fail publication. Temporary sources do not gain a source
snapshot guarantee. Keep producer DDL/DML quiescent during capture. Committed
output remains replayable after the producer closes and its temporary objects
are deleted. Existing deadlines, budgets, quota, expiry and revocation apply.

Future expansions must retain bounded admission, fixed expiry, explicit revoke,
independently authenticated readers and immutable replay after producer closure.
Existing failure/lifecycle suites remain necessary; this expansion does not
establish new deployment, concurrent snapshot or packaged-release evidence.
