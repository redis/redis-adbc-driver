# Validation

Date: 2026-10-29. Source: `95d6583` plus this change. Author/reviewer: Codex
(agent review only). See [intent](intent.md), [specification](spec.md), [plan](plan.md).

## Environment and execution

macOS arm64, Python 3.12, pandas 3.1.0, PyArrow 26.0.0, ADBC manager 1.13.0.
Built the current native library with
`GOCACHE=/tmp/redis-adbc-pandas-go-cache make -C go build`.
Started a dedicated Compose project `adbc-pandas-direct-test` using
`SOFTWARE_API_PORT=19449 SOFTWARE_DB_PORT=16386 make -C go redis-up`.
Setup confirmed Redis Software, Redis 8.6.2, one shard and Search available.

```sh
PYTHONPATH=python \
REDIS_URI=redis://127.0.0.1:16386/0 \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
/tmp/redis-adbc-dagster-venv/bin/python -m pytest -q python/tests/test_pandas.py
```

Result: **7 passed in 0.26s; no skips**. The first sandboxed attempt could not
connect to loopback; rerunning with loopback access produced this result.
`git diff --check` passed.

## Review and limits

Verified full DataFrame values and dtypes, unique fixture names and cleanup,
caller connection reuse, and multiple-batch fetch completion. Warnings are errors
for the default/Arrow read cases, checking that pandas accepts the native ADBC
connection without its unsupported-DB-API fallback warning. No driver fix was
needed. Existing CI automatically collects the new tests.

This run does not validate other pandas versions, table APIs, writes, bound
parameters, pandas chunksize, TLS, Flight SQL, clusters, other platforms or the
full repository suite. No release or remote CI run was performed.
