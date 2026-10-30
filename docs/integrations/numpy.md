# Redis integration guide for NumPy

Redis must run separately with Search (the Query Engine) enabled. Redis Software
with active verified 4x QPF is the project's development default. See
[installation and discovery](../python-integrations.md#install-and-discover-the-driver)
for supported wheel platforms, native source builds and TLS setup. Set
`REDIS_URI` in your environment, for example `redis://localhost:6379/0`;
use `rediss://` for TLS with certificate verification.

This is a custom Python workflow through the Redis ADBC driver. Existing Redis
keys need the driver's create/ingest/adopt workflow before SQL can query them.
Writes autocommit without rollback; failed writes may partially apply.

## Install and run

Use a Python version supported by both the driver and your NumPy release.

```sh
python -m pip install 'adbc-driver-redis[dbapi]' numpy
export REDIS_URI=redis://localhost:6379/0
```

Save as `redis_numpy.py` and run `python redis_numpy.py`. This read-only example
needs no pre-existing tables:

```python
import os

import numpy as np
from adbc_driver_redis import dbapi

with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    cur.execute(
        "SELECT 1 AS value UNION ALL SELECT 2 UNION ALL SELECT 3 ORDER BY value"
    )
    table = cur.fetch_arrow_table()

values = table.column("value").to_numpy()
np.testing.assert_array_equal(values, np.array([1, 2, 3]))
print(values, values.sum())  # [1 2 3] 6
```

For an existing table, select numeric columns explicitly and use `ORDER BY`
when row order matters. Arrow columns may contain several chunks; conversion
may allocate NumPy memory and is not a zero-copy guarantee. Decide how to handle
NULLs before constructing numeric arrays; missing values can change the dtype.
The example's non-null integer column avoids that ambiguity. Arrays materialize
in client memory, so bound large SQL results.

## Validation and limits

The workflow tests and their records are in [PR #248](https://github.com/redis/redis-adbc-driver/pull/248),
which was unmerged when this guide was written on October 30, 2026.
The example uses the public DB-API/Arrow interface, not a new tool-specific helper.
The saved runner and workflow-test extra require a checkout containing #248.
The Python example was also executed against isolated Redis Software 8.6.2 with
Search and active verified 4x QPF; see the
[live guide validation](../sdlc/changes/upstream-status-249/validation.md).

The [NumPy/scikit-learn record](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/plan.md)
reports two tests passed together with NumPy 2.5.4 and scikit-learn 1.9.1
against Redis OSS 8.6.2 with Search. The
[NumPy test](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/python/tests/test_ml_ingest.py)
checks the literal query and array values above. That historical run is not
Redis Software, TLS, cluster, distributed, or general NULL/type-conversion validation.

From a checkout containing #248, install NumPy and pytest alongside the driver,
then run `python/scripts/run-candidate-tests.sh ml` against the runner's
disposable Redis Software fixture; the `ml` selector also requires scikit-learn.

To rerun this guide's actual Python block with the installed tool and driver:

```sh
python python/scripts/check_integration_guide.py numpy
```

Set `REDIS_URI` to a dedicated Redis database with Search first. The checker
uses the active interpreter; source builds also need `REDIS_ADBC_LIBRARY` and
`PYTHONPATH=python` available to child processes and kernels.

[All integration guides](README.md) · [Upstream documentation status](../upstream-submissions/status.md#new-workflows-in-pr-248)
