# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `ef914bb3`; working-tree implementation described by [intent](intent.md),
[spec](spec.md), [plan](plan.md). [Sanitized outcomes](results.json).

## Behavior and scope

At the requestor's explicit direction, the driver now advertises PostgreSQL 18
compatibility: VERSION/pg_catalog.version say `PostgreSQL 18.0 (Redis ADBC driver)`,
server_version is `18.0`, server_version_num is `180000`, and SQLAlchemy's dialect
version is `(18, 0)`. Redis vendor-version reporting remains independent.
The target advertisement is not full PostgreSQL or SQL Core conformance.

Five #364 identity cases check the reference connection's startup version,
authenticated identity and captured complete build string against each actual
response, and the driver contract against its authenticated username and exact
advertisement. They report `context`, outside dialect passes. One exact source
query reports `extension` only for PostgreSQL 42601 and Redis's exact UTC
TIMESTAMPTZ result. The ordered window case permits permutation solely inside
contiguous equal-rank groups, preserving distinct-key ordering, values, types
and duplicate counts. Policies require exact file/case/SQL matches. No corpus,
source oracle, provenance or existing allowlist was changed.

## Checks

Owned Redis Software with Redis 8.8.0 and Search, three primary shards,
noeviction, authenticated verified TLS/RESP3. `make -C go redis-up` and each
live invocation's `software-setup.sh --exec` verify active 4x QPF, six Search
workers, mnp routing and 32 connections before execution. Client: macOS arm64,
Docker-published endpoint. PostgreSQL 18.6 uses the pinned amd64 image and C
collation. Raw reports, usernames, endpoints and credentials remain private.

- Before implementation, the revised VERSION scalar assertion failed with the
  old 16.0 string, and revised Software session settings failed. Both pass after
  changing the advertisement. An earlier misnamed test selection ran no matching
  tests and is not counted as coverage.
- `make -C go pgdiff-test`: **42 unit tests passed**. Negative controls reject
  wrong identities/versions/types, NULL/empty/duplicate rows, changed build text,
  altered extension values/errors, changed peer values/types/multiplicity, distinct
  group reordering and changed source SQL. The #359 audit independently rechecks
  context/extension outcomes and never counts them as dialect passes.
- Full `software-setup.sh --exec make pgdiff`: **605/645 dialect passes (93.80%)**,
  **40 failures**, **five context cases**, **one extension**, two existing allowed
  extensions, two exclusions, **zero infrastructure errors**, **655 total cases**.
  Exit 1 records retained compatibility failures. The denominator is six smaller
  because context/extension outcomes are now separate, not implemented SQL.
  All seven targeted statuses are recorded in results.json. An initial live
  comparison rejected an incorrect naive-timestamp extension expectation; the
  final exact policy checks the existing UTC TIMESTAMPTZ contract.
- Current-source offline reclassification of the seven retained raw live outcomes
  after final malformed-row/composed-outcome guard hardening reproduces all
  targeted statuses. These hardening checks did not change shipping code.
- `check_issue359.py ... --triage`: **188/188 triaged**; strict pass checking still
  requires actual dialect passes and remains nonzero for retained differences.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: passed all packages; integration
  skips are expected in this unit-only invocation.
- Focused Software `TestSQLSetParameters` with `-race`: passed, no skip/failure.
- `make -C go build`: passed with the version linker flag.
- Native SQLAlchemy typed-binding/RETURNING/reflection test: **1 passed, 4
  deselected**, including the dialect and both SHOW version assertions. An initial
  invocation omitted REDIS_ADBC_LIBRARY and failed setup; the configured rerun
  passed without weakening assertions.

No full Software Go suite, performance claim or SQL/Core certification is made.
The existing forty dialect failures remain visible. Unit-only success is not
Redis compatibility evidence; the live comparison supplies the bounded evidence.

## Review

Agent review followed REVIEW.md: checked fail-closed source binding, empty/error
outcomes, exact type/value/duplicate preservation, endpoint identity expectations,
unchanged fixtures/allowlist, and honest score denominators. Shipping changes
are limited to the requested version advertisement and SQLAlchemy metadata;
no storage, ownership, ACL or retry semantics changed. Human approval, hosted CI
and merge are not asserted by this local record.
