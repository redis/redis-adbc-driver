# redis_adbc for DuckDB

`redis_adbc` embeds the Redis ADBC driver and Go runtime in one native DuckDB
extension. It provides a streaming table function:

```sql
SELECT * FROM redis_adbc_scan(
    'redis://localhost:6379/0',
    'SELECT id, country FROM public.orders WHERE id > 100'
);
```

The [registry submission](https://github.com/duckdb/community-extensions/pull/3157)
has merged for DuckDB 2.0. `INSTALL redis_adbc FROM community` requires a
successful registry build and publication for your exact engine/platform.
For local development, build from source and load the resulting file with
unsigned extensions enabled in your DuckDB client.

## DuckDB 2.0 build

This submission targets DuckDB **2.0**, using the community registry's exact
source commit `d54cbbe3df17e8e5ce68312f483b4155836112aa` and its
`v2.0-cyanoptera` extension tools. The registry descriptor's `repo.ref` selects
this target. See [community submission #3157](https://github.com/duckdb/community-extensions/pull/3157).

Requires CMake, Ninja or Make, a C/C++17 compiler, Python 3, and network access
to fetch the pinned Go 1.26.10 toolchain and Go modules. Linux amd64/arm64 and
macOS amd64/arm64 are build targets; Windows and WebAssembly are excluded.
macOS requires version 12 or newer. No Go installation is required: CMake
downloads a platform-specific archive from go.dev and verifies its SHA-256.

```sh
git submodule update --init --recursive
OVERRIDE_GIT_DESCRIBE=v2.0.0 GEN=ninja make release
```

Output: `build/release/extension/redis_adbc/redis_adbc.duckdb_extension`.
Use the matching DuckDB 2.0 engine built from the pinned `duckdb` submodule.
The extension needs no ADBC driver manager, separate driver library, driver
manifest, or `adbc_scanner` extension at runtime.

Linux builds use the system allocator. Combining jemalloc's large TLS block
with Go's initial-exec TLS in the same loadable binary exceeds glibc's default
static-TLS budget; `extension_config.cmake` disables jemalloc to allow normal
extension loading without preloading libraries or changing loader settings.

For a quick build against an existing DuckDB checkout:

```sh
cmake -S /path/to/duckdb -B build/duckdb -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DOVERRIDE_GIT_DESCRIBE=v2.0.0 \
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
python -m pip install pyarrow adbc-driver-manager
REDIS_DUCKDB_CLI="$PWD/build/release/duckdb" \
REDIS_ADBC_EXTENSION="$PWD/build/release/extension/redis_adbc/redis_adbc.duckdb_extension" \
REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.so" \
REDIS_URI=redis://localhost:16389/0 \
python duckdb-extension/verify_cli.py
```

Use `.dylib` for the fixture library on macOS. Offline argument-validation
SQLLogicTests are in `test/sql/redis_adbc.test` and run through `make test`.

The CLI harness checks 13 runtime scenarios, physical column types and every
value in 10,003 rows against the matching 2.0 engine. CI runs it on Linux
amd64/arm64 and builds/tests the extension offline on macOS arm64. It does not
depend on the availability of a matching Python DuckDB wheel. The separate
`test/test_native.py` tests can also be run when a Python DuckDB wheel matching
the extension's engine version is available.
