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

No new live Cluster API deployment or additional published-wheel platform is claimed. Protocol fault tests exercise Cluster routing with a controlled endpoint; existing hosted CI remains the live multi-node gate. The published wheel predates this cleanup fix. Retained capture remains non-atomic, and declared primary-key metadata does not imply enforced uniqueness. Maintainer review is pending; this self-review is not approval.

See [intent](intent.md), [spec](spec.md), and [plan](plan.md).
