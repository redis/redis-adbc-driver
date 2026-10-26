# Plan

Date: 2026-10-21. Source: 053e638 (merged #413). Requestor: Albert Wong (keep going); author: Codex. Status: in progress. Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170).

1. Add a release-wheel consumer matrix, without rebuilding native libraries per cell.
2. Exercise local macOS and Linux ARM64 combinations and retain results; use hosted native x86_64 jobs for that architecture.
3. Validate workflow syntax, evidence and cleanup, inspect merged #413 checks, and open a PR.

See [intent](intent.md) and [spec](spec.md). No driver runtime changes.

Completed: release artifact consumer matrix, publication dependency/artifact filtering, Go-change PR trigger, eight local cells (120 checks), workflow contract/syntax checks and owned fixture cleanup. Hosted native x86_64 cells remain pending. See [evidence](evidence.md).
