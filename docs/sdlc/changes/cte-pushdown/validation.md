# Validation

2026-11-28 (UTC). Base: main `b77df5d7`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **SF1 Q39 on Software Flex** now returns DuckDB's 243 rows in 6.5 s; main times out at 60 s. Validation is described [below](#tpc-ds-sf1-on-flex).
- **All 99 TPC-DS queries** validate at SF 0.1.
- **Same-fixture A/B against main at SF 0.1:**
  - Q31 is 5× faster, Q39 4.6× and Q64 1.1×;
  - the other queries are unchanged.

## Unit tests (no Redis)

`cte_pushdown_test.go`, on an in-memory fixture shaped like Q39: a CTE over a grouped derived table, with NULL keys and a NULL sum.

- **`TestCTEPushdownPlan`:** the rows each FROM item's CTE relation holds.
  - Two references on a grouping column share 7 of the 8 groups.
  - An inner join keeps all 8, because its filters cover every month.
  - A single reference keeps 1.
  - A filter on an aggregate output (`s > 15`) reaches the CTE's projection but not below its `GROUP BY`.
  - Nothing is pushed for a LEFT JOIN, an unqualified reference or a conjunct with a subquery.
- **`TestCTEPushdownSharesOneEvaluation`:** two pushed references read the same evaluation.
- **`TestCTEPushdownResults`:** every case returns the same rows with and without the pushdown (`ctePushdownEnabled`). So do derived-table queries with a window function, a `LIMIT` and a `ROLLUP`.
- **`TestCTEPushdownRejectsUnsafeBodies`:** CTE bodies with `LIMIT`, a window function, `ROLLUP`, a volatile function, `DISTINCT ON` or a scalar aggregate keep every row.
- **Failures recorded first:**
  - With only the toggle defined, the four pushable plan cases failed: every item held all 8 rows.
  - The first implementation evaluated each reference separately. The shared-evaluation test and the changed counts were then written and failed against it before the final version.
- **Unit-only suite:** `cd go && env -u REDIS_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` passes; exit 0.
- **Formatting and vet:** `gofmt -l` reports nothing. `go vet` reports only an existing warning in `live_ingest_creation_integration_test.go`.

## Deployment

The TPC-DS fixture of [#729](https://github.com/redis/redis-adbc-driver/pull/729) and [#730](https://github.com/redis/redis-adbc-driver/pull/730):
- Redis 8.8.0 Flex, 1 shard, 8 GiB RAM tier of 64 GiB, verified active 4x QPF before and after every run, TLS, AOF every second, Search disk settings 8/2/3 MiB.
- TPC-DS SF1 in schema `tpcds` and SF 0.1 in `tpcds01`, managed tables, gated for compaction.
- A 4-CPU / 8 GiB Linux client in the server's network namespace.
- Another session's Redis container shared the Docker host.

**Libraries:**
- main `b77df5d7`: `6e33f948…`, built from a `git archive` of that commit.
- This change: `f7915355…`.
- An interim build that evaluated each reference separately: `8e6fda42…`.

## TPC-DS SF1 on Flex

| Run | Library | Result |
|---|---|---|
| Harness, `--query 39` | main | timeout in the warm-up: full-inventory streamed join (45,879 chunks), 939,714 rows read in 60 s |
| Harness, `--query 39` | interim (per reference) | **validated**, 8.34 s median (warm-up 13.17 s; 9.99 / 8.34 / 7.13 s), 396,069 rows read |
| Harness, `--query 39` | this change | 6.48 s median (warm-up 7.30 s; 6.48 / 6.79 / 6.25 s), 378,064 rows read; recorded `validation_failed` by the harness's own artifact check |
| Direct fetch and multiset comparison with DuckDB (relative 1e-7, absolute 1e-6) | this change | **243 rows, equal to DuckDB**, 7.64 s |

**Why the harness record failed:** the validator's scratch sort file `sort-0` read back with the recorded size and SHA-256 (`5f7b3ea9…`) but decoded 0 objects. That is the harness read-back problem seen for Q72 and Q78, which a separate harness change addresses. The direct comparison above therefore stands in for the harness validation.

The two months Q39 keeps match 360,000 inventory rows. Before this change the CTE read the whole year, 2.34M rows.

## TPC-DS SF 0.1 on Flex

`supervise.py tpcds --reuse-loaded` with this change: **99 of 99 validated**, summed medians 95.0 s.

**Same-fixture A/B,** harness focused runs alternating the libraries:

| Query | main | change | main | change |
|---|---:|---:|---:|---:|
| Q1 | 0.25 s | 0.26 s | 0.24 s | 0.26 s |
| Q31 | 1.85 s | **0.36 s** | 1.92 s | **0.37 s** |
| Q39 | 0.41 s | **0.09 s** | 0.44 s | **0.09 s** |
| Q64 | 3.17 s | **2.70 s** | 3.01 s | **2.83 s** |
| Q75 | 1.75 s | 1.78 s | 1.81 s | 1.86 s |
| Q30 | 0.17 s | 0.15 s | 0.16 s | 0.19 s |
| Q81 | 0.22 s | 0.22 s | 0.22 s | 0.22 s |
| Q72 | 2.72 s | 2.73 s | 2.73 s | 2.84 s |

- **The pushdown applies to Q31, Q39 and Q64** (filters below their CTEs' `GROUP BY`), and to Q75 (only into `all_sales`'s `WHERE`: its derived table is a `UNION`).
- **The per-reference interim build** made Q64 slower: 5.18 and 5.00 s against main's 3.19 and 3.12 s, because it computed `cross_sales` twice. That led to the shared evaluation.
- **Q1's 1.24 s** in the interim build's suite pass did not reproduce in the A/B. Its conjuncts are not pushable.

## Not covered

- **Other SF1 queries:** Q4, Q64, Q75 and Q78 still exceed 60 s. Pushed by hand into their CTEs, Q4, Q64 and Q78 timed out at 180 s, and Q75 took 97 s.
- **Not pushed into:** set-operation bodies, outer joins, derived tables written in FROM and views.
- **The integration suite and the PostgreSQL differential corpus** were not run locally; see the PR's CI.
- **Review:** human review is pending.
