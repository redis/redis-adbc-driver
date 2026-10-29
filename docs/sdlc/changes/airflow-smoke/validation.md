# Validation: Airflow integration

Date: 2026-10-29. Source: `95d6583` plus this uncommitted change.
Author/reviewer: Codex (self-review only). Status: local checks passed; no
human review, release, remote executor or scheduler deployment performed.
[Intent](intent.md), [spec](spec.md), [plan](plan.md).

## Environment

macOS arm64; Python 3.12.15; Go 1.27.2. Built this checkout using
`make -C go build`, producing `go/build/libadbc_driver_redis.dylib`.
Separate environment: `/tmp/redis-airflow-tests`. Airflow 3.3.2 and Apache Arrow
provider 0.1.1 installed with official Airflow 3.3.2 Python 3.12 constraints.
ADBC driver manager 1.12.0; PyArrow 25.0.1. Python driver installed from `./python[dbapi]`.

Existing local disposable Redis Software service. Management
API readback: Redis Software 8.6.2, active QPF scaling
factor 4, 6 Search workers, scheduling policy `mnp`, 32 connections. No server
configuration changes were needed.

## Checks and results

```sh
RUN_AIRFLOW_TESTS=1 REDIS_URI=redis://localhost:6379/0 \
  REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
  /tmp/redis-airflow-tests/bin/python -m pytest -q python/tests/test_airflow.py
```

Initial run before rebasing: **2 passed in 9.14s**, zero skips or failures. First test runs the
complete load/verify/cleanup DAG twice against its own temporary SQLite
metadata DB, asserting all task states. Four rows produce EU=(2,60), US=(2,40);
the bound amount filter returns IDs 3 and 4. Second test uses AdbcHook to
create a unique table, insert three rows in two batches, fetch count=2 and
sum=50 with a bound filter, and drop the table. It also confirms that default
`get_records()` raises `NotSupportedError`; the initial driver-manager
autocommit warning is expected and explicitly checked.

Without the opt-in flag: **2 skipped**, as intended, without importing Airflow.
`git diff --check`: passed. Final test output is also available locally at
`/tmp/redis-airflow-final-test.log` (temporary, not durable evidence).

## Revalidation on current upstream main

The original checkout had unrelated history to `redis/redis-adbc-driver` main.
Rebased this change onto `c664eb1`, preserving upstream documentation changes.
Rebuilt the native library and reinstalled the current Python package, which
requires PyArrow 26.0.0 or newer. Kept Airflow 3.3.2 and provider 0.1.1 pinned;
the second install intentionally permits PyArrow 26.0.0 beyond the official
Airflow optional-package constraint. No other package upgrades were needed.

`make -C go redis-up` verified Redis Software 8.6.2, one shard, active 4x QPF
and its effective worker/routing configuration. Repeated the command above:
**2 passed in 8.59s**, zero skips or failures. `pip check` reports no broken
requirements. Output: `/tmp/redis-airflow-rebased-test.log` (temporary).

After the Ray Data documentation merged, rebased onto `f287315` and resolved
the shared insertion point in `docs/python-integrations.md` by preserving both
Ray Data and Apache Airflow sections. No driver or Python runtime files changed
between those upstream bases. Verified Software 4x QPF again; focused Airflow
suite: **2 passed in 8.57s**. `git diff --check` passed.

## Review and limits

Reviewed cursor/connection closure, scoped table names, cleanup after errors,
small JSON-only XCom values, metadata isolation, assertions against upstream
failure masking, and documentation claims. No actionable findings remain in
this focused review. The provider's macOS library-discovery fallback is tested
with a temporary manifest alias, and its URI override is tied to the observed
0.1.1 implementation. These are configuration workarounds, not upstream fixes.

The generic TaskFlow path is demonstrated locally. Hook behavior, scheduler
operation, worker distribution, TLS and Linux execution beyond the checks above
are not inferred from these results. Existing broad Go/Python suites were not
rerun because no driver runtime behavior changed.
