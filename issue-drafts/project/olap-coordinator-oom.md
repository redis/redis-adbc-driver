Target: redis/redis-adbc-driver — new issue
Title: OLAP harness: a query worker's OOM can kill the coordinator because both share one client memory cgroup
Labels: enhancement
---
## Problem

`benchmarks/olap-release/run.py` runs each engine and validator as a separate, sequential child process. The children share one memory limit with the coordinator:
- the harness records a single `/sys/fs/cgroup/memory.max` (`run.py:149`);
- `supervise.py` and `isolation.py` read OOM kills from the same `memory.events` file (`supervise.py:30-32`, `isolation.py:507-510`).

So when a Redis worker exhausts the limit, the kernel can kill the coordinator instead of, or as well as, the worker:
- "Q4 had client cgroup OOM with coordinator loss and a recovered terminal record" (`docs/performance/tpcds-focused-followup-2026-11-23.md:181`).
- "Q4 exhausted its client cgroup and killed the coordinator as well as a query process" (`docs/sdlc/changes/olap-worker-status/validation.md:30`).

#601 made checkpoints atomic but scoped this case out: "Atomic documents do not protect the coordinator from its own cgroup OOM. An independent host supervisor/checkpoint recovery is required" (`docs/sdlc/changes/olap-worker-status/spec.md:10`, `intent.md:8`).

The gap is wider than full suites. Focused runs are told not to use `supervise.py` (`benchmarks/olap-release/README.md:32`), so nothing outside the coordinator records their failures. `supervise.py` itself runs inside the same cgroup.

Expected: a surviving process always records a worker OOM as that engine's failure.

Actual: the coordinator can die too, and the result has to be recovered by hand from checkpoints.

## Desired behavior

Keep the coordinator and supervisor outside the memory budget given to engine workers. Two possible approaches, neither decided:
- a child cgroup per worker with its own `memory.max`;
- running the coordinator outside the client container.

The worker's limit should stay equal to the recorded client limit, so results remain comparable with earlier runs.

## How to verify

- Add a test to `benchmarks/olap-release/test_isolation.py` with a synthetic adapter whose worker allocates past its limit. It should assert that the coordinator survives and records `worker_failed` with `oom_kills_delta` ≥ 1.
- The harness tests pass: `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py' -v`.
