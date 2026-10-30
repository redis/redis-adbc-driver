# Plan and verification

Date: 2026-10-29. Source: public main `820d5af`. Status: implemented and locally verified; not released or deployed.
[Intent](intent.md), [specification](spec.md).

1. Add database configuration, shared reporter and Arrow lifecycle instrumentation.
2. Add validated SQLite ingestion, authenticated read API and dashboard.
3. Add GitHub/PyPI snapshot tooling, Python option constants and setup documentation.
4. Verify disabled behavior, confidentiality, ownership, late failures, cancellation,
   concurrency, retries, retention, authentication, pagination and existing unit suite.
5. Inspect dashboard and review against REVIEW.md. No release/deployment in scope.

## Initial implementation validation

All commands were run on 2026-10-29 (America/Los_Angeles).

- `env -u REDIS_URI go test -count=1 -p 1 -json ./...`: passed, 301 passing
  test cases and 457 skipped cases (including subtests). Skips are Redis-dependent
  or other opt-in checks; this unit-only pass does not establish Redis compatibility.
- `REDIS_URI=redis://localhost:26379/0 go test -race -count=1 -p 1 -run
  '^TestUsage' ./...`: passed against an isolated Redis Software 8.6.2 RAM database
  with Search, plus the local Python collector. Verified 250 ingested rows,
  successful complete stream (250 rows), abandoned stream (10 delivered rows),
  canceled stream, snapshot retrieval and synchronous HTTP flush. Inspected
  SQLite's actual Go reports and matching totals. No race findings.
- `REDIS_URI=redis://localhost:26379/0 go test -count=1 -p 1 -json -run
  '^(TestUsage|TestSQLStream|TestSQLCancel|TestSQLBoundStrings)' ./...`: 39 passing
  test cases, no skips or failures, against that disposable Software database.
- `python3 -m unittest discover -s analytics -p 'test_*.py' -v`: 5 passed,
  covering protocol rejection, retries/order/restarts, identity immutability,
  mature retention cohorts, token separation, HTTP/static routes and download
  pagination. Download HTTP responses were mocked; live registry totals were not
  fetched or asserted.
- `make -C go build`: native macOS arm64 shared library built successfully.
- `node --check analytics/dashboard.js`, Python compilation, `git diff --check`:
  passed. A task-specific `/private/tmp` Go cache avoided sandbox cache writes;
  loopback tests used approved local networking.
- Browser inspection: authenticated dashboard rendered query mix, self-reported
  labels, histogram values, empty downloads and pending retention cohorts, using
  clearly labeled synthetic data plus the real local integration-test reports.
  Preview screenshot: `/private/tmp/redis-adbc-analytics-dashboard.png`.
- Temporary preview server, dedicated Compose container and network were stopped
  and removed after testing. No pre-existing Redis database was used.

## Review

Reviewed the final behavior against `REVIEW.md`: existing streaming cancellation,
Arrow ownership and query behavior passed their focused regressions. Payload
fields use an explicit allowlist and contain no SQL, result values, Redis addresses,
credentials or error text; explicit labels are user-controlled. Redirects are
refused. Read and write credentials are separate, and dashboard labels render as
text under a same-origin content security policy. No maintainer merge approval is
implied by this implementation review.

Limitations: telemetry defaults off and covers participating installations only;
company/team labels are self-reported; IDs must be persisted for retention;
time windows use receipt dates; scripts/batches count as one API execution;
client buffering is in memory; production hosting/TLS, customer identity mapping,
Scarf imports, scheduling download snapshots and release publishing remain
operator configuration. No deployment or release was performed.


## Public-main PR port

Ported only the analytics commit onto public main `820d5af`, preserving its
current IPC execution wrappers, partition lifecycle and dependency versions.
The unit fixture now supplies the execution transport configuration required
by that branch. No unrelated source history is part of the PR.

- Public-main Go unit suite: **650 passed, 572 skipped**, no failures. Skips are
  opt-in Redis/environment checks, not a claim of integration coverage.
- Collector tests: **5 passed** on the ported branch.
- Native macOS arm64 shared library rebuilt successfully.
- The new isolated Software fixture was started via `make redis-up` and verified
  active **4x QPF**, 6 Search workers, mnp routing and 32 connections before tests.
- `REDIS_URI=redis://localhost:26379/0 go test -race -count=1 -p 1 -run
  '^(TestUsage|TestSQLStream|TestSQLCancel|TestSQLBoundStrings)' ./...`: passed
  against that verified Software 4x QPF fixture, with no race findings.
- The dedicated Software container and its network were removed after validation.
