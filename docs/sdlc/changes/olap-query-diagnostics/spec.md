# Specification

2026-11-26, base main `7c83c486`. See [intent](intent.md), [plan](plan.md), [validation](validation.md).

All new fields are additive. Existing fields, timings, statuses, validation and retention keep their meaning. The coordinator imports neither engine: it reads the driver's JSON sidecar and issues `INFO` with `redis-py`, as the Flex compaction gate already does.

## Driver execution diagnostics

- **What runs:** the direct Redis worker sets `adbc.redis.execution_diagnostics_file` on the **warm-up** statement only, to `redis-warmup-diagnostics.json` in the private mode-0700 per-query directory. The driver replaces that file atomically once per second during execute and writes `execute_returned` or `execute_failed` when execute ends ([execution diagnostics](../../../execution-diagnostics.md)).
- **Why only the warm-up:** the timed runs stay uninstrumented, so their samples remain comparable with earlier records. The sampler writes a file once per second plus at start and end, and reads Go memory statistics. On the macOS Docker Desktop route the file is on a bind mount, which could be a visible share of a 10 ms query. Every SF1 Flex timeout in #696–#717 happened in the warm-up, so the warm-up is the run the issues needed.
- **When it is read:** by the coordinator after the worker exits, whatever its outcome. A worker that crashed or was killed leaves the driver's last `running` sample, and that is recorded.
- **Record field `redis_warmup_execution_diagnostics`:** a whitelisted copy. Nothing else from the sidecar reaches the record.
  - `status` (`running`, `execute_returned`, `execute_failed`) and `phase` (a driver identifier).
  - Numbers: `version`, `execute_elapsed_seconds`, `worker_grant`, `processed_rows`, `matched_rows`, `redis_commands`, `redis_round_trip_worker_seconds`, `socket_read_worker_seconds`, `connection_read_bytes`, `connection_written_bytes`, `process_allocated_bytes_during_execute`, `process_heap_bytes`.
  - `plan`: `kind`, plus listed driver-chosen booleans and sizes (worker counts, caps, chunk and page sizes, dimension counts and rows). Literal-bearing fields become structure only:
    - `search_query` becomes `index_filter` (true unless empty or `*`);
    - a `residual` expression becomes the boolean `residual`;
    - `pushed_limit` becomes `limit_pushed`; `sql_limit` and `sql_offset` are dropped;
    - column, sort, input-column and group-expression lists become `column_count`, `sort_key_count`, `input_column_count` and `group_expression_count`.
    - Join-order `inputs` keep only the driver's already-sanitized position, row counts and flags; `order` keeps integers and `scope` keeps booleans.
  - `aggregate_metrics`: the driver's numeric partitioned-aggregate counters and `strategy`.
  - Strings other than `status`, `phase`, `kind`, `reason`, `source` and `strategy` are dropped. Those must match `^[a-z][a-z0-9_]{0,63}$`, and non-finite numbers are dropped.
  - A sidecar that is not a JSON object is recorded as `{"unreadable": "<reason>"}`. No sidecar (the warm-up never reached execute) means no field.
- **Record field `redis_execution_diagnostics_error`:** a driver that rejects the option still runs the query; the redacted message (at most 200 characters) is recorded.
- **DuckDB over Redis ADBC:** the option is a statement option, and the scanner opens its own statements. As an ATTACH (database) option the driver rejects it. So `duckdb_adbc_execution_diagnostics_unavailable` states that, instead of a measurement.
- **Retention:** after a failed validation, the raw sidecar moves with the engine's other files into the private `failed-artifacts/` folder. It contains literals and is never public.

## Flex RAM/flash object split

- **When:** on a Flex profile (`flex_profile`), the coordinator samples just before starting each Redis-backed engine (`redis`, `duckdb_adbc`), after the compaction recheck. A RAM profile is not sampled.
- **How:** `INFO keyspace`, `INFO Bigredis-Keyspace` and `INFO memory` over RESP3, with the profile's URI and `SSL_CERT_FILE`. The shard reports its RAM and flash object counts as `ram=` and `disk=` on `bigdb0:` in `Bigredis-Keyspace`.
- **Record field `<engine>_flex_tiers_before`:**
  - `captured_utc`;
  - `keys`, from `db0:keys`;
  - `endpoint_used_memory`: `used_memory` as the endpoint reports it, which is not a RAM-tier figure;
  - `ram_objects` and `flash_objects`: the shard's `ram=` and `disk=` counts when the endpoint reports them, otherwise null. They overlap: on the measured shard `ram` was `clean + dirty`, and a clean object also has a flash copy;
  - `split_available`: true only when both are reported.
  - On a failure, only `captured_utc` and `error` (the exception type name) are recorded, and the query still runs.
- **Redis Software 8.8 Flex (finding, 2026-11-26):** the database endpoint does not report the split.
  - Its proxy answers only the Server, Clients, Memory, Persistence, Stats, Replication, CPU, `search_index`, Commandstats, Cluster and Keyspace sections. It returns nothing for `Bigredis`, `Bigredis-Keyspace` or `bigredis-stats`.
  - Its `db0:` line keeps `keys`, `expires` and `avg_ttl` only.
  - `INFO` is refused inside scripts, so a Redis Function cannot read the shard's sections either.
  - On that deployment the field therefore records `split_available: false` with `keys` and `endpoint_used_memory`. It never estimates a split.

## Pass order

- **Record field `pass_order`:** `{"position": N, "queries_before": [...], "engines": [...]}`.
  - `queries_before` lists, in order, the `query` of every record already in the output folder's `queries.jsonl`, including coordinator failures that `supervise.py` appended. A pass is one output folder, which the harness already refuses to reuse for a recorded query.
  - `position` is one more than its length.
  - `engines` is this query's engine order, so the third engine's record shows that direct Redis ADBC ran just before it.
- The field is in the checkpoint from the start, so a supervisor-written failure record keeps it.

## Checks

- Unit tests for the whitelist, including that SQL literals, column names, index queries, LIMIT values and unknown fields never reach the record. Tests for the INFO parsing of the proxy's and the shard's formats, and for capture errors.
- Real child-process tests with the synthetic driver: only the warm-up statement is instrumented; the record carries the sanitized sidecar, including a crashed worker's last `running` sample; an option refusal is recorded without failing the query; the raw sidecar is retained after a mismatch; and the third engine records its unavailability.
- Coordinator tests: the split is captured just before each Redis-backed engine on Flex and never on RAM, and `pass_order` across queries in one folder.
- `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'`.
- Live smoke run on a disposable Software Flex fixture with verified 4x QPF ([validation](validation.md)).
