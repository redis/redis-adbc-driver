# Plan

Date: 2026-11-26. Requestor: Albert Wong. Source: d65f3b52c9be0a2013711b0716137533f13e9d67. Status: released for driver v0.0.31; community-registry update in review.

1. Destination repositories: pin driver v0.0.31 and add nightly driver-`main`
   runs. Merged as
   [redis-adbc-integrations#2](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/pull/2)
   and [redis-adbc-duckdb#1](https://github.com/Redislabs-Solution-Architects/redis-adbc-duckdb/pull/1).
2. Destination repositories: driver-matched releases and latest-release CI, in
   [redis-adbc-integrations#3](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/pull/3)
   and [redis-adbc-duckdb#2](https://github.com/Redislabs-Solution-Architects/redis-adbc-duckdb/pull/2)
   (open when this record was first written; both merged on 2026-11-26).
3. This repository: update `README.md`, `docs/integration-installation.md`,
   `docs/python-integrations.md`, `docs/duckdb.md`, the six moved-integration
   stubs under `docs/integrations/` and `docs/sdlc/release.md`.
4. Owner actions outside these changes: add the PyPI Trusted Publisher for
   `redis-adbc-integrations` (workflow `driver-release.yml`, environment `pypi`),
   and update the DuckDB community-registry `ref` after each extension release.
5. After the first `redis-adbc-integrations` release reaches PyPI, replace the
   interim source-install wording with plain PyPI instructions. Done on
   2026-11-26 after 0.0.31 was published; registry-dependent wording in
   `docs/duckdb.md` and `docs/driver-reference.md` waits for
   [duckdb/community-extensions#3815](https://github.com/duckdb/community-extensions/pull/3815)
   to merge and publish.

See [intent](intent.md), [spec](spec.md) and [validation](validation.md).
