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

## First releases (2026-11-26, later the same day)

After the destination PRs merged and the PyPI Trusted Publisher was added,
manually dispatched `Driver release` runs published both matched releases:

- [redis-adbc-integrations run 45894168051](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/actions/runs/45894168051).
  It passed every framework suite against the PyPI `adbc-driver-redis` 0.0.31
  wheel, created GitHub release `v0.0.31`, and uploaded `redis-adbc-integrations`
  0.0.31 to PyPI requiring `adbc-driver-redis[dbapi]==0.0.31.*`. The PyPI wheel
  and sdist SHA-256 digests match the GitHub release assets.
- [redis-adbc-duckdb run 45894304221](https://github.com/Redislabs-Solution-Architects/redis-adbc-duckdb/actions/runs/45894304221).
  It built and tested on Linux amd64/arm64 and macOS arm64, then published
  `v0.0.31` with three platform archives and checksums. The tag commit pins
  driver `d68f9a21` (driver v0.0.31); no pin move was needed. Its tagged source
  still declares `v0.1.0`. The destination's follow-up commits each release's
  version into the source.

Not established here: a community-registry build that embeds v0.0.31
([duckdb/community-extensions#3815](https://github.com/duckdb/community-extensions/pull/3815)
was open, with its build running).

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
