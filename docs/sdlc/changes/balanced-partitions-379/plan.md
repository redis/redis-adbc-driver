# Plan and evidence

2026-10-21; issue #379; baseline `d7346f9`; see [intent](intent.md), [spec](spec.md).

1. Add bounded quantile split planning and explicit strategy option/reset.
2. Test dense/sparse/skewed/empty coverage, budgets, errors and independent reads.
3. Test native string option handling and exact per-partition rows.
4. Compare strategies on the official ClickBench prefix using existing correct
   process/timing/schema/value boundaries, then open a separate stacked PR.

Focused Go tests passed. On a tail fixture with 25,000 allocated IDs and 2,500
live rows, four balanced descriptors each return 625 exact rows; default equal
widths concentrate all 2,500 in the last range. Further outcomes pending.
No paid resources; reuse the task-owned local Redis from #377.

## Benchmark evidence

See [the dated report](../../../../benchmarks/partitions/balanced-2026-10-21/README.md).
All 30 final comparison trials passed exact source values and schemas.
Eight equal-width descriptors returned 0,0,0,0,0,0,0,10000 rows; balanced returned
1250 each. Four-worker wide reads improved 1.2639s to 0.8754s versus current
partitioning, but ExecuteQuery remained faster (0.8191s). Narrow reads were
nearly unchanged; keep the option off by default. Initial correct binary probes
were too expensive; reuse/interpolation reduced count overhead.
Two runs were interrupted by external Docker SIGTERM, not code failures or OOM.
The final run used a separately named task-owned container on port 17479.
This is local supplemental ClickBench-source evidence, not a production ranking.

## Final local verification

- Full Go integration on task-owned standalone localhost:17479: all three
  packages passed (main 142.911s; CLI 0.877s; benchproxy 0.240s).
- Full Go integration on task-owned three-primary OSS Cluster localhost:17501:
  all three packages passed (main 160.850s; CLI 1.293s; benchproxy 0.184s).
- Native build passed. Full Python ADBC validation with actual cap 137:
  370 passed, 8 existing feature skips, 3 subtests, 12.16s. All 8 partition
  cases passed, including the new native strategy/row-distribution test.
- Python harness compilation and diff checks passed. The benchmark source was
  clean; raw repetitions, versions, hashes and measurement limits are preserved.

The benchmark host was not exclusive; do not compare its absolute timings to
older environments. Both strategies used the same fresh worker/full-fetch/IPC
timing boundaries with planning included and exact source/schema checks.
