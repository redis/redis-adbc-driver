# Validation: Airflow get_records

Date: 2026-10-29. Source: `95d6583` plus this working-tree change.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

## Environment and reproduction

macOS arm64; Python 3.12.15; Airflow 3.3.2; Arrow provider 0.1.1;
common-sql 2.1.1; ADBC manager 1.12.0; PyArrow 26.0.0.
Disposable Docker image `redislabs/redis:8.2.0-78.18`, server reports Redis 8.6.2,
accessed on localhost:16384. Native library built from this checkout with
`GOCACHE=/private/tmp/redis-adbc-airflow-gocache make -C go build`.

Before the fix, the focused SELECT failed to discover the macOS library. After
explicitly resolving that library for the generic hook, the same SELECT failed
in `AdbcHook.set_autocommit(False)` with `NotSupportedError: transactions are
not supported (autocommit only)`.

## Checks and outcomes

With `PYTHONPATH=$PWD/python`, `REDIS_ADBC_LIBRARY` pointing at the built dylib,
`REDIS_URI=redis://localhost:16384/0` and a separate temporary `AIRFLOW_HOME`:

```sh
python -m pytest -q python/tests/test_airflow.py \
  python/tests/test_installation.py python/tests/test_driver.py::test_autocommit_rejected
```

Result: **8 passed, no skips**. Five Airflow tests cover fetched values, bound
parameters, empty/multiple results, get_first, insert/query with NULL and table
cleanup, rejecting false before connecting or changing driver state, and
forwarding TLS/database/connection options without changing cached extras.

## Review and limits

Self-review found no remaining actionable correctness or ownership issue.
Connection/cursor cleanup remains in the upstream context managers. No native
driver code or transaction semantics changed. The TLS check verifies forwarding,
not a live TLS handshake. Configured Linux CI and Airflow 2.x were not run here;
no human review or release approval is implied.
