# Validation and review

Date: 2026-11-25. Baseline: `0ebee3532fa5f195b06a708d51cc5a27e8373b91`.
Issue: [#660](https://github.com/redis/redis-adbc-driver/issues/660). See [intent](intent.md), [spec](spec.md), [plan](plan.md).

All runs were local: Docker Desktop on Apple silicon, Linux kernel `7.0.14-linuxkit`. No Redis server, database or benchmark query was used. The engines are synthetic adapters.

## Reproduced before the fix

A probe ran the unmodified `isolation.run_query` coordinator in a `--memory 256m --memory-swap 256m` container. Its synthetic Redis worker allocated until it was killed.

| Worker memory | Coordinator exit | OOM kills | Record |
|---|---:|---:|---|
| Anonymous (in the worker's RSS) | 0 | 1 | `worker_failed`, code -9 |
| Written to a tmpfs file (not in the worker's RSS) | 137 (killed) | 2 | lost |

The new live test was also run against the baseline `isolation.py`/`run.py`, in a privileged 256 MiB container. The anonymous subtest errored, because baseline records have no `memory` field. In the tmpfs subtest the OOM killer killed the test process itself (exit 137).

## Fixed harness

- `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py' -v`: 34 tests OK, 1 skipped (the opt-in live test), on macOS with Python 3.14.8. The same suite passed in `python:3.14-slim` (Python 3.14.10) on Linux without the opt-in.
- The CI command from the README ran `WorkerMemoryCgroupTests` in `--privileged --memory 512m --shm-size 256m`. It passed on `python:3.14-slim` and `python:3.12-slim`. In both modes the 64 MiB worker was OOM-killed inside its leaf. The coordinator exited 0 and recorded `redis_status: worker_failed`, return code -9, `oom_kills_delta` ≥ 1 and leaf `oom_kills` ≥ 1. The other engines still validated in their own leaves with zero OOM kills. No worker leaf remained.
- A pre-created empty `olap-worker-*` leaf was removed at the next start.
- A 400 MiB worker limit in a 512 MiB container was refused for insufficient headroom.
- `actionlint` reported no findings for `.github/workflows/olap-harness.yml`.

## Limits

This shows that a worker's OOM is contained, using synthetic workers. It is not an SF1 run, and it does not reduce any query's memory. The new CI job has not run on GitHub yet. The default profile keeps the shared cgroup, so existing callers see no change unless they opt in. Human review is pending.
