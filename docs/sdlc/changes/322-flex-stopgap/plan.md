# Implementation and verification

Date: 2026-11-05. Source: 95d6583. See [intent](intent.md), [spec](spec.md).

1. Add the explicit database option and positive plain-schema/read probe.
2. Persist plain-index metadata; exclude sortable pipelines and route all scan
   consumers (including tag checks) through bounded numeric ranges.
3. Reject adoption, foreign indexes and schema rewrites; delete owned rows
   without DD, retaining unrelated data.
4. Test live Software Flex, both RESP protocols and injected server caps/errors;
   run unit checks and standard RAM cursor/schema regression checks.
5. Document restrictions and outstanding benchmarks. Human review and release
   approval remain outstanding; no issue closure or release is claimed.

Validation results are recorded below after the final run.

## Initial-worktree results (2026-11-05)

- `env -u REDIS_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...`: all
  three packages passed. Live integration cases skip without their URI;
  this command is unit evidence only.
- `REDIS_FLEX_URI=redis://localhost:22000/0 go test -count=1 -p 1 -run
  '^TestFlex' -v .`: all Flex cases passed on Software 8.2.6-25 / Redis
  8.8.0 / Search 8.8.8, RESP2 and RESP3. See the guide for fixture details.
- Isolated RAM Software fixture: Redis 8.6.2, one shard. Focused
  TestSQLAliasesInExpressions, TestSQLAlterMultiAtomic,
  TestSQLScanReadsExactValues, TestSQLScanAcrossCursorPages,
  TestSQLTruncate, TestCursorReplies and TestSearchCountReplies all passed.
  The existing port-6379 fixture initially returned EOF during connection;
  no compatibility conclusion was drawn from that failed configuration.
- `GOCACHE=/private/tmp/adbc-go-cache make -C go build`: c-shared macOS
  library built successfully. `git diff --check`: passed.

Disposable fixtures were isolated under adbc-issue322 and adbc-issue322-ram.
No Cloud, multi-shard Flex, TLS Flex, full ADBC validation or performance resource
benchmark was run. No maintainer review or release decision is recorded.

## PR port to current main (2026-11-05)

The source worktree had unrelated Git history. Transplanted only this change
onto upstream main 69facea, preserving its transport cancellation, cleanup,
telemetry and bounded-scan behavior. Current main requires RESP3, so the live
PR test uses RESP3 only. CREATE INDEX, materialized-view operations and
reader-only partition mode are explicitly excluded from this stopgap.

- Full unit suite passed on the PR branch (integration tests skipped without URIs).
- All TestFlex cases passed on an authenticated TLS Software 8.2.6-25 / Redis
  8.8.0 / Search 8.8.8 fixture with verified active 4x QPF, 6 Search workers,
  mnp routing and 32 connections.
- Shared-library build and git diff whitespace checks passed.
- The setup fixture's copied public CA certificate needed mode 0644 so its
  container user could read it; no private-key permissions were relaxed.
- Comparative benchmarks, Cloud, multi-shard Flex and full ADBC validation
  remain pending. Initial RAM regression evidence above belongs to the source
  worktree; current-main integration CI remains separate evidence.
