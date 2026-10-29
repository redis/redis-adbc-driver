# Redis integration guide for pandas

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

## pandas SQL reads

Install `adbc-driver-redis[dbapi]` and pandas to pass an open ADBC
connection directly to `read_sql` or `read_sql_query`:

```python
import pandas as pd
from adbc_driver_redis import dbapi

with dbapi.connect("redis://localhost:6379/0") as conn:
    df = pd.read_sql_query("SELECT 42 AS answer", conn, dtype_backend="pyarrow")
    print(df)
```

Direct reads are covered by `python/tests/test_pandas_direct_reads.py`: default and Arrow
DataFrame types, SQL NULLs, Unicode, integers above 2^53, microsecond timestamps,
filtering, aggregation, index selection, empty results and multiple Arrow
batches. Local validation used pandas 3.1.0, PyArrow 26.0.0, ADBC manager 1.13.0
and Redis Software with Redis 8.6.2; see the
[validation record](https://github.com/redis/redis-adbc-driver/blob/main/docs/sdlc/changes/pandas-direct-reads/validation.md).
The Arrow backend preserves nullable integer types. These tests cover SQL
queries; pandas table reads, parameters and `chunksize` are outside this
validation. For write coverage, see the next section.

## pandas writes and round trips

pandas 2.2+ accepts an open ADBC connection directly for
[`read_sql`](https://pandas.pydata.org/docs/reference/api/pandas.read_sql.html)
and [`DataFrame.to_sql`](https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.to_sql.html).
Install `adbc-driver-redis[dbapi]` and `pandas>=2.2`. This example creates a
unique table, verifies the fetched rows and removes the table afterward:

```python
import uuid
import pandas as pd
from adbc_driver_redis import dbapi

table = "pandas_demo_" + uuid.uuid4().hex
source = pd.DataFrame({"id": [1, 2, 3], "amount": [10.5, 20.0, 30.25]})

with dbapi.connect("redis://localhost:6379/0") as conn:
    try:
        source.to_sql(table, conn, index=False)
        result = pd.read_sql(f'SELECT * FROM "{table}" ORDER BY id', conn)
        pd.testing.assert_frame_equal(result, source)
        print(result)
    finally:
        with conn.cursor() as cur:
            cur.execute(f'DROP TABLE IF EXISTS "{table}"')
```

Use `if_exists="append"` to append rows. `if_exists="replace"` drops and
recreates the table; writes autocommit and cannot be rolled back. For nullable
Arrow dtypes on reads, pass `dtype_backend="pyarrow"`. No SQLAlchemy engine is
needed for this path.

Run the focused integration tests against a disposable Redis with Search:

```sh
REDIS_URI=redis://localhost:6379/0 python -m pytest -q python/tests/test_pandas_writes.py
```

The recorded run used pandas 3.1.0, PyArrow 26.0.0 and ADBC manager 1.13.0;
see [validation](https://github.com/redis/redis-adbc-driver/blob/main/docs/sdlc/changes/pandas-smoke/validation.md).

[All integration guides](README.md)
