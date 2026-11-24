# Intent

2026-11-24; base `c27bb970` (main). Requested by the TPC-H issue work for
[#599](https://github.com/redis/redis-adbc-driver/issues/599): TPC-H SF1 Q19 through
DuckDB `adbc_scanner` (`query_pushdown false`) hit the 60-second deadline while direct
Redis ADBC completed in about 3.8 s. Status: local fix, not released. See
[spec](spec.md), [plan](plan.md), [validation](validation.md).

The scanner sends plain projected table scans to the driver. For Q19 it sends
`SELECT … FROM "public"."lineitem" WHERE "l_shipinstruct" = ?` (about 1.5M of 6M
rows at SF1) and an unfiltered `part` scan. Filtered managed snapshot scans read
Search candidates with keyset pages `(query) @__rowid:[(after +inf] SORTBY __rowid
LIMIT 0 page`. Search evaluates every remaining match before selecting each page,
so one scan costs O(rows²/page): measured per-page latency falls as `after`
advances, and a bounded row-ID window costs a small constant instead.

Make filtered candidate scans linear in matched rows without changing results,
visibility, ordering, identity checks or the number of queries for selective
predicates. Out of scope: the 256-row candidate page used when a residual is
present, scanner-side filter pushdown (DuckDB optional `IN` hints are dropped by
the scanner), parallel scanner partitions (Query-farm/adbc_scanner PR #22) and the
direct-route optimizer work in other TPC-H issues.
