# Plan and validation

1. Add bounded client traffic/path/error fields and cumulative diagnostics.
2. Validate and persist fields; aggregate by traffic/version/statement, scope summaries by traffic, and classify sample input size.
3. Add global traffic selection, comparison and diagnostic tables; preserve plain-text rendering and compact notes.
4. Verify race-safe collection, legacy compatibility, counter deduplication, privacy allowlists, filters and Chrome layout. Deploy collector before client rollout.

See [intent](intent.md) and [spec](spec.md). Validation (2026-11-05):

- `node --test analytics/test_dashboard.cjs`: 5 passed.
- Collector/backup tests against isolated key prefixes on local Redis Software: 18 passed, no skips.
- Telemetry race suite passed across Go packages with no Redis URI (Redis and hosted smoke cases skipped).
- Telemetry plus exact-partition aggregate and group-cap recovery tests with `-race -count=1 -p 1` on local authenticated TLS Redis Software: passed; hosted smoke intentionally skipped.
- Verified the fixture's management API: active 4x QPF, six Search workers, mnp routing, 32 connections.
- Real Go ingestion/query/cancel/abandon executions delivered to the local collector successfully. Chrome displayed version, path, cancellation category and size buckets. Application selection reduced comparisons and cards to its one synthetic application report; benchmark selection excluded CI/application reports.
- `git diff --check` and Python compilation passed.

Review: checked explicit unknown precedence, legacy missing fields, cumulative diagnostic deduplication, fixed-vocabulary privacy boundaries, safe text rendering, stale-response suppression and distinct sample/histogram semantics. No actionable findings remain. New driver binaries must be rebuilt/upgraded; no package release was performed.
