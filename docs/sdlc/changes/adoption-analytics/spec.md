# Specification

Date: 2026-10-29. Source: public main `820d5af`.
[Intent](intent.md), [plan and verification](plan.md).

- Collection defaults on (updated 2026-11-05); `enabled=false` disables collection
  and reporting even with a configured endpoint. The default AWS endpoint is
  `https://adbc-telemetry.sa.redislabs.com/v1/usage`; explicitly empty endpoints
  keep snapshots local. `REDIS_ADBC_TELEMETRY_DISABLED=true` changes the enabled
  default, with explicit options taking precedence. Product usage feedback informs
  product improvement.
  Immutable database-level configuration after first Open, shared across connections.
- Count ADBC ExecuteQuery/ExecuteUpdate calls, not individual statements within
  scripts/parameter batches. Distinguish type, success/error/cancellation/abandonment,
  delivered rows, affected rows, ready and end-to-end latency histograms.
- Reader exhaustion establishes success. Preserve Retain/Release and late failures.
  Bulk ingest/update complete at API return. Features use a fixed SELECT vocabulary.
- Default generated installation ID saved atomically in the local user config
  directory, reused across handles and restarts. Explicit IDs take precedence;
  unusable storage yields a marked temporary ID. Container persistence is external.
- Report self-reported source hostname and collector-observed public egress/PTR
  separately. Trust proxy headers only from loopback with explicit configuration.
- Keep 24 recent completed execution samples per client: fixed kind/features,
  random execution ID, outcomes/times and numeric per-execution work. Reuse
  existing scan counters/index metadata; unknown work stays null. Never export
  SQL, plans, relation names or hashes. Redis retains 10,000 samples, API latest 200.
- Optional organization/team/application/environment/
  integration/use-case labels. Report driver version and platform. No SQL, schema
  identifiers, values, credentials, endpoint addresses or error messages on the wire.
- Background HTTP timeout 2 seconds, no redirects, HTTPS except loopback, optional
  bearer authentication. Bounded local aggregation; final Close report is asynchronous.
  Unsent reports can be lost at process exit. An explicit database flush operation
  synchronously reports for short-lived clients; failure is returned only to that
  caller. No telemetry delivery failure changes query results.
- Versioned cumulative session snapshots with sequence numbers. Redis collector
  validates schema, monotonic counters, bounded labels, histogram consistency and
  stable session identity; WATCH/MULTI serializes retries and aggregates deltas.
  Redis stores session snapshots, a report stream and download indexes. No SQL
  storage is used. AOF and hourly validated RDB snapshots to private encrypted S3
  provide persistence and disaster recovery. Terraform provisions AWS resources.
- Separate write/read tokens; dashboard uses text rendering for untrusted labels.
  Identity labels are self-reported; time windows reflect UTC receipt dates.
- Dashboard shows 7/28-day installation activity, organizations, teams, query mix,
  outcomes, histograms, features, daily totals, mature week-4 cohorts and downloads.
- Download tooling preserves separate GitHub asset and PyPI rolling count sources.
  No claim that downloads are unique installs, companies or active users.

Checks: focused Go race tests and unit suite; Python collector/HTTP/pagination tests;
local dashboard inspection. Redis-backed integration is separately reported.
