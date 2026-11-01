# Redis integration guide for scikit-learn

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

```sh
python -m pip install 'adbc-driver-redis[dbapi]' numpy scikit-learn
export REDIS_URI=redis://localhost:6379/0
```

Save as `redis_sklearn.py` and run `python redis_sklearn.py`. It fetches a tiny
feature/label dataset from SQL and checks a fitted decision tree:

```python
import os

import numpy as np
from adbc_driver_redis import dbapi
from sklearn.tree import DecisionTreeClassifier

with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    cur.execute(
        "SELECT 0 AS x, 0 AS y, 0 AS label UNION ALL "
        "SELECT 0, 1, 1 UNION ALL SELECT 1, 0, 1 UNION ALL SELECT 1, 1, 0"
    )
    table = cur.fetch_arrow_table()

features = np.column_stack(
    [table.column("x").to_numpy(), table.column("y").to_numpy()]
)
labels = table.column("label").to_numpy()
model = DecisionTreeClassifier(max_depth=2, random_state=0).fit(features, labels)
predicted = model.predict(features)
np.testing.assert_array_equal(predicted, labels)
print(predicted)

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(pa.table({'prediction': predicted}))
```

For application tables, select features and labels in the same SQL query so
they remain aligned. Add an explicit order when reproducibility requires it.
Handle NULLs, encode categories and select compatible numeric dtypes before
training. Fetching the result and fitting this model happen in client memory.

The example deliberately predicts its training rows to verify the data path.
It does not measure model quality. Use a held-out test set or cross-validation
for a real model and prevent label leakage during preprocessing.

## Validation and limits

The workflow tests and their records are in [PR #248](https://github.com/redis/redis-adbc-driver/pull/248),
which was unmerged when this guide was written on October 30, 2026.
The example uses the public DB-API/Arrow interface, not a new tool-specific helper.
The saved runner and workflow-test extra require a checkout containing #248.
The Python example was also executed against isolated Redis Software 8.6.2 with
Search and active verified 4x QPF; see the
[live guide validation](../sdlc/changes/upstream-status-249/validation.md).

The [record](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/plan.md)
reports two combined NumPy/scikit-learn tests passed on Redis OSS 8.6.2 with
Search, NumPy 2.5.4 and scikit-learn 1.9.1.
The [test](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/python/tests/test_ml_ingest.py)
covers Arrow-to-array conversion and the small decision-tree fit/predict.
It does not establish a feature store, incremental training, distributed
training, model serving, TLS or Redis Software coverage.

From a checkout containing #248, install the driver, pytest, NumPy and scikit-learn,
then use `python/scripts/run-candidate-tests.sh ml`. The runner provisions
Redis Software and verifies active 4x QPF; no new result is asserted here.

To rerun this guide's actual Python block with the installed tool and driver:

```sh
python python/scripts/check_integration_guide.py scikit-learn
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
