# Preliminary validated full-data comparison

Checkpoint: 2026-10-21T20:36:20.206761+00:00 UTC. **11 of 43 queries**, 22 hot-trial outputs independently matched to complete typed DuckDB references. The full 129-trial sweep is still running; no aggregate ranking is assigned.

All Redis queries use 99,997,497 rows and the official PostgreSQL SQL. Hot means the minimum of trials two and three; both hot trials must match references before a comparison is eligible. Exact scalar values and column order are checked, with relative 1e-10 / absolute 1e-8 float tolerances. The three baseline files were checked against official upstream JSON at the pinned commit.

**Hardware is unequal:** Redis uses 48 c6a.4xlarge primaries plus one c6a.4xlarge client (784 vCPUs / 1,568 GiB aggregate host RAM). Each published baseline below uses one c6a.4xlarge. These ratios describe the selected deployments and do not establish equal-resource or equal-cost product rankings.

Redis is slower than the selected ClickHouse and DuckDB results on every checked query, despite the larger cluster. It is faster than the selected PostgreSQL baseline on these 11 queries. This is a partial finding for these exact versions/settings, not a product-wide conclusion.

| Query | Redis seconds | ClickHouse seconds | DuckDB seconds | PostgreSQL seconds |
|---|---:|---:|---:|---:|
| Q01 | 0.422804 | 0.001000 | 0.018000 | 258.348000 |
| Q02 | 96.978912 | 0.001000 | 0.041000 | 258.405000 |
| Q03 | 1.823886 | 0.034000 | 0.074000 | 258.393000 |
| Q04 | 86.520085 | 0.039000 | 0.086000 | 258.399000 |
| Q05 | 122.128880 | 0.242000 | 0.348000 | 272.345000 |
| Q06 | 29.399197 | 0.416000 | 0.338000 | 278.518000 |
| Q07 | 1.014479 | 0.012000 | 0.030000 | 258.363000 |
| Q08 | 1.639181 | 0.010000 | 0.038000 | 258.412000 |
| Q09 | 159.574347 | 0.479000 | 0.443000 | 284.296000 |
| Q10 | 280.696100 | 0.539000 | 0.612000 | 286.070000 |
| Q11 | 148.857686 | 0.138000 | 0.150000 | 259.033000 |

Sources: [clickhouse](https://github.com/ClickHouse/ClickBench/blob/e90045879276f75cc398f1ab8a73541a7716058d/clickhouse/results/20261008/c6a.4xlarge.json), [duckdb](https://github.com/ClickHouse/ClickBench/blob/e90045879276f75cc398f1ab8a73541a7716058d/duckdb/results/20260511/c6a.4xlarge.json), [postgresql](https://github.com/ClickHouse/ClickBench/blob/e90045879276f75cc398f1ab8a73541a7716058d/postgresql/results/20260921/c6a.4xlarge.json).

[Machine-readable ratios and checks](comparison.json), [source verification](data-verified.json), [reference manifest](references/manifest.json), [baseline provenance](baseline-sources.json), [checkpoint with all 43 SQL definitions](full.json). Raw hot outputs are in `full-raw/`; independent expected values are in `references/`. Only the first 11 queries are covered by this checkpoint.

Reproduce with the local ClickBench Python dependencies: `python validate_checkpoint.py`. It checks query/source provenance, every hot output and its SHA256, and equality of recorded trial times before accepting the checkpoint.
