# Redis Flex compatibility test report

Tested on 2026-10-06 against commit `40da0bf`. The Redis ADBC driver does
not currently support Redis Flex. A local Redis Software Flex database
confirmed that the driver refuses the connection before writing metadata,
and direct Search probes identified the missing server capabilities. The live
Redis Cloud Pro test authenticated to a new minimum-size Flex database.
The Cloud database had no Redis Search capability, so the driver stopped
at `FT._LIST` before reaching its Flex-specific
`FT.AGGREGATE` guard.

## Test status

| Target | Status | Result |
|-|-|-|
| Local Redis Software Flex | Completed | Expected pass: the driver returned ADBC `NotImplemented`, reported that Redis Flex is unsupported, and did not change the database key count. |
| New Redis Cloud Pro Flex | Completed; incompatible target | A minimum-size disposable database was created and authenticated successfully. `PING` worked, but `FT._LIST`, `FT.CREATE`, `FT.SEARCH`, and `FT.AGGREGATE` were not registered. The focused test failed because the Search module was absent. |

## Local Redis Software settings

The repository's `go/flex-setup.sh` and the `redis-flex` service in
`go/compose.yaml` created the local environment. Passwords and fixed test
credentials are intentionally omitted.

| Setting | Value used |
|-|-|
| Redis Software container | `redislabs/redis:latest` |
| Image digest | `sha256:807590042534ca42b78be1d7beefdcd2da12c614ce92db67c716495b7a47b72c` |
| Redis Software version | `8.2.0-78` |
| Database Redis version | `8.6.2` |
| Search version | `8.6.11` (`80611`) |
| Other modules | ReJSON `8.6.6`, Bloom `8.6.4`, Vector Set `1` |
| Architecture | Linux `aarch64` container on the local Docker host |
| Database name and type | `adbc-flex`, `redis/flash`, BigStore v2 |
| Database size | 1,024 MB total; 128 MB RAM; 896 MB flash allocation |
| Search on flash | `search_on_bigstore: true` |
| Shards | 1 |
| Replication | Disabled |
| Persistence | Disabled |
| OSS Cluster API | Disabled; single proxy endpoint |
| TLS | Disabled for the local database endpoint |
| Endpoint | `redis://localhost:12000/0` |
| Admin API | `https://localhost:9443`, self-signed TLS, bound to localhost only |
| License | Built-in Redis Software trial license |

The image was refreshed with `docker pull redislabs/redis:latest` immediately
before the test and Docker reported it was already current.

## Local driver settings used

Only the URI was passed explicitly. The remaining values are the driver's
defaults.

| Driver setting | Value |
|-|-|
| `uri` / `REDIS_FLEX_URI` | `redis://localhost:12000/0` |
| `adbc.redis.cluster` | `auto`; the Redis Software proxy reports a single endpoint |
| Redis database | 0 |
| Protocol | RESP2 for the driver |
| `adbc.redis.read_timeout` | 5 minutes |
| `adbc.redis.write_timeout` | Same as the read timeout |
| URI `dial_timeout` | 5 seconds |
| `adbc.redis.default_schema` | `public` |
| `adbc.redis.aggregate_pushdown` | `exact` |
| `adbc.redis.stream_results` | `true` |
| `adbc.redis.stream_batch_rows` | 65,536 |
| `adbc.redis.time_zone` | `UTC` |

The focused integration test was:

```bash
cd go
REDIS_FLEX_URI=redis://localhost:12000/0 \
  go test -count=1 -run TestFlex -v ./...
```

`TestFlexRefused` passed. It checked the key count before and after opening the
connection, verified ADBC `NotImplemented`, and verified the message
`Redis Flex databases are not supported`. The database had zero keys and no
Search indexes after the direct probes were cleaned up.

## Local Search capability probes

Temporary indexes and one HASH were created under `adbc-flex-probe:*`, then
removed. These are live results from Redis Software `8.2.0-78`, Redis
`8.6.2`, and Search `8.6.11`.

| Probe | Result |
|-|-|
| `FT._LIST` | Supported |
| `FT.CREATE ... TAG` with `SKIPINITIALSCAN` | Supported |
| `FT.CREATE ... TEXT` with `SKIPINITIALSCAN` | Supported |
| `FT.CREATE` without `SKIPINITIALSCAN` | Rejected: a Flex index requires `SKIPINITIALSCAN` |
| `FT.CREATE ... NUMERIC` | Rejected: `NUMERIC` fields are unsupported |
| `FT.CREATE ... GEO` | Rejected: `GEO` fields are unsupported |
| `FT.CREATE ... TAG SORTABLE` | Rejected: disk indexes do not support `SORTABLE` fields |
| `FT.SEARCH ... RETURN 2 ...` | Rejected: local Flex requires `NOCONTENT` or `RETURN 0` |
| `FT.SEARCH ... NOCONTENT` | Supported; returned matching document IDs only |
| `FT.SEARCH ... RETURN 0` | Supported; returned matching document IDs only |
| `FT.INFO` | Supported |
| `FT.AGGREGATE` | Rejected: unsupported in Redis Flex |
| `FT.ALTER` | Rejected: unsupported in Redis Flex |
| `FT.DROPINDEX ... DD` | Rejected: `DD` is unsupported in Redis Flex |
| `FT.DROPINDEX` without `DD` | Supported |

## Missing Flex functions and features

### Required by the driver

The following gaps are blockers rather than optional optimizations:

| Missing capability | Why the driver needs it |
|-|-|
| `FT.AGGREGATE` and aggregate cursors | Every table query reads complete rows through `FT.AGGREGATE`; document-ID-only `FT.SEARCH` results are not sufficient for the current execution path. |
| `NUMERIC` fields | Every table index includes a numeric row-ID field. Numeric SQL columns also use numeric Search fields when indexed. |
| `SORTABLE` fields | The mandatory numeric row ID is sortable, and sortable fields support server-side ordering and stable result paging. |

The local build also lacks `FT.ALTER`, `FT.DROPINDEX ... DD`, `GEO` fields,
background indexing, and field values in `FT.SEARCH` results. Supporting this
specific build would therefore require alternate implementations for schema
changes, table/index deletion, geospatial columns, initial indexing, and row
loading even after the three connection blockers above were removed.

The connection guard intentionally probes `FT.AGGREGATE` against a missing
index. Non-Flex Redis answers `No such index`; the tested Flex build answers
`FT.AGGREGATE is not supported in Redis Flex`. The driver returns
`NotImplemented` before it creates its metadata registry. A future Flex build
that implements `FT.AGGREGATE` will pass this guard and proceed to the normal
setup path, where the mandatory `NUMERIC SORTABLE` row-ID field must also
work.

### Redis Cloud Pro Flex Preview

Redis Cloud Essentials Flex does not provide Search and Query. Redis Cloud
Pro offers Redis Search on Flex as an opt-in beta. The beta flag must first
be enabled for the Pro subscription in the Redis Cloud console, and then a
new Flex database must be created with a Search index on tiered storage.
According to the current Redis Cloud documentation, the beta supports HASH documents;
`TEXT`, `TAG`, and `VECTOR` fields; `SORTBY` and `RETURN`; and high
availability, persistence, backup, and upgrades. It does not yet provide:

- JSON document indexing
- `NUMERIC` fields
- `GEO` fields
- `FT.AGGREGATE`
- `FT.HYBRID`
- Background indexing

The absent `NUMERIC` field type and `FT.AGGREGATE` are enough to prevent the
current driver from operating on Cloud Flex. Source:
[Create a Flex database](https://redis.io/docs/latest/operate/rc/databases/create-database/create-flex-database/).

Redis's documentation says Redis Software already has `NUMERIC`, `GEO`, and
`FT.AGGREGATE` on Flex. The current published `redislabs/redis:latest` image
tested here still rejects them. Treat the live image result as authoritative
for this exact container build and retest when a newer Redis Software image
is published.

## Redis Cloud test configuration

Resource names, numeric IDs, endpoint names, and credentials are omitted
because this report may be stored in a public repository. Database passwords
were held only in process memory and passed through `REDIS_FLEX_URI`; they
were not printed or written to disk.

### Disposable Pro Flex database created for this run

The Cloud API requires a minimum 250 GB dataset for a `ram-and-flash`
database in this subscription. Its dry-run price response described five
Flex-10 billing shards at USD 0.287 per shard-hour and a USD 1.43/hour minimum
price; both figures round to approximately USD 1.43/hour for this shape.

| Setting | Value used |
|-|-|
| Subscription and region | Existing Redis Cloud Pro subscription; GCP `us-east4` |
| Database type | Flex (`ram-and-flash`) |
| Redis version | `8.6`; automatic minor-version upgrades enabled |
| Dataset size | 250 GB, the API minimum for this subscription |
| API memory limit | 250 GB |
| RAM percentage | 10% |
| Billing shards | 5 Flex-10 shards in the dry-run price response |
| Throughput | 1,000 operations/second |
| Replication | Disabled |
| Persistence | None |
| Eviction policy | `noeviction` |
| Database RESP setting | RESP3; the driver still uses RESP2 |
| OSS Cluster API | Disabled; single proxy endpoint |
| TLS | Disabled |
| Password | Random per-test password; redacted and not persisted |
| Explicit modules | None; Redis 8 rejects explicit module selection |
| Query performance factor | Omitted; it requires an enabled Search capability |

API validation confirmed that Search was unavailable in these database configurations:

| Create request | Cloud API result |
|-|-|
| Redis 8.6 plus explicit `RediSearch` module | Rejected with `DATABASE_EXPLICIT_MODULES_NOT_SUPPORTED_FOR_THIS_REDIS_VERSION`; Redis 8 capabilities are bundled rather than selected as legacy modules. |
| No explicit module plus `queryPerformanceFactor: Standard` | Rejected with `DATABASE_QUERY_PERFORMANCE_FACTOR_SEARCH_IS_REQUIRED`; Search was not enabled for the subscription/database. |
| No explicit module and no query performance factor | Accepted; the database became active, but its Redis command table contained no Search commands. |

The authenticated focused test was:

```bash
cd go
REDIS_FLEX_URI='redis://default:<password>@<redacted-public-endpoint>/0' \
  go test -count=1 -run TestFlex -v ./...
```

`TestFlexRefused` failed because it received:

```text
Not Implemented: [redis] the Redis server does not provide the search
(RediSearch) module: ERR unknown command 'FT._LIST'
```

The test expected the later Flex-specific `NotImplemented` error about
missing `FT.AGGREGATE`. The actual error occurs earlier during ordinary Redis
Search discovery.

Read-only command-table probes produced these live results:

| Probe | Result |
|-|-|
| `PING` | `PONG` |
| `COMMAND INFO FT._LIST` | Not registered |
| `COMMAND INFO FT.CREATE` | Not registered |
| `COMMAND INFO FT.SEARCH` | Not registered |
| `COMMAND INFO FT.AGGREGATE` | Not registered |

No probe key or index was created. For this non-beta Cloud database, the
missing surface is the entire Redis Search command family, not only the
documented feature gaps of Search on Flex beta.

### Search beta enablement

Redis documentation instructs users to enable a beta flag for the Pro subscription in the Cloud console, but does not identify an exact menu path. The documented Setup > Advanced options > Redis Flex option enables tiered storage and does not confirm Search beta enablement. If the Search beta control is absent, ask Redis Support to confirm subscription eligibility and the required enablement procedure. Backend enablement may be required, but this was not confirmed during testing.

### Cloud cleanup status

The disposable database remains active because deleting a Cloud database is
irreversible and was not part of the original authorization. Delete this
specific test database promptly from the Cloud console or API to stop its
hourly charge.

## Driver functions not implemented independent of Flex

These are current driver-level ADBC gaps and should not be confused with the
Redis Flex server gaps:

| ADBC capability | Current behavior |
|-|-|
| Transactions | Autocommit only; connection `Commit` and `Rollback` return unsupported/invalid-state errors. |
| Partitioned result execution | `ExecutePartitions` and `ReadPartition` return `NotImplemented`. |
| Substrait plans | `SetSubstraitPlan` returns `NotImplemented`. |
| Statistics metadata | `GetStatistics` and `GetStatisticNames` are not implemented. |
| Multiple catalogs | Only the `redis` catalog exists; cross-database references are not implemented. |
| `UNIQUE` and `FOREIGN KEY` enforcement/metadata | Accepted by the SQL parser but ignored; they are not returned as constraints by `GetObjects`. `CHECK` and `PRIMARY KEY` are supported. |

The standard ADBC validation suite therefore skips tests for transactions,
statistics, a second catalog, and unsupported constraint kinds. These gaps
also exist on ordinary RAM-only Redis and are not caused by Flex.
