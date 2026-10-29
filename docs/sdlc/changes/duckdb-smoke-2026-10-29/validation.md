# DuckDB local verification, 2026-10-29

Author: Codex. Tested driver source: `95d6583`. Local results only; these are
not results for later main-branch commits or CI. See [intent](intent.md),
[specification](spec.md), and [plan](plan.md).

## Environment

- macOS arm64; Python 3.13.16; DuckDB 1.5.6.
- PyArrow 26.0.0; adbc-driver-manager 1.13.0; pytest 9.2.0.
- Redis Software image `redislabs/redis:8.2.0-78.18`, database Redis 8.6.2,
  one shard, Search available (`FT._LIST` succeeded).
- Redis Software REST API reported `query_performance_factor` active with
  `scaling_factor: 4` immediately before the standalone suite.
- Local disposable endpoint: `redis://localhost:6379/0`.

## ADBC scanner

Built the shared library with `make -C go build` (exit 0), then ran:

```sh
REDIS_URI=redis://localhost:6379/0 \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
RUN_DUCKDB_TESTS=1 QUERY_FARM_TELEMETRY_OPT_OUT=1 \
go/validation/.venv/bin/python -m pytest -v -s python/tests/test_duckdb.py
```

Result: exit 0; **1 passed in 2.29 seconds**, no failures or skips. The test
downloaded the community adbc_scanner extension, then checked attached catalog
reads, a local join, remote SQL, Arrow ingestion, SQL inserts, CTAS, explicit
transaction rejection and read-only enforcement. EXPLAIN showed
`ADBC_SCAN_TABLE`, projections `id,country`, and `Filters: id=1`; this confirms
filter delivery to the scan, not that every predicate executes in Redis.

## Standalone redis_adbc

Used the pinned DuckDB source commit
`069cc9f9b5be802405797faecc284961b07c70ef`, matching the Python engine's
reported source ID `069cc9f9b5`. Source was extracted from an existing local
checkout into `build/duckdb-src`. The build downloaded and verified the pinned
Go 1.26.6 toolchain and embedded the Go driver/runtime. CMake 4.4.4 and Ninja
1.13.2 were installed in the isolated test environment.

```sh
export PATH="$PWD/go/validation/.venv/bin:$PATH"
cmake -S build/duckdb-src -B build/duckdb -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DDUCKDB_EXTENSION_CONFIGS="$PWD/extension_config.cmake" \
  -DBUILD_UNITTESTS=OFF -DBUILD_SHELL=OFF -DEXTENSION_STATIC_BUILD=OFF \
  -DOVERRIDE_GIT_DESCRIBE=v1.5.6-0-g069cc9f9b5 \
  -DGIT_COMMIT_HASH=069cc9f9b5
cmake --build build/duckdb --target redis_adbc_loadable_extension -j 4
REDIS_ADBC_EXTENSION="$PWD/build/duckdb/extension/redis_adbc/redis_adbc.duckdb_extension" \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
REDIS_URI=redis://localhost:6379/0 \
go/validation/.venv/bin/python -m pytest -v duckdb-extension/test \
  --junitxml=build/standalone-duckdb-results.xml
```

Build and pytest exited 0. Result: **16 passed in 6.89 seconds**, no failures,
errors or skips. Coverage includes all values and Arrow types in 10,003 rows
(NULLs, Unicode, booleans, decimals and dates), projections, filtering, count,
join, empty results, LIMIT/early closure followed by a full fetch, SELECT CTEs,
prepared reexecution, changed-schema detection, stream error recovery, repeated
connection failures, invalid arguments, non-read SQL rejection and disabled
external access. Fixture teardown completed; test tables were removed.

The separate ADBC library seeds fixtures only. Standalone reads execute through
the extension's embedded driver. The local unsigned quick build resolves
DuckDB symbols from its host; it is not a distributable release build.

## Review and limits

Documentation was checked against captured commands and output, with relative
links and whitespace checked locally. No runtime changes or test relaxations
were needed. Human approval remains pending on the containing PR.

This run covers macOS arm64 and DuckDB 1.5.6 only. It does not verify DuckDB 2.0,
Linux, registry installation/publication, SQLLogicTests, a release/distributable
build, or performance. Suite durations above are test-run metadata, not query
benchmarks. Generated binaries and JUnit XML remain local under ignored build
directories and are not included in this documentation change.
