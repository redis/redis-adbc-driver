# Isolated SF1 analytical query harness

Compares native DuckDB, direct Redis ADBC, and optionally DuckDB over a Redis ADBC catalog. This is an informal end-to-end comparison, not an audited TPC result. Full-suite runs remain stopped; the current authorization covers a live third-path smoke check and TPC-H Q19 only.

Setup, each engine, and validation run in fresh sequential processes. The coordinator imports neither database engine. DuckDB exits before Redis starts; the Redis process opens no source DuckDB database or reference results. Each engine performs one warm-up and three fully fetched timed runs. Export/validation occur outside timing. Private typed result files retain exact Python values; external sorting uses 4096-row chunks and 32-way merges, preserving duplicates and the numeric tolerance (relative 1e-7, absolute 1e-6). Output ordering and exact cross-engine type identity are not validated. Temporary files are trusted child-created pickle files inside a mode-0700 directory; never supply untrusted artifacts. Each engine worker hashes the exact bytes it writes, fsyncs each result file and records its size and SHA-256 in its status file. The validator hashes the same bytes it decodes, including its own sort/merge scratch runs, and checks them against that record. A file that cannot be decoded, or that decodes but does not match its record, is `validation_failed` with an `<engine>_artifact_problem` (file, role, decode offset, objects decoded, zero bytes at that offset, first all-zero 4 KiB page, recorded and read-back sizes/digest prefixes). It is never reported as a `mismatch`: it says nothing about whether the engine's result was correct.

Failures distinguish setup/execute/export, engine crashes, and separate validation failures. No DuckDB reference means no Redis-backed queries. Ratios require successful validation. After a failed validation (mismatch, unreadable artifact or validator timeout) the engine's result and status files, with copies of the DuckDB reference, move to `failed-artifacts/<suite>-q<query>-<engine>-<UTC>-*/` inside the private profile (mode 0700), and the record names that folder relative to the profile in `<engine>_retained_artifacts`. They are trusted pickles like the originals; inspect them only with this harness and delete them when finished. Historical shared-worker output and duplicate query records are refused; use a new output directory. A supervisor cancellation/outer timeout kills its entire query process group. Each statement has a 60-second deadline; process ceilings include result export and validation overhead.

## Prerequisites

Run inside a Linux ARM64 client sharing an owned Redis Software server's network namespace, with authenticated TLS and effective active 4× QPF verified before tests. Do not substitute Redis Community or unsupported services. The caller supplies a dedicated preloaded fixture or explicitly chooses to load it. No database is automatically reset.

Install DuckDB, the Redis ADBC release wheel, PyArrow and ADBC driver manager, plus `redis-py` for the [Flex compaction gate](#flex-index-compaction-gate). A private directory supplies `connection.json` (`uri` and optional `db_kwargs`), `server.json` containing verified public settings only, and `tpch-sf1.duckdb` or `tpcds-sf1.duckdb`. First-time preparation uses the DuckDB TPC-H/TPC-DS extension generators at SF1. SSB also needs upstream `ssb-tools` and generated `ssb-dbgen` files. Set `SSL_CERT_FILE`. Data, credentials, license files and operational identifiers must stay outside public output.

The caller owns server topology, resource limits, source/release checksums, and provenance. `run.py` does not provision servers or verify their management API itself. Preparation/loading is separate from query timing. SQL is exported unchanged into `OUTPUT/SUITE/queries/`; the query-only path requires that export to exist. To avoid silently loading additional tables, focused debugging should preload only the referenced tables independently.

## Flex index compaction gate

On Redis Software Flex, a new Search index keeps one disk entry per document per field until the disk GC's first cycle compacts it. Until then every NUMERIC term scans all of a field's entries: about 0.6 s per term at 6.0M documents, against 6–11 ms afterwards. The first cycle came 5.9–9.4 minutes after `FT.CREATE` on the measured fixture, and Redis Software gives clients no way to start it. See [flex-index-compaction](../../docs/sdlc/changes/flex-index-compaction/spec.md).

So on a Flex profile (`db_kwargs["adbc.redis.flex"]` is `"true"`, or `server.json` has `search_on_bigstore: true`) the harness does not time an index that has not been compacted:

- **Setup** (after loading, or the count check with `--reuse-loaded`) polls `FT.INFO` for every Search index every 15 s, for up to 15 minutes. It maps each index to the loaded table whose columns contain its public fields (driver fields start with `__`). Every table needs at least one index, and each of its indexes must show `gc_stats.total_cycles` ≥ 1 and `disk_num_keys` below `num_docs` for every NUMERIC field that has disk entries. A field no document has, such as the driver's deletion marker on a table without deletes, reports no `disk_num_keys` and has nothing to scan; an index where no NUMERIC field reports them is not verified. Otherwise setup fails with `suite_error`, so `--load-only` exits non-zero and `supervise.py` stops before any query. Indexes with fewer documents than the verified `search-fork-gc-clean-threshold` (default 100) do not block: the GC may never run on them, and their full scan is that small.
- **Each query** rechecks the same state just before timing, without waiting. It refuses with a `flex_compaction_refused` event if setup recorded no passed gate (run `--load-only`, with `--reuse-loaded` for a loaded fixture, first) or if any index no longer qualifies. After a shard restart `total_cycles` starts again at 0, so a restarted fixture is refused until a new cycle runs. Under `supervise.py` a refused query gets a coordinator-failure record (`failed_phase` `coordinator`) and the pass continues with the next query.
- **Opt-in small-index exemption:** small single-batch indexes may never get a first cycle. On 2026-11-26 SSB `date_` (2,556 rows) and `supplier` (2,000) had none after 26 and 19.5 minutes ([record](../../docs/sdlc/changes/flex-ssb-compacted/validation.md#small-indexes-without-a-first-cycle)). A profile can set `"flex_compaction_exempt_max_docs"` (an integer from 1 to 10,000) in `connection.json`; the default is strict. An uncompacted index with at most that many documents is then `exempt_uncompacted` instead of blocking. Every exempt table needs its `FT.CREATE` time (UTC, from the shard log's "Successfully created index") in a private `flex-index-created.json`, for example `{"date_": "2026-11-26T04:03:16.434Z"}`. The gate also profiles one existing point term on the table's first NUMERIC column (`FT.PROFILE … NOCONTENT LIMIT 0 0`). Without the time or the profile the exemption is refused. Larger indexes are never exempt, and an exempt table that has compacted is recorded under `exemption.not_needed`.
- **Provenance:** `metadata.json` (`flex_index_compaction`) and each query record keep, per table and index, `num_docs`, `gc_total_cycles`, the largest and each NUMERIC field's `disk_num_keys`, plus the wait. Exempt indexes add `uncompacted_reasons`, `ft_create_utc`, `minutes_since_ft_create` and `point_term_profile` (total and iterator milliseconds), and the state lists `exemption.applied_to`. Index names and key prefixes are left out. `events.jsonl` records the wait (`flex_compaction_wait`, `flex_compaction_waiting`, `flex_compaction_ready`).

The gate reads `FT._LIST` and `FT.INFO` with `redis-py` over RESP3, using the profile's URI and `SSL_CERT_FILE`. It runs in the setup child and in the coordinator before each query; the coordinator still imports neither database engine. A RAM profile is not gated.

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
Provenance records numeric `requested_scan_threads`; effective readers and
partition concurrency require separate verification. A matching custom build
that needs the already imported DuckDB runtime's symbols may separately opt into
`"duckdb_adbc_global_symbols": true` (default false). It requires
`ADBC_SCANNER_EXTENSION`, promotes only the loaded `_duckdb` module before LOAD,
and records only boolean `custom_scanner_global_symbols`, never library paths.
The official released-scanner profile and direct Redis worker are unaffected.

A live six-row smoke check on Redis Software with verified 4× QPF matched direct Redis ADBC for grouped exact decimals, duplicates and NULLs. DuckDB's plan showed an ADBC table scan, filter delivery, and local hash aggregation. It used DuckDB 2.0.1 and scanner revision `98ff0a39fc`. SF1 results are separate evidence, not inferred from this smoke test.

## Worker memory isolation

By default every child (setup, engine and validator) shares the client cgroup with the coordinator and `supervise.py`. A worker that exhausts that limit can then get the coordinator OOM-killed as well, losing the query record ([#660](https://github.com/redis/redis-adbc-driver/issues/660)). This is most likely when the worker's memory is charged to the cgroup but not to its own resident set, for example result files on a tmpfs.

Optional integer `worker_memory_limit_bytes` in private `connection.json` (at least 16 MiB) runs each child in its own cgroup v2 leaf with that `memory.max` and `memory.oom.group` enabled. On first use, the coordinator moves every process in the client cgroup, including the supervisor, into a sibling `olap-harness` leaf. A worker OOM is then confined to that worker's leaf: the coordinator survives and records `worker_failed`. To keep results comparable with earlier runs, set it to the historical client limit (8 GiB is `8589934592`). Then give the client container at least 256 MiB more than that, for example `--memory 8448m --memory-swap 8448m`. The harness refuses to start when the tightest client limit leaves less headroom than that. It also refuses without cgroup v2, on a host's root cgroup, and when `/sys/fs/cgroup` is read-only. Docker mounts it read-only by default. Use `--privileged`, or `--cap-add SYS_ADMIN` and `mount -o remount,rw /sys/fs/cgroup` where the host's security profile allows the remount. Use only a disposable, dedicated client container, because the harness rearranges its cgroup.

Each engine record keeps the client-level `oom_kills_delta`. It adds `memory` with `isolation`, the leaf's `limit_bytes`, its own `oom_kills`, and `peak_bytes` (`memory.peak`, which includes page cache). Validator leaves are reported in `<engine>_validation_memory`. The peak is a cgroup measurement, not a driver heap profile. Without the setting, records show `"memory": {"isolation": "shared"}` and metadata says the cgroup is shared. Memory a dead worker leaves behind, such as tmpfs files, still counts against the client limit until the coordinator deletes it. Keep `/private` on disk, or add headroom for the DuckDB reference result.

## Checks

```sh
python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py' -v
```

The worker-OOM containment test runs only when explicitly enabled. It rearranges its cgroup, so run it only inside a disposable container. CI runs it with:

```sh
docker run --rm --privileged --memory 512m --memory-swap 512m --shm-size 256m -e OLAP_HARNESS_CGROUP_TESTS=1 -v "$PWD:/src:ro" -w /src/benchmarks/olap-release python:3.14-slim python -m unittest -v test_isolation.WorkerMemoryCgroupTests
```

`test_flex_compaction.py` covers the Flex gate with synthetic RESP3/RESP2 `FT.INFO` replies and a fake clock: uncompacted, cycle-only, restart and missing-metric states, table mapping, the bounded wait, setup refusal, the per-query recheck and record provenance. `test_supervise.py` runs `supervise.py` against a stub coordinator: a query that fails before writing a checkpoint gets a current-harness failure record, and later queries still run. Nine synthetic-adapter tests use real child processes without database queries: distinct PIDs/import isolation, crash attribution, missing references, mismatch/no-ratio, validator timeout, private cleanup, output reuse refusal, credential redaction, bounded multi-pass sorting, truncation and separate third-path reporting. Related harness issues: [#596](https://github.com/redis/redis-adbc-driver/issues/596) and [#660](https://github.com/redis/redis-adbc-driver/issues/660). Historical OOMs from a shared worker cannot establish the Redis driver's isolated memory requirement.

## Focused live follow-up

See [Q19 follow-up](Q19-FOLLOWUP.md) for the isolated SF1 measurements: direct Redis ADBC validates, while the third-path Q19 warm-up hit the 60-second deadline. [#599](https://github.com/redis/redis-adbc-driver/issues/599) closed on 2026-11-24 on 10% subset results after [PR #637](https://github.com/redis/redis-adbc-driver/pull/637); the SF1 third-path rerun is still pending in [#648](https://github.com/redis/redis-adbc-driver/issues/648). Full suites remain stopped.
