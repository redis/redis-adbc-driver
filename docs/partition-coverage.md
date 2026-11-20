# Remaining partition coverage

Current source contract, reviewed 2026-11-20. See [live reads](partitioned-reads.md),
[retained reads](retained-partition-results.md) and the [open parity gates](sdlc/changes/snapshot-parity/plan.md).
The published 0.0.19 macOS ARM64 wheel’s native live/retained TLS checks are
[historical platform-specific evidence](sdlc/changes/210-212/validation.md), not
validation of the current snapshot implementation or every release asset.
Ordinary SQL has its own supported subset and remains autocommit-only.

| Operation/source | Live partitions | Retained partitions | Ordinary SQL |
|---|---|---|---|
| Managed permanent table, row-local projection/filter | Restricted deterministic allowlist | Bounded evaluator | Supported |
| OCTET_LENGTH, BIT_LENGTH (VARCHAR/VARBINARY) | Supported, constant work/output per value | Supported | Supported |
| SUBSTR, LEFT, RIGHT | Rejected | Supported | Supported |
| Pattern/regex SUBSTRING | Rejected | Rejected | Supported forms |
| ORDER BY, LIMIT/OFFSET | Rejected | Once over complete captured input | Once per execution |
| DISTINCT, grouping, aggregates, windows | Rejected | Once at coordinator, within budgets | Supported subset |
| Joins, sets, nonrecursive CTEs, permanent views/subqueries | Rejected | Bounded supported subset | Supported subset |
| Managed copies of adopted HASHes and foreign/application indexes | Native managed partition path | Captured managed copy | Managed copy with explicit refresh |
| Explicit unmanaged registrations (application owns HASHes and indexes) | Rejected | External-key capture path; qualification below | Supported with live-source semantics |
| Live HASH registrations (application owns HASHes; driver owns indexes) | Rejected | Rejected | Supported with live-source semantics |
| Legacy storage without a current registration | Rejected | Rejected by current catalog bootstrap | Rejected; no migration |
| Temporary source | Rejected | Producer-owned temporary tables/views | Connection-owned lifetime |
| Recursive CTEs, generators, lateral/table functions | Rejected | Rejected | Supported subset |
| Volatile/side-effect functions | Rejected | Rejected | Supported subset |
| Atomic source snapshot under concurrent DML/DDL | One producer generation for the live set | One pinned managed generation during capture | One managed generation per statement |
| Retry after source changes | Same pinned generation until expiry/release | Replays immutable committed output until expiry/revocation | Fresh execution |

The retained expansion selected for
[#211](https://github.com/redis/redis-adbc-driver/issues/211) added
positional string slicing: SUBSTR, LEFT and RIGHT. These operations select from
an already bounded value; they do not generate unbounded output or execute
regex patterns. Existing scalar input, work, allocation and IPC limits remain.
Tests compare complete Arrow values/types with ordinary execution, including
NULL, Unicode, negative positions/lengths and extreme integer bounds. Global
operators still run once before chunking, never once per reader.

## Parallel reads by data and index ownership

The following matrix describes current source behavior, not a claim that every
released wheel or deployment has been validated. Concurrent ordinary SELECTs,
source partition workers and retained-output readers are different operations.
“Live partitions” is the deferred ADBC result mode, not live HASH registration.

| Ownership and deployment | Independent ordinary SELECTs | Deferred source partitions | Retained coordinator and replay | Automatic aggregate range workers |
|---|---|---|---|---|
| Driver owns data and indexes; standard Software | Supported; each statement pins its own generation | Supported for the restricted managed query subset | Bounded managed capture; immutable output replay | Eligible queries/layouts only; overlays can fall back |
| Driver owns data and indexes; compatible Software Flex with explicit stopgap | Supported; exact snapshot directories | Supported; exact directories, same descriptor/query limits | Flex-aware managed capture; independently stored replay | Serial fallback in the current range-worker implementation |
| Application owns data and indexes; unmanaged registrations on supported Software/Flex | Supported; source remains live | Unsupported; [#549](https://github.com/redis/redis-adbc-driver/issues/549) | Existing external-key capture path; no managed source snapshot; see qualification below | Numeric managed range workers do not apply; bounded typed aggregate page fetches can run concurrently |
| Application owns data; driver owns indexes; live HASH registrations | Supported; source remains live | Unsupported; [#550](https://github.com/redis/redis-adbc-driver/issues/550) | Explicitly rejected | Managed row-ID range workers do not apply |

Live HASH registrations themselves are unsupported in experimental Flex mode;
this is a deployment exclusion, not a supported mode with reduced parallelism.
Managed copies of application/foreign data belong to the first two rows, since
the driver owns the captured copy and its private indexes.

The retained coordinator has an external-key capture branch for adopted/keyed
application layouts in [partition_retained_capture.go](../go/partition_retained_capture.go).
It does not capture a managed snapshot of concurrent application writes.
That source path is distinct from both managed-copy capture and immutable
completed-output replay. The existing historical layout evidence does not
establish a current all-deployment retained matrix for explicit unmanaged
registrations; validate the selected registration, types, permissions and backend
before treating it as deployment support. Live HASH registrations are explicitly
refused before capture. See the [retained contract](retained-partition-results.md).

Implementation and focused evidence:

- [Managed partition planning](../go/partitions.go) requires a permanent managed
  row-ID table; [managed workers](../go/snapshot_partitions.go) seek exact pinned
  directories. [Flex snapshot validation](sdlc/changes/flex-snapshots/validation.md)
  covers managed partition generations on an actual Software Flex fixture.
- [Aggregate range eligibility](../go/parallel_aggregate.go) and
  [partitioned aggregate eligibility](../go/partitioned_aggregate.go) retain
  serial fallbacks for Flex and unsupported layouts/query shapes.
  [#551](https://github.com/redis/redis-adbc-driver/issues/551) tracks exact-directory
  aggregate workers. [Unmanaged page-fetch evidence](sdlc/changes/unmanaged-concurrency-20261114/validation.md)
  covers a separate bounded concurrency path; it does not add ADBC source partitions.
- [Live HASH ownership and exclusions](live-hash-tables.md) and
  [unmanaged ownership and exclusions](unmanaged-tables.md) remain in force.

Applications schedule independent queries and `ReadPartition` calls, including
across processes. `partition_count` controls split granularity and does not start
workers. Retained execution runs one bounded coordinator; parallel output replay
does not parallelize its global SQL. The [query-worker controls](worker-autotuning.md)
apply only to eligible internal workers and share capacity across connections
from one database handle, not across all processes. Redis QPF is a separate
server-side setting.

Independent managed SELECTs can pin different committed generations. Deferred
partition sets retain one producer generation; a connection-scoped
[shared read snapshot](shared-read-snapshots.md) has no exported cross-process
attachment API. Concurrent readers do not create a common point-in-time view of
application-owned data. [#552](https://github.com/redis/redis-adbc-driver/issues/552)
tracks this documentation reconciliation; #549–#551 remain feature requests.

## Source-consistent capture

Managed statement snapshots are always enabled. Live sets retain one producer
generation, retained coordinators evaluate one pinned generation, and ordinary
statements each acquire their own generation. Concurrent SQL DML/DDL does not
change a pinned input. Readiness checks and before/after counts do not establish
this guarantee; immutable data/catalog generations and their reference protocol do.

Application-owned HASHes and indexes remain external. Managed imports read a
private managed copy, and explicit refresh publishes a new copy for future
statements. Explicit unmanaged and live registrations read application HASHes
directly; their concurrent source values are not frozen by a managed catalog pin. External
writers do not participate in the driver’s snapshot protocol; a refresh does not
promise an atomic capture of arbitrary concurrently changing application data.
Completed copies and retained output preserve the captured exact values.

Retained capture admits temporary tables/views owned by the producing connection.
The coordinator copies its temporary catalog identity; unowned temporary schemas
are rejected. Independent readers access only committed namespace-authorized
output. Closing the producer cancels active work; completed output remains
replayable after the producer’s temporary objects are removed. Existing deadlines,
budgets, quota, expiry and revocation apply.

Full feature/platform and current-protocol Cluster recovery checks, process/input
resumption and comparative/sustained workload measurements remain open in the
parity plan. Targeted snapshot and replay checks do not establish complete
packaged-release or performance parity.

## Byte-length expansion (#304)

OCTET_LENGTH and BIT_LENGTH accept one VARCHAR or VARBINARY scalar (or NULL),
return BIGINT byte/bit counts, and share the ordinary evaluator across live and
retained execution. UTF-8 characters count their encoded bytes, embedded NULs
count, empty inputs return zero and NULL returns NULL. Other SQL types, including
fixed CHAR, and wrong arity reject during binding. There is no output expansion,
new retained per-row state or string copy: evaluation inspects the existing byte
length. Source fetch/batch admission and retained work/allocation/output limits
remain in force. Global operations still run once before retained chunking;
regex, recursion, generators, lateral and volatile functions retain their limits.
