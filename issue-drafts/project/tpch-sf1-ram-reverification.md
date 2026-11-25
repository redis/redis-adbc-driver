Target: redis/redis-adbc-driver — new issue
Title: TPC-H SF1 on Software RAM: re-verify Q1–Q22 on current main after the subset-based closures of #578–#587 and #599
Labels: enhancement, needs-triage
---
## Problem

#578–#585 and #587 were closed on 10% TPC-H subset results (150,000 orders / 600,093 lineitems). Each closing comment says the fix was "**not** re-verified at SF1 on this issue's Software RAM profile". #599 (Q19 through DuckDB over ADBC) closed the same way: "The full SF1 third-path A/B on the RAM profile was not run." Only #586 (direct Q19) closed on an SF1 run, with a 3.76 s median (`benchmarks/olap-release/Q19-FOLLOWUP.md:33-43`).

The change records asked for SF1 before closing:
- `docs/sdlc/changes/tpch-decimal-partitioned-sums/validation.md:42-43` (#578/#585)
- `docs/sdlc/changes/tpch-semijoin-page-probes/validation.md:40-43` and `docs/sdlc/changes/tpch-semijoin-parallel-probes/validation.md:70-71` (#580/#587)
- `docs/sdlc/changes/tpch-snowflake-joins/validation.md:71` (#579, #581–#584)
- `docs/sdlc/changes/snapshot-candidate-windows/validation.md:55` (#599)

`PERFORMANCE.md:324` adds: "no full TPC-H rerun was completed."

SF1 risks that have not been measured:
- Q1 peak RSS rose from 127 MB to 890 MB on the subset (`docs/sdlc/changes/tpch-decimal-partitioned-sums/validation.md:41`).
- Snowflake joins fall back to the eager join above 4,000,000 retained branch rows (`docs/sdlc/changes/tpch-snowflake-joins/spec.md:24`). #584's closing comment estimates about 2.3M for Q9 at SF1.
- SF1 has about ten times the semi-join probe keys (`docs/sdlc/changes/tpch-semijoin-page-probes/validation.md:40-42`).
- The last SF1 third-path Q19 run predates #637, and it timed out (`benchmarks/olap-release/Q19-FOLLOWUP.md:21,43`).

## Desired behavior

All 22 queries run on one current-main build with the historical profile (`PERFORMANCE.md:45-60,71-80`):
- Software RAM, one shard, verified 4× QPF, TLS
- 60 s statement deadline
- a 4-CPU / 8 GiB Linux client in the server's network namespace

## How to verify

Full suites need explicit authorization (`benchmarks/olap-release/README.md:3`, `PERFORMANCE.md:139`). Once authorized, with SF1 already loaded:

```sh
cd benchmarks/olap-release
python supervise.py tpch --private /private --output /work/results-tpch-<date> --reuse-loaded --include-duckdb-adbc
python report.py --input /work/results-tpch-<date>/tpch --output /work/results-tpch-<date>/TPCH.md
```

Success: every query has a validated warm-up and three timed runs under 60 s, with a client OOM-kill delta of 0. Reopen the matching issue for any query that misses.
