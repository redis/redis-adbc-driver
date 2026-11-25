# Validation

Date: 2026-11-24. Driver source: `ea16062cf9d997b83137930acfa12842db814ac8`
(`redis/main`) plus this change's harness. [Intent](intent.md),
[specification](spec.md), [plan](plan.md).

Host: macOS ARM64. Redis Software 8.8.0, three primary shards, Search enabled,
TLS, noeviction, in a dedicated Compose project. `make -C go redis-up` passed its
gates for active/effective 4x QPF, six Search workers and mnp routing/32
connections before each run. PostgreSQL 18.7 (Debian 18.7-1.pgdg13+5) in a
disposable container. ADBC driver manager 1.12.0, installed by Sling. No
throughput benchmark was performed.

Sling: **pre-release**, not the official release. Built at the user's request
from the upstream `v1.6.8` branch at `df2c03887b1328165604e9f4fcc05481f0c160d8`
with the release's build steps (`fts5` tag, cgo, the goreleaser `go mod` edits)
and version `v1.6.8.dev/2026-11-25.df2c0388`. Binary SHA-256
`7bbabf8135bf9a2df7c2e40c968aa50b2c8c4474796e60c482be1166845494eb`. A public
source build replaces `conns discover` with a stub. Sling's update check reported
an official dev build at the same commit; it was not downloaded or tested.

Command: `python3 scripts/test_sling_postgres.py --sling <built binary>`, with the
fixture's `COMPOSE_PROJECT_NAME` and `SOFTWARE_*_PORT` exported. The harness
reruns the setup gates and `make -C go build` before connecting. The connection
used no template override.

## Final run

| Case | Outcome |
| --- | --- |
| Redis connection, schema and two precreated tables | Passed |
| `SELECT label FROM <schema>.inbound WHERE 1=0` | Passed (Sling 1.6.6 panicked) |
| Default full-refresh into a new Redis table | Passed, 10,003 rows |
| Redis → PostgreSQL export and exact comparison | Passed, zero differences |
| Default full-refresh replacing that table | Passed, 10,003 rows |
| Redis → PostgreSQL export and exact comparison | Passed, zero differences |
| `conns discover --columns` | Unavailable: omitted from the source build |
| Truncate/direct into precreated table, iterations 1 and 2 | Passed, 10,003 rows each |
| Redis → PostgreSQL export and exact comparison, both iterations | Passed, zero differences |
| Empty source, default full-refresh | Passed; no target or staging table left |
| Empty source, `SLING_ALLOW_EMPTY=TRUE` staging | Passed; empty table created |
| Empty source, truncate/direct into the loaded table | Passed; table emptied |
| Empty Redis table → PostgreSQL, `SLING_ALLOW_EMPTY=TRUE` | Passed; empty table created |
| No `_tmp` staging table remains | Passed |
| Redis schema and PostgreSQL container/volume cleanup | Passed |

27 of 28 cases passed; the harness exited nonzero because `conns discover` was
unavailable. The committed harness clears inherited `SLING_ALLOW_EMPTY` and
`SLING_ALLOW_EMPTY_TABLES`. Its last run had `SLING_ALLOW_EMPTY=TRUE` set in the
caller's environment and produced the same outcomes, including no table for the
default empty-source case. The seed data, edge rows and seven column types match the
[Sling 1.6.6 record](../sling-postgres/validation.md).

Observed DDL (`information_schema.columns`):

- Redis table created by default staging, also for the empty source: `id bigint`,
  `label text`, `active boolean`, `amount numeric(38,9)`, `day date`,
  `happened_at timestamp(6) without time zone`, `ratio double precision`. The
  source declares `amount numeric(18,2)`.
- PostgreSQL copies from both the precreated and the staged Redis tables match the
  source except `amount`, which is unconstrained `numeric`. Sling 1.6.6 had also
  produced `label varchar(65500)`.

## First run

The same build ran the first version of the rewritten harness, whose empty-source
cases did not set `SLING_ALLOW_EMPTY`. All value transfers passed with zero
differences. Both empty full-refresh transfers succeeded without creating their
targets, so the Redis and PostgreSQL count checks failed. Sling logs this as its
default ("To allow Sling to create empty tables, set SLING_ALLOW_EMPTY=TRUE"),
for both target types. The final harness tests the default and the opt-in
separately and checks for leftover staging tables. `conns discover` failed with
the source build's "please use the official build" stub.

## Other checks

- Harness control flow, offline with stub `sling`/`docker`/`make` commands:
  all cases passing; a simulated staging failure marking its eight dependents
  `not-run` while independent cases still ran; nonzero exit; cleanup still ran.
- `scripts/prepare_docs.py`, `mkdocs build --strict` (mkdocs 1.6.1) and
  `scripts/check_docs.py`: passed, 45 pages. `git diff --check`: passed.
- `PYTHONPATH=scripts:python/scripts python3 -m unittest
  scripts.test_distribution.InstallationCoverageTests scripts.test_acceptance_runner
  scripts.test_integration_registry scripts.test_ci_test_matrix`: 34 passed, no
  skips. One test commits in a temporary repository and was rerun outside the
  command sandbox, where commit signing works.
- No driver code changed, so the Go suites were not run.

## Not validated

The official Sling 1.6.8 release and `conns discover`, other platforms,
incremental/upsert modes, failure atomicity, Sling-created Redis schemas, the gated
Arrow lane and exact DDL preservation. Raw logs remain under ignored `go/build/`.
Nothing failed on the Redis side, so no upstream comment was posted.
