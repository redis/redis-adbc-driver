# Isolated SF1 analytical query harness

Compares native DuckDB, direct Redis ADBC, and optionally DuckDB over a Redis ADBC catalog. This is an informal end-to-end comparison, not an audited TPC result. Full-suite runs remain stopped; the current authorization covers a live third-path smoke check and TPC-H Q19 only.

Setup, each engine, and validation run in fresh sequential processes. The coordinator imports neither database engine. DuckDB exits before Redis starts; the Redis process opens no source DuckDB database or reference results. Each engine performs one warm-up and three fully fetched timed runs. Export/validation occur outside timing. Private typed result files retain exact Python values; external sorting uses 4096-row chunks and 32-way merges, preserving duplicates and the numeric tolerance (relative 1e-7, absolute 1e-6). Output ordering and exact cross-engine type identity are not validated. Temporary files are trusted child-created pickle files inside a mode-0700 directory; never supply untrusted artifacts.

Failures distinguish setup/execute/export, engine crashes, and separate validation failures. No DuckDB reference means no Redis-backed queries. Ratios require successful validation. Historical shared-worker output and duplicate query records are refused; use a new output directory. A supervisor cancellation/outer timeout kills its entire query process group. Each statement has a 60-second deadline; process ceilings include result export and validation overhead.

## Prerequisites

Run inside a Linux ARM64 client sharing an owned Redis Software server's network namespace, with authenticated TLS and effective active 4× QPF verified before tests. Do not substitute Redis Community or unsupported services. The caller supplies a dedicated preloaded fixture or explicitly chooses to load it. No database is automatically reset.

Install DuckDB, the Redis ADBC release wheel, PyArrow and ADBC driver manager. A private directory supplies `connection.json` (`uri` and optional `db_kwargs`), `server.json` containing verified public settings only, and `tpch-sf1.duckdb` or `tpcds-sf1.duckdb`. First-time preparation uses the DuckDB TPC-H/TPC-DS extension generators at SF1. SSB also needs upstream `ssb-tools` and generated `ssb-dbgen` files. Set `SSL_CERT_FILE`. Data, credentials, license files and operational identifiers must stay outside public output.

The caller owns server topology, resource limits, source/release checksums, and provenance. `run.py` does not provision servers or verify their management API itself. Preparation/loading is separate from query timing. SQL is exported unchanged into `OUTPUT/SUITE/queries/`; the query-only path requires that export to exist. To avoid silently loading additional tables, focused debugging should preload only the referenced tables independently.

## Explicit client thread profile

Optional integer `client_threads` in private `connection.json` selects the native DuckDB and DuckDB-over-Redis thread setting. The default remains **4** for historical profile compatibility; explicit values from **1 through 32**, such as `16`, are accepted. Booleans, strings, fractional values and out-of-range counts are rejected before opening a DuckDB connection. A query-only/focused caller must retain the selected setting in its own preflight/provenance as well as its runtime resource allocation.

Setup metadata records `client_threads` and retains legacy `client_cpus` with the configured count. DuckDB worker provenance records only the numeric thread setting, alongside existing sanitized third-path fields. These are configured values, not measurements of effective CPU quota or host cores. The caller must independently verify the Linux client's CPU allocation and Redis driver's worker budget/GOMAXPROCS profile. This option does not configure Redis workers or alter the 8 GB DuckDB memory limit, statement timeout, data, isolation or validation rules. A 16-thread run is a separate resource profile; do not relabel or directly attribute differences from historical four-CPU runs to code alone.

## Focused execution

For a future authorized Q19 run, with its fixture already loaded and the unmodified SQL exported to the new output directory:

```sh
python run.py tpch --private /private --output /work/results-q19 --reuse-loaded --query 19 --include-duckdb-adbc
python report.py --input /work/results-q19/tpch --output /work/results-q19/Q19.md
```

The command above selects only Q19; do not invoke `supervise.py` for a focused run. Full suite execution is a separate explicit action; neither tests nor report generation starts it.

## Optional third engine

`--include-duckdb-adbc` adds `duckdb_adbc` records alongside `duckdb` and `redis`. DuckDB uses Query.Farm's `adbc_scanner` to attach a read-only Redis catalog using the same installed native library and `db_kwargs` as the direct route. It opens no local source database. Unmodified SQL runs after `USE r.public` (or the configured default schema). Whole-query remote pushdown is disabled with `query_pushdown false`; DuckDB plans joins/aggregates, while scan filter/projection delivery remains extension-dependent. This is not an `adbc_scan` wrapper executing the whole query as remote Redis SQL.

The signed Query.Farm repository supplies an extension for the installed DuckDB ABI. `ADBC_SCANNER_EXTENSION` can explicitly select a trusted matching local build; that opt-in allows unsigned extensions. A missing binary, unsupported option or attach failure is recorded, never replaced by native DuckDB. Driver and Query.Farm telemetry are disabled. Public records include extension version and native-library SHA-256, excluding paths/credentials. See [upstream v2.0 documentation](https://github.com/Query-farm/adbc_scanner/tree/v2.0) and [driver integration](../../docs/duckdb.md).

Private `connection.json` may opt into a compatible custom scanner with
`"duckdb_adbc_scan_threads": 16` (integer 1–1024). The default is **1** and omits
`scan_threads` from ATTACH entirely for released-scanner compatibility. Values
above one add that scanner option alongside `READ_ONLY` and
`query_pushdown false`; unsupported extensions fail without a serial retry.
This setting is independent of `client_threads` and Redis driver options such as
`db_kwargs["adbc.redis.partition_count"]`, which are forwarded unchanged.
Provenance records only numeric `requested_scan_threads`; effective readers and
partition concurrency require separate verification.

A live six-row smoke check on Redis Software with verified 4× QPF matched direct Redis ADBC for grouped exact decimals, duplicates and NULLs. DuckDB's plan showed an ADBC table scan, filter delivery, and local hash aggregation. It used DuckDB 2.0.1 and scanner revision `98ff0a39fc`. SF1 results are separate evidence, not inferred from this smoke test.

## Checks

```sh
python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py' -v
```

Nine synthetic-adapter tests use real child processes without database queries: distinct PIDs/import isolation, crash attribution, missing references, mismatch/no-ratio, validator timeout, private cleanup, output reuse refusal, credential redaction, bounded multi-pass sorting, truncation and separate third-path reporting. Related harness issue: [#596](https://github.com/redis/redis-adbc-driver/issues/596). Historical OOMs from a shared worker cannot establish the Redis driver's isolated memory requirement.

## Focused live follow-up

See [Q19 follow-up](Q19-FOLLOWUP.md) for the isolated SF1 measurements: direct Redis ADBC validates, while the third-path Q19 warm-up hits the 60-second deadline (tracked in #599). Full suites remain stopped.
