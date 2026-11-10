# Validation

Date: 2026-11-10. Driver source: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Host: macOS ARM64. Redis Software 8.8.0, three primary shards, Search enabled,
verified active 4x QPF, six Search workers, mnp routing/32 connections, TLS,
noeviction. PostgreSQL: 18.6 (Debian 18.6-1.pgdg13+2), disposable container.
No throughput benchmark was performed.

Sling: official v1.6.6 `sling_darwin_arm64.tar.gz` release, unmodified binary
SHA-256 `d1cee049d677c9c2f43a661f21a714ba7e9cc00296b406ba3233ea0c9ec2e115`.
Its automatically installed ADBC driver manager reports version 1.12.0.
Upstream source inspected at `9d5186194e76e3e146b7d6367a6e27a961242dce`;
that main-branch inspection is not a claim about the release's exact source SHA.

Commands: `make -C go redis-up`, `make -C go build`, then
`python3 scripts/test_sling_postgres.py --sling go/build/sling/sling`.
The harness reruns both setup/build gates before opening ADBC connections.

| Check | Actual outcome |
| --- | --- |
| Redis ADBC connection | Passed with verified TLS |
| Stock PostgreSQL -> Redis full-refresh | Failed: missing `schemas` template |
| Empty Arrow result | Failed: Sling bounds panic reading row zero |
| Configured PostgreSQL -> Redis, iteration 1 | Passed, 10,003 rows |
| Redis -> PostgreSQL full-refresh, iteration 1 | Passed, 10,003 rows |
| PostgreSQL duplicate-aware full-row comparison, iteration 1 | Passed, zero differences |
| Configured PostgreSQL -> Redis truncate reload, iteration 2 | Passed, 10,003 rows |
| Redis -> PostgreSQL full-refresh, iteration 2 | Passed, 10,003 rows |
| PostgreSQL duplicate-aware full-row comparison, iteration 2 | Passed, zero differences |
| Test schema and PostgreSQL container/volume cleanup | Passed |

Seven columns cover bigint, text, boolean, numeric(18,2), date, timestamp(6), and
double precision. Additional edge rows include both signed int64 boundaries,
negative 18-digit decimal, a pre-epoch timestamp, empty string, NULL fields and
an exact duplicate row. NULL text and Unicode/multiline/quoted text appear across
the generated rows. Two reloads establish that truncate does not append duplicates.

Initial exploratory 10,000-row round trip also passed with zero differences.
Exploration reproduced PostgreSQL-template `current_catalog` failure and the
empty-batch panic on staging metadata. No skips in the configured transfer checks;
the stock/empty-result failures remain failures. Gated Arrow lane, empty sources,
incremental/upsert, other platforms, failure atomicity, and exact DDL preservation
were not validated. Raw operational evidence remains under ignored `go/build/`.

Review: checked ownership/cleanup, private logs, full-fetch and duplicate-aware
comparison, truthful failure reporting and links. No driver implementation
changed; the full Go suite was not required for this external integration test.
Human review/approval has not been inferred.

Integration-listing follow-up, 2026-11-10: Sling is listed as entry 51 in the
support/installation inventories and in the README and guide index. Its guide is
registered with the standalone CLI runner, separately from Python profiles.
`python3 -m unittest discover -s scripts -p test_integration_registry.py -v`
passed all 10 offline registry/runner contracts. Added local documentation links,
both inventories' sequential 1–51 numbering, and `git diff --check` passed.
Unchanged live transfers were not rerun for the listing edits. The upstream
report is [Sling #836](https://github.com/slingdata-io/sling-cli/issues/836),
created with the repository template's existing `enhancement` label.

PR #371 CI correction, 2026-11-10: reproduced the helper failure in
`test_audit_covers_every_comparison_row` (51 comparison entries versus the
asserted 50, with Sling missing from the acceptance-audit table). Added the
standalone Sling disposition to that audit and updated the exact count without
removing the full table-equality assertion. The acceptance-runner suite then
passed all 13 tests; the CI-matrix suite passed all 9 tests. Added local audit
links and `git diff --check` passed. No live driver behavior changed.


PR #371 merge repair, 2026-11-10: merged main at `2c50655c` into PR head
`efb71b15`. Resolved the support-inventory conflict by preserving workflow groups
and placing Sling (#51) under ETL / ELT & integration (nine tools). Area totals
are now 28 data engineering and 23 data science & BI tools; historical evidence
counts retain their original scope. Checked all 51 numbered entries occur once,
with the original row contents preserved except the Sling display-name fix below.

The installation-coverage contract reproduced a preexisting PR failure: the
support table used a Markdown-linked Sling name while the installation table
used plain text. Made the support-table name plain text, retaining the adjacent
Sling ADBC, guide, validation and upstream-report links. The existing equality
assertion remains intact.

Checks after repair: registry 10 passed; acceptance runner 13 passed; CI matrix
nine passed; installation coverage one passed; zero skips in these offline tests.
Using the existing private docs virtual environment, `scripts/prepare_docs.py`,
`python -m mkdocs build --strict`, and `scripts/check_docs.py` passed (42 pages).
`git diff --check` passed. No live transfers or Go suites were rerun for this
inventory/merge repair; earlier Software/QPF evidence is historical validation,
not a new run. Reviewed resolution against REVIEW.md for row preservation,
truthful evidence scope and documentation links; no human approval is inferred.
