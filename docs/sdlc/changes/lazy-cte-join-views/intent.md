# Intent

2026-11-23; requested as a focused candidate for TPC-DS fixes before expanding
TPC-H work. Base: `07b24edb3c782037f48670f6cc499d66256a59e0`. Status: local
implementation with pure unit validation; no live performance claim or release.
See [spec](spec.md), [plan](plan.md), [validation](validation.md), and the
[TPC-DS Q4 issue #590](https://github.com/redis/redis-adbc-driver/issues/590).

Q4 reads the cached `year_total` CTE through six aliases. The join executor
previously made all six full copies of its row maps before applying their
different filters. Preserve one cached CTE while creating qualified row maps
only for an alias actually being scanned, then stop retaining unmatched maps.
Do not change predicates, CTE evaluation frequency, join ordering, numeric
semantics or result multiplicity. Managed Flex filtering and general join
streaming/spill are separate changes.
