# Specification

Date: 2026-10-29. Source: public main `820d5af`.
[Intent](intent.md), [plan and verification](plan.md).

- Reporting defaults off; enabling without an endpoint provides local snapshots.
  Immutable database-level configuration after first Open, shared across connections.
- Count ADBC ExecuteQuery/ExecuteUpdate calls, not individual statements within
  scripts/parameter batches. Distinguish type, success/error/cancellation/abandonment,
  delivered rows, affected rows, ready and end-to-end latency histograms.
- Reader exhaustion establishes success. Preserve Retain/Release and late failures.
  Bulk ingest/update complete at API return. Features use a fixed SELECT vocabulary.
- Optional stable installation ID and organization/team/application/environment/
  integration/use-case labels. Report driver version and platform. No SQL, schema
  identifiers, values, credentials, endpoint addresses or error messages on the wire.
- Background HTTP timeout 2 seconds, no redirects, HTTPS except loopback, optional
  bearer authentication. Bounded local aggregation; final Close report is asynchronous.
  Unsent reports can be lost at process exit. An explicit database flush operation
  synchronously reports for short-lived clients; failure is returned only to that
  caller. No telemetry delivery failure changes query results.
- Versioned cumulative session snapshots with sequence numbers. SQLite collector
  validates schema, monotonic counters, bounded labels, histogram consistency and
  stable session identity; ignores retries/old reports and aggregates deltas.
- Separate write/read tokens; dashboard uses text rendering for untrusted labels.
  Identity labels are self-reported; time windows reflect UTC receipt dates.
- Dashboard shows 7/28-day installation activity, organizations, teams, query mix,
  outcomes, histograms, features, daily totals, mature week-4 cohorts and downloads.
- Download tooling preserves separate GitHub asset and PyPI rolling count sources.
  No claim that downloads are unique installs, companies or active users.

Checks: focused Go race tests and unit suite; Python collector/HTTP/pagination tests;
local dashboard inspection. Redis-backed integration is separately reported.
