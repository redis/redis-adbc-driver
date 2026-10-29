# Plan and verification

Date: 2026-10-29. Source baseline: `95d6583`. Author: Codex.
Status: completed locally; not submitted or released.
See [intent](intent.md) and [specification](spec.md).

1. Inspect DB-API lifecycle, Ray public APIs and local test environment. Done.
2. Build the current native library and install Ray in an isolated temporary
   Python environment. Done.
3. Add `python/tests/test_ray.py`, run against Redis Software, and fully compare
   results. Done.
4. Document usage, reproducible commands and validation boundaries in
   `docs/python-integrations.md`. Done.
5. Review lifecycle, data ownership, opt-in behavior and evidence. Done locally;
   no actionable finding remains. No independent maintainer review occurred.

Environment: macOS arm64, Python 3.12.14, Ray 2.59.0, PyArrow 26.0.0,
ADBC driver manager 1.13.0, locally built Redis driver from baseline `95d6583`,
Redis Software container `go-redis-software-1`, Redis 8.6.2 on port 6379.

Checks:

- `PATH=/opt/homebrew/bin:$PATH make -C go build`: passed.
- `RUN_RAY_TESTS=1 REDIS_URI=redis://localhost:6379/0
  REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib"
  /private/tmp/redis-adbc-ray-venv/bin/python -m pytest -q -s
  python/tests/test_ray.py`: **3 passed, 3 warnings in 10.43s**. Warnings
  originate from Ray's deprecated internal DataContext settings. Runtime
  initialized with two CPUs; actual SQL reads used one task.
- Same test file without `RUN_RAY_TESTS`: **3 skipped in 0.01s**, as intended.
- Independent metadata query after the run: no `it_python_ray_` tables remain.
- `git diff --check`: passed.

No broad Go suite was rerun because native driver behavior was unchanged.
No transaction guarantees, distributed-read scaling, parallel-write scaling
or throughput conclusions follow from this smoke test.
