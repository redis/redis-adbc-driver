# Specification: Airflow get_records

Date: 2026-10-29. Source: `1619837`. Status: implemented.
[Intent](intent.md), [plan](plan.md), [evidence](validation.md).

## Contracts

- `adbc_driver_redis.airflow.RedisAdbcHook` subclasses the Arrow provider hook.
- `get_conn()` uses the Redis DB-API facade, preserving database/connection
  options, TLS URIs, entrypoint and library overrides without mutating extras.
- `run()` defaults to autocommit, including inherited `get_records()` and
  `get_first()`. Explicit false is rejected before opening a connection.
- `set_autocommit(False)` is rejected; true uses upstream driver-level setting
  and Python bookkeeping. Native transaction behavior remains unchanged.
- Preserve bound parameters, empty results and per-statement result lists.
- Airflow dependencies are optional; CI runs them in an isolated environment.

## Boundaries and risks

The wrapper is imported explicitly by Python tasks; selecting an `adbc` connection
in a generic SQL operator still selects the upstream hook. Inherited inserts need
`autocommit=True`. Writes cannot roll back. Upstream API changes require retesting.

## Acceptance evidence

Reproduce upstream failure, then compare fully fetched rows for constants, bound
values, empty and multiple results, inserted values and NULL. Test transaction
rejection, connection-option forwarding and adjacent package installation checks.
