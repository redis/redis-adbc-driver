# Verification and final review — November 14, 2026

[Intent](intent.md), [spec](spec.md), [plan](plan.md).
[PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Final selected production source `3f9cf6a85a2cf4b7744f785bca8517b3ff18859c`;
baseline `31426a96866c4bc12f7f691b3b1de23a6af9dbef` is the first PR candidate
plus its test-only CI correction. Both clean versioned libraries/CLIs and the
identical harness were frozen before timing. Post-run binary hashes match. Later
commits add evidence/docs only; they do not relabel the measured source.

## Checks

| Check at final source | PASS | SKIP | FAIL |
|---|---:|---:|---:|
| Verified Software combined race selection | 108 | 2 | 0 |
| Unit-only all Go packages, URIs intentionally unset | 668 | 896 | 0 |
| Separate full Software snapshot suite | 319 | 62 | 0 |

[Checks](../../../../benchmarks/oltp-query-efficiency/2026-11-14-followup/checks.json)
retain actual commands, full source IDs, counts, wrapper timing and every skip
name. URI-unset checks are unit evidence, not Redis compatibility. Optional
operator/backup/topology/profile and capability skips are not compatibility proof.
Final focused race includes the later restricted/queue and per-key ACL-selector
guards, temporal row changes, exact bytes/int64/NULL, real publication refusal
regressions, ownership, pin/schema freshness, unknown replies and no replay.

All live commands used an owned Redis Software fixture created via
`make -C go redis-up`, independently gated for active/effective 4× QPF, six workers,
mnp/32, 4-GiB noeviction, strict data TLS and RESP3. Shared live slots were exclusive;
all builds/tests completed before the final consecutive native matrices. Native
runtime was uncapped, with Go runtime tuning unset and telemetry explicitly off
for both builds. Selected versions/resources are in the archived environment.

Both full native matrices validated **168 records** each, all **42 combinations**
and **5,472 measured operations**. No failed result, conflict, retry or discarded
native sample. Complete typed fetch and independent whole mutation-state checks
are required by the unchanged harness. Every JSON/CSV comparison, p50/p95,
throughput, source/input/harness/build hash and cleanup proof passes the offline
full-pair audit. Separate point, unmanaged and unselected managed archives/audits
retain complete actual-source measurements; offline corruption tests pass.
Formatting and `git diff --check` pass. No new ClickBench, bulk/transfer, Flex,
Flight SQL or native Cluster API measurement is claimed.

## Actual outcomes and selection

The [complete report](../../../../benchmarks/oltp-query-efficiency/2026-11-14-followup/report.md)
retains all cases. Unmanaged filtered COUNT/SUM p50 improves 107.430→82.651 ms
with one client and 192.297→81.154 ms with four (1.30×/2.37×). Point latency
falls about 5–9%. Managed four-client INSERT improves 56.370→43.792 ms.

Slower cases remain explicit: managed four-client projection 16.489→21.422 ms
(+29.9%); managed one-client filtered COUNT/SUM 3.955→4.331 ms (+9.5%); managed
one-client UPDATE 45.377→47.878 ms (+5.5%). Small flat/slower cases are retained
as well. Three finite measured trials do not establish cause or confidence bounds.
Do not multiply these ratios with the earlier main→first-candidate comparison.

The managed directory/readiness batching experiment is **not selected**. Native
pilots showed mixed results, including 30.3% higher four-client DELETE median
latency despite higher throughput and lower p95. All positive/negative Go and
native evidence remains archived; an experiment patch permits reproduction.
Production retains standalone ZCARD and FT.INFO, with direct-native failure guards.

## Independent review and cleanup

Subagents independently reviewed the selected final source: no actionable
correctness/security finding. Native HMGET non-NULL evidence and whole later
HGETALL replacement preserve live source semantics; per-key refusals cannot force
fallback for unrelated later keys. Unknown/composed outcomes are checked before
any fallback. Root-map prefetch is atomic/bounded, authorization checked, per-pin
only, skipped for restricted/shared readers and discarded before execution on a
local queue error. Managed unknown proof outcomes cannot create replacement
indexes or select polling. Original publication/lease/ownership proofs remain.
This is agent review, not maintainer approval. Sparse/all-NULL wide-row costs and
unavailable server CPU attribution remain explicit limitations.

All Go/native processes and clients exited before owner-checked data/index
clearing. Final empty/QPF/TLS/RESP3 verification passed before teardown.
[Cleanup](../../../../benchmarks/oltp-query-efficiency/2026-11-14-followup/cleanup.json)
confirms owned container/network and generated credentials/environment removed;
preexisting containers/networks and TLS files were preserved. Private operational
artifacts, identifiers, credentials/licenses and raw logs are excluded.

The [original CI-fix run](https://github.com/redis/redis-adbc-driver/actions/runs/43645143585)
passed at 31426a96 before these follow-ups. A pre-evidence CI attempt was canceled
intentionally to avoid duplicate work. Final-evidence-head CI is tracked in the
PR body; the earlier green run is not relabeled as validation of later code.

## Selection follow-up

Root-map prefetch is not selected after broader native evidence showed material
tail and mixed-workload regressions plus control drift. This is a conservative
selection decision, not proof of causality. Metadata proof/read Go files restore
31426 exactly; the six-file experiment patch applies cleanly to that baseline.
Earlier measurements retain their actual sources. A fresh selected-source native
comparison and final checks supersede experimental-source performance claims.
