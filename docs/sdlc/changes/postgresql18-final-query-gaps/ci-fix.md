# PR #381 CI follow-up

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Source: `6eaa43f469fe675dadc03ed9675bf5690b445450` plus the files identified in
[follow-up results](ci-fix-results.json). The original [results](results.json)
remain historical evidence for the initial implementation.

## Failure and correction

Both Software CI architectures in [the original run](https://github.com/redis/redis-adbc-driver/actions/runs/42899246967)
failed the same regressions. The tuple-aggregation allowlist still recognized
DATE_PART but omitted the new internal EXTRACT name. The existing count-group
spill tests and small query-shape fixtures therefore rejected the optimized path.
Add EXTRACT to the existing recursive expression admission; retain its exact
numeric evaluation, spill/reduction value checks and all admission assertions.
This is a SQL regression fixture check, not a ClickBench performance run.

The empty-schema test compared a value-resolved unconstrained numeric result
(decimal128(4,0) for the fixture's year) with an execution that intentionally reads
no rows. Empty/schema-only execution retains the existing canonical decimal256
(76,16) declaration. The test now independently asserts the complete fetched
schema, all empty forms, ExecuteSchema, full CTAS and empty CTAS against those
exact contracts. It does not infer precision from rows an empty query must not
read. Other columns keep exact schema checks; no assertions or skips are removed.

The interval test still expected float-style epoch/second display. Its values
now assert numeric scale explicitly: 93600.000000 and 30.500000.
No corpus SQL, allowlist, numeric contract, storage protocol or snapshot ownership
behavior changes.

## Checks

- Complete unit suite with REDIS_URI unset: passed; endpoint-dependent skips are
  not Redis compatibility evidence.
- Dedicated Redis Software / Redis 8.8.0, three shards, verified active 4x QPF,
  six Search workers, mnp routing, 32 connections and noeviction. Started with
  `make -C go redis-up`; live checks entered via `software-setup.sh --exec`.
- All original failing tests plus the query-gap regression passed in 8.152s:
  `go test -count=1 -p 1 -run
  '^(TestSQLCountExpressionSpillGroups|TestSQLClickBenchFollowupShapes|TestSQLEmptySchema|TestSQLIntervals|TestSQLPG18QueryGaps)$' .`.
- Full unchanged PostgreSQL comparison and case-by-case regression accounting are
  recorded in [follow-up results](ci-fix-results.json).
- `gofmt`, `git diff --check`, documentation links and source hashes checked.

Hosted amd64/arm64 reruns start on push; their live checks are separate evidence.
Raw logs and infrastructure identifiers remain private. Review followed
[REVIEW.md](../../../../REVIEW.md); agent review is not maintainer approval.
