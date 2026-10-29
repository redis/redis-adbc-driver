## Compatibility and validation

On **2026-10-08**, the validation suite against the default local **Redis
Software** database (Redis 8.6.2, Search 8.6.11) had **360 passed, 8 skipped,
3 subtests passed, and 0 failed**. The full Go suite also passed, with explicit
skips for protocol-level ACL administration and resettable command counters
that Software blocks. Those checks passed separately on the OSS three-shard
compatibility environment. See [Building and testing](driver-reference.md#building-and-testing).
The other validation results below are from earlier runs.

| Server | Version | Connection |
|-|-|-|
| **Redis Software (default)**, `redislabs/redis:8.2.0-78.18` | Redis 8.6.2, Search 8.6.11 | `redis://` |
| Redis Open Source (`redis:8.6.2` Docker image) | 8.6.2, Search 8.6.0 | `redis://` |
| Redis Open Source cluster, 3 shards (OSS Cluster API) | 8.6.2, Search 8.6.0 | `redis://` |
| Redis Cloud, single shard | 8.6.2, Search 8.6.10 | `redis://` and TLS (`rediss://`)*† |
| Redis Cloud Pro, 2 shards, through the proxy endpoint | 8.6.2 | `redis://`† |
| Redis Cloud Pro, 2 shards, OSS Cluster API enabled | 8.6.2 | `redis://`† |

**Redis Software with verified 4× Query Performance Factor (QPF) is the
default development, local test, and CI environment.** All Redis Software
test fixtures must enable 4× QPF before tests run.
`make redis-up` starts the pinned `redislabs/redis:8.2.0-78.18` image,
bootstraps a single-node cluster, and creates a Redis 8.6 RAM database with
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

Redis Flex (RAM + SSD) databases are not supported yet, and the driver
refuses them when it connects; see
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

