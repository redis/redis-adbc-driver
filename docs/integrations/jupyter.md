# Redis integration guide for Jupyter

Redis must run separately with Search (the Query Engine) enabled. Redis Software
with active verified 4x QPF is the project's development default. See
[installation and discovery](../python-integrations.md#install-and-discover-the-driver)
for supported wheel platforms, native source builds and TLS setup. Set
`REDIS_URI` in your environment, for example `redis://localhost:6379/0`;
use `rediss://` for TLS with certificate verification.

This is a custom Python workflow through the Redis ADBC driver. Existing Redis
keys need the driver's create/ingest/adopt workflow before SQL can query them.
Writes autocommit without rollback; failed writes may partially apply.

## Install into the notebook kernel

Install the driver in the Python environment used by the kernel:

```sh
python -m pip install 'adbc-driver-redis[dbapi]' jupyterlab ipykernel
python -m ipykernel install --user --name redis-adbc --display-name "Redis ADBC"
export REDIS_URI=redis://localhost:6379/0
jupyter lab
```

Choose the **Redis ADBC** kernel. The kernel inherits the environment of the
server that starts it. For remote kernels, install the driver there and configure
Redis network access there; `localhost` refers to the kernel's machine.
A source build also needs `REDIS_ADBC_LIBRARY` set before the kernel starts.

## Query in a notebook

Run this as one code cell:

```python
import os

from adbc_driver_redis import dbapi

with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    cur.execute("SELECT 42 AS answer, '雪' AS label")
    data = cur.fetch_arrow_table()

assert data.to_pylist() == [{"answer": 42, "label": "雪"}]
data.to_pandas()

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(data)
```

The fetched Arrow table remains usable after the connection closes. Subsequent
cells can inspect `data.schema`, use `data.to_pylist()`, or convert to NumPy/pandas.
For application data, replace the literal SQL with a query over a driver-managed
or adopted table. Limit large results before displaying or converting them.

Do not embed credentials in notebook cells or saved outputs. Notebook execution
can save returned data; share only outputs appropriate for the notebook's audience.
Restart the kernel after changing native-driver configuration.

## Validation and limits

The workflow tests and their records are in [PR #248](https://github.com/redis/redis-adbc-driver/pull/248),
which was unmerged when this guide was written on October 30, 2026.
The example uses the public DB-API/Arrow interface, not a new tool-specific helper.
The saved runner and workflow-test extra require a checkout containing #248.
The Python example was also executed against isolated Redis Software 8.6.2 with
Search and active verified 4x QPF; see the
[live guide validation](../sdlc/changes/upstream-status-249/validation.md).

The [workflow record](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/validation.md)
reports five cases passed across Jupyter, Datasets, Streamlit and SciPy on Redis
Software 8.6.2 with Search and verified active 4x QPF.
The [notebook test](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/python/tests/test_jupyter.py)
used ipykernel 7.4.0 and nbclient 0.11.0 to execute two cells in a separate kernel,
read stored typed rows with NULLs/Unicode, and save/reopen notebook output.
JupyterLab browser interactions, remote kernels and hosted notebook services were
not tested by that run.

From a checkout containing #248, install `python[workflow-tests]` in a compatible
virtual environment and run `python/scripts/run-candidate-tests.sh jupyter`.

To rerun this guide's actual Python block with the installed tool and driver:

```sh
python python/scripts/check_integration_guide.py jupyter
```

Set `REDIS_URI` to a dedicated Redis database with Search first. The checker
uses the active interpreter; source builds also need `REDIS_ADBC_LIBRARY` and
`PYTHONPATH=python` available to child processes and kernels.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md) · [Upstream documentation status](../upstream-submissions/status.md#new-workflows-in-pr-248)
