# Validation

2026-10-30. Author: Codex. Base: `9124f0f`. Verification complete with the broad Go limitation recorded below.

The new blocked-READ/blocked-DEL regression failed against the baseline with
“cursor cleanup blocked cancellation” with disabled read timeouts. After bounding
cleanup, the actual localhost race run passed the twelve new cleanup cases plus
execution interruption, deadline and stream status tests. A sandbox-only initial
run skipped listener tests and is not network validation evidence.

An isolated Redis Software 8.6.2 one-shard RAM database was started through
`make -C go redis-up`. Setup verified active 4x QPF, six Search workers, mnp
routing, 32 connections and noeviction. TLS and independent credentials are
managed by the existing owned fixture scripts. Operational material stays private.

## Completed focused checks

- `go test -count=1 -p 1 -race -run 'TestCursorCancelBoundsCleanup|TestExecutionTransport|TestTransportDeadline|TestStreamPageDeadline' -timeout 90s .`: passed with actual localhost sockets. Twelve cleanup cases cover context cancellation, owner cancellation and deadlines, with finite/disabled timeouts and single/Cluster routing. Deadline failures retain Timeout/57014; explicit cancellation retains Cancelled/57014.
- Focused retained publication tests: passed, including six failure subcases and the new same-cardinality update/replay test. Replacement DDL leaves no manifest, chunks or reserved byte charge.
- `make -C go build` with a temporary writable GOCACHE: passed with the Makefile version linker flag.
- Native TLS checker against the rebuilt source library: **17 partition checks and 4 metadata checks passed**, including complete Arrow values/types, independent restricted credentials, cancellation, release, source changes, immutable retained replay and revocation. [Sanitized source report](source-tls-validation.json).
- A freshly downloaded PyPI `adbc_driver_redis-0.0.19-py3-none-macosx_12_0_arm64.whl`, installed without a source library override: **17 partition checks and 4 metadata checks passed**. Python 3.15.2, ADBC manager 1.13.0, PyArrow 26.0.0, redis-py 8.1.0. [Wheel/library hashes and scoped results](wheel-validation.json).
- Fixture helper tests: **38 passed, 20 subtests passed**. Python syntax/import checks, changed Markdown relative links and `git diff --check` passed. The metadata helper path selects the existing full eight-cell CI matrix.

## Broad validation setup

Two full Go attempts against the deliberately constrained TLS fixture were stopped and are not passing evidence. The first exposed assumptions about the fixture's 137-row aggregate cap; the second also exposed test helper connections that do not pass the private CA. An interrupted process left fixture data that affected discovery. No TLS verification, test assertions or skip conditions were weakened. The successful native TLS acceptance above remains separate.

Full Go/native validation uses a new, isolated standard Software fixture created by `make -C go redis-up`, with normal aggregate limits and freshly verified effective 4x QPF. The full standard-fixture Go run completed with **1,361 passing, 39 skipped and 25 failing test/subtest events**. Both auxiliary packages passed. Root package elapsed time: 202.549 seconds. All **262 metadata/partition/transport/stream passing events** completed without a failure (10 feature-fixture skips). Failures were EOFs during unrelated connection/catalog/DML operations plus a discovery assertion affected by tables left after cleanup EOFs. These observations are consistent with [#217](https://github.com/redis/redis-adbc-driver/issues/217); no common cause has been established here. No unsafe retry was added, and this is not a clean full-Go pass.

The eight affected top-level cases were rerun: the EOF cases passed, while discovery initially still saw tables left by the earlier failures. After those cases performed their normal cleanup, discovery passed separately (one test). The combined rerun had 31 passing events and the one residual discovery failure before that final cleanup check. See [event counts, test names and native result](standard-validation.json).

Full native ADBC suite: **383 passed, 8 skipped, 3 subtests passed** in 14.02 seconds, using the source library with the Makefile linker version. Upstream validation revision: `5db1f44782f4868c5ae9e89d6ac9812ca6e1758c`. The native suite includes partition worker death/retry, expiry, budgets/quotas, revocation, temporary producer lifetime, complete Arrow values and canonical statistics schemas. Skips cover unsupported UNIQUE/FOREIGN KEY metadata, catalog selection/secondary catalog, table-type filtering, two catalog-ingest cases and transactions. Go skips include Software protocol ACL-user restrictions, optional probes/deployments and process-helper entry points. Restricted Software ACL acceptance is supplied by the separate verified TLS checker.

The TLS fixture was removed after acceptance. The standard fixture was also removed after the passing final discovery check. Raw logs, credentials and management reports remain outside git; checked-in JSON is explicitly sanitized.

## Review and limitations

Self-review covers REVIEW.md correctness/lifecycle, security/ownership and evidence scope. The runtime change preserves coordinator identity and one-attempt deletion; a failed cleanup relies on server cursor idle expiry. It neither rolls back writes nor terminates remote server work. Tests check local cursor/Arrow ownership and pool reuse without replaying READ.

The local protocol fault tests exercise Cluster routing with a controlled
endpoint; the hosted results below supply the live multi-node gate. The
published wheel predates this cleanup fix. Retained capture remains non-atomic,
and declared primary-key metadata does not imply enforced uniqueness. The
self-review above is not an independent approval; the maintainer merge is
recorded below.

## Final hosted validation and issue closeout

Verified 2026-10-30 against PR head
`b544e5485148b4160a7ce1e4eb4c66886200f33b`.
[PR #260](https://github.com/redis/redis-adbc-driver/pull/260) was merged by
Albert Wong as `de3404298309d46d17a8dce0518806c434454cda`.

- [Test run 41163759546](https://github.com/redis/redis-adbc-driver/actions/runs/41163759546):
  all ten jobs passed. Eight Linux architecture/deployment cells passed Go and
  native ADBC validation. Applicable standalone TLS, three-primary TLS routing,
  and Redis Software single/sharded proxy TLS with independent RBAC steps passed,
  including their cleanup steps. Helper and coverage jobs also passed.
- [Release binaries run 41163759593](https://github.com/redis/redis-adbc-driver/actions/runs/41163759593):
  sixteen build/package/client jobs passed; the PR upload job was intentionally
  skipped. This includes Linux x86_64/ARM64 and macOS ARM64 builds, ten Linux
  distribution/architecture jobs, and three Python 3.14 / ADBC 1.12 wheel-client
  jobs. Applicable installed-wheel TLS and independent partition checks passed.
  These jobs validate built packages; they do not publish a new release.

The issue acceptance checklists now link this merged implementation, successful
hosted runs, and the scoped local source/published-wheel evidence:

| Closed issue | Final acceptance and retained boundary |
|---|---|
| [#210](https://github.com/redis/redis-adbc-driver/issues/210) | Independent restricted statistics readers, explicit Search/SCAN/source-key denials, declared PRIMARY KEY/CHECK metadata through rename/removal. UNIQUE/FOREIGN KEY enforcement remains #114; no atomic statistics guarantee. |
| [#211](https://github.com/redis/redis-adbc-driver/issues/211) | Worker death/retry, expiry, budgets, revocation, temporary lifetime, independent readers, prepublication replacement rejection and immutable replay after source changes. Published 0.0.19 availability is verified only for macOS ARM64; capture remains non-atomic. |
| [#212](https://github.com/redis/redis-adbc-driver/issues/212) | Bounded blocked-READ/DEL cleanup, pool reuse, no unsafe replay, cursor/owner/Arrow release, and successful hosted deployment checks. RESP3 supersedes RESP2; remote work termination and rollback are not guaranteed. |

The historical failed local broad Go run remains failed evidence. Successful
hosted runs do not establish its EOF root cause or resolve the separate #217
investigation. No transaction development or new constraint enforcement was
required for this closeout. No runtime changes were made for this documentation
follow-up; verification consisted of checking live PR/run/job/issue state,
relative documentation links and `git diff --check`.

See [intent](intent.md), [spec](spec.md), and [plan](plan.md).
