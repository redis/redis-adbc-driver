# Local verification

Date: 2026-10-29. Base source: `95d6583`, with the added tests in this checkout.

- Native build: `GOCACHE=/private/tmp/redis-adbc-pandas-go-cache make -C go build`
  succeeded on macOS.
- Python 3.12, pandas 3.1.0, PyArrow 26.0.0, SQLAlchemy 2.1.6, ADBC driver
  manager 1.13.0, and the freshly built native library.
- Target: existing disposable Redis Software container
  `aa-ordinary-test-redis-software-1`, database port 16384 on localhost.
- With `PYTHONPATH=python`, `REDIS_ADBC_LIBRARY` pointing at
  `go/build/libadbc_driver_redis.dylib`, and
  `REDIS_URI=redis://127.0.0.1:16384/0`, ran:
  `/private/tmp/redis-adbc-dagster-venv/bin/python -m pytest -q python/tests/test_pandas.py python/tests/test_sqlalchemy.py`.
- Result: **4 passed**, zero skips, in 0.36 seconds. Initial sandbox-only
  attempts could not access the local Redis socket; the successful run had
  authorized local network access.
- PR preparation: cherry-picked the tests onto destination main at `5c1641f`,
  rebuilt the native library, and reran the same command: **4 passed**, zero
  skips, in 0.86 seconds.

Review: assertions check actual fetched values and types, and fixtures clean up
their table. No production code changed. This is local evidence for the listed
versions, not a claim about a version matrix or human approval.
