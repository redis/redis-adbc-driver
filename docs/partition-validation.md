# Partition validation and deployment ownership

2026-10-21. [Issue #170](https://github.com/redis/redis-adbc-driver/issues/170)
remains broader than local validation. A workflow definition is not a test result.
See [change evidence](sdlc/changes/partition-followups/evidence.md).

| Environment | Durable job / remaining gate |
|---|---|
| Linux amd64/arm64, OSS standalone/Cluster and Software single/proxy sharded | Existing Test matrix runs full configured Go and Python validation. JUnit artifacts preserve all feature skips; a gate rejects skipped/missing native partition tests |
| Linux packaged tarballs | Existing Release platforms matrix validates packaged libraries on supported distributions; native partitions are included in the full test collection |
| macOS arm64 native library | Local full validation evidence recorded with this change; packaged-wheel loading remains in Release. Remote endpoint tests can be dispatched separately; no local Linux result substitutes for macOS packaging evidence |
| Direct three-primary TLS endpoints | [Real-owner evidence](sdlc/changes/partition-cluster-tls-363/evidence.md): four native/wheel runs passed 17 checks each on macOS/Linux ARM64, including physical row presence on every owner and all-primary ACL/cap checks. Linux Test/Release gates added; hosted native x86_64 execution pending |
| Manual TLS slot migration | [ASK/MOVED evidence](sdlc/changes/partition-tls-redirects-363/evidence.md): two macOS ARM64 native/wheel runs passed 19 checks each, including persistent worker connections, ASKING denial/grant and full post-move values/physical owners. Existing Linux gates reuse the checker; replica failover, concurrent writes and mid-stream recovery remain open |
| Cloud / TLS / independent credentials | [Two-shard Cloud Pro evidence](sdlc/changes/cloud-partition-routing-363/evidence.md): macOS arm64 native proxy 16 checks and Linux ARM64 installed-wheel automatic Cluster API 17 checks; two backend shards expose one external proxy slot owner. [Earlier macOS minimum-client evidence](sdlc/changes/cloud-partition-acl-363/evidence.md) covers ADBC 1.8. Remaining platform/client, direct backend routing and failure scenarios remain open |
| Installed-wheel Python/ADBC versions | Release `wheel-clients` consumes existing platform artifacts for Python 3.13/3.14 × ADBC manager 1.8/1.12 on Linux x86_64/ARM64 and macOS ARM64; [eight local cells passed](sdlc/changes/partition-wheel-client-matrix-363/evidence.md), hosted 12-cell execution pending. These are TLS partition checks; parent jobs retain full upstream suites |
| Performance | [Benchmark harness](../benchmarks/partitions/README.md), fixed-data schema/value checks and environment records; small smoke runs are not production measurements |

New Cloud validation and benchmark deployments default to **Redis Cloud Pro**.
Provision a dedicated non-production database, record its estimated cost and owner,
and delete temporary resources after validation.

The deployment workflow builds the library once per platform/client combination on Linux or
macOS, runs the full upstream suite, native cross-process partitions and separate skip
reports. GitHub environment `partition-validation` must provide secret
`REDIS_URI`, a disposable Redis endpoint (use rediss for TLS), and optional
`REDIS_WORKER_URI` for independent read-only worker credentials. Set repository
variable `PARTITION_TEST_OWNER` to the responsible owner and document the service,
version, routing, expected cost cap, deletion date and cleanup owner in the run
record. Runner and database costs belong to that configured owner; the workflow
does not provision accounts, paid services or databases. Do not run the
full suite against a production database; it performs destructive test DDL/data
operations within test namespaces.

The native worker tests can use REDIS_WORKER_URI only for read connections;
planning/setup/cleanup retain REDIS_URI. Workers need metadata and row read access,
FT.AGGREGATE/FT.CURSOR permissions, Search routing access and TLS trust. If CONFIG
is hidden, planning must include a valid guaranteed scan cap. A descriptor grants
no permissions. Read-only worker roles must not receive schema/ingestion rights.

For independent Cloud credentials, use the [owned Cloud Pro runner](cloud-partition-validation.md).
It creates database-scoped ACL principals and preserves interruption cleanup records.

Before publishing Cloud/Software compatibility, archive server/service versions,
client versions, endpoint topology (without credentials), TLS trust setup,
commands, JUnit results, all feature skips, malformed/stale descriptor cases,
cancellation/early release and cross-process evidence. Use the service's own
monitoring for server peaks and billable network/storage. An absent run remains
an evidence gap, not a passed compatibility gate.
