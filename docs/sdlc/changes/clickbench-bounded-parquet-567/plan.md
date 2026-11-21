# Bounded Parquet stream ownership: plan and evidence

Date: 2026-11-20. Source: 1b5918e6. Status: implementation complete; native/performance evidence pending.
Issue: https://github.com/redis/redis-adbc-driver/issues/567

## Implementation and verification

stream_loading.py, test_stream_loading.py, run.py telemetry and README.md. Test serial/four-worker coverage and all105 values on synthetic Parquet; assert reader policy/lifetime. Record process peak RSS separately from Arrow live/pool peak. Go heap/container cache need external measurement. Full-scale memory plateau and remote read-ahead comparisons remain unmeasured.

Nine focused synthetic tests passed. Full harness command: `uv run --python 3.14 --with pyarrow --with redis --with adbc-driver-manager --with duckdb python -m unittest discover -s benchmarks/clickbench -p 'test_*.py'`: 135 tests passed, 5 skips (3 existing Linux-only and 2 new native fixture tests). Environment: Python 3.14.10, PyArrow 26.0.1. `git diff --check` passed. A first broader run lacked optional DuckDB and failed imports; rerun with the dependency passed.

Self-review checked disjoint prefix coverage, no replay, failed/unknown acknowledgments, EOF versus publication timing, current-reader cleanup, reader/C Arrow ownership, and absence of private operational artifacts. Material behavior still requires human maintainer review; self-review is not approval. Native tests require allocated independently verified Software QPF4; skipped without fixture variables. No AWS, full ingestion, backup creation, speedup or full-scale memory plateau was attempted.

See [intent](intent.md) and [spec](spec.md).
