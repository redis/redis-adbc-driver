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

Install the wheel's `sqlalchemy` extra for SQLAlchemy >=2.1.9.

Superset 6.1 is unsupported on Python 3.14+. The Superset adapter moved to the
[redis-adbc-integrations guide](https://github.com/Redislabs-Solution-Architects/redis-adbc-integrations/blob/main/docs/integrations/superset.md);
[#471](https://github.com/redis/redis-adbc-driver/issues/471) is closed, and no
Superset 7.x run is recorded in this repository.

```python
import os
from uuid import uuid4
from sqlalchemy import create_engine, inspect, text

engine = create_engine(os.environ["REDIS_URI"].replace("://", "+adbc://", 1))
name = "guide_sa_" + uuid4().hex
print(inspect(engine).get_schema_names())
with engine.connect() as conn:
    try:
        conn.exec_driver_sql(f'CREATE TABLE "{name}" (id integer, label text)')
        conn.execute(text(f'INSERT INTO "{name}" VALUES (:id, :label)'),
                     {"id": 1, "label": "O'Reilly"})
        conn.execute(text(f'UPDATE "{name}" SET label=:label WHERE id=:id'),
                     {"id": 1, "label": "雪"})
        with engine.connect() as reader:
            assert reader.exec_driver_sql(f'SELECT * FROM "{name}"').all() == [(1, "雪")]
        conn.execute(text(f'DELETE FROM "{name}" WHERE id=:id'), {"id": 1})
        assert conn.exec_driver_sql(f'SELECT * FROM "{name}"').all() == []
        print("REDIS_ADBC_READ_WRITE_OK")
    finally:
        conn.exec_driver_sql(f'DROP TABLE IF EXISTS "{name}"')
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

## Rerun this guide

From the repository root, with the guide's dependencies and native driver
installed, set `REDIS_URI` to the dedicated local Software fixture and run:

```sh
python python/scripts/check_integration_guide.py sqlalchemy
```

This executes the guide's code blocks or its linked runnable example. For
Superset it exercises the documented backend workflow in an isolated application;
it does not automate the browser UI. See the
[per-guide results](../sdlc/changes/integration-guides-262/validation.md).

[All integration guides](README.md)
