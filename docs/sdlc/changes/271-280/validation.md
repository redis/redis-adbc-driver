# Verification and review evidence

Date: 2026-10-20. Starting source: `a1eec58`; results apply to the uncommitted
changes in this directory and the linked Python/scripts/workflow changes.
[Intent](intent.md), [spec](spec.md), [plan](plan.md). Author/reviewer: Codex;
this is an agent review, not human approval.

## Environment

macOS arm64; Python 3.12; local Go 1.27.2 (the release uses go.mod's 1.26.6).
Disposable container `redis-adbc-issues-271-280`, `redis:8.6.2`, standalone with
Search, port 26379, isolated from the user's existing Redis containers. Packages:
ADBC manager 1.12.0, PyArrow 26.0.0, Polars 2.0.0, pandas 3.0.6, SQLAlchemy 2.1.5,
Dagster 1.13.26, DuckDB 1.5.6. Separate Superset 6.1.0 environment uses SQLAlchemy
1.4.54, pandas 2.1.4, PyArrow 18.1.0, sqlglot 28.10.1. Its unconstrained latest
Flask-Caching failed initialization; 2.3.1 works. Superset engine discovery also
needed cachetools. These test-environment dependencies are explicit in CI.

## Checks and outcomes

- `GOCACHE=/private/tmp/redis-adbc-go-cache env -u REDIS_URI go test -count=1 -p 1 ./...`
  from `go/`: all three packages passed. Redis integration tests skip without a
  server. Initial sandbox run could not bind the benchproxy listener; rerun with
  loopback permission passed. No assertions/skips were weakened.
- Shared library: `make -C go build` passed with writable temporary GOCACHE.
  Final local build uses `MACOSX_DEPLOYMENT_TARGET=13.0`,
  `CGO_CFLAGS=-mmacosx-version-min=13.0` and matching CGO_LDFLAGS for Go 1.27.
  Simply changing the deployment environment without CFLAGS reused cached C
  objects targeting newer macOS; the final build regenerated them without warnings.
- `REDIS_URI=redis://localhost:26379/0 GOCACHE=/private/tmp/redis-adbc-go-cache go test -count=1 -p 1 -run 'TestBugIssue|TestSQLBugIssues|TestSQLPostgresCatalogCompatibility|TestSQLCatalogOIDLifecycle|TestSQLCatalogArrayOperations' ./`
  from `go/`: passed, including existing #271 byte-order/SQLSTATE regressions.
- `RUN_DUCKDB_TESTS=1 REDIS_URI=redis://localhost:26379/0 REDIS_ADBC_LIBRARY=<built dylib> python -m pytest -q python/tests/`:
  **17 passed, 1 skipped** (Superset deliberately requires its separate environment).
  This covers DB-API/Polars, manifests, native COLLATE, standalone installation,
  SQLAlchemy 2.x reflection/chart SQL, Arrow/pandas/polars/readers, real Dagster
  definitions/materializations, partition retries and an injected stream failure.
- Separate environment:
  `RUN_SUPERSET_TESTS=1 REDIS_URI=redis://localhost:26379/0 REDIS_ADBC_LIBRARY=<built dylib> python -m pytest -q python/tests/test_sqlalchemy.py python/tests/test_superset.py`:
  **3 passed**. Superset discovers both plugin entry points, uses PostgreSQL
  parsing, connects, lists schemas/tables, persists a dataset and reflects columns,
  executes grouped/time-series dataset queries, and runs its raw SQL Lab cursor
  path. Tests create only temporary local Superset metadata and Redis fixtures.
- Wheel: `python scripts/build_python_wheel.py --library go/build/libadbc_driver_redis.dylib --platform macosx_13_0_arm64 --version v0.1.0 --out /private/tmp/redis-adbc-wheels`:
  built successfully. Installed into a clean virtual environment, without editable
  source installation or REDIS_ADBC_LIBRARY; `python -m pytest -q python/tests/test_driver.py`:
  **4 passed** (native connection, Polars URI loading, manifest discovery, COLLATE).
  The local wheel targets macOS 13 because it uses Go 1.27. Release CI sets Go
  1.26 and explicit C/compiler/linker deployment target 12. The builder rejects a
  library whose Mach-O minimum OS exceeds its tag, and validates ELF/Mach-O arch.
- `ruff check python scripts`, formatter, `sh -n scripts/install.sh`, workflow YAML
  parsing and `git diff --check`: passed. Python files formatted; no Go source changed.

## DuckDB observations

Current community extension downloaded into a temporary directory with
`QUERY_FARM_TELEMETRY_OPT_OUT=1`. Attach by manifest, `r.public.t`, `r.t`, join,
`adbc_scan`, `adbc_insert`, `adbc_execute`, catalog INSERT and CTAS all pass.
Catalog INSERT, adbc_execute and adbc_insert inside explicit BEGIN fail before
changing rows; ROLLBACK succeeds and row counts stay unchanged. READ_ONLY rejects
writes. EXPLAIN shows `ADBC_SCAN_TABLE`, projections `id,country`, `Filters: id=1`.
No latency/throughput or universal Redis predicate-pushdown claim is made.

Upstream Query-farm/adbc_scanner #19/#20 are closed (2026-10-10); behavior was
retested instead of preserving obsolete failures from #278/#280. Older extension
builds still need the documented function-based write workaround.

## Review passes and limitations

Reviewed ownership/lifetimes, quoted SQL names and bound partition values, staged
failure cleanup, native transaction rejection, TLS alias/URL handling, manifest
absolute paths, native wheel tags and release gating. Existing row storage and
Arrow ownership code are unchanged. Retained non-atomic write caveats in code and
docs. Roundtrip tests compare values without relying on unordered row order.

Not run here: Linux amd64/arm64 wheels and distribution matrix, macOS 12 runtime,
full Redis Software/cluster/ADBC-validation/pgdiff suites, browser-rendered Superset
charts, a TLS server handshake, or Columnar's separate DuckDB extension. Those
limits are not represented as passing checks. CI jobs are configured, not yet
executed. Release/PyPI uploads and registry publication were not performed.
