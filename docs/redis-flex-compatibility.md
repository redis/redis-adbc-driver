# Redis Flex compatibility test report

The current [opt-in experimental stopgap](redis-flex-stopgap.md) has separate
validation. Results below describe historical default-mode behavior.


Historical tests below ran on 2026-10-06 against commit `40da0bf`.
See the 2026-11-04 retest immediately below for the newer release. The Redis ADBC driver does
not currently support Redis Flex. A local Redis Software Flex database
confirmed that the driver refuses the connection before writing metadata,
and direct Search probes identified the missing server capabilities. The live
Redis Cloud Pro test authenticated to a new minimum-size Flex database.
The Cloud database had no Redis Search capability and is excluded from
Search-on-Flex compatibility results. The driver stopped
at `FT._LIST` before reaching its Flex-specific
`FT.AGGREGATE` guard.

## Retest: 2026-11-04, Redis Software 8.2.6-25

Retested against driver commit `95d6583` using an isolated Docker cluster.
The refreshed `redislabs/redis:latest` image reports Software **8.2.6-25**,
Redis **8.8.0**, Search **8.8.8**, ReJSON **8.8.2**, and Bloom **8.8.1**
on `aarch64`. Image index digest:
`sha256:b3993a5ed453ffe2aeacff63db7dd3702253b7e2f732032676e2b221413e0e06`.

The new `adbc-flex` database used BigStore v2, `search_on_bigstore: true`,
1,024 MB total, 256 MB RAM (25%), one shard, no replication or persistence,
and the built-in trial license. Host ports were 29443 (admin API) and 22000
(database); the existing RAM test clusters were left running.

| Probe | Live result |
|-|-|
| `FT.CREATE ... TAG`, `TEXT`, `NUMERIC`, `GEO` | Supported |
| `FT.CREATE` without `SKIPINITIALSCAN` | Accepted (background population of existing documents was not measured) |
| `FT.CREATE ... NUMERIC SORTABLE` | `SEARCH_QUERY_BAD Disk index does not support SORTABLE fields` |
| `FT.SEARCH ... RETURN 2 tag n` | Returned document fields `tag=hello`, `n=42` |
| `FT.SEARCH ... NOCONTENT` | Returned matching document ID |
| `FT.AGGREGATE ... LOAD 2 @tag @n` | Returned both field values |
| `FT.AGGREGATE ... WITHCURSOR COUNT 1` | `SEARCH_FLEX_UNSUPPORTED_ARGUMENT WITHCURSOR is not supported in Redis Flex` |
| `FT.ALTER` | `FT.ALTER is not supported in Redis Flex` |
| `FT.DROPINDEX ... DD` | `DD is not supported in Redis Flex` |
| `FT.DROPINDEX` without `DD` | Supported |

Raw commands and replies are recorded in
[flex-probes-2026-11-04.jsonl](flex-probes-2026-11-04.jsonl).
Probe indexes and the temporary HASH were removed; the direct probe key count
was unchanged (one metadata key before and after), and `FT._LIST` was empty.

### Driver results on the new release

```bash
cd go
REDIS_FLEX_URI=redis://localhost:22000/0 go test -count=1 -run TestFlex -v ./...
REDIS_URI=redis://localhost:22000/0 go test -count=1 -run '^TestSQLAliasesInExpressions$' -v .
```

Both tests failed, for distinct reasons:

- `TestFlexRefused`: `connected to a Redis Flex database; want an error`.
  The missing-index aggregate probe now returns `SEARCH_INDEX_NOT_FOUND`,
  so the driver passes its guard and creates its metadata registry.
- `TestSQLAliasesInExpressions`: its first `CREATE TABLE` failed with
  `I/O: [redis] failed to create search index: SEARCH_QUERY_BAD Disk index does not support SORTABLE fields`.

Tracked in [GitHub issue #314](https://github.com/redis/redis-adbc-driver/issues/314).

**Flex remains incompatible with the current driver.** The old refusal guard
no longer detects this release, and connection success must not be treated as
support. `SORTABLE` and aggregate cursors remain blockers; schema alteration
and deletion with `DD` also require alternate implementations.
The disposable cluster was removed after testing, including driver metadata.
These runs used the historical `95d6583` fixture, without TLS or a verified
QPF setting; they do not validate the current default TLS/4x QPF fixture.

Requirements and limitations are documented in Redis's
[8.2.6-25 release notes](https://redis.io/docs/latest/operate/rs/release-notes/rs-8-2-releases/rs-8-2-6-25/)
and [Search on Flex documentation](https://redis.io/docs/latest/operate/rs/flex/search/).
The Word report retains the historical 2026-10-06 results and does not include
the later Cloud evidence clarification below.

## Test status

| Target | Status | Result |
|-|-|-|
| Local Redis Software Flex | Completed | Expected pass: the driver returned ADBC `NotImplemented`, reported that Redis Flex is unsupported, and did not change the database key count. |
| New Redis Cloud Pro Flex, 2026-10-06 | Setup lacked Search; excluded from Search-on-Flex compatibility results | Authentication and `PING` succeeded, but no Search commands were registered. No valid Cloud Search-on-Flex compatibility test was completed. |

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
defaults at the tested commit. The current driver defaults to RESP3; the RESP2
entries below describe these historical runs.

| Driver setting | Value |
|-|-|
| `uri` / `REDIS_FLEX_URI` | `redis://localhost:12000/0` |
| `adbc.redis.cluster` | `auto`; the Redis Software proxy reports a single endpoint |
| Redis database | 0 |
| Protocol | RESP2 for the driver at the tested commit |
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
`FT.AGGREGATE` on Flex. The image tested on 2026-10-06 rejected them. The 2026-11-04 retest
above confirms that the newer build supports all three, with remaining
limitations on sortable fields and cursors.

## Historical Redis Cloud test configuration: 2026-10-06

These settings, commands and API responses describe the historical attempt,
not current provisioning requirements or a current test procedure. The current
driver requires RESP3 and offers experimental Software Flex snapshot execution; see the separate
[experimental stopgap scope](redis-flex-stopgap.md). Cloud SQL remains unvalidated.

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
| Database RESP setting | RESP3; that historical driver run used RESP2 |
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

### Historical Terraform follow-up and resource status

[Private PR #209](https://github.com/alberttwong/redis-adbc-driver/pull/209),
head `ad56da770b5fb77d5466abcfc82fac5070436d43`, records a second provisioning
attempt on 2026-10-06 with Terraform 1.15.2 and the official
`RedisLabs/rediscloud` provider 2.19.1. It requested Redis 8.6, a 250 GB Flex
dataset, 10% RAM, TLS and `query_performance_factor = "Standard"`.
The plan proposed one addition, but apply returned
`DATABASE_QUERY_PERFORMANCE_FACTOR_SEARCH_IS_REQUIRED`. The recorded empty
Terraform state and API inventory showed that this request created no database.
That inventory also showed the earlier disposable database was absent.

This is historical evidence from the PR, not a new provisioning attempt or
current resource inventory. The provider/schema and beta enablement conclusions
from that attempt have not been revalidated. The Terraform fixture is not
ported here, and the rejection does not establish a present-day Cloud blocker.

A Cloud Search-on-Flex validation must establish Search availability on the
actual target before claiming compatibility: confirm authenticated `FT._LIST`,
create a uniquely named HASH index with a supported field, insert a matching
document, and fully validate the `FT.SEARCH` result. Clean up the owned probe
index and document. A missing Search capability is a setup blocker; successful
Search probes alone do not establish driver SQL or snapshot compatibility.

## Driver functions not implemented independent of Flex

These are current driver-level ADBC gaps and should not be confused with the
Redis Flex server gaps:

| ADBC capability | Current behavior |
|-|-|
| Transactions | Autocommit only; connection `Commit` and `Rollback` return unsupported/invalid-state errors. |
| Partitioned result execution | `ExecutePartitions` and `ReadPartition` return `NotImplemented`. |
| Substrait plans | Bounded binary Substrait 0.108.0 read/filter/project; [subset and limits](substrait.md). |
| Statistics metadata | `GetStatistics` and `GetStatisticNames` are not implemented. |
| Multiple catalogs | Only the `redis` catalog exists; cross-database references are not implemented. |
| `UNIQUE` and `FOREIGN KEY` enforcement/metadata | Accepted by the SQL parser but ignored; they are not returned as constraints by `GetObjects`. `CHECK` and `PRIMARY KEY` are supported. |

The standard ADBC validation suite therefore skips tests for transactions,
statistics, a second catalog, and unsupported constraint kinds. These gaps
also exist on ordinary RAM-only Redis and are not caused by Flex.
