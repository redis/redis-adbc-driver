# Long managed ClickBench streams: plan and evidence

Date: 2026-11-20. Baseline: `1b5918e6`.
Status: implementation and focused Software/Flex correctness validated;
full-scale performance acceptance remains open.
Issue: [#566](https://github.com/redis/redis-adbc-driver/issues/566).
[Intent](intent.md) · [Specification](spec.md)

## Delivered behavior

`run.py --load-strategy managed-streams` creates the table through the coordinator,
then uses one long append on each of 1–4 independent ADBC connections. Workers
receive disjoint, exhaustive row-group slices, including uneven source prefixes.
The default batch strategy is retained. Streams have no producer queue or replay;
source-yielded rows and successful publication acknowledgments are separate.
Private staging, index-seal and maintenance phases are explicitly unavailable;
normal driver proofs/maintenance remain enabled and backup readiness stays false.

Startup is gated before any append. Failure, deadline and interruption actively
cancel statements, including after source EOF, and join every owner before return.
Unresolved outcomes preserve owned data and suppress optional DDL cleanup.
Standard main-thread SIGINT is deferred at ownership boundaries; custom/ignored
handlers are preserved. Validator watchers protect startup/join, retain ordinary
query-body interrupts and repeat cancellation across execution-start races.
A broken/noncooperative native driver needs a process supervisor for a hard limit.

Membership validation streams only row IDs to rotating temporary Parquet files.
Strict adjacent checks use a linear sorted fast path; unordered output uses a
bounded DuckDB external sort. There is no global Go DISTINCT state or repeated
wide Redis scan. Logical-ID point samples require exact canonical column names,
order and Arrow physical types, then compare all values with independent
`references.typed_source` SQL casts. Logical IDs never imply source ordinals.
Validation has explicit memory, temporary-disk and time budgets. Provenance keeps
DDL `schema_sha256` separate from `arrow_schema_sha256` and optional source hash.

## Verification and review

The integration coordinator ran the full ClickBench Python suite on Linux:
**156 tests, OK, 3 unrelated Linux-isolation skips**. This includes real native
serial, four-stream and four-stream NOINDEX ingestion on independently verified
Redis Software Flex (database reported Redis 8.8.0) with active 4× QPF. Each native case uses a synthetic
23-row, 105-column source with exact values, uneven groups, reader closure and
native membership checks. Environment: Python 3.14.10, PyArrow 26, ADBC manager
1.13, DuckDB 1.5.6, built Linux C shared driver, owned server network namespace.

Synthetic regressions additionally cover 70,003 sparse/unordered IDs, cross-batch
duplicates, spool rotation, value/schema corruption, budgets, partial thread
startup, EOF cancellation, repeated interrupts and watcher ownership. Independent
agent review reports no outstanding findings; eight focused review checks passed.
`git diff --check` passed. Human maintainer approval is not recorded.

## Remaining evidence

The full-scale load remains stopped. No 99,997,497-row load, all-row value proof,
full-scale throughput/RSS result, reusable backup or backup/restore measurement
was produced. No AWS resources were started. Tiny native tests establish focused
correctness, not a full-scale speedup, memory plateau or completed benchmark.

Product-version wording is clarified in the [measurement provenance erratum](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/provenance-erratum.md); historical timings and settings are unchanged.
