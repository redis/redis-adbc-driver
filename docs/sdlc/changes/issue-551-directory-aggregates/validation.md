# Validation and review

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex.
Initial baseline `845c95b4`, rebased onto `1d0d0b3b`. Final production/test source
`04091bca456318b463d5a3e8731f2c31ad672320`; the identical published tree is `46d1cbf4d813af69ca161f5d32c0a25dc53c2d5d` after author/committer privacy rewriting. Later edits only record evidence.
Status: implemented and locally validated; awaiting human review. No release recorded.

[Intent](intent.md) · [Spec](spec.md) · [Plan](plan.md)

## Environment and evidence

Dedicated Redis Software RAM and actual Flex fixtures: Software 8.2.6-58,
Redis 8.8.0, TLS/RESP3/noeviction, one primary shard each. Standard three-shard
allocation initially returned the explicit management insufficient-resource
refusal; the supported one-shard fixture was then used, without weakening QPF.
Both setup paths verified active 4x QPF, six effective Search workers, mnp routing
and 32 proxy connections before tests, and reverified immediately before profiling.
Flex used BigStore v2, 1 GiB total and 256 MiB RAM with a container-directory flash
path. This is local correctness/warm-data evidence, not cold SSD or packaged
release validation. All temporary instances are owned by this change and cleaned
up after verification; operational identifiers, secrets and raw logs stay private.

[Measurements, exact environment and file hashes](../../../../benchmarks/directory-aggregate-workers/README.md)
record all 30 samples. Source provenance is frozen before recording results;
preliminary profiles are not mixed into that result set.

## Checks

Run endpoint-bearing commands only after the corresponding setup gate, against
an explicitly disposable database: the snapshot harness clears keys and indexes.

| Check | Outcome |
|---|---|
| Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` | All packages passed on final source. Integration tests skip without endpoints; this row is not Redis compatibility evidence. |
| RAM `REDIS_SNAPSHOT_URI="$REDIS_URI" go test -race -count=1 -p 1 -v -timeout=7m -run '^TestSnapshotDirectoryAggregate' .` | Passed, 26.086s; 11 passing test/subtest entries. Opt-in profile skipped in this selection. |
| Actual Flex, explicit stopgap: same focused race selection with `REDIS_SNAPSHOT_FLEX=true` | Passed, 29.338s; 10 passing entries. Skips: native IDs beyond 2^53 (existing Flex allocator limit) and opt-in profile. |
| RAM aggregate/reducer/worker regression race selection | Passed on final source, 16.791s. Includes existing SQL parallel aggregates, exact integer inputs/error ordering, reducer merge, worker-budget/tuner tests and snapshot index/tuple aggregate paths. |
| `make -C go build` | Passed with Makefile driver-version linker flag; native macOS arm64 shared library. |
| RAM opt-in exact-directory profile, five samples each for 1/4/auto | Passed, 9.325s. All Arrow values and schemas validated; active/queued grants zero after every fetch. |
| Actual Flex equivalent profile | Passed, 10.297s. Same full result validation and resource checks. |
| `git diff --check`, changed Go formatting, local Markdown targets | Passed. |

The final regression selection is:

```text
^Test(SQLParallelAggregates|AggregateInteger|SQLAggregateExpressionInputs|AggregateIntegerBoundaries|MergeExactSumPartitions|WorkerBudget|WorkerTuner|SnapshotIndexAggregates|SnapshotPartitionedTupleAggregates)
```

The focused fixtures cover native baseline/overlay layouts, full pushed-only and
mixed pushed/residual predicates, failure when a pushed query lacks its complete
predicate, exact int64 IDs above 2^53 on RAM, sparse/empty ranges, replacements and
tombstones, integer/decimal and NULL/NaN/infinity reduction, unsupported shapes,
queued cancellation/autotuning, a permission refusal and injected transport error,
expired reader pins and automatic-training discard, and worker cancellation/join
before grants are returned. An overlap fixture pauses four active readers while
later SQL writes and compaction publish, then verifies the original aggregate.

## Findings resolved during implementation

- Initial directory completion relied on the outer executor's final pin check.
  A direct expired-pin fixture demonstrated a successful backend result; directory
  aggregates now validate their shared lease before return, grant release and
  autotuner completion, including one-worker grants. Failed expiry cannot train.
- Review of baseline promotion found `planWhere` retained original predicates
  only for overlays/Flex. A high-ID standard baseline with a pushed condition
  could lose that condition when switching to directories. Native baselines now
  retain the full predicate; explicit pushed-only/mixed tests prove the fix. A
  nontrivial pushed query without the original predicate fails closed.
- Scan failures previously waited for sibling work to finish naturally. They
  now cancel/join siblings and preserve the originating scan failure rather than
  returning a sibling cancellation. Pure input-expression error ordering keeps
  its existing immutable serial-evaluation fallback; scan failures take priority.

## Review and limitations

Reviewed correctness/lifecycle, ownership/security and intent/evidence against
REVIEW.md. The new range path retains the caller's pin, exact directory identities,
complete predicates, bounded pages and existing reducers; there is no metadata
publication, source mutation, descriptor-format change or application-owned table
support added here. The existing shared budget remains local to a database
handle, not a global server/process coordinator.

Local measurements show lower completion times for parallel settings in this
small warm workload. They do not establish an optimum automatic allocation,
full-scale throughput, Linux-native route behavior, cold Flex performance or
cross-process admission. Automatic runs remained in a noisy baseline state.
Grouped/DISTINCT/FILTER/unsupported shapes retain their existing paths, as does
the separate Flex-excluded partitioned tuple/spill engine. Hosted CI, other
platforms, current failover/restart coverage and released binaries are not inferred
from these local checks. No human review approval or merge decision is recorded.
