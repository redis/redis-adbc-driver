# Validation and review

Date: November 13, 2026 (America/Los_Angeles). Author/reviewer: Codex; requestor:
repository user. [Intent](intent.md), [spec](spec.md), [plan](plan.md),
[PR #436](https://github.com/redis/redis-adbc-driver/pull/436). Human review pending.

- `make -C go redis-up` completed for an owned isolated Software fixture. Actual
  Software 8.2.6-25 / Redis 8.8.0 / Search 80808; active/effective 4x QPF, six
  workers, mnp/32, 4-GiB noeviction shard, strict data TLS/RESP3 and actual
  4-CPU/8-GiB container limits verified. Client is Apple M5 Max/macOS arm64,
  uncapped, with native GOMAXPROCS unset.
- Reused exact ca685806 versioned library/CLI from the prior 156-record run.
  The small pilot completed all 84 records (64 point rows/10K query rows,
  four operations/queries, warmup plus one measured round). Standard execution
  completed all 168 records (1K point/100K query rows, 32 point operations/eight
  queries, warmup plus three measured rounds). Every result/type/value/count and
  complete mutation post-state independently validated; no conflicts or retries.
- Measured harness commit `cebdc45f` and its actual SHA256 remain in provenance
  and raw metadata. Later failure-retention guards and exporter checks are
  separately unit-tested. Per-operation action/query/timer bodies did not change
  in that follow-up; historical observations are not relabeled as a new build.
- `python3 benchmarks/table-modes/audit_scenario_charts.py`: PASS, including
  168 standard +84 pilot records, reuse of 156 prior records, exact regeneration
  of 84 current chart rows/CSV, separate 44-value historical product and eight-value
  paired aggregate series, input hashes, settings/provenance and cleanup proof.
- `python3 benchmarks/table-modes/merged_scoped_audit.py`: PASS. Its historical
  defaults remain pinned; optional explicit identities allow the same full-matrix
  arithmetic audit to support future compatible driver builds.
- `python -m unittest discover -s benchmarks/table-modes -p 'test_*.py'`: 13 PASS.
  Tests cover independent integer oracles, mutation-key uniqueness, cursor cleanup
  without replay, failed-worker barrier release, failure-sample retention,
  chart coverage/arithmetic/build compatibility, and historical series separation.
- Worker/client processes ended before owner-checked data/index cleanup and a
  final QPF/TLS/RESP3 gate. Owned container/network and generated credentials/TLS
  removed; prior container and 31 prior networks preserved. Credential scan:
  zero generated-secret matches. Public evidence excludes operational identities,
  endpoints, credentials and private logs.
- Native deck: slide 4 now has representative absolute timings and units; slide
  19 has measured point-read/INSERT/UPDATE/DELETE p50 and numeric client multiples;
  slide 20 has numeric HSET ratios; slide 21 has filter/group timing. The user
  explicitly authorized removal of former slides 26–31; their evidence links
  remain in notes. Final 29-slide export readback verified complete text, tables,
  notes, order, geometry and unchanged historical ClickBench data. All 29 pages
  visually checked; no fit defects. Issue checker: zero errors, two unchanged
  inventory-caption font warnings retained as template exceptions.

Review: correctness/measurement boundaries, complete evidence retention,
resource ownership/security, and actual claims versus source/settings checked.
No production Go behavior changed. These are finite synthetic/workload-specific
samples, not SLA/capacity evidence or a new product ranking. No new 100M, Flex or
Flight SQL benchmark ran. No actionable findings remain in this change; agent
review is not human approval.
