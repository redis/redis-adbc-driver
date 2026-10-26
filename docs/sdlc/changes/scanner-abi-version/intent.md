# Scanner ABI version: intent

Date: 2026-10-21. Source: 7025103. Requestor: Albert Wong, reporting
GitHub Actions errors. Status: implementation ready for review.

Restore the required Python DuckDB integration on Linux amd64 and arm64.
Completed job [119377899052](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39644940043/job/119377899052)
failed loading a scanner stamped `2.0.0` into the wheel reporting `v2.0.0`.
Preserve all existing integration assertions and version validation.

See [spec](spec.md) and [plan/evidence](plan.md).
