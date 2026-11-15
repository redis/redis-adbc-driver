# Unmanaged projection and current CRUD performance

**No general CRUD latency improvement was demonstrated.** This change reuses the existing exact live projection reader for eligible unmanaged known-key row retrieval, so ordinary non-NULL reads omit registered-but-unselected and unregistered HASH fields. The independent command tests prove HMGET selects only required fields. This is a data-transfer reduction, not a demonstrated fix for the roughly 2-ms point-read latency. Managed execution and write/publication protocols are unchanged.

The full comparison retained **1,184 validated trials**: 672 ordinary CRUD/query trials and 512 dedicated width trials, including 296 warmups and 888 measured trials. All 30,976 timed operations, complete typed outputs and mutation/source-state checks succeeded. No failed window or sample was discarded. The separate rejected metadata-hint screen has another 96 records and must not be pooled with these results.

## Ordinary CRUD and query results

The table shows pooled one-client medians and p95, in milliseconds, for the complete four-column/1K-row point workload. Positive “less elapsed” means a smaller median; negative means slower. Each build/case pools 192 measured operation observations across two windows. These are finite observations, not confidence intervals or production-capacity estimates.

| Operation | Mode | P50 baseline → candidate, ms | P95 baseline → candidate, ms | P50 less elapsed |
|---|---|---:|---:|---:|
| delete | managed | 6.2360 → 5.9425 | 41.7261 → 39.1390 | 4.71% |
| delete | redis_py | 0.0416 → 0.0416 | 0.1079 → 0.1148 | 0.15% |
| delete | unmanaged | 2.2458 → 2.2701 | 3.8142 → 3.4910 | -1.08% |
| insert | managed | 5.8048 → 5.7686 | 40.3083 → 31.3746 | 0.62% |
| insert | redis_py | 0.0457 → 0.0472 | 0.1180 → 0.1690 | -3.10% |
| insert | unmanaged | 2.2572 → 2.2760 | 3.1066 → 3.3425 | -0.83% |
| point_read | managed | 1.9984 → 2.0485 | 2.5780 → 3.5658 | -2.51% |
| point_read | redis_py | 0.0540 → 0.0545 | 0.1193 → 0.1700 | -0.97% |
| point_read | unmanaged | 2.0720 → 2.0547 | 3.5497 → 3.4978 | 0.83% |
| update | managed | 6.4975 → 6.3115 | 33.2178 → 32.8378 | 2.86% |
| update | redis_py | 0.0439 → 0.0446 | 0.0794 → 0.1296 | -1.61% |
| update | unmanaged | 2.2569 → 2.2534 | 3.7768 → 3.7685 | 0.15% |

One-client point reads are 2.0485 ms managed and 2.0547 ms unmanaged versus 0.0545 ms redis-py in the candidate windows. Managed mutations remain about 5.8–6.3 ms at p50 and unmanaged mutations about 2.3 ms. The original shared-read target of at least 10% less elapsed time in **both** driver modes remains **not met**. The projection was selected for excluding unnecessary values, not because it passed that shared-path latency target.

All 42 pooled cases and both complete forward/reverse pairs, including the 100K-row filtered/grouped analytical queries and four-client results, are in the [complete ordinary comparison](standard-comparison/report.md), [full-precision JSON](standard-comparison/comparison.json) and [CSV](standard-comparison/comparison.csv). Analytical execution is unchanged by this known-key branch. Redis-client workloads have different consistency/publication costs and are useful controls, not equivalent managed transactions.

Unfavorable results remain visible: four-client unmanaged point-read p50 rose 5.1368→5.4521 ms (+6.14%), four-client unmanaged UPDATE p50 rose 10.97%, and four-client managed INSERT p95 rose 196.8607→281.6880 ms (+43.09%). The managed production path is identical between builds; these observations still limit acceptance claims. Controls also vary, including four-client redis-py DELETE p50 falling 16.43%. No drift correction or winner-only subset is used. These windows do not establish the cause of individual changes or demonstrate absence of regressions.

## Dedicated record-width control

Each wide payload is 64 KiB and is not selected. “Registered” adds one declared public field; “unregistered” adds a source-only field after registration; “both” contains both. All widths use the same membership-index definition. Four selected columns include exact int64 values above 2^53 and an actual NUL in text. The complete source of every one of the 1,000 HASHes is independently validated before and after each layout.

| Extra payload | Selected columns | Clients | Unmanaged P50 baseline → candidate, ms | P95 baseline → candidate, ms | P50 less elapsed |
|---|---:|---:|---:|---:|---:|
| both | 1 | 1 | 2.0019 → 1.9766 | 2.9288 → 2.6761 | 1.26% |
| both | 1 | 4 | 4.9278 → 5.2584 | 9.2723 → 9.3030 | -6.71% |
| both | 4 | 1 | 2.0337 → 2.0139 | 2.7867 → 2.7269 | 0.98% |
| both | 4 | 4 | 4.8976 → 5.2908 | 8.3927 → 9.2085 | -8.03% |
| none | 1 | 1 | 1.9696 → 1.9620 | 2.9734 → 3.0065 | 0.39% |
| none | 1 | 4 | 5.0980 → 5.0341 | 8.7693 → 8.9301 | 1.25% |
| none | 4 | 1 | 1.9707 → 2.0466 | 2.6131 → 3.0006 | -3.85% |
| none | 4 | 4 | 4.7854 → 5.1511 | 9.2157 → 9.3692 | -7.64% |
| registered | 1 | 1 | 1.9792 → 1.9408 | 3.5612 → 2.8124 | 1.94% |
| registered | 1 | 4 | 5.2259 → 4.6421 | 9.0038 → 9.8307 | 11.17% |
| registered | 4 | 1 | 1.9901 → 1.9829 | 2.6943 → 2.6766 | 0.37% |
| registered | 4 | 4 | 4.8339 → 4.9138 | 9.8105 → 8.7660 | -1.65% |
| unregistered | 1 | 1 | 1.9843 → 1.9344 | 3.2609 → 3.0904 | 2.51% |
| unregistered | 1 | 4 | 4.6840 → 4.9358 | 8.7715 → 9.1215 | -5.38% |
| unregistered | 4 | 1 | 2.0026 → 1.9831 | 2.9071 → 2.8007 | 0.98% |
| unregistered | 4 | 4 | 5.1118 → 4.9997 | 9.4754 → 8.6730 | 2.19% |

There is no consistent material width-latency win. For one client with a wide payload, pooled p50 reductions range only 0.37–2.51%; four-client results are mixed. Both-wide/four-column/four-client p50 is 8.03% slower and p95 rises, while its raw Redis control p50 is also 12.43% slower. Both results and pair directions are retained without interpreting one as a causal normalization of the other. The isolated 11.17% registered/one-column/four-client median reduction is not a general speedup claim.

The [complete width comparison](width-comparison/report.md), [JSON](width-comparison/comparison.json) and [CSV](width-comparison/comparison.csv) retain all 32 pooled cases, 64 pair cases and 16 pooled raw Redis controls. Byte oracles quantify exact source/selected **values**, excluding field names, RESP and TLS. They are not packet or measured wire-byte counters. Command-selection tests separately verify the actual HMGET fields. All-selected-NULL/missing rows and HGETALL-only permissions can require an extra HMGET/fallback and full HASH transfer; those latency cases are not timed here. Foreign/filter and Flex paths retain their original eligibility/membership behavior.

## What the investigation established

- The separate [Go command and native cursor diagnostics](../diagnostics/README.md) retain 396 Go operations and two complete 1,536-operation native runs. Managed/unmanaged reads observed 10 foreground client-hook calls and 19/18 commands, versus one raw HMGET. Hooks are not packet RTTs or exclusive server CPU.
- Native nested observations put previous-result close near 0.4–0.5 ms and actual ADBC ExecuteQuery near 1.5 ms on point reads. Nested spans overlap; independently pooled Go/native medians cannot be subtracted into a causal decomposition. Existing release-time GC remains unchanged because safe Arrow/foreign-buffer lifetime changes were not proved by this work.
- Installed driver-manager 1.13.0 does not add implicit ExecuteSchema/GetParameterSchema to unbound literal CRUD. Prepare is a small measured span, and existing same-SQL parsing reuse already exists. No new plan cache or prepared-query performance claim is selected.
- The [descriptor routing-hint experiment](../../metadata-hint-experiment/README.md) missed the predeclared 10% gate: 9.44% and 7.11% less one-client p50 in managed/unmanaged modes. Its bounded fresh-proof/ACL implementation, failures, corrections, complete screen and unapplied patch are retained. It adds no production cache, option or storage-protocol change.

Further general CRUD improvement needs an independently bounded metadata/publication or native result-lifetime change with its own correctness and latency gates. Rewriting SQL parsing or switching languages is not supported as the next fix by these measurements.

## Exact builds, environment and checks

Baseline: `3b42245402a2d7cf6f363faf7ac713cd36621f90`, version `v0.1.0-dev.crud-refactor-base`. Measured projection candidate: `a944c99c500f2f5904ff669bea5cb9fb9b6b8e39`, version `v0.1.0-dev.crud-projection-final`. Later DML-RETURNING and observer-only tests/docs do not relabel the measured native builds. Production Go code in the final PR remains equivalent to the measured candidate.

[Frozen identities](frozen-builds.json) include library, CLI and all executed harness/helper hashes and were checked before/after every job. [Window status](window-status.json) records actual ordered execution: A1 standard/width, B1 standard/width, B2 standard/width, A2 standard/width. The measured width harness predates a comment-only FT.INFO clarification; its original hash is explicitly pinned by the comparator. All eight jobs were serialized, with no concurrent builds/tests/profiles. Each exact private owner marker was verified before resetting the owned fixture between jobs.

Owned Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808, one 4-GiB noeviction RAM shard, active/effective **4× QPF**, six Search workers and mnp/32. Server container 4 CPUs/8 GiB; client 4 CPUs/4 GiB. Native Linux/arm64 clients shared the server network namespace with trusted localhost TLS/RESP3, avoiding the macOS published-port route. Apple M5 Max host, 18 logical CPUs/36 GiB; Docker VM 18 CPUs. Exact runtime/resource facts are in [environment](environment.json), [policy gate](server-gate.json), packages in every result. Persistent clients/cursors are outside per-operation timings; lazy first transport work is included. No caller replay; declarations do not measure internal wire retry/reconnect events.

- [Final selected-source checks](checks.json): ordinary Software suite 2,630 pass events, 694 skips, zero failures across ten packages; disposable snapshot suites disabled there and exercised separately for changed unmanaged behavior. Focused live unmanaged race suite: 254 pass events (91 top-level tests), six opt-in diagnostics skipped, zero failures.
- [Test-only DML RETURNING follow-up](returning-check.json): three race pass events (parent plus both stream modes), zero skips/failures. Exact >2^53/NUL typed results, required SET/RETURNING fields, source preservation and complete-key DELETE checked.
- [Observer-only correction](observer-check.json): two endpoint-free race tests passed; known command wrappers now contribute supported returned-value bytes. Historical observer hashes/counters remain unchanged. Python auditors: 14 CRUD tests and nine width audit/comparison tests passed; diagnostic audit reproduces all published summaries from retained raw inputs.
- Independent subagent source review found no actionable correctness/lifecycle/security/ownership defect. This is agent review, not human approval. Native ACL experiment failures/successes belong to the rejected prototype, not a newly claimed shipping ACL benchmark.
- [Cleanup](cleanup.json): all timed clients ended, exact markers checked, database empty/index-free and trusted TLS probe passed; owned fixture/network/client tag removed, generated credentials/owner files removed, preexisting containers/images preserved and 29 inventoried networks unchanged. One unrelated, previously empty network was absent at final inspection and was not named by the owned teardown; its cause/timing is unverified. The initial post-teardown inventory assertion and this limitation are retained. [Temporary ACL cleanup](temporary-acl-cleanup.json) completed before final timing windows. No ClickBench or cloud deployment was run.

## Reproduce the offline audits

From the repository root (fresh output directories):

```sh
python3 benchmarks/crud-execution/compare.py \
  --a1 benchmarks/crud-execution/results/2026-11-15/standard-A1.json \
  --b1 benchmarks/crud-execution/results/2026-11-15/standard-B1.json \
  --b2 benchmarks/crud-execution/results/2026-11-15/standard-B2.json \
  --a2 benchmarks/crud-execution/results/2026-11-15/standard-A2.json \
  --candidate-source a944c99c500f2f5904ff669bea5cb9fb9b6b8e39 --out /tmp/new-crud-audit
python3 benchmarks/unmanaged-point-projection/compare.py \
  --a1 benchmarks/crud-execution/results/2026-11-15/width-A1.json \
  --b1 benchmarks/crud-execution/results/2026-11-15/width-B1.json \
  --b2 benchmarks/crud-execution/results/2026-11-15/width-B2.json \
  --a2 benchmarks/crud-execution/results/2026-11-15/width-A2.json \
  --harness-sha256 a2fb987ce380a9158cdeb550ddf7cd69000b52908aed1433b044b6a54e397d23 \
  --out /tmp/new-width-audit
python3 benchmarks/crud-execution/results/diagnostics/audit.py
```

Comparison input/result digests are retained. Live reruns require a newly owned Software fixture and verified settings, not these deleted credentials or raw operational artifacts. See the [change intent](../../../../docs/sdlc/changes/crud-execution-refactor-20261115/intent.md), [spec](../../../../docs/sdlc/changes/crud-execution-refactor-20261115/spec.md) and [plan](../../../../docs/sdlc/changes/crud-execution-refactor-20261115/plan.md).
