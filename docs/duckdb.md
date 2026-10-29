# DuckDB

The standalone [`redis_adbc` extension](../duckdb-extension/README.md) targets
DuckDB 2.0 and embeds
the driver for streaming SELECT queries without a separate driver installation.
Its [community-registry submission](https://github.com/duckdb/community-extensions/pull/3157)
has merged; the public extension list still describes DuckDB 1.5.6.
The integration below uses the
existing `adbc_scanner` extension and supports attached catalogs and writes.

**Recommended Redis configuration:** Redis Search (Query Engine) enabled
and query performance factor (QPF) set to **4×** where supported.
Search is required; 4× QPF is recommended for concurrent query performance
and needs sufficient server CPU capacity. Configure these on the Redis server;
see [server requirements](driver-reference.md#server-requirements) and the
[QPF benchmarks](driver-reference.md#query-performance-factor-qpf).

Python integration CI selects the latest stable DuckDB **2.0.x** with
`duckdb==2.0.*`. It builds a pinned commit of
[Query.Farm's v2.0 source](https://github.com/Query-farm/adbc_scanner/tree/v2.0)
against the selected DuckDB release, because matching community binaries are
unavailable. `ADBC_SCANNER_EXTENSION` points the integration test at that local
unsigned build; installation and loading failures remain test failures.

[Query.Farm's adbc_scanner](https://query.farm/products/extensions/adbc_scanner/)
loads the native driver into DuckDB. Install a driver manifest first (see
[Python and native installation](python-integrations.md#install-and-discover-the-driver)),
or replace `driver 'redis'` below with the absolute library path.

When loaded, this extension sends its version and DuckDB's version/platform to
Query.Farm unless `QUERY_FARM_TELEMETRY_OPT_OUT` is set. To opt out, set that
environment variable before launching DuckDB:

```sh
export QUERY_FARM_TELEMETRY_OPT_OUT=1
```

```sql
-- DuckDB 2.0 uses Query.Farm's signed extension repository.
SET allow_extension_repositories = 'allowed';
CREATE EXTENSION REPOSITORY IF NOT EXISTS query_farm
  WITH PREFIX 'https://duckdb-extensions.query.farm';
INSTALL adbc_scanner FROM query_farm;
LOAD adbc_scanner FROM query_farm;
ATTACH 'redis://localhost:6379/0' AS r (TYPE adbc, driver 'redis', READ_ONLY);
SELECT * FROM r.public.orders LIMIT 10;
USE r.public;
SELECT * FROM orders LIMIT 10;
SELECT * FROM adbc_scan('r', 'SELECT country, COUNT(*) AS n FROM public.orders GROUP BY country');
DETACH r;
```

If a binary for your exact DuckDB release/platform has not been published,
build the upstream v2.0 source against that release, as CI does. Do not use a
binary built for a different engine version. The standalone `redis_adbc`
extension is built separately from this repository; it is not `adbc_scanner`.

Use the driver's actual schema (`public` by default, configurable through
`adbc.redis.default_schema`). Current adbc_scanner reads the connection's default
schema; older builds defaulted to `main`, so explicit `r.public.orders` is also
portable across those versions.

For writes, attach without `READ_ONLY` and run outside an explicit transaction:

```sql
ATTACH 'redis://localhost:6379/0' AS r (TYPE adbc, driver 'redis');
CALL adbc_execute('r', 'CREATE TABLE IF NOT EXISTS public.duckdb_orders (id integer, country text)');
SELECT * FROM adbc_insert(
  'r', 'duckdb_orders',
  (SELECT * FROM (VALUES (1, 'US'), (2, 'CA')) AS source(id, country)),
  mode := 'append'
);
SELECT o.id, o.country, c.label
FROM r.public.duckdb_orders o
JOIN (VALUES ('US', 'United States'), ('CA', 'Canada')) c(code, label)
  ON o.country = c.code;
DETACH r;
```

Current community builds also support `INSERT INTO r.public.t` and
`CREATE TABLE r.public.t AS SELECT ...` in autocommit. This was verified with
DuckDB 1.5.6 and the current v1.5 community extension. Older extension builds
reject those writes; upgrade the extension or use `adbc_insert` / `adbc_execute`.
The upstream changes are tracked by
[adbc_scanner #20](https://github.com/Query-farm/adbc_scanner/issues/20) and
[#19](https://github.com/Query-farm/adbc_scanner/issues/19).

The driver still cannot provide multi-statement transactions. A write inside
`BEGIN` fails before changing rows; `ROLLBACK` does not become a no-op successful
transaction. Autocommit statements can partially apply on failure. `READ_ONLY`
rejects writes through the attachment, including the write functions; it is not
a substitute for Redis ACL permissions.

`adbc_scan` sends its SQL to the driver, which uses Redis Search pushdown when
safe. SQL over `r.public.t`, including local joins/aggregates, is planned by
DuckDB. Our three-row smoke query `SELECT country ... WHERE id=1` showed an
`ADBC_SCAN_TABLE` with `Filters: id=1` and projections `id,country` in `EXPLAIN`;
this establishes filter delivery to the scan, not a throughput claim or proof
that every predicate executes in Redis. Use `adbc_scan` with an explicit remote
predicate when remote execution matters, and inspect/measure your own queries.

[Columnar's separate adbc extension](https://github.com/columnar-tech/duckdb-adbc-client)
(`read_adbc`) has not been tested here. Its commands and capabilities are not
interchangeable with these examples.
