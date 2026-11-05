# Plan and verification

Date: 2026-10-29. Initial source: public main `820d5af`.
Status updated 2026-11-05: verified and deployed to AWS; not released.
See the later sections for latest-main merge and live deployment evidence.
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

Original implementation limitations (before the 2026-11-05 opt-out update):
telemetry defaulted off and covered participating installations only;
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


## Opt-out update (2026-11-05)

Source: PR #244 head `691e48ab41432da804b60bd540952f9c4550b1e4`.
Requestor: Albert Wong. Collection now defaults on; explicit `enabled=false`
disables collection and reporting, including when an endpoint is configured.
Explain product usage feedback as a way to understand user usage and improve
workloads, reliability and the product. Preserve the payload exclusions.

1. Change both option readback and zero-value startup defaults. Completed.
2. Update setup examples, opt-out instructions, purpose and feature records. Completed.
3. Verify implicit collection/reporting, explicit opt-out, privacy and lifecycle
   with the focused Go race tests. Passed; Redis integration skipped without REDIS_URI.
4. Collector tests: 5 passed (including local HTTP access control). Full Go
   regression: `env -u REDIS_URI go test -race -count=1 -p 1 -json ./...`
   passed: 653 passing test cases, 572 environment-dependent skips, no failures.
   Evidence: [go-tests.json](evidence/opt-out-go-tests.json). `git diff --check` passed.
5. Route 53 endpoint selected: `https://adbc-telemetry.sa.redislabs.com/v1/usage`.
   Terraform deployment prepared; exact-resource approval pending.

Review: opt-out returns before reporter allocation or background network startup;
endpoint configuration alone enables reporting. No changes to wire fields, label
handling, redirect rejection, TLS requirements or Arrow ownership.


## Latest main, Redis storage and Terraform (2026-11-05)

Merged public main `3aa87e2` in `6d2118c`; resolved README and database/connection
field and Close conflicts by retaining telemetry and RBAC cleanup. No RDS was
created; the user canceled that storage choice and requested Redis with S3 backups.

- Default driver reports go to the AWS hostname, with no required client secret.
  Explicit empty endpoint selects local snapshots; explicit `enabled=false` opts
  out. Environment opt-out is available for applications and regression tests.
- Replaced SQLite with pooled RESP3 Redis storage, WATCH/MULTI deduplication,
  report stream pagination and download indexes. Storage failures return 503.
- Terraform prepares 14 AWS resources, including a private versioned/encrypted
  S3 bucket and prefix-scoped upload role. No resources were created during plan.
- Host bootstrap configures private Redis with AOF/noeviction, authenticated
  local collector, public rate-limited HTTPS proxy, protected dashboard reads,
  automatic TLS renewal and hourly checked RDB-to-S3 backups. Restore validates
  checksum/size/RDB and refuses to overwrite a file. Secrets stay on the host.
- Latest-main Go unit race suite: 1014 passes, 615 environment-dependent skips,
  no failures. Dedicated Redis Software 8.8.0 with verified 4x QPF: 44 focused
  telemetry/streaming/cancellation test cases passed, no skips or race findings.
- Redis collector and backup tests: 9 passed, no skips. S3 calls mocked for these
  tests; Redis storage used the dedicated Software fixture. Terraform validation
  and read-only plan passed; formatting, Python compilation and shell syntax pass.
- Evidence: [redis-terraform-validation.json](evidence/redis-terraform-validation.json).

Deployment awaits explicit approval of the reviewed resource settings following
an automatic approval-review rejection of an earlier provisioning command. That
command was never executed. Final deployment verification must include DNS/TLS,
HTTP ingestion, dashboard authentication, Redis persistence and a real S3 backup.


## AWS deployment completed (2026-11-05)

The user explicitly approved the exact resource, access and cost plan. Terraform
created 14 resources, then applied five in-place descriptive tag updates. All
supported resources have project/purpose/management/owner tags, including the
root volume and IAM role. Remote state is encrypted/versioned in S3 with native
locking; the host role cannot access the state prefix.

- DNS and trusted HTTPS are live at `adbc-telemetry.sa.redislabs.com`.
- Health 200; accepted report 200; duplicate ignored; SQL payload 400; dashboard
  without token 401; authenticated summary 200.
- Real Go driver hosted test passed with the race detector and default AWS URL.
- Collector, Redis and nginx active; backup and certificate renewal timers enabled.
- Actual S3 RDB backup downloaded, checksum/encryption/version verified, checked
  with `redis-check-rdb` and loaded into an isolated Redis process. Test reports
  were present. Temporary restore process/files removed afterward.
- Repository tests and CI workflows now opt out to avoid inflating product usage.
  Clearly labeled deployment smoke reports are removed after backup verification;
  other unattributed traffic is preserved. Initial CI traffic can remain in totals.
- Final telemetry race tests, workflow YAML parsing and diff checks passed.
- [Deployment evidence](evidence/aws-deployment.json) and [runbook](../../../../analytics/deploy/README.md).

No release was published. The original automatic approval rejection was resolved
by explicit user approval of the concrete resource plan before Terraform apply.


## Workload details, source DNS and persisted IDs (2026-11-05)

Requestor: Albert Wong. Replace low-value cross-query row totals with recent
per-execution work and a list of reporting machines and public reverse DNS.
Clarify that weekly activity deduplicates installation IDs, not machines/people.
Correct the old per-handle generated-ID default: publish a generated ID file
atomically and reuse it across handles/process restarts, falling back to an
explicitly marked temporary ID when persistence is unavailable.

- Reuse existing input candidate counters and index metadata; do not add Redis
  commands or export plans/SQL/identifiers. Distinguish unknown from zero. Bound
  completed execution samples to 24 per client and 10,000 stored samples.
- Preserve older aggregate reports. Deduplicate sample IDs across retry snapshots.
- Record client hostnames separately from collector-observed public egress IP/PTR.
  Only trust overwritten proxy headers from loopback with explicit configuration;
  resolve DNS in bounded background workers rather than request threads.
- Relabel the seven-day card as distinct active installation IDs with persistence
  and container caveats. Show individual workloads and a filterable source list.
- Automated evidence: [workload-sources-validation.json](evidence/workload-sources-validation.json).
  Full Go race suite: 1017 passes, 616 skips, no failures. Focused Software suite:
  47 passes, hosted-only check skipped, no failures. Collector tests: 11 passed.
- The updater takes an S3 backup first, stages and compiles source, preserves
  current read/write tokens, extends local ACLs, and retains rollback files.
  New driver builds are needed for hostname, persistent-ID status and samples;
  historical reports cannot recover missing fields or merge old ephemeral IDs.


Live follow-up: both Go hosted checks passed with the race detector using a
combined public/private CA bundle. The fixture-only CA initially prevented trust
of the public AWS certificate; no certificate verification was disabled. A real
SELECT reports 250 indexed documents, 250 input candidates and 250 delivered
rows; early release shows 250 input candidates and 10 delivered rows separately.
Client hostname, public source and saved-file ID status reach the dashboard.
A real background PTR lookup resolves 8.8.8.8 to dns.google. The existing read
token still works. Delivery failures now include safe HTTP status information,
without response bodies, SQL, tokens or raw transport errors.


Final dashboard revision deployed at `34a557a29de96dd8f6c41572b406f34e3cd507e4`.
Browser inspection verified the distinct-ID label and explanation, saved-local ID
classification, both source hostname and real public PTR, SELECT filtering, and
per-execution input/returned counts. A filtered SELECT returns three candidates
and three output rows; index population remains unknown for that particular plan
rather than borrowing another statement's measurement. Validation screenshots
were captured before removing labeled smoke data; other sources were preserved.
No new AWS resources were required and the dashboard read token was preserved.
