# Specification

Date: 2026-11-23. Public stack base: `c73e5b66bf45d8c0eb1203171c04b399dbcbc4ca` (#607). Local candidate.
See [intent](intent.md), [plan](plan.md).

After every retained dimension has been prepared, derive necessary independently
bounded fact-key conditions from its existing join buckets. Admit only exact-
typed integer/DATE values and at most maxUnionTerms (1000) distinct non-NULL
values per equality pair. Missing/unsafe actual types, unsupported key types,
and over-cap sets skip only that pair; other safe pairs remain useful. A prepared
empty dimension or proven all-NULL equality set makes matching impossible;
unprepared nil buckets are not an emptiness proof. Preserve exact int64 values
above 2^53. Sort deduplication keys for deterministic query construction.

AND all admitted sets into the fresh private fact-filter list before scanning.
Do not mutate the original query, residual, cached sources, or dimension rows.
Independent sets may admit correlated cross-combinations; existing complete
compound hash probes, local filters, duplicate dimension buckets, and full
residuals still determine actual output/multiplicity. Existing native planner
encodes literals and preserves complete residual evaluation; guarded UNION
projections propagate only their established safe typed conditions.

This is necessary predicate pushdown, not disjoint fact batching or a new join
algorithm. Source/read/capability errors retain existing fail-closed behavior.
Unrelated nonmatching HASHes need not be read under the existing candidate
contract. Shared weekly aggregates gain no consumer year inference, and large
unfiltered calendar keysets remain over cap. No outer/lateral eligibility or
retained-execution fence changes. DATE/CAST local-filter preflight boundaries
remain unchanged and can independently select ordinary join fallback.
