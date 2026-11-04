# Validation: installed-wheel C harness

Date: 2026-11-04. Baseline: `4fd0894`. Author: Codex.
[Intent](intent.md), [spec](spec.md), [plan](plan.md).

Original failure: [Release binaries run](https://github.com/redis/redis-adbc-driver/actions/runs/41932772218).
Linux amd64/arm64 and macOS arm64 each failed test_substrait_c_admission at C
compilation during installed-wheel validation; the other 563 tests passed in
each job. Test and DuckDB workflows passed. Reproduced the old harness failing
with `libadbc_driver_redis.h file not found` in a headerless library directory.

Local correction evidence on macOS ARM64:

- Five focused Substrait native tests pass, including original and renamed,
  headerless libraries loaded from an unrelated temporary working directory.
- Built and installed an actual platform wheel from the unchanged native
  library. Its packaged filename is .so and there is no generated header.
  Complete native validation against that exact installed path passes:
  **565 passed, 8 explicit skips, 3 subtests passed**, 31.09 seconds.
- Dedicated Redis Software / Redis 8.8.0, three shards, strict TLS/trusted CA,
  active verified 4x QPF through the repository setup script. Operational
  artifacts and credentials remain private.
- C compilation, Python compilation and git diff whitespace checks pass.

Review: only test-loading infrastructure changes. The public ADBC vtable
replaces direct exported-symbol linking; all rejection and no-poison assertions
remain. The regression variant enforces the installed-wheel layout rather than
requiring extra packaging headers or skipping the test. No runtime/ACL/storage
changes, maintainer approval or release claim.

Hosted replacement checks are pending. The separate statement-snapshot branch's
ACL/catalog and row-read failures are distinct from the #316 wheel failure.
