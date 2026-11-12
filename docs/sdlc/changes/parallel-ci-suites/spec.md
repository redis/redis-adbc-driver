# Parallel CI suites specification

Date: 2026-11-12. Base: `53b76248`. Requestor: Albert Wong. Author: Codex.
Status: implemented and checked locally; hosted execution pending.

- `driver`, `snapshots` and `integrations` depend only on coverage selection and
  use its existing matrix and architecture-specific runners, with fail-fast off.
- Each suite starts its own authenticated TLS Redis Software fixture through
  `make redis-up`, verifying active 4x QPF and noeviction before tests. Each
  fixture has failure diagnostics and unconditional cleanup.
- Move Python analytics/scanner, Ray, Airflow and dlt steps without modifying
  their commands, conditions, dependency versions, TLS requirements or options.
- Move the snapshot race selection intact, retaining the verified Software
  wrapper and `REDIS_SNAPSHOT_URI`. No concurrent suites share this database.
- Keep native validation, partition/security evidence and the explicit SQL RBAC
  failure gate in the driver job. Keep package parallelism at `-p 1` and disable
  test result caching with `-count=1`.
- Preserve original `linux-<arch>-<environment>` names as summary checks that
  require all split matrix jobs to succeed; failures, cancellations and skips
  cannot pass. The summary conservatively waits for both selected architectures.
- Add `-v` to Go suites to report individual test elapsed times in CI logs.
- Retain Superset isolation, telemetry opt-out, cancellation policy and artifacts.

The extra hosted fixtures/builds trade some runner minutes for lower elapsed
time. A 20–25-minute warm-cache target is an estimate, not a measured outcome.
Runner concurrency, cache misses and queueing can limit the gain.

See [intent](intent.md) and [verification](plan.md).
