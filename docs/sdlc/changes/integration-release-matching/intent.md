# Intent

Date: 2026-11-26. Requestor: Albert Wong. Source: d65f3b52c9be0a2013711b0716137533f13e9d67. Status: documentation; destination automation in review.

After the [2026-11-18 extraction](../extract-integrations/intent.md), both destination
repositories built and tested a fixed driver commit (`3b40dd59`) that is in no
driver release. Driver releases v0.0.30 and v0.0.31 therefore never reached
DuckDB extension users. `redis-adbc-integrations` was not on PyPI, and its
`adbc-driver-redis>=0.1.0` floor could not be satisfied from PyPI.

The requested outcome: each driver release gets a matching DuckDB extension
release and a matching `redis-adbc-integrations` release on PyPI. Both share the
driver's version and are tested against that release, and both repositories test
the latest driver `main` every night. This repository's documentation must
describe that installation and release relationship.

Excluded: changing this repository's release workflow or making driver releases
wait for downstream repositories, the DuckDB community-registry update
(DuckDB-managed), and PyPI Trusted Publisher configuration (account owner action).

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
