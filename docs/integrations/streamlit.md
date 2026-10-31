# Redis integration guide for Streamlit

Redis must run separately with Search (the Query Engine) enabled. Redis Software
with active verified 4x QPF is the project's development default. See
[installation and discovery](../python-integrations.md#install-and-discover-the-driver)
for supported wheel platforms, native source builds and TLS setup. Set
`REDIS_URI` in your environment, for example `redis://localhost:6379/0`;
use `rediss://` for TLS with certificate verification.

This is a custom Python workflow through the Redis ADBC driver. Existing Redis
keys need the driver's create/ingest/adopt workflow before SQL can query them.
Writes autocommit without rollback; failed writes may partially apply.

## Install and start the app

```sh
python -m pip install 'adbc-driver-redis[dbapi]' streamlit pandas
export REDIS_URI=redis://localhost:6379/0
```

Save the following as `app.py`, then run `streamlit run app.py`. Use a dedicated
test database: each app execution creates and removes its own uniquely named
example table.

```python
import os
from uuid import uuid4

import pyarrow as pa
import streamlit as st
from adbc_driver_redis import dbapi

st.title("Redis ADBC query")
minimum = st.number_input("Minimum id", min_value=1, max_value=3, value=1)
name = "streamlit_example_" + uuid4().hex
source = pa.table(
    {"id": [1, 2, 3], "label": ["alpha", None, "雪"], "amount": [2.5, None, 7.5]}
)

with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    try:
        cur.adbc_ingest(name, source, mode="create")
        cur.execute(
            f'SELECT * FROM "{name}" WHERE id >= ? ORDER BY id',
            (int(minimum),),
        )
        data = cur.fetch_arrow_table()
    finally:
        cur.execute(f'DROP TABLE IF EXISTS "{name}"')

st.dataframe(data)
st.metric("Rows", data.num_rows)
```

Changing the input reruns the script and executes a bound SQL predicate.
Expect three rows initially and two rows at a minimum of 2. The example avoids
a shared connection across user sessions. For an application, query an existing
table and remove the demo ingestion/drop block; keep connections scoped to
their operations. Bind values instead of concatenating user input into SQL.

Configure `REDIS_URI` in the server environment or adapt to Streamlit secrets.
Install the driver on the app server, which must reach Redis.
Returned data materializes in server memory; select needed columns and limit
large results. The example does not cache results; add a deliberate freshness
policy if introducing Streamlit caching.

## Validation and limits

The workflow tests and their records are in [PR #248](https://github.com/redis/redis-adbc-driver/pull/248),
which was unmerged when this guide was written on October 30, 2026.
The example uses the public DB-API/Arrow interface, not a new tool-specific helper.
The saved runner and workflow-test extra require a checkout containing #248.
The Python example was also executed against isolated Redis Software 8.6.2 with
Search and active verified 4x QPF; see the
[live guide validation](../sdlc/changes/upstream-status-249/validation.md).

The [record](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/validation.md)
reports five cases passed across Jupyter, Datasets, Streamlit and SciPy on Redis
Software 8.6.2 with Search and verified active 4x QPF.
The [Streamlit test](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/python/tests/test_streamlit.py)
uses Streamlit 1.66.1 AppTest to display stored rows, change a numeric input,
rerun the bound query, and compare rows and metrics.
AppTest is not browser, authentication or Streamlit Community Cloud validation.
The exact guide app, including its unique demo table and cleanup, also passed
an initial AppTest run and the input-change rerun in the live guide validation.

From a checkout containing #248, install `python[workflow-tests]` and run
`python/scripts/run-candidate-tests.sh streamlit`.

To rerun this guide's actual Python block with the installed tool and driver:

```sh
python python/scripts/check_integration_guide.py streamlit
```

Set `REDIS_URI` to a dedicated Redis database with Search first. The checker
uses the active interpreter; source builds also need `REDIS_ADBC_LIBRARY` and
`PYTHONPATH=python` available to child processes and kernels.

[All integration guides](README.md) · [Upstream documentation status](../upstream-submissions/status.md#new-workflows-in-pr-248)
