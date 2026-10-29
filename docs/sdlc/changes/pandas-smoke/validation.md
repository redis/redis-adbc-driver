# Validation

## Original PR evidence

The following records the original private-repository run. Its test filename
was `python/tests/test_pandas.py`; the Redis port uses
`python/tests/test_pandas_writes.py`. Current port validation is recorded below.

Date: 2026-10-29. Source: `95d6583` plus this uncommitted test/documentation change.
Author: Codex. Local verification only; no release or human approval recorded.

Environment: macOS arm64, Python 3.12.14, pandas 3.1.0, PyArrow 26.0.0,
ADBC manager 1.13.0, pytest 9.2.0, Go 1.27.2. Local Redis Software container
image: `redislabs/redis:8.2.0-78.18`, host port 6379.

Build: `GOCACHE=/private/tmp/redis-adbc-pandas-go-cache make -C go build` passed.
Focused run (using an existing Python environment containing dependencies):

```sh
PYTHONPATH="$PWD/python" \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
REDIS_URI=redis://127.0.0.1:6379/0 \
/private/tmp/redis-adbc-dagster-venv/bin/python -m pytest -q python/tests/test_pandas.py
```

Result: **3 passed**, no skips, in 0.38 seconds. Initial runs corrected test
timestamp parsing and aligned the source string dtype with pandas' Arrow read
dtype; no driver changes were needed. Sandbox localhost access was denied;
the successful run used approved access outside the sandbox.

PR preparation: rebased the change onto `c997337` (the repository's current
`main`), rebuilt the native library and ran `make -C go redis-up`. The setup
verified Redis Software, Redis 8.6.2, one shard and active 4x QPF, including
effective Search workers and routing. The same focused command then passed:
**3 passed**, no skips, in 0.71 seconds. `git diff --check` also passed.

Review: each test uses a generated table name, closes cursors and connections,
cleans its table in a finalizer and compares complete fetched results.
Replacement is tested only on the test's own table. No production data, Redis
flush, driver behavior changes, performance claims or full-suite run.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Redis repository port

Date: 2026-10-29. Port base: `71625e153e5b682715e99be1052f5dcaec689a49`.
Source: original PR #517, commit `b38c9f2cd771769c865d23e549ccc0d9ac470a0d`.
The source tests are preserved in `python/tests/test_pandas_writes.py` so the
Redis repository's existing `test_pandas.py` and `test_pandas_direct_reads.py`
remain intact. The write example is added alongside the existing read guidance.

Build: `GOCACHE=/private/tmp/redis-adbc-pandas-port-go-cache make -C go build`
passed. `make -C go redis-up` verified Redis Software, Redis 8.6.2, one shard,
Search and active 4x QPF. Environment: macOS arm64, Go 1.27.2, pandas 3.1.0,
PyArrow 26.0.0, ADBC manager 1.13.0 and pytest 9.2.0.

```sh
PYTHONPATH="$PWD/python" \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
REDIS_URI=redis://127.0.0.1:6379/0 \
/private/tmp/redis-adbc-dagster-venv/bin/python -m pytest -q \
  python/tests/test_pandas_writes.py \
  python/tests/test_pandas_direct_reads.py python/tests/test_pandas.py
```

Result: **12 passed**, no skips, in 1.01 seconds. `git diff --check` passed.
The integration run used approved local network access outside the sandbox.
The existing CI Python integration command discovers the added test module.
No full-suite run or hosted CI result is claimed here.

Port review: checked the source diff against the new module, preserved existing
read/SQLAlchemy coverage, inspected unique table cleanup and complete DataFrame
comparisons, and verified documentation links. No actionable findings from this
self-review; no human approval recorded.
