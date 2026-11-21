# Long managed ClickBench streams: plan and evidence

Date: 2026-11-20. Source: 1b5918e6. Status: implementation complete; native/performance evidence pending.
Issue: https://github.com/redis/redis-adbc-driver/issues/566

## Implementation and verification

run.py, stream_loading.py, test_stream_loading.py and README.md. Implement source partitioning, long append ownership and progress first; validate failures, exact values and native SQL membership. Full-scale loading and backup performance remain unmeasured and are outside this change; the user stopped the operational load.

Nine focused synthetic tests passed. Full harness command: `uv run --python 3.14 --with pyarrow --with redis --with adbc-driver-manager --with duckdb python -m unittest discover -s benchmarks/clickbench -p 'test_*.py'`: 135 tests passed, 5 skips (3 existing Linux-only and 2 new native fixture tests). Environment: Python 3.14.10, PyArrow 26.0.1. `git diff --check` passed. A first broader run lacked optional DuckDB and failed imports; rerun with the dependency passed.

Self-review checked disjoint prefix coverage, no replay, failed/unknown acknowledgments, EOF versus publication timing, current-reader cleanup, reader/C Arrow ownership, and absence of private operational artifacts. Material behavior still requires human maintainer review; self-review is not approval. Native tests require allocated independently verified Software QPF4; skipped without fixture variables. No AWS, full ingestion, backup creation, speedup or full-scale memory plateau was attempted.

## Review corrections and current evidence

Independent root review found that the initial implementation only stopped input
and could leave an EOF peer sealing, and that global COUNT DISTINCT / repeated
WatchID-filtered native scans were unsuitable for full-scale validation. The
follow-up registers public statement cancellation, checks coordinator deadlines
after EOF, joins every worker/cancel owner, and preserves unknown cancellation
outcomes without replay or optional DDL cleanup. A hard limit for a broken native
driver requires process isolation; no daemon native Arrow owners are abandoned.

Membership now spools only row IDs to rotating Parquet files. Exact adjacent
checks use a linear sorted fast path or a memory/disk bounded DuckDB external
sort for valid unordered SQL output. Destination logical-ID point samples are
compared with independent DuckDB typed_source SQL casts, not ingestion conversion.
Tests include 70,003 sparse/unordered IDs, duplicates across batches, spool
rotation, value corruption and disk/deadline cleanup, plus EOF sibling-error,
deadline and KeyboardInterrupt cancellation.

Current full harness: 146 tests, 5 skips on Python 3.14.10 / PyArrow 26.0.1 /
DuckDB 2.0.1. The 21 focused stream tests also passed with DuckDB 1.5.6 (2 native
skips); a subsequent spool-rotation test passes on the current full suite. Native
tests remain skipped in this worktree, pending root's allocated verified fixture.
`git diff --check` passes. Full-scale throughput/RSS/backup evidence is still absent.

See [intent](intent.md) and [spec](spec.md).

A subsequent independent review identified an uncertain append reply after EOF
without a cancel error. All unresolved worker publication outcomes now preserve
owned data. The regression asserts both the progress state and run.main's shared
DDL-cleanup decision helper refuse cleanup after that unknown result.
