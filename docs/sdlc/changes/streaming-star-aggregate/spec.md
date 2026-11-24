# Specification

Draft implementation: [PR607](https://github.com/redis/redis-adbc-driver/pull/607).

Date: 2026-11-23. Status: local candidate.
See [intent](intent.md), [plan](plan.md), [verification](verification.md).

Only serial grouped aggregation opts in. Group keys must be simple exact typed
columns without floating, interval or fixed-character representative ambiguity.
Admit order-insensitive COUNT/SUM/AVG/MIN/MAX, without DISTINCT, aggregate FILTER
or ordering. Admit exact column inputs, pure searched CASE, and decimal arithmetic; division requires
a nonzero numeric literal. Checked integer arithmetic, subqueries and volatile
functions remain eager. The existing accumulator evaluates the original AST and
retains decimal scale, exact sums, NULL handling and overflow diagnostics.

A qualifying join has a largest fact input and at most 262,144 source dimension
rows combined, using immutable published physical row counts or already-cached
in-memory lengths. Private owned source directories retain their existing
executor before any source reads because their counts are not immutable
published admission bounds. Observe the decoded dimension cap as a corruption defense.
All edges must be same-type simple column equalities directly linking the fact
to a dimension. Reject outer/isolated/lateral/merged joins, presence tags,
derived sources and other unsafe residual expressions. Pure local comparisons
and boolean combinations use the existing planWhere and paged residual path. Zero-row
sources retain the original short-circuit path. Retained executions keep their
original budgeted executor.

Build dimension hash buckets once, preserving every duplicate. Existing paged
managed scans retain lease, exact-directory visibility, authoritative HASH decode
and failure checks. Qualify cached in-memory facts a page at a time without
mutating source rows or ASTs. Probe each fact row through dimension buckets and
emit at most 256 completed rows per callback, without retaining a complete fact
or join output. Frame compound keys by length, preserving embedded NUL bytes.
Needed hidden source provenance passes through the same owned merged maps.

The existing grouped accumulator retains its group states and representatives;
this change bounds join intermediates, not group cardinality or arbitrary field
byte sizes. Input enumeration/group discovery order can change. Exact eligible
aggregate results do not depend on it; SQL row ordering remains governed by the
query's ORDER BY. Independent source errors may be encountered in a different
order because dimensions are prepared first. All fail closed. Save a reduction
callback error and finish authoritative fact scanning, allowing a later source
proof/decode/transport failure to supersede it. Cancellation is checked throughout.

A physically larger dimension with a pure local pushed filter may be attempted
without treating physical rows as kept rows. Count every decoded kept dimension
row across all buckets. If the actual cap is exceeded, discard all buckets and
fall back before any fact output; repeated immutable reads have no side effects.
Native transport/ACL/proof failures still fail closed and never trigger fallback.
Pure CASE predicates contain only simple typed comparisons/boolean combinations;
all branch expressions must satisfy the original exact-input guard. Evaluate the
original CASE AST lazily in the existing accumulator. Cancellation is checked
again after the final callback, including a final partial batch.
