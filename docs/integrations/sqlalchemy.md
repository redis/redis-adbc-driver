# Redis integration guide for SQLAlchemy

Redis must run separately with the Query Engine (Search). See
[installation and driver discovery](../python-integrations.md#install-and-discover-the-driver)
for Python/native setup, supported wheel platforms, TLS and source builds.
Tables must be created, ingested or adopted through the driver; arbitrary Redis
keys do not automatically become SQL tables. Writes autocommit without rollback.

This guide describes current repository capabilities. A newly added helper or
extra may require a build containing its implementation. If a released wheel
lacks it, build the native driver from this checkout and install the relevant
extra from `./python`; set `REDIS_ADBC_LIBRARY` to the built library. Do not infer
that an older published wheel contains every helper documented here.

## SQLAlchemy and Superset

Install the wheel's `sqlalchemy` extra for SQLAlchemy >=2.1.6. Use the separate
`superset` extra in a Python 3.12 environment for Superset 6.1, which requires
SQLAlchemy 1.4 and older pandas/greenlet dependencies. The `superset` extra uses Arrow 18; the other extras use Arrow 26.
These extras cannot share an environment; Superset also cannot share the latest
pandas used by `dask` and `test`. The dialect supports both SQLAlchemy versions. It compiles PostgreSQL SQL and reflects
the driver's catalogs, including schemas, tables, views, column types, primary
keys, indexes and comments. Redis version metadata comes from ADBC; PostgreSQL
catalog compatibility is version 16, not the Redis server version.

```python
from sqlalchemy import create_engine, inspect, text

engine = create_engine("redis+adbc://localhost:6379/0")
print(inspect(engine).get_schema_names())
with engine.connect() as conn:
    print(conn.execute(text("SELECT :value AS value"), {"value": 42}).all())
engine.dispose()
```

Use `rediss+adbc://` for TLS. Connection options can be passed as
`create_engine(..., connect_args={"db_kwargs": {"adbc.redis.default_schema": "analytics"}})`.
URI query parameters follow the native driver's Redis URI options.

This is an analytics dialect. Only `AUTOCOMMIT` isolation is accepted. SQLAlchemy's
begin/commit/rollback housekeeping is a no-op; a failed context or `rollback()`
does **not** undo writes. Savepoints and two-phase transactions are rejected.
Use explicit values for IDs; ORM identity generation and transactional workloads
are outside this connector's contract.

[All integration guides](README.md)
