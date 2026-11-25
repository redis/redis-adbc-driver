Target: redis/redis-adbc-driver — new issue
Title: TPC-DS SF1: run Q1–Q99 on one current-main build (Q10 was interrupted, Q11–Q99 never ran on Redis)
Labels: enhancement, needs-triage
---
## Problem

The only TPC-DS SF1 run on Redis (v0.0.31 release driver, Software Flex) stopped early: "Q10 was interrupted by the stop request; Q11–Q99 were not run on Redis" (`PERFORMANCE.md:7-10,28,218`). 89 queries have never run against the driver.

The eight failing cases (#588–#595) were later validated, but on different combined candidate builds (`docs/performance/tpcds-focused-followup-2026-11-23.md:5,39`). That document says "there was no single-build all-eight rerun or full 99-query benchmark" (`:104`, `:186`).

The candidate source commits recorded there (for example Candidate 6 `119633325`, Candidate 12 `a494bdc6`) are not ancestors of `main`, checked with `git merge-base --is-ancestor`. The code reached main through later PRs, so current main has never been measured on these queries. Other caveats:
- The Q6 warm-up took 60.9 s end to end, and the Q2/Q9 warm-ups were near the limit (`:22`).
- Q4 passed only on a separate 16-CPU client profile (`:15`, `:60`).

## Desired behavior

One SF1 pass of all 99 queries on a single current-main build, using the recorded Flex profile (`PERFORMANCE.md:45-67`, `docs/performance/tpcds-focused-followup-2026-11-23.md:167`). The RAM setup was refused for insufficient memory (`PERFORMANCE.md:94-98`). Each failing query gets its own issue, as with #588–#595. #627 (Q4 through DuckDB over ADBC) stays separate.

## How to verify

After explicit authorization (full suites are stopped: `benchmarks/olap-release/README.md:3`), with the retained, count-verified SF1 fixture:

```sh
cd benchmarks/olap-release
python supervise.py tpcds --private /private --output /work/results-tpcds-<date> --reuse-loaded
python report.py --input /work/results-tpcds-<date>/tpcds --output /work/results-tpcds-<date>/TPCDS.md
```

Success: 99 query records from one build identity. Each is either validated (warm-up plus three timed runs under 60 s) or linked to a filed issue.
