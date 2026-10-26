# Plan and evidence

2026-10-21; issue #380; see [intent](intent.md), [spec](spec.md).

1. Add conservative bounded predicate inference and narrow planning spans.
2. Add a HASH input source to the shared iterator/Arrow reader and direct
   partition selection after descriptor/target validation.
3. Exercise exact multiset/schema parity, unknown cases, bounds/budgets,
   permissions, cancellation, ownership, stale targets and live writes.
4. Test native typed bindings/decimal/binary/NULL values after planner close;
   compare preserved #385 native library to the new library on identical
   supplemental ClickBench-source predicates and record measurements.
5. Run full standalone/Cluster Go suites and native validation; open a PR.

Initial regression checks found an existing ordinary equality shortcut rounded
__rowid=2.1 into key 2 and removed the residual, returning a false positive.
Restrict that shortcut to integer types; fractional values retain normal
numeric Search/residual handling. The new parity regression failed before and
passes after the fix. Existing HASH NOPERM and mid-stream stale-prefix errors
remain IO errors, consistent with the shared reader's existing contract.

## Verification

- Full standalone Go suite (localhost:17479): all three packages passed;
  main 154.083s, CLI 1.985s, benchproxy 0.237s.
- Full three-primary OSS Cluster Go suite (localhost:17501): all three passed;
  main 159.755s, CLI 1.290s, benchproxy 0.178s.
- Focused Cluster race checks for predicate inference, pruned readers,
  permissions and balanced planning passed (4.418s).
- Native build passed. Full C/Python ADBC validation with the real server cap
  lowered to 137: 371 passed, 8 existing feature skips, 3 subtests (12.71s).
  Required partition collection gate: all 9 cases passed, including decimal,
  binary, embedded NUL, typed binding, closed-planner and direct cancellation.
- Python compile and diff checks passed. No test assertions or skip lists were
  relaxed; HASH permission/stale-read errors are asserted against the existing
  IO contract, including NOPERM/stale diagnostic text.

See [dated measurements](../../../../benchmarks/partitions/pruned-2026-10-21/README.md)
for descriptor/command counts, planning/full-fetch timings, RSS observations,
raw repeated exact-result checks, versions/hashes and limitations. Sparse range
HASH work can grow despite improved elapsed time; no universal speed/memory
claim. No paid resources provisioned.
