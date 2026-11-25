Target: redis/redis-adbc-driver — new issue
Title: Semi-joins: choose page probes or the whole decorrelated build by cost, and explain a 70.9 s Q21 outlier (low priority)
Labels: enhancement
---
## Problem

These are two low-priority follow-ups from the TPC-H Q4/Q21 semi-join work (#630, #632, #633).

**1. Choosing a strategy.** A decorrelated EXISTS / NOT EXISTS probes its inner side in 256-key batches for each outer page. The whole decorrelated build is used only in three cases (`docs/sdlc/changes/flex-semijoin-whole-build/spec.md:5-7`):
- the inner side is already in memory;
- the inner side is an overlay that cannot use Search candidates;
- the query runs under Flex (since #640).

On the subset, the whole build beats probing above about 9,000 / 35,000 / 73,000 distinct keys with 1 / 4 / 8 workers (`docs/sdlc/changes/tpch-semijoin-parallel-probes/validation.md:60-68`). `plan.md:18-20` lists "a cost-based switch to the whole decorrelated build for in-memory outer sides, whose distinct keys are all known before probing" as a possible follow-up. Q4 and Q21 fall on the probe side of that crossover, so this matters only for other query shapes.

**2. An unexplained outlier.** One parallel-probe Q21 run took 70.9 s, against 1.8–5.7 s in 16 other runs. It was the first repetition of an in-process series, its work counts matched the other runs, and "its cause was not identified" (`docs/sdlc/changes/tpch-semijoin-parallel-probes/validation.md:41-46`). Q4 showed a smaller first-run outlier: 1.35 s against 0.36–0.57 s.

Not verified: whether the outlier recurs on current main or at SF1.

## Desired behavior

- When an in-memory outer side's distinct key count is known and exceeds the measured crossover for the granted workers, build the inner side once. Results and error behavior stay unchanged.
- Diagnose the first-run outlier (for example cold connections, index readiness, or GC), or show that it no longer recurs.

## How to verify

- `TestSQLSemiJoinParallelProbes` and the semi-join/subquery selection pass under `-race`, run through `cd go && bash software-setup.sh --exec go test -race …`.
- A new test asserts which strategy is chosen on each side of the threshold.
- Repeated fresh-process and in-process subset Q21 series, captured with `adbc.redis.execution_diagnostics_file`, show no run above about 3× the median.
