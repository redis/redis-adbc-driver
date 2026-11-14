# Final selected verification — November 14, 2026

[Intent](intent.md), [spec](spec.md), [plan](plan.md).
[PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

Selected native source `c0d9f1e76716ffc87f3f1176a98527740571d508`, baseline
`31426a96866c4bc12f7f691b3b1de23a6af9dbef`. Clean versioned builds and identical
harness frozen before consecutive timing; post-run hashes match. Later commits
publish evidence/docs only. Metadata proof/read files restore 31426 exactly, with
matching file hashes in final provenance. No prefetch/batching runtime remains.

## Selected checks and results

- Verified Software combined race: **99 PASS / 2 SKIP / 0 FAIL**.
- Unit-only all packages: **660 / 895 / 0**, URIs intentionally unset; not Redis
  compatibility proof. Commands and every skip name retain their actual source.
- Native full matrices: **168 records per build**, all 42 comparisons,
  **5,472 measured operations**, zero result failures/conflicts/retries/discards.
  Complete typed fetch and whole mutation post-state independently validated.
- The earlier experimental source 3f9 passed 319 snapshot tests/62skips and 108 race
  tests/2skips; those results retain 3f9 and are not relabeled as selected code.
  Final selected-head full driver/snapshot/integration CI is tracked in PR #439.

[Selected report](../../../../benchmarks/oltp-query-efficiency/2026-11-14-final/report.md)
retains every raw sample, all 42 p50/p95/throughput comparisons, controls, input/build/
harness/source hashes, actual environment and cleanup. Unmanaged filtered COUNT/
SUM improves 106.645→86.399 ms at 1 client and 184.006→99.741 ms at 4; projections improve
4.655→4.291 ms / 11.602→10.800 ms. These are finite workload observations.

Slower p50 and material p95 cases remain explicit, including managed four-client
COUNT/SUM 10.531→15.537 ms and managed four-client DELETE 56.899→61.188 ms. Managed
fast paths are unchanged by selected follow-ups apart from error handling; this
sequential pair does not attribute all variation to the native-read change. No
point-read speedup is claimed for selected code. Three trials do not establish
confidence bounds, causal attribution or saturated capacity.

## Experiments not selected

[Root-prefetch pilot](../../../../benchmarks/point-root-prefetch/2026-11-14/report.md)
and its [broader matrix](../../../../benchmarks/oltp-query-efficiency/2026-11-14-followup/report.md)
retain their sources and all observations. Point p50 gains coincided with material
tails/mixed-workload losses and control drift; prefetch is omitted conservatively.
The complete six-file patch applies cleanly to 31426. No persisted migration needed.

[Managed batching](../../../../benchmarks/managed-publication-readiness/2026-11-14-native/report.md)
is omitted after mixed native results, including 30.3% higher four-client DELETE
p50 despite throughput/p95 improvements. All 384 Go and 96 native records remain.
Only direct-native safety fixes and real-validator regressions are integrated.

## Review, environment and ownership

Independent final source review found no actionable correctness/security finding.
Native non-NULL HMGET evidence, complete later HGETALL replacement and residual
rechecks preserve live rows; exact key refusal stays local, command refusal may
select scan-local fallback. Every outer/command unknown is inspected before
fallback; both native reads retain NoRetry. Direct managed proof classifiers
cannot rebuild/poll after an uncertain reply. Restricted metadata/lease behavior
is the restored original path. Agent review is not maintainer approval.

Every live command used owned Software created through `make -C go redis-up`,
independently gated active/effective 4× QPF, six workers/mnp32, 4 GiB/noeviction and
certificate-verified TLS/RESP3. Native builds finished before timing; no competing
task-owned tests/builds/heavy work. Actual versions/resources are archived. Unit,
race, offline corruption/complete-pair/pilot audits, formatting/diff checks pass.
Optional fixtures/capabilities skipped are not compatibility evidence. Sparse/
all-NULL wide rows may require a second full read; no such performance gain is
claimed. No new bulk/transfer/ClickBench/Flex/Flight/native Cluster API measurement.

All clients/processes exited before owner-checked clearing and final empty/settings
verification. Owned fixture/network and generated credentials/environment removed;
preexisting containers/networks/TLS files preserved. Operational IDs, credentials,
licenses and raw logs stay private. Earlier CI-fix run 43645143585 passed at 31426;
final evidence-head CI outcome is reported in the PR body.
