# Issue #336 acceptance audit

Date: 2026-11-06. Scope: foreground fixed/per-write cost reduction in
[#336](https://github.com/redis/redis-adbc-driver/issues/336), following minimal
snapshot delivery. Production remains `fc10e0d2`; this closeout adds an opt-in
profiling fixture and evidence, not another runtime optimization.

## Acceptance mapping

| Requirement | Evidence |
|---|---|
| Source-specific admission, metadata/copy/dependency, sealing, readiness and publication attribution | [Warm-connection phase profile](publication-stage-profile.json) below, plus baseline/candidate [command attribution](attribution-summary.json) and the earlier [manifest-proof profile](../foreground-manifest-proof/validation.md) |
| Supported native primitives preferred; evaluate upstream barrier without assuming support | PR #346 batches existing native reads; PR #347 reuses an authorized decoded value. No new Lua/format/grant/cache/retry protocol. Upstream evaluation below retains existing readiness proofs |
| Publication/dependency conflicts, exact values/provenance, permissions, unknown replies and reclaimability preserved | [Complete local validation](validation.md): 559 snapshot race events / 44 explicit skips, actual RBAC 21 / no skips, native snapshot 19 / no skips, full native 565 + 3 subtests / 8 skips, all unit packages pass. Current-head hosted checks remain a separate delivery gate |
| Matched complete before/after operations on verified QPF4 Software, both orders, small/no-op/unrelated cases, regressions and storage/index costs | [Descriptor comparison](native-comparison.json): 12 processes / 280 measured operations; [earlier comparison](../foreground-manifest-proof/native-comparison.json): 20 / 392. All samples retained, including negative results/repeats. Private-key and native index costs below |

The full race selection includes independent colliding-bucket publication,
read-source conflicts, pre-workspace dependencies, sealed-output mutation refusal,
metadata-only CAS retries and lost-merge-reply resolution. It also includes
unrelated metadata independence, managed imported-source identity/late projection,
unknown reservation/index-notification refusal and reference reclamation cases.
These are actual executed cases in the source-mapped report, not inferred from
unit integration skips. The separate recovery investigation #345 stays open.

## Publication phase attribution

`TestSnapshotForegroundPublicationStageProfile` is opt-in through
`REDIS_SNAPSHOT_STAGE_PROFILE`. It drives existing uncontended helpers in normal
order, with one atomic publication command, no SQL/input retry and exactly one
final index build. It is not a second production publication path or a native
throughput benchmark. A warmed connection excludes initial TLS/dial setup;
background workers are disabled while normal bounded foreground reclamation
remains. Setup and final storage probes are outside phase timers.

The fixture creates a 128-row target with exact BIGINT values above 2^53, updates
one row and checks all 128 published rows plus all unrelated values. Both 0 and
32 unrelated one-row tables are exercised. One planning dependency is retained
in the workspace in both cases. Metadata/dependency collection and immutable
source validation share one measured phase; this does not separately estimate
the CPU cost of an individual map assignment.

| Phase, milliseconds | 0 unrelated tables | 32 unrelated tables |
|---|---:|---:|
| Admission, including bounded reclamation after setup | 6.780 | 5.189 |
| Metadata resolution, immutable input proof and dependency collection | 1.452 | 1.450 |
| Reservation and shared metadata-root references/dependency inheritance | 0.730 | 0.825 |
| Private metadata and one-row SQL stage | 4.633 | 5.678 |
| Writer lease validation | 0.208 | 0.210 |
| Final index sealing/population and descriptor update | 2.450 | 2.491 |
| Readiness, exact indexed-row coverage and final descriptor/queue preparation | 1.716 | 1.564 |
| Deletion pruning (no deletion in this workload) | <0.001 | <0.001 |
| Atomic metadata seal, CAS and publication | 0.275 | 0.400 |

These are one controlled observation per catalog size, not steady-state means
or a before/after speedup claim. Reclamation depends on earlier setup and phases
contain client/instrumentation overhead. Raw command counts/times are retained;
concurrent heartbeat accounting is not asserted to equal helper wall time.
The first exploratory run included initial connection setup; final timings above
warm it first. No latency improvement is attributed to that method change.

The final actual Software profile passes **three test events / zero skips**,
parent **1.81 seconds** including setup/final checks. The final profiling fixture
also passes separately under the race detector: three pass events, no skips or
race reports; report aggregation is mutex-protected. Default no-endpoint execution
compiles and explicitly skips the opt-in case; that skip is not deployment proof.
Production sources match `fc10e0d2`; the later profiling source is test-only.
Software **8.2.6-25 / database Redis 8.8.0**, strict TLS, effective QPF4 and
noeviction are verified before execution, on the same Apple ARM64 environment
recorded in the comparison. This adds no Cluster API/failover coverage.

## Private storage and index costs

Memory probes run only after timed phases. They use known owned keys, without
keyspace enumeration. These are selected-key allocator measurements, not net
DB memory, peak allocation, shared-graph cost or retained-version totals.

| Native key memory, bytes | 0 unrelated | 32 unrelated |
|---|---:|---:|
| New complete row HASH | 181 | 181 |
| New exact row directory | 127 | 127 |
| New table descriptor | 1,408 | 1,408 |
| Root map of bucket-reference identities | 287 | 2,377 |
| Owned metadata-key directory | 1,032 | 1,032 |

The new output contains **one row**, not a 128-row copy. In both cases there is
**one final FT.CREATE**, no index drops during sealing/readiness, and zero
client-observed unrelated descriptor payload GET/COPY requests. Shared bucket
reference bookkeeping still occurs; the larger root-map memory above is retained
as a real catalog-size cost. This does not claim that unrelated reference IDs
are never inspected or that total metadata cost is independent of catalog size.

Native FT.INFO for the new one-document index reports, in both cases:
`doc_table_size_mb=0.023229598999023438`,
`inverted_sz_mb=0.000370025634765625`,
`sortable_values_size_mb=0.00015735626220703125`, and `key_table_size_mb=0`.
These are native per-index fields, not a total DB or full-retention memory claim.
The existing published source index is retained. Repeated maintenance/retention
costs remain under #337, not a performance-parity completion claim here.

## Upstream primitive evaluation

On 2026-11-06, the official [FT.INFO reference](https://redis.io/docs/latest/commands/ft.info/)
documents indexing progress, counts and failures, with an index-name argument;
no acknowledged-write/incarnation token is described there. The requested
[RediSearch #12220 barrier](https://github.com/RediSearch/RediSearch/issues/12220)
remains an open proposal. Neither page establishes the requested replacement
contract as available for this Software version.

Current [RedisVL visibility documentation](https://redis.io/docs/latest/develop/ai/redisvl/concepts/search-and-indexing/#search-visibility-after-writes)
notes that Redis 8.8 background indexing can lag key writes. The driver therefore
retains its actual readiness, failure, exact-prefix/directory and warning-free
indexed-row proof. QPF is not reduced and no synchronous-indexing assumption or
unsupported barrier is used to waive validation. The two optimizations use
already supported native requests/local decoding rather than new server machinery.

## Completion boundary

#336 requires measured foreground cost reduction with preserved correctness,
not elimination of every remaining cost or parity with mutable storage. The
six avoided round trips across PRs #346 and #347 are source-specific; their
latency samples come from different sessions and must not be pooled or compounded.
The measured gains, modest/flat cases, negative controls and earlier investigated
regression remain visible. No improvement is claimed for sustained compaction,
resumable operations or cross-shard snapshots.

Delivery remains gated on final-head hosted checks, final source/evidence review,
owned fixture cleanup and merging PR #347. The issue must not be closed before
those steps complete. Formal GitHub human approval is not inferred from an agent
review; delivery is being carried out under the user's explicit instruction to
work on #336 until closed, subject to actual repository requirements.
