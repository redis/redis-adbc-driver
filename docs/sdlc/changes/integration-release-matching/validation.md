# Validation

2026-11-26. Documentation-only change in this repository; no driver code,
workflow or SQL behavior changed, and no Redis suite was run for it.

## This repository

- `scripts/prepare_docs.py`, `mkdocs build --strict` (mkdocs 1.6.1) and
  `scripts/check_docs.py` passed. That covers 45 HTML pages, local links/assets,
  canonicals, sitemap, search and publication scope.
- The new runbook link renders to the GitHub anchor
  `docs/sdlc/release.md#driver-matched-integration-releases-2026-11-26`, matching
  its heading. The change record's relative links resolve. `git diff --check` is
  clean.
- Linked destination PRs: redis-adbc-integrations#2 and redis-adbc-duckdb#1 are
  merged; redis-adbc-integrations#3 and redis-adbc-duckdb#2 were open.
- `adbc-driver-redis` 0.0.31 is the latest PyPI version. `redis-adbc-integrations`
  had no PyPI release, so the documentation keeps source installation until one
  appears.

## Destination repositories (recorded from their PRs)

- The first changes' PR CI passed. That covers every framework suite against the
  v0.0.31 source build, and the DuckDB 2.0 build, SQLLogicTests and Linux runtime
  harness. Post-merge `main` runs passed in both repositories.
- The follow-up PRs record local unit tests (13 helper tests), simulations of
  release resolution, archive verification, version stamping and pin bumps, and
  a stamped 0.0.31 package build requiring `adbc-driver-redis[dbapi]==0.0.31.*`.
  Their release workflows can run only after merge. The PyPI upload also needs
  the Trusted Publisher.

Not established here: a published downstream release, a PyPI upload of
`redis-adbc-integrations`, or a community-registry build that embeds v0.0.31.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
