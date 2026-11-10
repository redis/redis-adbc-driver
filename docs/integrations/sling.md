# Sling and PostgreSQL

[Sling](https://github.com/slingdata-io/sling-cli) can move PostgreSQL data into
Redis through this driver's native ADBC library and read it back into PostgreSQL.
Tested on macOS ARM64 with Sling 1.6.6, PostgreSQL 18.6, and Redis Software
8.8.0 with three shards and verified active 4x QPF. This is a tested configuration,
not a claim that every Sling load mode works.

## Run the integration test

Install the official Sling 1.6.6 binary for your platform. Docker, a C compiler,
Go, Python 3, curl, jq, and OpenSSL must be available for the repository setup.
From the repository root:

```sh
python3 scripts/test_sling_postgres.py --sling /absolute/path/to/sling
```

The [test](../../scripts/test_sling_postgres.py) runs `make -C go redis-up` to
verify effective QPF=4, six Search workers, mnp routing/32 connections and
noeviction, then builds the shared library with `make -C go build`. It starts a
disposable PostgreSQL 18 container on a randomly assigned loopback port, seeds
10,003 rows, runs two complete round trips, and verifies both directions with
PostgreSQL `EXCEPT ALL`. It removes its uniquely named Redis schema and PostgreSQL
container/volume afterward. The shared Redis Software fixture stays running.
Raw logs, isolated Sling configuration, and local endpoints stay in ignored
`go/build/sling-*/` directories. Sling telemetry is disabled by the test.
Sling may download its ADBC driver manager into that isolated directory.

## Connection and load configuration

Use `type: adbc`, `driver_name: redis`, and an explicit absolute path to
`libadbc_driver_redis.dylib` (macOS) or `libadbc_driver_redis.so` (Linux).
The Redis URI uses verified TLS. For example, an environment connection value is:

```json
{
  "type": "adbc",
  "driver_name": "redis",
  "driver": "/absolute/path/to/libadbc_driver_redis.so",
  "uri": "rediss://default:PASSWORD@localhost:6379/0",
  "adbc.redis.tls_ca_file": "/absolute/path/to/ca.pem",
  "adbc.redis.partition_control_persistence": "guaranteed",
  "insert_concurrency": 1
}
```

Set the persistence declaration only after verifying the endpoint's policy;
the test performs that verification. Use a dedicated test database. Tables must
be created or imported through the driver; arbitrary Redis keys are not SQL tables.

Copy [the Redis ADBC template](../../scripts/sling/adbc.yaml) to
`$SLING_HOME_DIR/templates/adbc.yaml`. Use an isolated Sling home because this
override applies to all generic ADBC connections in that home. It supplies
schema/column discovery and idempotent DROP/rename SQL through Sling's supported
[template override mechanism](https://github.com/slingdata-io/sling-cli/blob/9d5186194e76e3e146b7d6367a6e27a961242dce/core/dbio/dbio_types.go).

Precreate the Redis schema and table through `sling conns exec REDIS_ADBC` with
the intended SQL types. Then run:

```sh
sling run --src-conn PG_SLING --src-stream demo.source \
  --tgt-conn REDIS_ADBC --tgt-object demo.inbound \
  --mode truncate --tgt-options '{"direct_insert":true}'

sling run --src-conn REDIS_ADBC --src-stream demo.inbound \
  --tgt-conn PG_SLING --tgt-object demo.roundtrip --mode full-refresh
```

Truncate/direct mode destroys the target's existing rows before loading and is
not an atomic replacement. Failure can leave an empty or partially loaded table.
No rollback or CDC/upsert guarantee is implied. The verified path uses Sling's
ordinary row conversion and Arrow bulk ingestion; the token-gated Arrow lane
was not enabled.

## Observed limitations

- Stock `driver_name: redis` full-refresh into Redis fails with
  `Could not find template schemas` before ingestion. Redis is not one of
  Sling's recognized driver names for SQL-template delegation.
- Selecting PostgreSQL templates while retaining the explicit Redis library
  fails on Sling's metadata query `(select current_catalog)`. That configuration
  was investigated but is not the tested solution.
- After adding metadata templates, default staging encounters a Sling panic
  when an absent table's column query returns an empty Arrow batch. The smaller
  reproducer is `sling conns exec REDIS_ADBC 'SELECT label FROM demo.inbound WHERE 1=0'`
  against an existing table. Sling 1.6.6 exits nonzero with an Arrow string
  array bounds panic. Its `makeRecordNextFunc` tries row zero without skipping
  zero-row batches. Empty record batches are valid Arrow; no driver change was
  made to hide this consumer behavior. Precreation/direct mode avoids this
  path for the nonempty transfers tested here. Empty-source transfers remain
  unvalidated and should not be assumed to work.
- Destination DDL is not preserved exactly: Sling inferred `varchar(65500)`
  from `text` and unconstrained `numeric` from `numeric(18,2)` on the return trip.
  All tested values and NULLs survived exactly; this is value compatibility,
  not complete PostgreSQL schema compatibility.

See [validation evidence](../sdlc/changes/sling-postgres/validation.md).
The interoperability results and compatibility gaps were reported upstream in
[Sling issue #836](https://github.com/slingdata-io/sling-cli/issues/836).
