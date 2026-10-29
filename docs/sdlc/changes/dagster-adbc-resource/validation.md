# Validation: community Dagster ADBCResource

Date: 2026-10-29. Base source: `95d6583` with this change applied. Author: Codex.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Environment: macOS arm64, Python 3.12.14, dagster-adbc 0.0.2, Dagster 1.13.27,
ADBC driver manager 1.13.0, PyArrow 26.0.0. Existing disposable container
`aa-ordinary-test-redis-software-1`, image `redislabs/redis:8.2.0-78.18`,
database exposed on localhost:16384. The image identifies the test target;
the server's runtime build and cluster topology were not independently queried.

```sh
GOCACHE=/tmp/redis-adbc-native-go-cache make -C go build
PYTHONPATH=python REDIS_URI=redis://localhost:16384/0 \
  REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
  /tmp/redis-adbc-dagster-venv/bin/python -m pytest -q \
  python/tests/test_dagster_adbc.py python/tests/test_dagster.py \
  --junitxml=/tmp/redis-adbc-community-results.xml
git diff --check
```

Build passed. Tests: **14 passed in 2.52s, zero skips** (six community resource
cases, eight existing IO-manager cases). Direct-library and manifest cases
passed ingestion/fetch, bound values, database-option forwarding, reconnect
persistence, resource injection, downstream asset results and exception cleanup.
Warning-as-error coverage confirms explicit autocommit connects without the
default-setting warning. The first test run had two test-authoring failures
from a nonexistent `get_autocommit` getter; that assertion was removed in favor
of warning-free ingestion and persisted-write checks, then the suite passed.

Self-review under REVIEW.md found no actionable issue: schemas are unique and
cleaned in fixture teardown; manifest discovery is isolated with monkeypatch;
connections/cursors have context-managed lifetimes and Arrow results are fully
consumed. This is local evidence, not a human approval or live CI result. Other
platforms, TLS, clusters, other package versions and the full Python suite were
not tested in this task.
