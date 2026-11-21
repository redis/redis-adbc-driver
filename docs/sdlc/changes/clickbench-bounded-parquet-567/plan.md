# Bounded Parquet stream ownership: plan and evidence

Date: 2026-11-20. Baseline: `1b5918e6`.
Status: bounded source, original-group memory/lifetime and local policy tradeoff
acceptance measured; scopes and remaining separate full-ingestion limits stated.
Issue: [#567](https://github.com/redis/redis-adbc-driver/issues/567).
[Intent](intent.md) · [Specification](spec.md)

## Delivered behavior

`stream_loading.py` gives each managed-stream worker one current row-group reader
with explicit `pre_buffer=False`, `memory_map=False`, `buffer_size=0` and decoding
threads disabled. Workers share immutable file metadata, have no producer queue,
and close readers deterministically at transitions, cancellation and failure.
Exported Arrow buffers retain their own lifetime. No per-batch GC or allocator
purge was added. One-worker streams exercise the same bounded source policy.

Arrow batches, current row-group/page buffers, conversion and native allocations
still consume memory; the policy is not a hard RSS cap. Progress separates process
peak RSS, Arrow live bytes and Arrow pool peak. The harness does not directly expose Go heap or container file cache; external
measurements below report these components separately. Source identity,
optional source content hash, DDL/Arrow schema hashes, OS and route are recorded.

The companion validator spools only IDs, rotates Parquet files to bound footer
metadata, and externally sorts valid unordered output under DuckDB memory/spill
limits. Temporary files are removed on success/failure; disk and time are checked.
Destination logical-ID point samples require exact canonical names/order/physical
Arrow types before independent DuckDB SQL value comparison. Query nullability may
widen deliberately. No source ordinal is inferred from a logical ID, no global
Go DISTINCT hash is built, and Redis avoids repeated full-width WatchID scans.
Worker and deadline-watcher ownership remains joined through startup failures,
EOF cancellation and repeated interrupts; unknown outcomes retain owned data.

## Verification and review

The integration coordinator's Linux full ClickBench suite completed with
**156 tests, OK, 3 unrelated Linux-isolation skips**. Real native serial,
four-stream and four-stream NOINDEX cases passed on independently verified Redis
Software Flex (database reported Redis 8.8.0) with active 4× QPF. Synthetic native sources contain 23 rows and
all 105 columns, including exact int64 values beyond double precision, temporal
values, NULL/NUL strings, uneven groups and reader transitions. Full typed results
and native membership were checked after file-reader closure.

Environment: Python 3.14.10, PyArrow 26, ADBC manager 1.13, DuckDB 1.5.6,
built Linux C shared driver and owned server network namespace. Additional source
and validator tests cover large sparse/unordered ID streams, duplicates, physical
type corruption, spool rotation, resource budgets and cancellation ownership.
Independent agent review reports no outstanding findings; eight focused review
checks passed. `git diff --check` passed. Human maintainer approval is not recorded.

## Measurement scope and remaining limits

The source-only full-file passes and native mixed-column controls below establish
observed source RSS plateaus, local policy tradeoffs and separate memory
components. They do not identify the cause of historical client growth. Combined
100M-row native RSS, remote storage tradeoffs and reusable backup/restore remain
unmeasured. No AWS resources were created.

Product-version wording is clarified in the [measurement provenance erratum](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/provenance-erratum.md); historical timings and settings are unchanged.

## Reader measurement closeout

The [issue #567 measurement record](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/issue-567.md) records full original 226-group / 99,997,497-row serial/four-reader source passes, n3 safe warmed 16-group policy comparisons, separate RSS/Arrow/file-cache/native Go observations, exact native all-105-value controls and type/lifetime evidence. Source-only full reads are not reported as full native ingestion. Remote storage and combined 100M native peaks remain unmeasured; that does not add #566's explicit native 100M requirement to #567.
