# Plan and verification

Date: 2026-11-23. Base: `0bed7ce5`. Isolated prototype.
See [intent](intent.md), [specification](spec.md).

1. Prove full residual totality and source-value shape before consuming rows.
2. Bind only necessary integer/bool clauses, then qualify fewer private maps.
3. Keep complete original residual evaluation and normal source ownership.
4. Parent independently reviews before any native Q4 activation; native7 or later
   parallel-source work may independently resolve the memory limit.

Focused pure verification passed1.064s, from `go/`:
`env -u REDIS_URI GOMAXPROCS=2 go test -count=1 -p 1 -run
'^(TestMemoryAlias|TestNativeQ8|TestNativeDimension|TestNativeLookup|TestDeferredDimension|TestDeferredComposite|TestDeferredLeft|TestStarFactDemand|TestDeferredUnion|TestUnionStar|TestStar)' .`
Six alias-focused tests include a real parsed six-alias shared CTE: exactly six
maps are copied from30 source rows before string/decimal residuals reduce them
to one output. Tests assert private ownership/cleanup, metadata slot order,
NULL/default/missing/type handling, earlier division error and volatile effect
preservation, cancellation, nil context, and retained fallback. Formatting/diff
checks passed. No live queries, source artifacts, native builds, containers,
benchmark runs, issue changes or publication occurred. Independent review and
native memory/performance validation remain pending.

## Dynamic NUMERIC and literal-only year follow-up

A realistic parsed SUM/UNION six-alias fixture reproduced the eligibility gap:
alias a refused filtering because unbounded NUMERIC metadata has a declared
scale while actual exact SUM Values preserve runtime scales. The follow-up
accepts only decimal Values under unbounded NUMERIC metadata with matching
non-typmod attributes, preserving the exact coefficient/type/scale and typed
NULLs. Fixed typmods, malformed values and other kind/attribute mismatches still
fall back before copies.

Private rebound predicates may fold at most16 checked non-NULL integer-literal
arithmetic operations within128 visited nodes. Functions, columns, parameters,
unknown/NULL literals and casts are never evaluated. Any overflow/division/error
refuses the optimization without raising early; the untouched full residual
keeps its original short-circuit/error order. Special column roles are refused
before substitution can erase their binding flags.

The actual SUM/UNION fixture with second-year syntax2001+1 now selects necessary
year filtering for all six shared aliases: six maps copied from30 each, with
runtime NUMERIC scales and typed NULL groups preserved. Added tests verify
both earlier errors and division/overflow in short-circuited later arms, literal
operation bounds/exclusions, exact coefficient/type preservation and special
column-role fallback. Final combined focused command above passed1.096s after
the realistic fixture was observed red before the fix. Formatting/diff checks
passed. This remains private/unactivated pending independent review and native
validation; no external SQL, artifacts, containers or publication changed.
