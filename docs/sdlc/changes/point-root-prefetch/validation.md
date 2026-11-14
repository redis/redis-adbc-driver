# Validation

**NOT SELECTED.** Frozen experiment evidence is retained; selected code restores the original metadata path. See the [final validation](../performance-followup-20261114/validation.md).

Date: November 14, 2026. Author: Codex. Status: native pilot complete; later
restricted-mode eligibility guard awaits coordinating root's integrated checks.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

## Actual executed source and evidence

Measured candidate `762d4ef2` follows prototype `f569b078`; baseline is PR #439
head `7b94a93c`. Their production code is frozen for the archived observations.
The [complete native point-only ABBA pilot](../../../../benchmarks/point-root-prefetch/2026-11-14/report.md)
retains all 96 records, six exact comparisons, every latency sample and pre/post
binary/harness hashes. The [offline audit](../../../../benchmarks/point-root-prefetch/audit.py)
recomputes per-window/pooled medians, nearest-rank p95, finite throughput, case
inventory, exact source/settings, check source and file manifest. It passes.
No historical PR #439 data were changed or relabeled.

Focused verified Software command:

```sh
go test -v -race -count=1 -p 1 -timeout=15m \
  -run 'Test(SnapshotRootPrefetch|SnapshotAcquireProof|SnapshotDescriptorRead|SnapshotMetadata|SnapshotManifest|SnapshotDirectory|SnapshotStream|SharedRead|StatementRepeatedSQL)' .
```

**38 top-level PASS, zero SKIP, zero FAIL**, package 12.328 s / wrapper 33.067 s.
Native bounds cover missing/small/exactly256/257 entries, huge field/value/tail
values and wrongtype, including exact unchanged native HGETALL on a decline.
Deterministic checks cover root-map scope/consumption, exact native fallback,
unknown/composed outcomes, malformed reply/width, original metadata/lease error
precedence and no lease digest repair after a later unknown/malformed root reply.
The actual command, source and pass names are archived in checks.json.

Four native Python/C-boundary windows ran baseline/candidate/candidate/baseline,
with 24 validated records each, one warmup and three measured rounds per case,
32 operations per round, all modes and one/four clients. Every result was fully
fetched and independently checked, and each 1K-row fixture's complete post-state
was verified. 2,304 measured observations, zero retries/conflicts/failed or
discarded records. Both builds completed before any timings; no live tests,
compilers or competing benchmarks overlapped the exclusive window.

Pooled p50 milliseconds (192 observations/case/build):

| Mode | Clients | Baseline | Candidate | Baseline/candidate |
|---|---:|---:|---:|---:|
| managed | 1 | 3.0393 | 2.7283 | 1.114× |
| managed | 4 | 6.0321 | 5.5272 | 1.091× |
| unmanaged | 1 | 3.0223 | 2.7483 | 1.100× |
| unmanaged | 4 | 5.7206 | 5.4456 | 1.050× |
| redis-py | 1 | 0.2433 | 0.2409 | 1.010× |
| redis-py | 4 | 0.4937 | 0.4934 | 1.001× |

The observed 5–10% driver p50 reductions justify retaining the prototype for
integrated validation. This pilot is not the full 168-record/build suite, a
capacity claim or a causal CPU/allocator profile; all individual windows,
p95 and finite throughput are retained. The driver remains much slower than
redis-py. No ingestion, mutation, aggregation, transfer, shared/restricted-mode,
Cluster API, ClickBench or Flex timing is claimed.

All live work used the root-controlled owned Redis Software fixture created
through `make -C go redis-up`, independently verified before each command/window
for active/effective 4× QPF, six workers/mnp32, one 4-GiB noeviction RAM shard,
strict certificate-verified data TLS/RESP3 and recorded actual server/resources.
Each matrix process/client exited before exact owner-marker clearing; final
fixture data/indexes were empty when the agent released its slot. Final shared
container/network/credential teardown belongs to coordinating root after the
remaining agents finish; it is not claimed here. Operational artifacts stay private.

## Review corrections and separate source follow-up

Cross-review found combined expired-lease/malformed-prefetch precedence. Pure
local interpretation of a successful lease reply now occurs before new root
decoding, while unaccounted pipeline failures retain original precedence and
lease digest repair waits for every new outcome. Root review also found an
accounted new decline could mask an unknown lease diagnostic; terminal lease
errors now retain their own diagnostic. Both corrections and regressions were
included before the executed `762d4ef2` race/native source.

Separate post-measurement source `a623a94f` skips `pin.reader` and restricted
stores, so known restricted readers do not probe commands outside their intended
ACL. A declined probe shares the existing pipeline and adds no round trip; it
would otherwise add avoidable rejected-command/ACL cost while retaining the
original separate native HGETALL. This follow-up adds no capability cache or
cross-statement authorization reuse. Its eligibility cases were authored after
the timing window and require root's integrated check; earlier source evidence
is not relabeled. Ordinary privileged fixture eligibility is unchanged.

Agent reviews are not maintainer approval. Link the final integrating PR and
its additional checks/whole-matrix evidence separately.

A later failure-path follow-up explicitly propagates a local pipeline `Process`
queue error and discards previously queued proof commands before `Exec`. Generic
`once` ignores a `Process` return value; without this follow-up a custom local
queue refusal could lose its original EOF diagnostic. A custom-pipeline regression
asserts zero Exec/direct calls, one discard and no installed map. This is separate
unmeasured guard source; the successful measured wire path is unchanged. It awaits
the coordinating root's integrated guard checks, not a relabeled earlier pass.

## Final integrated verification

The [combined follow-up validation](../performance-followup-20261114/validation.md)
records final guarded source 3f9cf6a8, integrated race/unit/snapshot outcomes, all 42
final native comparisons and ownership cleanup. Earlier pilot/diagnostic sources
and limits above remain unchanged; they are not relabeled as the integrated build.

## Selection follow-up

Root-map prefetch is not selected after broader native evidence showed material
tail and mixed-workload regressions plus control drift. This is a conservative
selection decision, not proof of causality. Metadata proof/read Go files restore
31426 exactly; the six-file experiment patch applies cleanly to that baseline.
Earlier measurements retain their actual sources. A fresh selected-source native
comparison and final checks supersede experimental-source performance claims.
