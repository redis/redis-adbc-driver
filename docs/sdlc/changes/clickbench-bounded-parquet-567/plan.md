# Bounded Parquet stream ownership: plan and evidence

Date: 2026-11-20. Baseline: `1b5918e6`.
Status: bounded source policy and focused correctness validated;
full-scale memory/performance acceptance remains open.
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
peak RSS, Arrow live bytes and Arrow pool peak. Go heap and container file cache
remain explicitly unavailable and require external measurement. Source identity,
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
Software 8.8 Flex with active 4× QPF. Synthetic native sources contain 23 rows and
all 105 columns, including exact int64 values beyond double precision, temporal
values, NULL/NUL strings, uneven groups and reader transitions. Full typed results
and native membership were checked after file-reader closure.

Environment: Python 3.14.10, PyArrow 26, ADBC manager 1.13, DuckDB 1.5.6,
built Linux C shared driver and owned server network namespace. Additional source
and validator tests cover large sparse/unordered ID streams, duplicates, physical
type corruption, spool rotation, resource budgets and cancellation ownership.
Independent agent review reports no outstanding findings; eight focused review
checks passed. `git diff --check` passed. Human maintainer approval is not recorded.

## Remaining evidence

Full-scale serial/four-worker RSS plateaus, Go/Arrow/file-cache attribution,
throughput tradeoffs and remote read-ahead comparisons remain unmeasured. These
small correctness cases do not establish bounded full-scale RSS or identify the
cause of historical client growth. The stopped full-scale load was not resumed;
no AWS resources, reusable backup or backup/restore measurements were created.
