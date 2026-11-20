# Plan and evidence: configurable operation deadlines

Date: 2026-11-20. Source: `c6a75ca2`. Status: implemented and locally verified; ready for maintainer review.
Requestor: Albert Wong. Author: Codex. Issue: [#546](https://github.com/redis/redis-adbc-driver/issues/546).
Related: [intent](intent.md), [specification](spec.md).

## Ordered work

1. Add option parsing/getters and connection/session reset defaults.
2. Resolve statement/session policy in SQL and Arrow execution contexts.
3. Carry policy through source creation/replacement and nested metadata import.
4. Retain independent leases, cancellation, cleanup and reader ownership.
5. Add focused fault-injection/precedence regressions and update public docs.
6. Run unit and verified Software checks, review the final diff, create a PR.

## Verification

All checks below passed on 2026-11-20. Redis tests used a dedicated disposable
Redis Software 8.8.0 database with three active primary shards, TLS/RESP3,
noeviction and verified active 4x QPF (6 Search workers, mnp routing,
32 connections). Setup used `make -C go redis-up`; every Software test invocation
used `software-setup.sh --exec` to reverify effective settings. Fixture routing
and operational identifiers are intentionally omitted; these are correctness
checks, not performance measurements.

From `go/`, with the verified disposable endpoint configured:

- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  passed across packages. Integration skips are expected in this unit-only pass;
  it is not Redis compatibility evidence.
- `go test -count=1 -p 1 -timeout=8m -run
  'TestManagedStatementTimeout|TestStatementTimeout|TestSnapshot(IngestAdmission|SQLAdmissionCancellation|StreamRetainsGeneration|LiveArrow)|TestUnmanagedArrow|TestLiveArrow' .`:
  passed, 30.571 seconds. Both URI variables referenced the same disposable
  endpoint for this snapshot-only selection because one existing NUL fixture's
  raw-client helper reads REDIS_URI. An earlier invocation omitted it and failed
  that helper with an empty URL; the corrected invocation passed unchanged.
- Separately, `go test -count=1 -p 1 -timeout=8m -run
  'TestSQLSet|TestTimeoutOptions|TestSQLCancel' .` with REDIS_URI:
  passed, 3.491 seconds.
- `go test -race -count=1 -v -p 1 -timeout=8m -run
  'TestManagedStatementTimeout|TestStatementTimeout' .`:
  passed, 8.160 seconds; no skipped selected tests or race reports.
- `make -C go build`: passed, including the versioned C shared library.
- `git diff --check`, change-record links and the timeout documentation anchor:
  passed.

The new SQL and short Arrow tests were also run with the helper temporarily
forced back to the original fixed 15-minute deadline: both failed because the
operation completed instead of timing out. Restoring the configurable policy
made them pass. The checked-in tests additionally cover session/default/reset
precedence, durations above 15 minutes, explicit unlimited execution, earlier
caller cancellation/deadlines, streamed-result expiry, both application ownership
modes, and retained old streams/no partial managed append/pin release.

No ClickBench, full default Software suite, full-scale ingestion, or multi-node
cluster run was performed. Operational fixture files, credentials and raw setup
logs remain private.

## Review decision

Agent review completed the correctness/lifecycle, security/data-ownership and
intent/evidence passes in REVIEW.md with no actionable findings. Only operation
contexts/defaults changed: writer ownership, publication, lease renewal, fences,
cleanup scripts and replay policy remain on their existing paths. Human
maintainer review remains outstanding; this is not approval to merge or release.
