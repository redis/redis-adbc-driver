# Intent: analytics clients and installation

Date: 2026-10-20. Source: `a1eec58`. Status: implementation and verification.
Requestor: repository user in this Codex chat. Author: Codex. No human review or
release approval is implied. [Spec](spec.md), [plan](plan.md), [evidence](validation.md).

## Problem and consumers

Issues [271](https://github.com/redis/redis-adbc-driver/issues/115),
[273](https://github.com/redis/redis-adbc-driver/issues/117),
[275](https://github.com/redis/redis-adbc-driver/issues/119),
[276](https://github.com/redis/redis-adbc-driver/issues/120),
[277](https://github.com/redis/redis-adbc-driver/issues/121),
[278](https://github.com/redis/redis-adbc-driver/issues/122) and
[280](https://github.com/redis/redis-adbc-driver/issues/124) request useful
analytics integrations, predictable installation and explicit compatibility.
Python users otherwise distribute absolute native-library paths themselves;
Superset needs a dialect and engine registration, Dagster needs asset storage,
and DuckDB needs runnable examples and checks.

## Outcomes and scope

Ship platform wheels with DB-API helpers and URI aliases, opt-in native manifests,
an analytics SQLAlchemy/Superset connector, a Dagster IO manager, and DuckDB
smoke tests/documentation. Recheck existing COLLATE support rather than changing
its byte-order contract. Test current upstream behavior: adbc_scanner's #19/#20
are already closed and autocommit catalog writes work in its current build.

## Exclusions and unresolved decisions

No fake transactions, PyPI publication, registry registration, external upstream
messages, release upload, production configuration or automatic privileged install.
Real Redis multi-statement transactions remain a separate project (#269).
Superset UI clicks and chart rendering are distinct from server-side query smoke
tests. Linux release executions require CI; local native validation is macOS arm64.
