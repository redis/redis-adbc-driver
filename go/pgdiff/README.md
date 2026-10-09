# Postgres differential corpus

From `go/`, against an already running test Redis:

```sh
REDIS_URI=redis://localhost:6379/0 make pgdiff
make pgdiff-test
```

Requires Docker and uv. The target builds the current driver, starts ephemeral
Postgres **16.15**, pinned by its linux/amd64 manifest digest, and removes it on
exit. The reference uses UTF-8, `--locale=C`, UTC, ISO MDY dates and Postgres
interval display. Redis returns Arrow intervals directly. Every corpus file gets
a fresh `pgdiff_<uuid>` schema and fresh connections; transactions on every named
connection are rolled back before schema cleanup. Closing those connections also
cleans up temporary tables. No database-wide flush is used. Use a test database:
driver connections can recover abandoned driver operations.

## Measured baseline

Measured October 9, 2026 against Redis OSS 8.6.2 and the driver from main
`f0cf7e9`, with the pinned Postgres reference:

| Tier | Passed / scored | Pass rate |
|---|---:|---:|
| Compatibility issues | 62 / 191 | 32.5% |
| Postgres regression subset | 229 / 371 | 61.7% |
| Captured dbt SQL | 19 / 23 | 82.6% |
| Smoke cases | 13 / 13 | 100% |
| Additional type checks | 4 / 5 | 80% |
| **Total** | **327 / 603** | **54.2%** |

There are **607 cases** in total: the four unscored cases are two exactly
allowlisted extensions (`TIMESTAMP(9)`, `TRY_CAST`) and two explicit scope
exclusions (XML, DO/PL/pgSQL). No fixture or infrastructure errors occurred.
This measures the checked-in curated corpus, not full Postgres conformance.
Unresolved SQL compatibility failures remain visible in the report and belong
to the language issues under #274.

## Coverage and provenance

- [issue-coverage.json](issue-coverage.json) inventories #274's issue repro
  tables, including #187/#188. The SQL rows are covered with stable case IDs;
  parameterized LIMIT/OFFSET, quoted identifiers, metadata, constraint failures,
  rollback/savepoints, visibility and two-connection SKIP LOCKED have explicit
  fixtures. Client/tool integration issues #273/#275–#280 and the DB-API connect
  warning in #269 are recorded as outside the SQL corpus. Crash recovery and
  concurrent atomicity stress tests remain outside this deterministic runner.
- [postgres-coverage.json](postgres-coverage.json) records selected statement
  IDs and inclusive omitted-statement ranges with reasons, for all 17 requested
  regression areas. The original files from **REL_16_15**, commit
  `7d3e000c5961a544302072058a1184e9a588837b`, source URLs, SHA-256 checksums and
  PostgreSQL license are retained in `upstream/postgres`. These are independent
  subsets with reduced fixtures, not the full upstream regression database.
  External psql/COPY datasets, volatile queries, procedural features and later
  queries requiring more fixtures have explicit omission reasons.
- [dbt-coverage.json](dbt-coverage.json) maps the corpus back to captured SQLQuery
  events. Three passing functional tests ran against the pinned reference:
  upstream `BaseSimpleMaterializations.test_base`, `BaseIncremental.test_incremental`
  and the supplied dbt_utils fixture. `dbt-core`, `dbt-postgres` and
  `dbt-tests-adapter` were **1.9.1**; dbt_utils was **1.3.0**, commit
  `e164c645238ca98f21b2ecc558912b94b3d75a9f`. The 217 SQL events, seed CSVs,
  version manifest and licenses are in `upstream/dbt`. The importer chooses
  materializations, incremental writes, metadata, a join-based comparison,
  surrogate keys, safe division and union_relations. It removes catalog
  qualifiers, substitutes the isolated schema and stabilizes temporary suffixes;
  these adaptations are recorded, including source-string changes in
  union_relations. No credentials or external data are needed to replay them.

Rebuild the checked-in derived corpora offline from their pinned sources:

```sh
uv run --project pgdiff python pgdiff/import_postgres.py
uv run --project pgdiff python pgdiff/import_dbt.py
```

Postgres SQLSTATE expectations are recorded in `upstream/postgres/oracle.json`
from the pinned reference with these exact reduced fixtures. Their SQL hashes
are checked during import. They describe both successful queries and intentional
upstream error tests; they don't approve a Redis mismatch. Changing a fixture or
query requires reviewing its reference expectation.

To recapture dbt events, from the repository root (network required only for
capture dependencies and the pinned dbt_utils source):

```sh
uv run --no-project --python 3.12 \
  --with dbt-postgres==1.9.1 --with dbt-core==1.9.1 \
  --with dbt-tests-adapter==1.9.1 --with pytest --with 'psycopg[binary]' \
  --with adbc-driver-manager --with pyarrow --with sqlparse \
  python go/pgdiff/capture_dbt.py
```

Capture logs can interleave across threads. Reimport and review the resulting
case IDs/source hashes after recapture; normal test runs use the committed corpus.

## Fixtures and comparison

Each JSON file declares `area`, `tier`, optional source/provenance, `setup`
(single SQL statements), and `cases` with stable IDs. Cases run sequentially,
so mutations remain visible within the file. Independent mutation scenarios
belong in separate files. A failed setup marks `fixture-error`, blocks that file,
and doesn't enter the compatibility pass-rate denominator.

A simple case contains `sql`. Optional `params` use canonical `$1`, `$2`, etc.:
only placeholder tokens are translated to psycopg `%s` or ADBC `?`; literals and
comments are preserved, and repeated parameters keep their values. A scenario
contains `steps`, each with SQL and an optional named `connection`; each name
gets a separate paired connection to the same schema. Every step's outcomes are
retained, and any failing step makes the scenario fail. This measures transactions
and locks rather than faking their behavior through autocommit.

`{{schema}}` and `{{catalog}}` are controlled environment substitutions. Optional
`context_values` maps a column index to exact expected Postgres/Redis values,
for example `{"0":{"postgres":"postgres","redis":"redis"}}`. Only those
correct values compare as equivalent; arbitrary catalog values don't disappear.
Raw outcomes stay unchanged in the report. `expect_postgres` declares the
expected SQLSTATE, or null for success; a changed reference outcome is a fixture
error rather than a dialect score.

Rows compare as multisets, preserving duplicates, unless an outer `ORDER BY`
constrains output order. Subquery, window and aggregate ordering don't order the
result. `ordered: true/false` can override detection. Choose total outer ordering
when ties matter; the window subset adds a documented empno tie-breaker. Avoid
unordered LIMIT samples except explicitly deterministic recursive generators.
The script scanner handles PostgreSQL E/dollar quoting, nested comments and psql
commands without discarding intentionally invalid Unicode/error-test literals.

Errors compare SQLSTATE; missing SQLSTATE never counts as a pass. Transport
errors are infrastructure failures. Arrow decoding failures are reported as
compatibility differences, without aborting the rest of the corpus. Floats
compare exactly (including signed zero/NaN), with no tolerance. Decimals retain
all digits but ignore trailing fractional zeros. JSON text compares as returned.
Nanosecond timestamps retain all digits without requiring pandas.

## Type comparisons and limits

Physical comparisons include integer/float widths, bool, binary, string, date,
time, timestamps with/without zone, decimal and interval. The report retains
Postgres OIDs/typmods and Arrow type details. A case may opt into extra checks,
e.g. `"type_checks":{"0":["precision","scale"]}` for a declared numeric column,
or `"type_checks":{"0":["unit"]}` for a timestamp.

Declared numeric precision/scale are decoded from typmods. Temporal typmods are
mapped to the corresponding Arrow unit (1–3 fractional digits use ms; 4–6 use us),
with declared precision also retained. Checks must use fixtures with sufficient
metadata: arbitrary-precision expression results often have no numeric typmod.
Arrow cannot identify a declared timestamp precision within one storage unit.
High-precision TIME and BC/out-of-Python-range Arrow date/timestamp values still
have representation limits; conversion errors stay visible.

Postgres text/varchar/bpchar/name, JSON/JSONB and timetz map to physical Arrow
strings. Arrow result metadata alone cannot prove those semantic identities.
The type corpus therefore also queries information_schema names and exercises
JSON/JSONB and timetz values; it does not claim native JSON storage is equivalent
to VARCHAR. Unmapped arrays/composites/user-defined result types retain their
Postgres OIDs and are not silently equated to unrelated Arrow types.

## Exceptions, reports and CI

`allowlist.json` maps `relative/file.json:case-id` to reason, issue and exact
normalized Postgres/Redis outcomes. Only those snapshots are allowed; changed
outcomes fail and a fixed case passes. Unresolved compatibility bugs aren't
allowlisted. An explicit `unsupported` declaration requires a documented scope
reason plus an expected SQLSTATE, or the complete exact Redis error when its
SQLSTATE is missing. A changed message/state then fails; missing functions or
parser errors aren't generally excluded.

The runner prints counts/rates by file, area and tier, plus every unexpected
difference. `build/pgdiff-report.json` includes both outcomes, step results,
provenance, grouped summaries and infrastructure errors. Allowed, unsupported,
blocked and fixture errors are separate from `pass / (pass + fail)`.

Exit codes: **0** all scored cases match; **1** compatibility differences;
**2** fixture, startup, transport or cleanup failures. The manual
[pgdiff workflow](../../.github/workflows/pgdiff.yml) publishes the JSON and
console artifacts, reports dialect gaps without failing the job, and fails for
fixture/infrastructure problems. CI is optional and doesn't run on every PR.

```sh
make pgdiff PGDIFF_ARGS='--corpus /path/to/corpus --report build/other-report.json'
```
