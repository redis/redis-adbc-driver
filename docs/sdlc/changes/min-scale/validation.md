# MIN_SCALE validation

Date: 2026-11-17. Source baseline: `19ebcb46`, plus the working-tree MIN_SCALE
change described by [intent](intent.md), [spec](spec.md) and [plan](plan.md).

## Regression and unit checks

Before implementation, `TestMinScale` failed: every intended supported call
returned 42883 because no `min_scale` function existed. After implementation:

- `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_FLEX_URI go test -count=1 -p 1 -run '^(TestMinScale|TestFuncRegistry)' .`: passed.
- The same focused selector with `go test -race -count=1 -p 1`: passed (2.605 s).
- Parent task: full uncached, sequential unit-only `go test -count=1 -p 1 ./...`: passed; Redis-dependent cases skip without endpoints.
- Parent task: `make -C go build`: passed, with the Makefile version linker flag.
- Parent task: focused `make -C go test-software` race run passed all eight selected top-level tests: `TestFuncRegistryEvaluates`, `TestFuncRegistryCoversEvaluator`, `TestFuncRegistryShape`, `TestSQLMinScale`, `TestSQLBoundMinScale`, `TestMinScale`, `TestMinScaleResolutionBeforeRows`, and `TestMinScaleBoundValues`. No matching test skipped.

The owned Software fixture had three primaries, Redis 8.8.0 with Search,
management-verified active/effective 4x QPF (six Search workers, mnp routing,
32 connections), noeviction, TLS/RESP3. The client was macOS arm64 through
Docker's published-port route. These are correctness checks, not performance
measurements. Operational identifiers and credentials remain private.

Unit regressions cover exact finite decimals, signs/zeros, fractional trailing
zeros, integer widths, NULLs, unknown exponent/underscore literals, typed invalid
arguments, parameter type/value handling, no-row planning and unchanged caller
coefficients. Integration fixtures cover managed numeric(76,6) rows, empty int32
schema and decimal128 bound batches with NULLs and retained input ownership.

## Independent PostgreSQL oracle

A dedicated local PostgreSQL 18.6 `postgres:18` container was queried through its
local socket with `docker exec`, with no published endpoint. These are read-only
scalar oracle queries, not Redis tests or benchmarks. The first opportunistic
oracle fixture disappeared before the batch; it was not counted as a passing
batch. The complete dedicated-fixture results are retained in
[postgresql-oracle.json](postgresql-oracle.json), without operational identifiers.

Thirteen query groups confirm expected finite values/int32 SQL identity, integer
coercions, unknown literals, strict NULLs, six absent signatures (42883), invalid
unknown literal (22P02) and scale overflow (22003) even over empty input. The last
group explicitly records PostgreSQL returning NULL for NaN/Infinity: the driver's
existing numeric special-value rejection remains unresolved under #362. This is
not a full PostgreSQL compatibility claim.

No ClickBench, performance windows or shipping dependency changes were performed.
The dedicated PostgreSQL oracle container was removed after the read-only checks;
Software fixture cleanup remains owned by the parent task.

## Review

Independent subagent review followed the repository's correctness/lifecycle,
input ownership and intent/evidence passes. No blocking finding remained.
A test-only input-mutation assertion was corrected to snapshot the coefficient
before evaluation and check it unconditionally afterward. This is an agent
review, not human maintainer approval.
