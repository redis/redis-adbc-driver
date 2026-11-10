# PostgreSQL 18 recursive LIMIT validation

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Status: two targeted #361 cases pass; three #361 corpus cases remain unresolved.
Source: base `ce62690afed3c115eba561cb7ad7cbfa631b06f4` plus file contents identified
by SHA-256 in [sanitized results](results.json). This branch builds on merged
PR #376 in PR #375's branch; it does not include later main-only release changes.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Result and environment

The unchanged, fully fetched curated corpus reports **599/651 scored passes
(92.01%)**, **52 failures**, two allowed extensions, two explicit exclusions and
**zero infrastructure errors** (655 total cases). All **597 prior passes remain
passing**. The only gains are `postgres/with.json:statement-0010` and
`statement-0011`. Corpus SQL, allowlist and exclusions are unchanged. The retained
#359 inventory now has 147 passes and 41 deferred failures. The original eleven
#360 cases remain failures. The full runner still exits 1 for mismatches (`make`
exits 2); the pass rate is not complete PostgreSQL compatibility or SQL conformance.

Reference: pinned PostgreSQL 18.6 linux/amd64, C collation. Target: Redis Software /
Redis 8.8.0, three shards, verified active 4x QPF, six effective Search workers,
mnp routing, 32 connections and noeviction. The dedicated fixture was started
with `make -C go redis-up`; every live command entered via
`software-setup.sh --exec`, whose management gate checks the effective settings.
Raw logs, infrastructure details and endpoints remain private/ignored.
The corpus retains REL_16_15 provenance; it is not the full upstream regression suite.

## Implementation and checks

A consumer-local context bound identifies its exact CTE and required row count.
Only a sole owned recursive CTE, direct unfiltered column/star projection and
finite resolved LIMIT/OFFSET qualify. Both terms must have at most one output
row per iteration. The loop stops before evaluating the next unrequested row.
UNION duplicate suppression, natural termination and existing row/iteration caps
remain intact. Bounded results bypass the shared CTE cache. A direct regression
reuses the executor and CTE definition to verify a subsequent full consumer sees
all eight rows. Parameter re-execution checks that each bound is resolved afresh.

- Pre-fix unit regression reproduced the recursion-cap error with a LIMIT consumer.
- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: passed. Redis-dependent
  skips in this unit-only run are not live compatibility evidence.
- `make -C go pgdiff-test`: 36 harness tests passed.
- Verified Software `go test -count=1 -p 1 -run
  'Test.*Recursive|TestSQLLateral|TestSQLWith|Test.*Empty' .`: passed. Includes
  existing recursive SEARCH/CYCLE/cap tests, lateral execution and empty schemas,
  plus new consumer, cache, parameter, row-cap and cancellation regressions.
- Separate adversarial corpus `pgdiff/adversarial/recursive361`: **20/20** passed
  against PostgreSQL. Includes UNION/ALL, duplicate cycles, NULL/zero/negative and
  expression limits, offsets near int64 maximum, early errors, empty seeds,
  natural termination, sorted/filtered/aggregate/join/shared consumers and wider
  finite frontiers. These cases do not change the scored corpus denominator.
- Unchanged full `make pgdiff` report: 599 passes as above. Case-by-case comparison
  to the preceding JSON batch confirms zero regressions.
- `gofmt`, `git diff --check`, relative documentation links, triage accounting and
  source hashes checked.

Review followed [REVIEW.md](../../../../REVIEW.md): correctness/lifecycle,
ownership/security and evidence. The one-row frontier proof avoids evaluating
additional rows within an eagerly computed iteration. Demand is never applied
to filters, sorting, aggregates/windows, joins, subqueries, shared definitions,
SEARCH/CYCLE or wider frontiers. Retained partition recursion remains unsupported.
No storage, snapshot, index or ACL protocol changes. Agent review is not maintainer
approval; no assertions, skips or allowlists were weakened.

## Remaining scope

Three #361 corpus failures remain: planner folding in a row-dependent CASE,
EXTRACT numeric identity and derived text identity through CTAS. General recursive
streaming beyond the restricted progression shape also remains outside this batch.
The other corpus failures stay visible. See [spec](spec.md) for admission details
and the [PostgreSQL WITH documentation](https://www.postgresql.org/docs/18/queries-with.html)
for the broader demand-evaluation behavior.
