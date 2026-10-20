# redis_adbc for DuckDB

`redis_adbc` embeds the Redis ADBC driver and Go runtime in one native DuckDB
extension. It provides a streaming table function:

```sql
SELECT * FROM redis_adbc_scan(
    'redis://localhost:6379/0',
    'SELECT id, country FROM public.orders WHERE id > 100'
);
```

The extension is being submitted to the community registry. Until accepted,
build it from source and load the resulting file with unsigned extensions
enabled in your DuckDB client. `INSTALL redis_adbc FROM community` will work
only after the registry accepts and builds it.

## Build

Requires CMake, Ninja or Make, a C/C++17 compiler, Python 3, and network access
to fetch the pinned Go 1.26.6 toolchain and Go modules. Linux amd64/arm64 and
macOS amd64/arm64 are build targets; Windows and WebAssembly are excluded.
macOS requires version 12 or newer. No Go installation is required: CMake
downloads a platform-specific archive from go.dev and verifies its SHA-256.

```sh
git submodule update --init --recursive
GEN=ninja make release
```

Output: `build/release/extension/redis_adbc/redis_adbc.duckdb_extension`.
Use the same DuckDB version as the build's pinned `duckdb` submodule (1.5.6).
The extension needs no ADBC driver manager, separate driver library, driver
manifest, or `adbc_scanner` extension at runtime.

For a quick build against an existing DuckDB checkout:

```sh
cmake -S /path/to/duckdb -B build/duckdb -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DDUCKDB_EXTENSION_CONFIGS="$PWD/extension_config.cmake" \
  -DBUILD_UNITTESTS=OFF -DBUILD_SHELL=OFF -DEXTENSION_STATIC_BUILD=OFF
cmake --build build/duckdb --target redis_adbc_loadable_extension
```

This quick build resolves DuckDB symbols from the host process. Distributable
builds should use the standard `make release` path, which embeds DuckDB's
required symbols as configured by its extension build tools.

## Behavior and limits

- Requires Redis Search, including for constant queries. Redis Cloud, Redis
  Software and Redis 8.x with Search are supported by the driver; Redis Flex,
  Valkey and Redis 7.2 derivatives are unsupported.
- The query must be exactly one SELECT. SELECT CTEs and set operations work;
  DDL, DML, transaction commands, and scripts are rejected before connecting.
  The Redis driver's parser and supported SQL determine accepted syntax.
- Schema discovery connects to Redis and describes the query without executing
  it. Execution opens a fresh connection and checks that the result schema
  still matches. A prepared statement can execute repeatedly. There is no
  snapshot spanning schema discovery and execution.
- Arrow batches are streamed, including NULLs and supported decimal/date types.
  Each scan has one consumer thread and owns its connection. LIMIT/close and
  errors release the Arrow stream, statement, connection, and database.
- SQL inside the function runs in the Redis driver and may use Search pushdown.
  DuckDB applies surrounding filters, projections, joins and aggregates locally.
  Put predicates in the remote SQL when remote execution matters.
- This is a read interface, with no ATTACH catalogs, writes, ingestion API,
  bound query parameters, or transaction support. For those integration APIs,
  see the existing [adbc_scanner workflow](../docs/duckdb.md).
- `rediss://` uses the driver's TLS verification. Credentials in URI literals
  can appear in DuckDB query history/profiling. Use client parameters and Redis
  ACLs appropriate to the data. `enable_external_access=false` rejects scans.

## Tests

Use a disposable Redis server with Search; tests create and drop unique tables.
The test fixture uses a separately built ADBC library only to seed test data.
The extension reads through its embedded driver.

```sh
make -C go build
python -m pip install duckdb==1.5.6 pytest pyarrow adbc-driver-manager
REDIS_ADBC_EXTENSION="$PWD/build/release/extension/redis_adbc/redis_adbc.duckdb_extension" \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" \
REDIS_URI=redis://localhost:16389/0 \
python -m pytest -q duckdb-extension/test
```

Use `.dylib` for the fixture library on macOS. Offline argument-validation
SQLLogicTests are in `test/sql/redis_adbc.test` and run through `make test`.

For a different DuckDB version, build its matching extension and CLI, then run
`python duckdb-extension/verify_cli.py` with the same environment variables
plus `REDIS_DUCKDB_CLI=/absolute/path/to/duckdb`. It checks 12 runtime scenarios
and every value in 10,003 rows without depending on a matching Python DuckDB
wheel. This harness was also run against the community registry's DuckDB 2.0
source commit `d54cbbe3df17e8e5ce68312f483b4155836112aa`.
