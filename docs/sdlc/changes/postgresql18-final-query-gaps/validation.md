# PostgreSQL 18 final query-gap validation

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Status: all three targeted cases validated; broader scope remains explicit.
Source: base `b14aea157a5abaa753b8f58ad375787006c7c646` plus file contents
identified by SHA-256 in [sanitized results](results.json).
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

See the [CI follow-up](ci-fix.md) for the aggregation-admission correction and
exact empty-schema/interval assertions after the original hosted run.

## Result and scope

The unchanged, fully fetched curated corpus reports **602/651 scored passes
(92.47%)**, **49 failures**, two allowed extensions, two explicit exclusions and
**zero infrastructure errors** (655 total cases). All **599 prior passes remain
passing**. The three gains are `dbt/utils.json:event-0207-0`,
`postgres/case.json:statement-0020` and `postgres/window.json:statement-0041`.
All nineteen retained #361 cases now pass. The retained #359 inventory has 150
passes and 38 deferred failures; the original eleven #360 failures remain.
The corpus SQL and allowlists are unchanged. Full comparison still exits 1 for
mismatches (`make` exit 2). This is neither complete PostgreSQL compatibility,
the full upstream regression suite nor SQL conformance.

Reference: pinned PostgreSQL 18.6 linux/amd64, C collation. Target: Redis Software /
Redis 8.8.0, three shards, verified active 4x QPF, six effective Search workers,
mnp routing, 32 connections and noeviction. The dedicated fixture was started
with `make -C go redis-up`; each live command used `software-setup.sh --exec`
with its management-backed gates. Operational logs and identifiers remain private.
Input fixtures retain their REL_16_15 provenance.

## Changes and review

- EXTRACT uses a separate internal function and returns numeric, while DATE_PART
  returns double precision. Fractional fields use integer components and big
  integers, preserving microseconds for large and negative epochs, intervals,
  times and time zones. Existing unconstrained-numeric Arrow shape resolution
  applies; NULL/empty outputs keep its canonical declaration. Unconstrained SUM
  must retain the input's actual scale rather than fixing it to zero.
- CASE planning checks immutable constant operators and a small allowlist of
  pure functions. It prunes provably dead constant branches and retains runtime
  row-dependent/volatile evaluation. Literal query facts propagate through
  eligible derived/sole-reference CTE projections and aliases, never from
  observed stored row values. Shared CTEs retain their planning boundary.
  This is scoped analysis, not PostgreSQL's complete optimizer or arbitrary
  function inlining. COALESCE now stops at its first non-NULL argument, including
  all-NULL and bound-parameter result typing.
- MD5 returns logical text. CTAS/catalog propagation already retains that flag;
  stored string decoding now also preserves logical identity when fetching it.
  Physical Arrow storage remains UTF8. No metadata protocol, publication,
  index, ACL or snapshot ownership invariant is relaxed.

Review followed [REVIEW.md](../../../../REVIEW.md). Independent edge comparisons
caught eager COALESCE evaluation, lost facts after a derived column rename,
scale-zero rounding in window SUM and lost stored-text identity; these were
corrected before final checks. Agent review is not maintainer approval.

## Checks

- Focused pre-fix unit cases reproduced all three mismatches. PostgreSQL oracle
  checks additionally distinguished a single constant VALUES input (dead branch
  pruned) from a real table variable (constant error raised during planning).
- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: passed. Redis-dependent
  skips in this pass are not live compatibility evidence.
- `make -C go pgdiff-test`: 36 harness tests passed.
- Separate `pgdiff/adversarial/query361`: **34/34** PostgreSQL comparisons passed,
  covering CASE/boolean/COALESCE pruning, sole/shared CTEs, aliases, row-dependent
  and volatile branches, exact large/negative epochs, interval/timetz fields,
  NULLs, errors, CASE/window numeric propagation and CTAS/stored text identity.
  These cases remain outside the scored denominator.
- Verified Software selection: query-gap, window, datetime/time-zone, conditional,
  numeric/decimal, CTAS, metadata and aggregate regressions passed. The command
  used `go test -count=1 -p 1 -run` with the selection recorded in [results](results.json).
  This is a focused sweep, not the full Software package suite.
- Full unchanged `make pgdiff`: 602 passes and zero regressions as above. A
  discarded focused invocation supplied a file where the runner requires a
  directory and ran zero cases; it is not compatibility evidence.
- `gofmt`, `git diff --check`, source hashes, relative links and retained-case
  triage accounting checked.

Old temporal assertions expected BIGINT/float8 for EXTRACT. They now assert
numeric and its exact decimal display/scale, following the independent
PostgreSQL result-type and value checks. Numeric epochs no longer display in
float scientific notation. No assertions were removed, skips broadened or
oracles/allowlists changed.

## Limits and references

The other 49 failures remain visible. General recursive streaming outside the
previous single-frontier scope, the complete PostgreSQL planner, numeric special
values and full-range temporal representation remain separate work. Driver
nanosecond precision remains its documented extension; this batch preserves
existing precision rather than silently rounding values through float64.

Primary references: [conditional expressions](https://www.postgresql.org/docs/18/functions-conditional.html),
[date/time functions](https://www.postgresql.org/docs/18/functions-datetime.html),
[string functions](https://www.postgresql.org/docs/18/functions-string.html).
