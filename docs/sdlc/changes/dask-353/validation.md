# Validation: partitioned Dask reads

Date: 2026-10-20. Source: `faa03dd` plus this working-tree implementation.
Author: Codex. Status: locally verified; no human review or release approval.
[Intent](intent.md), [spec](spec.md), [plan](plan.md).

## Environment

macOS arm64, Python 3.12.14, isolated `redis:8.6.2` container bound only to
`127.0.0.1:16390`, local `go/build/libadbc_driver_redis.dylib`.
Temporary environment: `/private/tmp/redis-adbc-dask-353-venv`.
Dask/Distributed 2026.8.0, pandas 3.0.6, PyArrow 26.0.0, manager 1.12.0.

## Evidence

Commands below ran from the repository root with `REDIS_URI=redis://localhost:16390/0`
and `REDIS_ADBC_LIBRARY=$PWD/go/build/libadbc_driver_redis.dylib` for integration
execution. Only the isolated container was used. During this task, the unrelated
DuckDB work was committed separately as `a08573b`; final checks used that HEAD
with these Dask changes uncommitted.

- Current environment: `python -m pytest -q python/tests/ -rs` — **24 passed,
  2 skipped**. Skips were the explicitly opt-in DuckDB community-extension
  download and separate Superset application environment.
- `python python/examples/dask_read.py` — passed, two worker processes,
  partition sizes `[1, 2, 2, 0, 1]`, all six rows and Arrow dtypes matched.
- Minimum environment `/private/tmp/redis-adbc-dask-353-min`:
  Dask/Distributed 2024.5.0, dask-expr 1.1.0, pandas 2.3.3, PyArrow 14.0.2,
  NumPy 1.26.4, manager 1.12.0. `python -m pytest -q python/tests/test_dask.py`
  — **8 passed**. This includes the two-process distributed read.
- Initially tested Dask 2024.4.0: failed during its import with an
  `inspect.signature`/property descriptor TypeError on Python 3.12.14.
  Raised the dependency floor to the successfully tested 2024.5.0.
- Ruff checks and formatting for all three new Python files — passed.
- `git diff --check` — passed.

Coverage includes lazy graph construction without connecting, invalid input
rejection, cursor/connection cleanup after execute/fetch errors, schema mismatch
rejection, exact row coverage at range boundaries, a separate NULL partition,
empty-partition dtypes, a Dask projection and an unparameterized query.

## Review

Reviewed all changed files using the three passes in `REVIEW.md`. No actionable
correctness, lifecycle or ownership findings remain. Parameter values stay bound;
test/example identifiers are generated locally. No native query or storage code
changes. Documentation makes the explicit partition/snapshot/memory contract
visible. This is an agent review, not human approval. Linux/arm64/amd64 CI and
release-wheel installation are not established by this local source-install
validation. The disposable test container is removed after verification.
