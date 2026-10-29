# Validation

Date: 2026-10-29. Source: public main `da1e7c1` plus this change. Author/reviewer: Codex
(agent review only). See [intent](intent.md), [specification](spec.md), [plan](plan.md).

## Environment and execution

macOS arm64, Python 3.12, pandas 3.1.0, PyArrow 26.0.0, ADBC manager 1.13.0.
Built the current native library with
`GOCACHE=/tmp/redis-adbc-pandas-go-cache make -C go build`.
Started a dedicated Compose project `adbc-pandas-direct-test` using
`SOFTWARE_API_PORT=19449 SOFTWARE_DB_PORT=16386 make -C go redis-up`.
Setup confirmed Redis Software, Redis 8.6.2, one shard, active verified 4x QPF
(including effective Search workers/routing), and Search available.

```sh
PYTHONPATH=python \
REDIS_URI=redis://127.0.0.1:16386/0 \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
/tmp/redis-adbc-dagster-venv/bin/python -m pytest -q python/tests/test_pandas.py python/tests/test_pandas_direct_reads.py
```

Result: **9 passed in 0.91s; no skips** (two existing cases and seven new cases).
The original checkout at `95d6583` separately passed the seven new cases in
0.26s, before discovering the public repository has unrelated history. That
earlier run did not verify QPF and is superseded by the current-main run above.
Loopback access was enabled for the successful runs.
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
