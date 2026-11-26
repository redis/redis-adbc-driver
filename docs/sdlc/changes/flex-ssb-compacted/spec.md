# Spec

2026-11-26; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Harness gate

Applies to `benchmarks/olap-release/run.py` (and so to `supervise.py`, which calls it). Details are in the [README](../../../../benchmarks/olap-release/README.md#flex-index-compaction-gate).

**When.** A profile is Flex when `connection.json` sets `db_kwargs["adbc.redis.flex"]` to `"true"` or `server.json` has `search_on_bigstore: true`.
- Setup gates after loading and after the `--reuse-loaded` count check. It waits up to 900 s, polling every 15 s.
- Each query rechecks just before its engines start, without waiting.
- `--duckdb-only` and RAM profiles are not gated.

**Criterion, per index** (from `FT.INFO`):
- `gc_stats.total_cycles` ≥ 1, and
- `disk_num_keys` < `num_docs` for every NUMERIC attribute that reports it, including the driver's `__rowid`.
- A NUMERIC field that no document has reports no `disk_num_keys`: on this server, the driver's `__adbc_snapshot_deleted` marker on a table without deletes. It has no entries to scan and does not block.
- An index where no NUMERIC field reports `disk_num_keys` (a RAM index), or that has no NUMERIC fields, cannot be verified and fails.
- An index with fewer documents than the verified `search-fork-gc-clean-threshold` (default 100) does not block. The disk GC needs that many changes, so it may never run there, and the full scan is that small. SSB's smallest table has 2,000 rows.

**Opt-in exemption** (added 2026-11-26 at the requesting session's direction, after the strict gate refused this fixture; see [validation](validation.md#small-indexes-without-a-first-cycle)):
- `connection.json` `flex_compaction_exempt_max_docs`: an integer from 1 to 10,000. It is absent by default, which keeps the gate strict.
- An index that fails the criterion and has at most that many documents is `exempt_uncompacted`, and its reasons are kept. Indexes above the limit, including SSB `customer`, `part` and `lineorder`, still block until compacted.
- Each exempt index records `num_docs`, `gc_total_cycles` and the largest `disk_num_keys`. It also records `ft_create_utc` and `minutes_since_ft_create`, from a caller-supplied `flex-index-created.json` of shard-log times, and `point_term_profile`: one `FT.PROFILE` of an existing point term on the table's first NUMERIC column, with total and iterator milliseconds. A missing time or a failed profile refuses the exemption.
- A table small enough to be exempt that has compacted is listed under `exemption.not_needed`. Exempt ones are listed under `exemption.applied_to`.
- The setup gate and every per-query recheck record the evidence again, so each query record carries its own.

**Tables.** An index belongs to the loaded table whose columns contain all of its public fields (fields not starting with `__`).
- Every loaded table needs at least one index, and all of its indexes must pass.
- An index that fits several tables cannot be attributed and fails the gate.
- An index that fits none (another suite or schema) is counted and not checked.

**Refusal.**
- Setup raises after the bound: `--load-only` exits non-zero, and `supervise.py` stops before any query. The `suite_error` event carries the last observation.
- A query refuses (non-zero exit, `flex_compaction_refused` event) if setup recorded no passed gate or an index no longer passes. A shard restart resets `total_cycles` to 0, so a restarted fixture is refused until a new cycle runs.

**Provenance.** `metadata.json` (`flex_index_compaction`) and every query record keep, per table and index, `num_docs`, `gc_total_cycles` and each NUMERIC field's `disk_num_keys`, with polls and waited seconds. Index names and key prefixes are left out, because they contain operational identifiers.

**Client.** `redis-py` over RESP3 (`FT._LIST`, `FT.INFO`) with the profile's URI and `SSL_CERT_FILE`, read-only. It runs in the setup child and in the query coordinator. The coordinator still imports neither DuckDB nor the Redis ADBC driver.

## SSB SF1 rerun

- **Fixture:** Redis Software 8.2.6 Flex from `go/flex-setup.sh`: Redis 8.8.0, 1 shard, 64 GiB with an 8 GiB RAM tier, `noeviction`, TLS, active 4x QPF verified through the management API, Search disk settings 8% / 2% / 3 MiB, AOF every second, 20 GiB container cap, the local image not re-pulled.
- **Driver:** Linux ARM64 library from main `ac82cfa0` (`go/build-linux.sh arm64`), loaded into the v0.0.31 wheel with `REDIS_ADBC_LIBRARY`.
- **Run:** `run.py ssb --load-only` loads the five tables from the private profile's `ssb-sf1.duckdb`, then the gate. `supervise.py ssb --reuse-loaded` runs all 13 queries: a warm-up and three timed runs, 60 s statement deadline, fully fetched and validated against DuckDB.
- **Client:** `adbc-olap-release-client:20261123`, 4 CPUs and 8 GiB without swap, in the server's network namespace.
- **Comparison:** the [2026-11-26 key-demand run](../flex-ssb-key-demand/validation.md#ssb-sf1-on-flex), which validated 4 of 13 on uncompacted indexes.
