# Intent

2026-11-24; base `14fcd545` (main). Requested by the TPC-H issue work for
[#599](https://github.com/redis/redis-adbc-driver/issues/599), following
[#634](https://github.com/redis/redis-adbc-driver/pull/634). Status: local change, not
released. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

DuckDB `adbc_scanner` with parallel scans (upstream Query-farm/adbc_scanner PR #22,
`scan_threads`) calls `ExecutePartitions` once and reads each partition with an
independent `ReadPartition`. For TPC-H Q19 the lineitem scan is
`SELECT … FROM "public"."lineitem" WHERE "l_shipinstruct" = ?`, which matches about a
quarter of the rows.

A managed snapshot partition reader always read its whole row-ID span from exact
directories and applied the WHERE clause client-side, with 256-row pages because a
residual was present. A selective partition therefore fetched about four rows for
every row it returned. On a dedicated 10% fixture, eight or sixteen parallel readers
were no faster than one ordinary filtered scan (median Q19 1.84 s and 1.64 s, against
1.51 s for a single stream).

Make a filtered partition read only its matching rows, as an unpartitioned filtered
scan does, without changing results, visibility, identity checks or partition shape.

Out of scope:
- unfiltered and non-indexable partition predicates, which stay on exact directories;
- wider pages for the directory path with a residual (measured, no gain);
- scanner-side filter pushdown;
- the pending full-memory SF1 third-path A/B for #599.
