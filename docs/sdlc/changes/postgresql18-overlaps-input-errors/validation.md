# PostgreSQL 18 overlaps and input errors: validation

Date: 2026-11-10. Author: Codex. Source base: `61043e1a`.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and [sanitized results](results.json).

The unchanged curated corpus reports **604/651 scored passes (92.78%)**, **47
failures**, **2 allowed extensions**, **2 explicit exclusions**, and **0
infrastructure errors**. Both targeted cases now pass: `issue-266.json:row-19`
and `postgres/float8.json:statement-0022`. Every one of the previous 602 passes
is preserved. The denominator, upstream SQL, oracle and allowlist are unchanged.
The full harness still returns exit 1 for the 47 known failures; this is not a
claim that the complete PostgreSQL corpus passes.

Separate adversarial coverage reports **131/131 passes**, including a single
256-row Cartesian endpoint/NULL matrix, reversed/touching/singleton ranges,
positive and negative interval lengths, TIME/TIMETZ/TIMESTAMP/TIMESTAMPTZ,
unknown literals and ambiguous/wrong signatures, operator precedence, spring
and fall DST transitions, diagnostic overflow/underflow/invalid/valid inputs,
NULL scalar records, field selection, logical text metadata through CTAS,
FROM/LATERAL/ROWS FROM and ordinality. These cases are outside the scored corpus.
Native Arrow nanoseconds are covered by a separate unit regression.

Environment: pinned PostgreSQL 18.6 linux/amd64, C collation; Redis Software,
Redis 8.8.0, 3 shards, active verified 4x QPF, 6 Search workers, mnp routing,
32 connections and noeviction. `software-setup.sh --exec` verifies the effective
management configuration before each live command. Raw reports, fixture
identifiers, credentials and logs remain private under ignored build/tmp paths.

Checks:

- Both original gaps reproduced before implementation.
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages pass; live tests
  skip in this unit-only run.
- `make -C go pgdiff-test`: 36 harness tests pass.
- Focused live Software Go suites pass: new bound-parameter and empty Arrow
  schema tests, temporal/timezone, whole-row, GenerateSeries, parameter schema,
  empty-result and previous query-gap regressions. A review regression also
  verifies that WHERE false cannot hide malformed unknown temporal literals.
- Full differential comparison and separate adversarial run as above.
- Retained #359 audit: 152/188 pass, 36 deferred; 188/188 triaged. The original
  eleven #360 failures are separate. Broader #110 remains open.

One intermediate final-corpus attempt encountered a startup 40001 catalog
conflict while another live suite used the same dedicated database. It produced
no compatibility measurement and is excluded from scored evidence. The final
corpus run was serialized after live Go testing completed.

Review followed `REVIEW.md`: SQL NULL/timezone/precision/type/error behavior,
record shape and Arrow metadata, read-only data ownership, and evidence/scope.
No unresolved actionable finding remains in the implemented scope. This agent
review is not human approval. Diagnostic target types, exact numeric limits and
array-shape limits are explicitly scoped in the spec; no full PostgreSQL or
SQL:2023 conformance claim is made. No release or merge was performed here.
