# Incremental-read benchmark plan and verification

2026-10-22. See [intent](intent.md), [spec](spec.md), and [validation](validation.json).

1. Reuse the independently typed ClickBench fixture and native bounded-fetch
   helpers; create the isolated Redis 8.6.2/Search server with four CPUs/2 GiB.
2. Add matching output batch sizes and common first-batch/IPC timing boundaries;
   compare public partitions with eight explicit SQL ranges on four readers.
3. Run all 14 cases as smoke checks, then one warmup and six balanced measured
   rounds per case. Preserve exact-value validation and every raw trial.
4. Review results, reproduce medians and artifact hashes, document limits,
   remove the task-owned container, and publish a focused PR.

Complete: 14 smoke executions and 98 canonical executions pass exact full-value
and multiplicity validation. At 4,096 rows/batch, explicit ADBC ranges reduce
four-reader median elapsed time 10.8% (1.778 to 1.586 seconds), close to native
Search + HMGET at 1.561 seconds with overlapping ranges. No ADBC first-batch
advantage is demonstrated. One-reader full transfer remains faster for ADBC.

Changed files: `benchmarks/incremental-read/run.py`, its dated evidence/report,
a link in `benchmarks/wide-transfer/README.md`, and these change records.
The runner bytes exactly match the canonical measured runner hash. The shared
fixture/native helper is unchanged between measurement and publication bases.
Syntax, CLI import/help, local evidence links, medians and checksums are checked
for publication. Production Go/native suites were not rerun for this change.

Review performed by Codex using the three REVIEW.md passes: complete-output
validation and stream cleanup inspected; the dedicated fixture's ownership and
sanitized environment metadata checked; all numerical claims reconciled with
raw results. No actionable finding remains in scope. This is not independent
human approval. CI and maintainer review remain pending; no merge requested.
Publication: [PR #451](https://github.com/alberttwong/redis-adbc-driver/pull/451).
