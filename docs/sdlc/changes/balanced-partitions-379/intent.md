# Intent

2026-10-21; baseline `d7346f9` (PR #381); requestor alberttwong; author Codex.
Issue [#379](https://github.com/alberttwong/redis-adbc-driver/issues/379).
Status: implemented and locally validated; no maintainer approval.

Improve usable parallelism for supported managed-table reads whose live rows
are sparse/skewed within allocated IDs. Add opt-in workload-aware cuts with
bounded planning cost; retain the cheap default and complete live-read coverage.
See [spec](spec.md), [plan/evidence](plan.md).
