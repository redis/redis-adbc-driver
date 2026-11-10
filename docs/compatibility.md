## Compatibility and validation

The SQL dialect target is **PostgreSQL 18 compatibility through ADBC**
([#118](https://github.com/redis/redis-adbc-driver/issues/118)). The
[differential harness](../go/pgdiff/README.md) now selects a pinned PostgreSQL 18
reference; its existing curated upstream inputs retain PostgreSQL 16 provenance.
The PostgreSQL 16 measurements below are historical, not PostgreSQL 18 evidence
or a SQL:2023 Core conformance score.
See the [initial SQL surface audit and evidence requirements](postgresql18-compatibility.md).
The [2026-11-10 curated comparison](sdlc/changes/postgresql18-359/validation.md)
at implementation revision `b35a4739` passed **574/651 scored cases (88.2%)**,
with **77 failures**, two allowed extensions, two explicit exclusions and zero
infrastructure errors. Of #359's 188 retained baseline failures, 122 now pass
and 66 have explicit deferred dispositions; the other eleven failures are the
original #360 storage-model cases. Deferred cases remain failing in the runner.
The [proposed query-profile milestone and categorized failure inventory](postgresql18-query-profile.md)
explain a narrower future claim; no complete profile pass has been established.


On **2026-10-08**, the validation suite against the default local **Redis
Software** database (Redis 8.6.2, Search 8.6.11) had **360 passed, 8 skipped,
3 subtests passed, and 0 failed**. The full Go suite also passed, with explicit
skips for protocol-level ACL administration and resettable command counters
that Software blocks. Those checks passed separately on the OSS three-shard
compatibility environment. See [Building and testing](driver-reference.md#building-and-testing).
The other validation results below are from earlier runs.

| Server | Version | Connection |
|-|-|-|
| **Redis Software (default)**, `redislabs/redis:latest` | Redis 8.8.0, Search 8.8.8 (2026-11-04) | `redis://` |
| Redis Open Source (`redis:8.6.2` Docker image) | 8.6.2, Search 8.6.0 | `redis://` |
| Redis Open Source cluster, 3 shards (OSS Cluster API) | 8.6.2, Search 8.6.0 | `redis://` |
| Redis Cloud, single shard | 8.6.2, Search 8.6.10 | `redis://` and TLS (`rediss://`)*† |
| Redis Cloud Pro, 2 shards, through the proxy endpoint | 8.6.2 | `redis://`† |
| Redis Cloud Pro, 2 shards, OSS Cluster API enabled | 8.6.2 | `redis://`† |

**Redis Software with verified 4× Query Performance Factor (QPF) is the
default development, local test, and CI environment.** All Redis Software
test fixtures must enable 4× QPF before tests run.
`make redis-up` pulls the `redislabs/redis:latest` image,
bootstraps a single-node cluster, and creates a Redis 8.8 RAM database with
Search and 4× QPF. Setup verifies six Search workers, `mnp` routing, and
32 connections through the management API, including on reused databases.
Open Source standalone and cluster environments are optional
compatibility targets; CI also tests the three-shard OSS Cluster API.

Earlier local and cluster runs had 325 passed, 0 failed. The Cloud runs
marked † had 322 passed, 0 failed, before the two temporary-table ingest
tests were enabled; they have not been re-run since. The TLS run marked \*
predates cluster support and has not been re-run since.

Those historical runs skipped statistics, unsupported constraints, a second
catalog and transactions. Current source validation also exercises approximate
row-count statistics. `CHECK` and `PRIMARY KEY` discovery run; `UNIQUE` and
`FOREIGN KEY` discovery remain skipped.

Redis Flex (RAM + SSD) databases are not supported yet. Older builds are
refused on connection; the tested 8.2.6-25 build fails during table creation. See
[Server requirements](driver-reference.md#server-requirements).

The differential harness compares rows, physical Arrow result types and
SQLSTATEs with digest-pinned amd64 Postgres 16.15 and **C collation** (byte order).
Run `REDIS_URI=redis://localhost:6379/0 make -C go pgdiff`.
On October 9, 2026, its expanded curated corpus passes **327/603 scored cases
(54.2%)**, including **13/13 smoke cases**, **19/23 captured dbt cases**, and
**229/371 Postgres regression cases**. Four more cases are separately classified
as documented extensions or explicit scope exclusions, for **607 cases total**.
This measures the checked-in corpus rather than full Postgres conformance.
Unresolved differences remain visible; fixture/infrastructure failures have a
separate exit status. See [the harness guide](../go/pgdiff/README.md) for the
coverage inventories, pinned sources, type-comparison limits and manual CI run.


## Latest Software retest (2026-11-04)

New RAM databases default to Redis 8.8; `SOFTWARE_REDIS_VERSION` overrides
the requested version at creation. Both Software Compose services pull the
latest image on startup. Existing databases retain their database version
when reused. The default TLS, authentication, three-shard topology, and
verified 4x QPF configuration are preserved.

The earlier checkout at `95d6583` was tested against Software 8.2.6-25 /
Redis 8.8.0 / Search 8.8.8 using an isolated single-shard RAM database.
`go test -count=1 ./...` reported one failure in
`TestTimeTZSessionZoneColumns`; the other packages passed. The same focused
test failed on Software 8.2.0-78 / Redis 8.6.2. Its expected row set assumes
New York has a different UTC offset from January, which is false on this
date. This is historical evidence for that revision and fixture, not a
passing validation of the current default topology.

The [Flex retest](redis-flex-compatibility.md) records improved Search
capabilities and the remaining driver incompatibilities in
[issue #314](https://github.com/redis/redis-adbc-driver/issues/314).

The PR fixture check on current public main passed the focused
`TestSQLAliasesInExpressions` test with three shards, TLS/authentication,
and verified active 4x QPF (six workers, `mnp`, 32 connections). Setup now
requests routing and connection count explicitly because Redis 8.8 can
retain `cmp`/five connections when only the QPF factor is set.

### Redis 8.8 defaults versus our test fixture

On Software 8.2.6-25, a fresh Redis 8.8 RAM database created with active
4x QPF returned the values below before explicit routing overrides.
“Observed” describes that database, not every Redis Software deployment or
an unconfigured database. In particular, we requested 4x QPF at creation;
this does not establish the product's default QPF factor.

| Setting | Observed before routing overrides | Required by our fixture |
|---|---|---|
| QPF active / scaling factor | `true` / `4` (explicitly requested) | `true` / `4` |
| Effective Search workers | `6` | `6` |
| `sched_policy` | `cmp`: choose the busiest eligible connection below its pipeline limit | `mnp`: choose the connection with the fewest pending commands |
| `conns` | `5` | `32` |

These are connections between the Software proxy and the shards, rather
than application-side connection-pool settings. Redis documents `cmp` as
“Closest to max_pipelined” and `mnp` as “Minimal pipeline” in the
[BDB API reference](https://redis.io/docs/latest/operate/rs/references/rest-api/objects/bdb/).
For example, if eligible connections have 8, 3, and 0 pending commands,
`cmp` chooses the queue of 8 if it is below the limit; `mnp` chooses 0.

The fixture preserves its established configuration to spread requests
across more internal connections and make concurrency available to the
Search workers. QPF controls Search capacity separately from the proxy's
connection scheduling. Thirty-two connections do not mean 32 Search workers
or CPU cores, and this check is not a benchmark showing `mnp` is always
faster than `cmp`.

Setup explicitly applies the required routing and connection count when
any effective QPF requirement is unmet, then waits for the management API
to confirm all five values. It leaves an already compliant database alone
and refuses to run tests if verification fails. TLS, authentication, and
the default three-shard topology are fixture requirements preserved by
this change; they were not inferred from the routing-default observation.

### Restricted CONFIG GET responses in newer Software

The TLS/RBAC smoke check previously required an empty `CONFIG GET` reply
for a restricted worker. Software 8.2.6-25 / Redis 8.8 instead returned
`NOPERM` in both Linux architecture jobs. The check now accepts either an
empty filtered reply or an explicit permission denial, records which one
occurred, and still rejects any readable configuration. It also retains the
separate checks that the worker cannot mutate configuration or write data.
Unrelated server and connection errors are not treated as permission denials.
