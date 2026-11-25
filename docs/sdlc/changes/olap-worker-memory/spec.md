# Specification

Date: 2026-11-25. Baseline: `0ebee353`. Issue: [#660](https://github.com/redis/redis-adbc-driver/issues/660).
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Contract

- Optional integer `worker_memory_limit_bytes` in private `connection.json` (at least 16 MiB) enables isolation. Booleans, strings, floats and smaller values are rejected. Without it, behaviour is unchanged: the shared client cgroup.
- When isolation is enabled, the coordinator finds its cgroup in `/proc/self/cgroup`. If it is already in the `olap-harness` leaf, the client cgroup is that leaf's parent. Otherwise it moves every process in the client cgroup into `olap-harness`, then enables `+memory` for the client's children. That ordering is required by cgroup v2's no-internal-processes rule.
- Each setup, engine and validator child runs in a fresh `olap-worker-<label>-<random>` leaf, with `memory.max` set to the limit and `memory.oom.group` set to 1. A `/bin/sh` wrapper joins the leaf, then `exec`s the unchanged command. The child therefore keeps its PID, runner timeouts and return codes.
- After each child, the coordinator reads the leaf's `oom_kill`, `oom_group_kill` and `memory.peak` values. It kills anything left in the leaf, then removes the leaf. Stale empty leaves from a killed coordinator are removed at the next start.
- Refusals, before any query: no cgroup v2 unified entry, or a path outside the namespace; a client cgroup without `memory.max` (a host's real root); less than 256 MiB between the worker limit and the tightest visible ancestor limit; or interface files that can't be written (read-only mount).
- Records keep the client-level `oom_kills_delta` and add `<engine>_worker.memory`, with `isolation`, `limit_bytes`, `oom_kills`, `oom_group_kills` and `peak_bytes`. Isolated validators add `<engine>_validation_memory`. Shared runs record `{"isolation": "shared"}`. Metadata adds `worker_memory_limit_bytes` and `worker_memory_isolation`. `client_memory_limit_bytes` accepts the value `max`. No cgroup path, PID set or private path is recorded.

## Risks and limits

- The harness rearranges the client cgroup. Use only a disposable, dedicated client container. Docker needs `--privileged`, or SYS_ADMIN plus a read-write remount.
- Memory a killed worker leaves behind, such as tmpfs files, is reparented to the client cgroup. It counts there until the coordinator deletes the worker's artifacts. The 256 MiB headroom is for the coordinator, the supervisor and a small reference result.
- `peak_bytes` includes page cache. It is a cgroup measurement, not a driver heap profile.
- CPU limits are unchanged and still shared at the client level.
