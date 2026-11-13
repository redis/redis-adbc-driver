# Validation

Date: November 13, 2026. Evidence complete; parent source review pending.
No human approval is inferred. No production optimization is proposed: existing
metadata gates prevent the simple batching proposals, and the measured release
latency needs further causal evidence before changing cleanup or consistency.

## Sources and execution

Base production: `24f654b76b98f3d8960af94a3ca0230185d467a1`. All 269 non-test Go
and module files were verified byte-identical; their SHA-256 values are retained
in [results.json](results.json). Only opt-in test diagnostics changed.

| Evidence | Frozen source | Actual outcome |
|---|---|---|
| Pipeline privacy/accounting unit | `263bdc1c2585cdce74dd87f92a0bfc9dce0d9ad4` | One endpoint-free race-enabled test passed |
| Six original dense profiles | `263bdc1c2585cdce74dd87f92a0bfc9dce0d9ad4` | Three subprocesses, two cases each; all passed |
| Alternating empty-catalog release probes | `197bc1e6ee5b264c58e16618f8f529bac95b7133` | One subprocess; all 64 probes passed |
| One dense job with release I/O counters | `b0ebc65cb28a4c7c1959eb45175a72f2c3a2f13c` | One subprocess; complete job and all 317 phase releases passed |

Every dense job independently fetched/checked all 20K exact BIGINT/payload rows
and the unrelated 20K-row table. There were no failed, skipped, restarted or
discarded test/profile attempts. Original raw test JSON, controller stages and
hashes are private. Long JSON Output chunks were reassembled per test in their
original order before parsing. No postprocessing recovery or measurement rerun
was needed. No identical-production baseline/candidate timing was performed;
root-owned native controls are separate evidence.

All Go executions used Go 1.27.3, GOMAXPROCS=4. The focused unit used `-race`;
latency/profile runs did not. Host: Apple M5 Max, 18 logical CPUs, 36 GiB,
macOS 26.7.3/arm64, recorded after measurement. Root serialized heavy work; other
light work continued on the shared workstation. This does not prove host idleness.

The owner provisioned Redis Software through `make -C go redis-up`. Immediately
before/after live blocks the verifier checked active/effective 4x QPF, six Search
workers, mnp/32 connections, one 4-GiB noeviction RAM shard and certificate/hostname-
verified data TLS/RESP3. Actual container limits were four CPUs/eight GiB. Actual
Software API version was `8.2.6-25`, Redis `8.8.0`, Search `80808`. The owner retained
a fixed recorded local container image ID throughout; its image reference was
not digest-pinned. Management API trust used the existing self-signed bootstrap.
Operational image/container/network identities and credentials remain private.

## Why the simple reductions were rejected

Manifest/directory/map counts already share a native pipeline. The root-map read
follows the committed-manifest/cardinality proof; fetching it earlier could read
an oversized corrupt map before the existing 256-bucket bound. Bucket entry reads
already share their directory/cardinality/reference proof pipeline. The physical
descriptor key is known only after that entry resolves, and its GET follows an
object reference/existence gate. Pipelining GET with that gate issues it even
when ownership, reference or ACL proof fails. Both proposals were rejected.

Segment FT.INFO and directory-cardinality checks already share a bounded pipeline.
Copy source-directory ranks/pages and physical row images are batched after their
preceding proofs. Copy commit and lost-reply resolution depend on the durable
checkpoint and cannot be sent together. Index-phase FT.INFO and output ZCARD offer
at most one independent call per job. A new Lua proof-plus-GET operation would
change parsing/ACL/error paths for a small hypothesized benefit. Neither warrants
an uncontrolled production change in this pass. Fresh pins, source/schema/OID/
lineage/index readiness, permissions, reference bounds and unknown-outcome handling
remain unchanged.

## Six unchanged dense profiles

Supplemental ordered pipeline-shape counts retain only fixed command categories;
no arguments/keys/values are recorded. The original round-trip categories, workload,
pacing, timing boundaries and full-value checks remain unchanged. Counts supplement
existing measurements; their small observer overhead is inside step/wall timing.

| Repeat | Paced | Wall ms | Copy steps | Copy calls | Copy commands |
|---:|:---:|---:|---:|---:|---:|
| 0 | Yes | 8091.479 | 314 | 4719 | 30682 |
| 0 | No | 8142.153 | 314 | 4718 | 30681 |
| 1 | Yes | 8111.878 | 314 | 4718 | 30681 |
| 1 | No | 8119.751 | 314 | 4718 | 30681 |
| 2 | Yes | 8129.789 | 314 | 4718 | 30681 |
| 2 | No | 8154.652 | 314 | 4718 | 30681 |

Each copy phase contains 314 manifest pipelines (HGET/ZCARD/HLEN), 314 bucket
pipelines (HMGET/HLEN/ZCARD/HGET), 314 object-reference pipelines (HMGET/EXISTS),
and 314 segment-validation pipelines (four FT.INFO/ZCARD pairs). There are 314
source-directory pipelines and 313 image pipelines: 312 of 64 HGETALL commands
and one of 32. Separate root-map reads, descriptor GETs, current-root acquisition,
pin release and maintenance gates remain in the original counts.

The first paced job retained one extra `copy_commit` call. Its 314 calls carried
17,672 script-header bytes, versus 313 calls/12,520 bytes in the other jobs. The
frozen copy body is 5,152 bytes, SHA-1 `a038ad11b981c822a22dcaa20d2b751d19b429a9`.
The totals imply 313 digest headers and one full body. Source/count inference is
consistent with a cold NOSCRIPT fallback: top-level EVALSHA denial would instead
set sticky EVAL-only mode for later batches. The original native refusal message
was not recorded, so this is not an observed-error claim. The extra call remains
in every total; no sample was removed or repeated.

Median wall time was 8.112s paced and 8.142s unpaced. Copy-phase release-hook time
was 4.388s paced (4.294–4.399s) and 6.209s unpaced (6.173–6.216s). Paced jobs also
include their explicit five-millisecond per-step waits. These are current-main
diagnostics, not speedup comparisons, server CPU measurements or isolated
admission-wait estimates. Hook/phase durations must not be added together.

## Separate release attribution

The empty-catalog probe used one empty table, matched manifest validation, 32
fresh pins per arm and alternating order. The direct arm used the same fresh
five-second background context as production heartbeat cleanup. Every pin had
exactly one successful release command and an explicit absent-pin check.

| Empty-catalog arm | Median caller ms | Median hook ms | Median TLS Read ms | Median TLS Write ms |
|---|---:|---:|---:|---:|
| Heartbeat delegated | 0.242 | 0.230 | 0.218 | 0.0056 |
| Direct | 0.231 | 0.229 | 0.222 | 0.0046 |

All 64 had one Read/Write and zero pool misses/timeouts. The 20ms dense delay was
not reproduced. This shows that delegation and the five-second context alone are
insufficient explanations; it does not vindicate all release/transport behavior.

A separate single unpaced dense job retained the original setup/index state,
phase order, 314 copy steps, 4,718 calls/30,681 commands and complete validation.
It added a TLS-level Read/Write duration/count wrapper with NetConn traversal to
the underlying TLS/TCP transport, plus release-hook pool counters. The original
six jobs were not rerun or replaced. Instrumentation is an additional limitation.

This job took 8.169s. All 317 phase releases passed (314 copy, one index, two
install); each had one Read/Write and zero pool misses/timeouts. The first copy
release, before the first output-index creation/copy, took 10.348ms in the hook
and 10.324ms in Read. Remaining 313 releases had median hook 20.073ms
(10.594–22.483ms), Read 20.051ms (10.581–22.454ms), Write 0.0073ms.
Copy-release totals were 6.235s hook, 6.224s Read and 0.00263s Write; Read
accounted for over 99.8% of hook time.

The hook starts inside the heartbeat's deferred release, so preceding goroutine
startup/join waiting is excluded. The test store has no executionTransport hook.
The release Lua returns after TIME/format checks and HDEL, before reader/GC/catalog
enumeration. Read includes reply/network wait and TLS processing: neither
Search CPU, backend queuing nor a TCP mechanism has been established. The first
pre-copy delay also prevents attributing the onset solely to the new output
index's copy work. Retain this lead for a separately justified investigation;
no cleanup removal, stale pin reuse or production default change follows.

## Audit, reproduction and cleanup

Run `python3 benchmarks/table-modes/compaction_proof_audit.py` for all observations,
case counts, pipeline-call/command accounting and arithmetic. For a new owned
verified Software fixture, use the recorded source/toolchain and private URI/CA
configuration with `REDIS_SNAPSHOT_COMPACTION_PROFILE=true` and
`REDIS_SNAPSHOT_COMPACTION_DENSE_PROFILE=true`; execute
`go test -count=1 -p 1 -json -run '^TestSnapshotCompactionPhaseProfile$' -timeout=5m .`
from `go/`, three times, retaining all output. Unit selection is
`^TestSnapshotCompactionProfilePipelineShapes$` with `-race` and Redis environment
unset. Release probes use `REDIS_SNAPSHOT_COMPACTION_RELEASE_PROFILE=true` and
`^TestSnapshotCompactionReleaseProfile$` once. Dense I/O attribution additionally
sets `REDIS_SNAPSHOT_COMPACTION_RELEASE_IO=true` and selects
`^TestSnapshotCompactionPhaseProfile$/^paced_false$` once. Do not relabel a new run
with these source/build hashes, timing values or server settings.

Every loan ended with all subprocesses/clients closed, DBSIZE zero, FT._LIST empty
and effective QPF/TLS/RESP3 reverified. Root owns the shared fixture and final
infrastructure/credential cleanup, which is pending. No child teardown is claimed.
Raw logs, operational identifiers, credentials and license material remain
private. No ClickBench, Flex or alternate provider was run.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
