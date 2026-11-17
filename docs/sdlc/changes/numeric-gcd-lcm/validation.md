# Verification and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Base: `75220f2f` plus this change. Local verification complete; PR review pending.
[Intent](intent.md), [spec](spec.md), [plan](plan.md), [sanitized results](results.json).

## Reproduction and implementation

Before this change, finite fractional GCD/LCM returned 42883. New focused scalar
regressions reproduce that failure. The PostgreSQL 18 probe independently returns
0.15 and 3.6 for inputs 1.2 and 0.45; the driver rejected both. Initial test
construction mistakes (using a string-rendering helper as a Value and duplicate
cast typmods) were corrected; they are not implementation failures or passes.

Decimal operands now use copied integer coefficients at the maximum input scale
(clamped to zero for negative declared scales), with exact GCD and LCM arithmetic.
Results use Arrow decimal256 precision 76; oversized results fail with 22003.
No float conversion or mutation of input coefficients occurs. Existing integer
arithmetic is unchanged. Explicit NUMERIC precision/scale is recommended; the
driver's existing default for unconstrained NUMERIC remains a separate limit.

## Live checks

Owned Redis Software / Redis 8.8.0, Search, three primary shards, noeviction,
authenticated verified TLS/RESP3. `make -C go redis-up` and the per-run setup gate
verify active/effective QPF 4x, six Search workers, mnp routing, 32 connections.
Client: macOS arm64 through Docker published port. Reference: pinned PostgreSQL
18.6 with C collation. Raw reports and infrastructure details remain private.

- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages passed; Redis
  integration tests skip without their endpoint, so this is unit-only evidence.
- Focused Software race run `Test.*NumericGCDLCM`: **five top-level tests passed,
  zero failures/skips**. Covers exact finite fractions, signs/zeros, mixed scales,
  large coefficients, NULLs, unsupported non-NULL argument types, overflow,
  source preservation, managed reads/UPDATE, empty-result schema and Arrow-bound
  decimal128 input producing exact decimal256 coefficients/scale. An initial
  bound test compared display strings that trim zeros; it now checks the exact
  integer coefficients and declared Arrow scale independently.
- [Retained edge comparison](../../../../go/pgdiff/regressions/numeric-gcd-lcm.json):
  **67 passes, one explicit scope exclusion, zero failures/infrastructure errors**.
  Includes a 50-case sign/zero/decimal matrix, mixed integer/decimal inputs,
  typed NULLs, scale queries, negative declared scales, 38-digit fractional
  precision, large values, range boundaries and unsupported bool/text/float types.
  The exclusion is a PostgreSQL result beyond 76 digits: the driver must return
  22003. It is not a passing equivalence case. This file is separate from the
  unchanged 655-case curated corpus.
- Full curated PostgreSQL 18 comparison on the final native candidate:
  **605/645 dialect passes, 40 failures, five contexts, one extension, two allowed
  extensions, two exclusions, zero infrastructure errors**. All seven #364
  policies retain their expected status; no prior passing case regressed.
- `make -C go build`: passed with the version linker flag. No dependency override.

Reproduce from go/: `software-setup.sh --exec make pgdiff
PGDIFF_ARGS='--corpus pgdiff/regressions --report build/numeric-gcd-lcm.json'`.
The regression directory is deliberately outside the scored curated corpus.

## Remaining gaps and review

[Issue #475](https://github.com/redis/redis-adbc-driver/issues/475), created with
the existing bug label, records independently reproduced pre-existing ambiguity
errors for unknown/smallint GCD calls. Overload resolution, invalid typed-NULL
and empty-relation signature errors are not fixed by arithmetic alone. Numeric
nonfinite representation remains #362, and #110 retains its broader overload,
formatting and function surface. No full GCD/LCM or PostgreSQL/Core claim.

Agent review followed REVIEW.md: checked integer-only coefficient math, negative
scales, zero/overflow, aliases and input ownership, typed Arrow schema/value
agreement, no replay/storage changes, unchanged fixtures and full failure counts.
No unresolved finding was identified within the finite arithmetic scope. This
is not human approval, hosted CI, a merge or a full Software Go suite result.
