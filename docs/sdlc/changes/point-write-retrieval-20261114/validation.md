# Validation and review — draft

Date: November 14, 2026 (America/Los_Angeles). Requestor: repository user.
Author/review coordinator: Codex with three delegated agents. Human review pending.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

**Draft status:** this records completed evidence through integrated source
`665c3f1b2712776890c1f288fefe86b609dd136f`. Final rebased production source,
subsequent guard validation, completed ordinary/full-snapshot suites, isolated
native matrices, evidence audit and fixture cleanup remain pending. Do not
interpret this document as final validation or a completed native speedup claim.

## Environment and measurement boundaries

The coordinator provisioned its owned Redis Software fixture using
`make -C go redis-up`. Its independent gate verified active/effective 4x QPF,
six Search workers, mnp/32, a 4-GiB noeviction RAM shard, actual four-CPU/eight-GiB
container limits, certificate/hostname-verified data TLS and RESP3 before the
recorded live selections. Agents used exclusive serial live slots and closed
their clients before returning those slots. This is not final infrastructure
teardown evidence; the fixture remains under coordinator ownership until the
remaining suites and matrices finish. Endpoints, operational identifiers,
credentials, licenses and raw logs remain private.

Scoped Go diagnostics used Go 1.27.3 and GOMAXPROCS=4. Concurrent compilation/host
activity could affect their wall times. They establish client-command shape and
limited workload observations, not isolated Python/ADBC latency, server CPU,
production capacity, confidence intervals or a product-wide ranking. No new 100M
ClickBench, Flex, Flight SQL, transfer or bulk-ingestion result is established by
this change. Historical results retain their actual source/server labels.

## Completed correctness checks

The following are **top-level Go test counts per completed command**, not a sum
of distinct tests. Counts come from completed verbose output and successful exit
records; package execution time excludes compilation/wrapper overhead.

| Completed selection | Source scope | Pass | Skip | Fail | Package seconds |
|---|---|---:|---:|---:|---:|
| Existing managed sustained/stage/index-wait diagnostics | unchanged 0b216478 production | 3 | 0 | 0 | 8.565 |
| Matching managed 1K-row baseline diagnostic | unchanged 0b216478 production plus new diagnostic test | 1 | 0 | 0 | 7.185 |
| New managed point-proof regressions | scoped managed candidate | 4 | 0 | 0 | 1.726 |
| Matching managed candidate diagnostic | scoped production 6af289dd | 1 | 0 | 0 | 6.686 |
| Managed version/publication/Arrow/strict-partition regressions | scoped managed candidate | 33 | 0 | 0 | 8.983 |
| Point-read baseline profile/manifest checks | unchanged 0b216478 production | 3 | 0 | 0 | 1.176 |
| Point-read candidate profile/corruption/lifetime/freshness checks | scoped point candidate; predates final error-precedence refinements | 26 | 0 | 0 | 14.678 |
| Live unmanaged projection/filtered membership/Arrow lifetime checks | scoped retrieval candidate | 6 | 0 | 0 | 4.691 |
| Full-size unmanaged baseline diagnostic | exact restored baseline query source | 1 | 0 | 0 | 8.811 |
| Full-size unmanaged candidate diagnostic | scoped timed retrieval source | 1 | 0 | 0 | 8.110 |
| Integrated focused Software race selection | 665c3f1b | 80 | 2 | 0 | 22.764 |

The integrated race wrapper completed successfully in 42.278 seconds including
build/runner overhead. Its command was:

```sh
go test -v -race -count=1 -p 1 -timeout=15m \
  -run 'Test(SnapshotPointWriteRead|SnapshotDirectory|SnapshotMetadata|SnapshotManifest|SnapshotValid|SnapshotControl|UnmanagedProjectionRead|UnmanagedFiltered|UnmanagedStatement|SnapshotRuntimeExactBigInt|SnapshotStream|SharedRead|SQLRekeyTakeoverWait|ExecutionPool|StatementRepeatedSQL)' .
```

Its exact skips were:

- `TestSnapshotControlScriptEvalOnlySoftware`: requires an owned Software
  EVAL-only operator fixture, which was not configured for this selection.
- `TestSnapshotControlScriptPointLeaseSoftware`: requires an owned Software
  operator without HGETALL, which was not configured for this selection.

These skips do not establish native compatibility for those operator profiles.
The integrated selection includes the new MaxInt64 row-ID/opaque-byte test and
joined/wrapped absence guard. Its pattern **does not include** the new
`TestSnapshotAcquireProof*` or `TestSnapshotDescriptorRead*` unit-test families;
the point agent's scoped unit/race checks covered them, but a final combined
race selection including both families is still pending here.

Scoped unit-only all-package checks passed for managed and unmanaged work;
Redis-dependent cases skip without endpoints. Their nonverbose outputs do not
supply exact native skip counts, so none are inferred. The managed offline audit
passes all 96 baseline plus 96 candidate diagnostic observations, complete case
coverage, source/input hashes and comparison arithmetic. Scoped formatting and
`git diff --check` passed. These are not substitutes for final combined checks.

## Completed diagnostic observations

- **Point proof batching:** 13 to 11 client exchanges in each mode, while the
  original 20 managed/19 unmanaged Redis command counts and four lease-valid
  checks remain. The new shapes are `hget,zcard,hlen,control_valid` and
  `hmget,exists,get`. Tiny warm Go medians were **slower** in that diagnostic:
  managed 2.790 to 4.003 ms, unmanaged 3.130 to 3.926 ms. No native latency gain
  is inferred. Later error-precedence refinements retain separate test evidence.
- **Managed mutations:** the matching diagnostic independently checked all 96
  mutations and complete value/count states. Scoped p50 milliseconds were
  INSERT 39.934 to 45.245 (**slower**), UPDATE 64.733 to 44.743, DELETE 49.587 to
  42.910. Source/hash-pinned [complete observations and audit](../../../../benchmarks/managed-point-images/2026-11-14/report.md)
  retain every sample and client-call category. HASH/directory commands execute
  inside the fused scripts; reduced client exchanges do not imply fewer native
  HASH reads. Background maintenance can change segment counts between runs.
- **Index readiness:** fresh final indexes were ready on the first poll in the
  diagnostic, with 1.77–2.60 ms create-to-readiness. Faster readiness polling was
  not implemented. Native slowlog was available but its threshold-selected,
  uncorrelated events do not attribute the observed admission wait to Lua CPU.
- **Unmanaged filtered aggregation:** a matched 100K-row Go execution/Arrow
  profile observed filtered COUNT/SUM median 243.75 to 94.55 ms, validating every
  Arrow INT64 result against independent arithmetic. Transfer fell about 2.02
  to 1.06 MB per call, while allocation rose about 18.8 to 21.0 MB. It still
  performs 10K per-key reads plus Search discovery. The unchanged grouped
  LOAD-star path remains around 390 ms. This is not the final Python/C native
  matrix and does not claim fanout elimination. Later source-address/error
  accounting guards retain their actual later test sources.

Detailed scope records:
[point-read proofs](../point-read-proof-pipelines/validation.md),
[managed point images](../managed-point-image-read/validation.md),
[live unmanaged projection](../unmanaged-live-projection/validation.md).
Do not relabel those earlier scoped measurements with the final combined source.

## Review and fixes

Three agents reviewed the isolated changes and their interactions; agent review
is not human approval. The coordinator and independent cross-review checked:

- Native commands/keys/namespace arguments and error precedence in queued
  lease and descriptor proofs; one full-body fallback only after a direct
  native NOSCRIPT/EVALSHA-command refusal and successful metadata proof.
  Unknown pipeline failures remain terminal, shared pins keep their existing
  terminal-loss path, and descriptor values/authorization are not cached.
- Single-key writer-scope read fusion retains newest-to-oldest early stopping,
  tombstones, conditional directory proofs, exact string/int64/NULL/NUL behavior,
  request bounds and all outer pin/ownership/publication fences. MaxInt64 row
  IDs and opaque HASH bytes passed in the integrated race selection. Strict
  direct readers and multi-key batches retain their prior execution path.
- Live projected HASH retrieval keeps source discovery/membership and the real
  source address authoritative, including application fields named `__key`.
  Atomic existence/type/field reads preserve existing all-selected-fields-NULL
  HASHes and established missing/wrong-type policy. Command/byte bounds and
  cursor/Arrow/cancellation lifetime remain explicit.
- Cross-review reproduced unsafe composed absence/refusal handling in the
  retrieval predecessor: `errors.Is(err, goredis.Nil)` could unwrap an unknown
  transport failure and permit omission or fallback. The fix requires direct
  nil equality and accounted direct native outcomes, inspects every pipeline
  command, and retains unknown diagnostics without repair/replay. Earlier timed
  observations are not relabeled as this later correctness-source measurement.

No additional actionable finding remained in the completed scoped reviews.
This does not assert final combined review completion for subsequent guards or
rebased source. The collector scheduling fixture correction is tracked separately
in [PR #438](https://github.com/redis/redis-adbc-driver/pull/438); its intentionally
competing collectors and original assertions remain, without sleeps/retries or
broader skips. Do not attribute performance gains to that test-only correction.

## Pending before final publication

1. Record the final rebased production commit, PR reference and independent
   review outcome for any subsequent guard; preserve each earlier check's
   actual source rather than relabeling it.
2. Finish and summarize the ordinary Software suite with exact passes/skips/
   failures and skipped test names. Run the separate full snapshot suite and
   final combined race selection, explicitly including acquisition/descriptor
   proof tests and all new guard regressions. Record capability/topology limits.
3. Freeze clean versioned baseline/candidate library and CLI builds plus the
   identical scenario harness; finish both complete native matrices
   consecutively with other heavy tasks paused. Retain every warmup/measured
   sample, validation result, failure and p50/p95/throughput comparison,
   including flat/slower cases. Record actual resource/server/settings and
   pre/post source/binary/input hashes.
4. Audit final chart/comparison arithmetic, case completeness, provenance,
   private-artifact sanitization and all claimed validations. New native source
   measurements remain pending until those runs and their independent fetch/
   post-state checks complete successfully.
5. After every client/process exits, perform ownership-checked data/index cleanup,
   final QPF/TLS/RESP3 verification and owned fixture/network/generated-secret
   teardown while preserving preexisting resources. Publish sanitized cleanup
   proof; never infer cleanup from process exit alone.
6. Update this draft, scoped records and the PR around actual final results and
   unresolved limits; human maintainer approval and merge remain separate.
