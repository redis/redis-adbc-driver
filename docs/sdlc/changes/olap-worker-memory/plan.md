# Plan

Date: 2026-11-25. Baseline: `0ebee353`. Issue: [#660](https://github.com/redis/redis-adbc-driver/issues/660).
See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Reproduce coordinator loss against the unmodified harness in a memory-limited container.
2. Add `worker_memory_limit_bytes`, `WorkerCgroups` and the per-child leaf wrapper in `benchmarks/olap-release/isolation.py`. Route engine and validator children through it, and the setup child in `run.py`. Record the memory details.
3. In `test_isolation.py`: add profile and precondition unit tests, and the opt-in live test `WorkerMemoryCgroupTests` with anonymous and tmpfs-backed memory hogs.
4. Add the `worker-memory-cgroup` job to `.github/workflows/olap-harness.yml`, running the live test in a privileged container.
5. Document the option, container flags, headroom and records in `benchmarks/olap-release/README.md`.
6. Run the harness tests on macOS and Linux, run the live test with and without the fix, and lint the workflow.

No driver code, database operation or benchmark query is part of this change.
