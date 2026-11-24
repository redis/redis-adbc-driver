# Isolated query results

Scope: TPC-H Q19 only; full suites remain stopped. No full-suite score is implied.

Each successful engine has one warm-up and three fully fetched, validated timed runs.
Values above 1 mean slower than the native DuckDB reference. Failed validation gets no ratio.

| Query | Engine | Median (ms) | Ratio to native DuckDB | Status |
|---|---|---:|---:|---|
| 19 | Native DuckDB | 8.437 | 1.00× | ok |
| 19 | Direct Redis ADBC | 12555.884 | 1488.14× | ok |
| 19 | DuckDB over Redis ADBC | — | — | error |

Failure details:

- Query 19, DuckDB over Redis ADBC: INTERRUPT Error: Interrupted!

The third engine uses the DuckDB planner over a read-only Redis ADBC catalog.
Whole-query remote pushdown is disabled; scan pushdown remains extension-dependent.
