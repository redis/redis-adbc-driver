# Plan and evidence

2026-10-21; baseline `85d6807`; issue #377. See [intent](intent.md), [spec](spec.md).

1. Capture a failing deterministic test using real scans with controlled FT.INFO
   state instead of relying on nondeterministic backfill timing.
2. Add shared readiness validation and ordinary HASH fallback selection.
3. Exercise initial/open/page/range transitions, cursor cleanup, stale identities,
   cancellation, restricted ACLs and a building non-coordinator Cluster primary.
4. Run full Go, native cap-137 validation and focused Cluster/race checks.
5. Open a PR before starting #379 and #380, as requested.

Before fix: TestBoundedScanRejectsBuildingIndex failed because openScan returned
no error for indexing=1. After fix, that regression and focused transitions pass.
Shared bounded-path fixtures now explicitly wait for backfill readiness so they
still require the intended scan path; the building-index test separately requires
ordinary HASH fallback and exact global LIMIT/OFFSET results.

Completed validation (Go 1.27.2, macOS 26.7.1 arm64, Redis OSS 8.6.2/Search 8.6.0):

- `REDIS_URI=redis://localhost:6379/0 go test -count=1 -p 1 ./...` from go/:
  all three packages passed; main 93.208s, CLI 1.175s, benchproxy 0.231s.
- `REDIS_URI=redis://localhost:7001/0 go test -race -count=1 -p 1 -run
  '^(TestRangeReadiness|TestBoundedScan|TestOrdinaryBuilding|TestPartition|TestSharedScan)' .`:
  passed 7.155s, including a building non-coordinator primary and ACL regressions.
- Native library build passed. Full Python validation on 127.0.0.1:16390 with
  actual/guaranteed cap 137: 369 passed, 8 existing feature skips, 3 subtests,
  11.22s. The native partition JUnit gate passed all 7 cases.
- `git diff --check` passed. Software/Cloud/proxy compatibility remains subject
  to CI/deployment evidence; no new fixed server version is claimed.

No paid resources. Task-owned local Redis container
redis-adbc-partition-377 on 127.0.0.1:16390, real cap 137, is retained for the requested #379/#380 follow-ups and cleaned up afterward.

Initial CI exposed a Cluster backfill readiness response rejected as unavailable.
The parser now recognizes any finite positive numeric indexing flag as building
before requiring a completed percentage, including transient NaN progress; a
zero flag still requires finite completed progress. Numeric flag/progress parsing
regressions are retained. Follow-up CI results remain pending.
