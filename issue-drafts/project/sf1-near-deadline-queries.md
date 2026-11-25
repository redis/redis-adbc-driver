Target: redis/redis-adbc-driver — new issue
Title: SF1 queries passing with little margin: TPC-H Q10/Q12/Q13/Q20 (29–38 s) and SSB Q2.1/2.2/3.1/4.1/4.2 (21–32 s) against a 60 s deadline
Labels: enhancement
---
## Problem

In the v0.0.31 release run, several validated queries used half or more of the 60 s statement deadline (`PERFORMANCE.md:73`):

| Query | Redis median | Redis / DuckDB |
|---|---:|---:|
| TPC-H Q10 | 37.7 s | 920× |
| TPC-H Q12 | 30.9 s | 2,497× |
| TPC-H Q13 | 28.8 s | 555× |
| TPC-H Q20 | 38.4 s | 2,382× |
| SSB Q2.1 / Q2.2 | 20.8 / 31.4 s | 2,335× / 2,599× |
| SSB Q3.1 | 29.4 s | 2,635× |
| SSB Q4.1 / Q4.2 | 32.1 / 27.7 s | 2,925× / 1,953× |

Sources: `PERFORMANCE.md:176-186` (TPC-H, Software RAM, 8 GiB client) and `:150-158` (SSB, Software RAM, 12 GiB client per `:58`).

None of these has an issue, because they passed. No later change record re-measures them, and none mentions SSB. The star and snowflake join work since v0.0.31 (#635, #643, #644 and the parallel star readers) may have changed their times; that is unverified.

## Desired behavior

Re-measure these queries on current main. For any still above about 30 s, record the plan and worker grant, identify the dominant cost, and either fix it or file a focused issue per query.

## How to verify

Run each query on its own against an already loaded fixture (export the SQL first):

```sh
cd benchmarks/olap-release
python run.py tpch --private /private --output /work/results-tpch-q10 --prepare-only
python run.py tpch --private /private --output /work/results-tpch-q10 --reuse-loaded --query 10
python report.py --input /work/results-tpch-q10/tpch --output /work/results-tpch-q10/Q10.md
python run.py ssb --private /private --output /work/results-ssb-q2.1 --prepare-only
python run.py ssb --private /private --output /work/results-ssb-q2.1 --reuse-loaded --query 2.1
```

Add `adbc.redis.execution_diagnostics_file` (`docs/execution-diagnostics.md:3-12`) to the private connection options to capture each plan.

Success: each query is validated, with its median and plan recorded on current main.
