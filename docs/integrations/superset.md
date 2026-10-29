# Redis integration guide for Apache Superset

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

## Install and connect

Use the driver's `superset` extra in a separate Superset 6.1/Python 3.12 environment
with SQLAlchemy 1.4 and PyArrow 18. Do not combine it with the driver's SQLAlchemy
2.x or latest pandas/Dask extras. See [dependency guidance](https://github.com/redis/redis-adbc-driver/blob/main/docs/python-integrations.md#sqlalchemy-and-superset).

In Superset, install the package in the same environment as every web worker and
SQL Lab worker, then restart them. The `superset.db_engine_specs` entry points
register Redis and Redis TLS; SQL parsing uses sqlglot's PostgreSQL dialect.
Add a database using `redis+adbc://host:6379/0`, test the connection, select a
schema and table, and create a dataset. `DATE_TRUNC` supplies PostgreSQL time
grains for time-series charts; grouped dimensions and `SUM` support bar charts.
Enable SQL Lab access as appropriate for that database. No PostgreSQL server or
psycopg dependency is needed. This does not add SQL constructs the driver lacks.

[All integration guides](README.md)
