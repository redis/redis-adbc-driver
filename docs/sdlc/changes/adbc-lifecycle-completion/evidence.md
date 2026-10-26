# Verification and review

2026-10-22. Author: Codex. Base: 7a62902. Local verification complete; maintainer review pending.

make -C go redis-up verified Redis Software 8.6.2, one shard, active QPF 4x.
Noeviction verified by setup's management-plane check; retained tests declare
REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed because proxy CONFIG does not
expose this policy. Operational artifacts/credentials stay private.

An initial full Go run reproduced errors in TestSQLAlterTypeClose,
TestSQLRekeyCloseDuringMove, TestSQLRekeyCloseGivesUp and
TestSQLExecutionDiagnosticsAndStreamTopK. Focused reruns passed after fixes.
No rollback, SQLSTATE, metadata or diagnostics assertions were weakened.

The cancellation fault matrix covers single endpoint and Cluster client routing,
finite/disabled read timeout, command/pipeline/cursor and owner/context cancel.
Each cancellation returns within one second, preserves CANCELLED/57014, observes
no replay, leaves unrelated pools usable and releases owner registrations.
Race detection and child-context recovery are checked separately from Redis.

No live Cluster/TLS deployment or published wheel availability is inferred.
Protocol ACL SETUSER restrictions on Software limit real-user ACL tests; all
actual skips will be listed. Atomic snapshots remain unavailable as assessed in
partition-coverage.md, with transaction/versioning dependency #269. Server work
can continue after client socket interruption. No performance claim is made.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Subsequent shared-fixture runs were invalidated by another concurrent test
process dropping/replacing the same test tables. They are not claimed as
passing verification; final suites use a separate disposable Software fixture.
No other process was stopped and no assertions were loosened.

## Final checks

Final acceptance used a separate, freshly initialized single-shard Redis Software
8.6.2 fixture with verified active QPF 4x and management-verified noeviction.
No other process used that fixture. Endpoint/management identifiers remain private.

- Full Go: `go test -count=1 -p 1 -json ./...`, with the fixture supplied through
  REDIS_URI, REDIS_ADMIN_URI and REDIS_WORKER_URI and retained persistence set to
  guaranteed after verification: all three packages passed; 1,380 passing
  test/subtest events, 39 skips, zero failures. Root package: 224.784 seconds.
- After that run was compiled, the stream deadline classification fix/regression
  was added. Focused race tests for the page-deadline and execution transport
  passed, followed by the rebuilt library's complete native suite.
- `go test -count=1 -p 1 -race -run 'TestStreamPageDeadline|TestExecutionTransport'
  -timeout 45s .` with REDIS_URI unset: passed. The interruption matrix has 24
  cases plus child-context recovery; the separate deadline transport matrix also
  passed under race detection.
- Baseline reproduction with a temporary Go overlay: the new page-deadline test
  fails against merged stream.go with CANCELLED/57014; corrected code returns
  TIMEOUT/57014 and closes the reader. No repository source was reverted.
- `make -C go build`: passed, preserving the driver version linker flag.
- Full native suite with the isolated fixture and the available Python 3.13
  validation environment: `python -m pytest -q -ra tests/`: **383 passed,
  7 skipped, 3 subtests passed** in 16.96 seconds. Includes statistics schemas,
  exact-request/approximate-flag behavior, constraints, temporary producer lifetime,
  complete live/retained Arrow comparisons, SQL/stream cancellation, partition
  retries/death, quotas, expiry and revocation.
- gofmt, `git diff --check` and new-record relative-link checks passed.

Go skips include Software protocol ACL-user creation restrictions, optional
profiles/probes, process-helper entrypoints and fixtures for live Cluster/TLS,
policy admission, Flex rejection and command counters. Native skips are the two
unsupported constraint kinds, catalog selection/secondary catalog, two catalog
bulk-ingest cases and transactions. No skip or assertion was weakened.

The isolated three-shard attempt encountered connection EOFs during reads and
connection setup despite subsequent successful QPF re-verification. It was stopped
and is not passing multi-shard evidence. The disposable fixture was replaced with
the standard single-shard topology for the clean final acceptance run. The cause
of those EOFs is not established here; no new multi-shard/TLS claim is made.

## Review

Self-review covered all REVIEW.md passes: Arrow/resource ownership and complete
fetches; cursor coordinator identity and non-replay; cancellation/deadline status;
lease renewal/rollback; quotas/temporary-source ownership; ACL and diagnostics
boundaries; and actual evidence versus unsupported snapshot/release guarantees.
All reproduced lifecycle findings in this diff are fixed. Atomic snapshots remain
a separately assessed transaction/versioning dependency, not a claimed feature.
Maintainer review is still required; self-review is not approval.
