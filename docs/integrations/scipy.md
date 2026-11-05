# Redis integration guide for SciPy

Redis must run separately with Search (the Query Engine) enabled. Redis Software
with active verified 4x QPF is the project's development default. See
[installation and discovery](../python-integrations.md#install-and-discover-the-driver)
for supported wheel platforms, native source builds and TLS setup. Set
`REDIS_URI` in your environment, for example `redis://localhost:6379/0`;
use `rediss://` for TLS with certificate verification.

This is a custom Python workflow through the Redis ADBC driver. Existing Redis
keys need the driver's create/ingest/adopt workflow before SQL can query them.
Writes autocommit without rollback; failed writes may partially apply.

## Install and run a sparse solve

```sh
python -m pip install 'adbc-driver-redis[dbapi]' numpy scipy
export REDIS_URI=redis://localhost:6379/0
```

Save as `redis_scipy.py` and run `python redis_scipy.py`. Use a dedicated test
database; the example creates a unique table and removes it after fetching.

```python
import os
from uuid import uuid4

import numpy as np
import pyarrow as pa
from adbc_driver_redis import dbapi
from scipy.sparse import coo_matrix
from scipy.sparse.linalg import spsolve

name = "scipy_example_" + uuid4().hex
coefficients = pa.table(
    {
        "row_id": [0, 0, 1, 1],
        "column_id": [0, 1, 0, 1],
        "value": [3.0, 1.0, 1.0, 2.0],
    }
)
with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    try:
        cur.adbc_ingest(name, coefficients, mode="create")
        cur.execute(f'SELECT * FROM "{name}" ORDER BY row_id, column_id')
        table = cur.fetch_arrow_table()
    finally:
        cur.execute(f'DROP TABLE IF EXISTS "{name}"')

matrix = coo_matrix(
    (
        table["value"].to_numpy(),
        (table["row_id"].to_numpy(), table["column_id"].to_numpy()),
    ),
    shape=(2, 2),
).tocsr()
target = np.array([9.0, 8.0])
solution = spsolve(matrix, target)
np.testing.assert_allclose(solution, [2.0, 3.0])
np.testing.assert_allclose(matrix @ solution, target)
print(solution)

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(pa.table({'solution': solution}))
```

For application data, validate index ranges, matrix shape, non-NULL coefficient
types and duplicate-coordinate behavior before constructing the sparse object.
The query and matrix materialize in client memory. SciPy performs the solve
locally; it is not pushed into Redis. Larger systems need appropriate numerical
conditioning and solver selection.

## Validation and limits

The workflow tests and their records are in [PR #248](https://github.com/redis/redis-adbc-driver/pull/248),
which was unmerged when this guide was written on October 30, 2026.
The example uses the public DB-API/Arrow interface, not a new tool-specific helper.
The saved runner and workflow-test extra require a checkout containing #248.
The Python example was also executed against isolated Redis Software 8.6.2 with
Search and active verified 4x QPF; see the
[live guide validation](../sdlc/changes/upstream-status-249/validation.md).

The [workflow record](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/validation.md)
reports five cases passed across four tools on Redis Software 8.6.2 with Search
and active verified 4x QPF.
The [SciPy test](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/python/tests/test_scipy.py)
used SciPy 1.18.1 to ingest/fetch coefficients, construct CSR data, solve the
system and independently check the solution/residual. This establishes the
small local workflow, not performance, large-scale solving, distributed
execution, TLS or support for every SciPy routine.

From a checkout containing #248, install `python[workflow-tests]` and run
`python/scripts/run-candidate-tests.sh scipy`.

To rerun this guide's actual Python block with the installed tool and driver:

```sh
python python/scripts/check_integration_guide.py scipy
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
