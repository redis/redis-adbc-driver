# Plan

See [intent](intent.md), [contract](spec.md).

1. Capture focused failures for serial INSERT and disabled numeric filtering.
2. Batch direct INSERT with bounded non-replaying pipelines and per-reply counts.
3. Enable conservative integer candidate filtering and authoritative HASH scans.
4. Add failure/race/live-read regressions and update partial-write documentation.
5. Run unit and focused integration checks on verified Software 4x QPF, compare
   completed results and command counts, then review correctness/ownership/evidence.

Completed: implementation, focused failing-before checks, unit tests, Software
integration/race regressions, before/after diagnostic and self-review. See
[validation](validation.md) for exact commands, results and limits.
