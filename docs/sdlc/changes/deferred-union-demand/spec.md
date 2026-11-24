# Specification

Date: 2026-11-23. Dependency: [PR #603](https://github.com/redis/redis-adbc-driver/pull/603). Status: draft.
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Deferred source eligibility

Only uncorrelated UNION ALL trees with identity types at each pairwise set-operation boundary qualify. Leaves are direct-column projections or checked literal/cast constants. Reject shared, recursive or DML CTEs; unsafe or volatile expressions; branch joins; grouping, windows, DISTINCT, ORDER BY, pagination, branch WHERE and correlated reads. Projection-only wrappers over an eligible deferred source remain eligible. Existing eager behavior remains the fallback. Shared CTE results are not truncated for a first consumer.

Retain the original pairwise tree, output positions, branch order and duplicate rows. Keep the same executor, params, pinned source metadata and snapshot/ownership fences. Deferred output remains materialized when consumed; this draft does not promise bounded streaming of the fact output.

## Demand and join order

Push only total, exact-typed integer/date column-versus-literal comparisons, bounded OR sets and safe conjunctions. Other predicates stay in the consumer's original residual. A dynamic join restriction contains at most 1,000 distinct non-NULL keys per compatible equality pair. Composite demand conjuncts independently safe key sets; unsafe or over-cap pairs are skipped independently. Cross-combinations can remain, so the original composite join must recheck pair correlation and preserve multiplicity. A proven empty key set forbids right matches; the unchanged real LEFT join still supplies null extension. RIGHT/FULL join behavior is not rewritten.

The ordering rule runs only in the existing all-inner, non-lateral reorder path. It can postpone only a connected deferred UNION to collect an unconnected stable memory relation or pinned managed snapshot dimension whose saturating product estimate remains at most 1,000. It rejects live/unmanaged/lazy/view/group/join dimensions and unknown sources. Cardinality estimates are not runtime admission: existing work/retained-memory limits still own actual row counts. Larger/unsafe shapes retain the existing connected-first order.

## Alias and semantic boundaries

Join qualification and explicit derived column aliases clone the lazy output mapping and rename by position, without mutating cached originals. Every branch and projection wrapper observes the final consumer column names. The independent alias regression exposed stale output keys when metadata had been renamed while the lazy map retained old names; `renameColumns` now clones/remaps both together.

Do not push a consumer year predicate through a shared weekly aggregate. Weeks spanning years require all facts in the selected weeks; any future shared-demand optimization must combine requested week keys across consumers while preserving dimension multiplicity. This draft does not implement it. The LEFT-joined web-return UNION branch of Q5 remains ineligible. No Q2/Q5 fix claim follows from unit tests.

Draft implementation: [PR #606](https://github.com/redis/redis-adbc-driver/pull/606).
