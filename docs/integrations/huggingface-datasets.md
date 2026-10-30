# Redis integration guide for Hugging Face Datasets

Redis must run separately with Search (the Query Engine) enabled. Redis Software
with active verified 4x QPF is the project's development default. See
[installation and discovery](../python-integrations.md#install-and-discover-the-driver)
for supported wheel platforms, native source builds and TLS setup. Set
`REDIS_URI` in your environment, for example `redis://localhost:6379/0`;
use `rediss://` for TLS with certificate verification.

This is a custom Python workflow through the Redis ADBC driver. Existing Redis
keys need the driver's create/ingest/adopt workflow before SQL can query them.
Writes autocommit without rollback; failed writes may partially apply.

## Install and run locally

```sh
python -m pip install 'adbc-driver-redis[dbapi]' datasets
export REDIS_URI=redis://localhost:6379/0
```

Save as `redis_dataset.py` and run `python redis_dataset.py`. It queries Arrow
results and persists a Dataset locally without contacting the Hugging Face Hub:

```python
import os
from tempfile import TemporaryDirectory
from uuid import uuid4

os.environ["HF_HUB_OFFLINE"] = "1"
os.environ["HF_DATASETS_OFFLINE"] = "1"

import pyarrow as pa
from adbc_driver_redis import dbapi
from datasets import Dataset, load_from_disk

name = "hf_example_" + uuid4().hex
source = pa.table({"id": [1, 2, 3], "label": ["alpha", None, "雪"]})
with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    try:
        cur.adbc_ingest(name, source, mode="create")
        cur.execute(f'SELECT * FROM "{name}" ORDER BY id')
        table = cur.fetch_arrow_table()
    finally:
        cur.execute(f'DROP TABLE IF EXISTS "{name}"')

dataset = Dataset(table)
assert dataset.to_list() == source.to_pylist()
with TemporaryDirectory() as directory:
    path = os.path.join(directory, "dataset")
    dataset.save_to_disk(path, num_shards=1)
    restored = load_from_disk(path)
    assert restored.to_list() == dataset.to_list()
    assert restored.features == dataset.features
    selected = restored.filter(
        lambda row: row["id"] >= 2, load_from_cache_file=False
    )
    assert selected.to_list() == source.slice(1).to_pylist()
    print(selected.to_list())
```

Run this example against a dedicated test database. It creates and removes a
unique table. SQL and Dataset transformations materialize data locally; this
is not a streaming or distributed dataset reader.

## Empty results and validation

Use `num_shards=1` explicitly when saving an empty result. In the recorded
Datasets 5.1.0 run, the default zero-shard save could not be reloaded. A single
empty shard preserved the schema.

The workflow tests and their records are in [PR #248](https://github.com/redis/redis-adbc-driver/pull/248),
which was unmerged when this guide was written on October 30, 2026.
The example uses the public DB-API/Arrow interface, not a new tool-specific helper.
The saved runner and workflow-test extra require a checkout containing #248.
The Python example was also executed against isolated Redis Software 8.6.2 with
Search and active verified 4x QPF; see the
[live guide validation](../sdlc/changes/upstream-status-249/validation.md).

The [record](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/docs/sdlc/changes/candidate-test-runner/validation.md)
reports five cases passed across four tools on Redis Software 8.6.2 with Search
and active verified 4x QPF. The
[Dataset tests](https://github.com/redis/redis-adbc-driver/blob/a99c882b4088aaff32e1abdad449f6e1b575b41a/python/tests/test_hf_datasets.py)
cover populated/empty Arrow results, types, NULLs/Unicode, local save/reload and
filtering with Datasets 5.1.0. Hub access, remote datasets/models, training, TLS,
clusters and hosted runtimes were outside that validation.

From a checkout containing #248, install `python[workflow-tests]` and run
`python/scripts/run-candidate-tests.sh datasets`.

To rerun this guide's actual Python block with the installed tool and driver:

```sh
python python/scripts/check_integration_guide.py huggingface-datasets
```

Set `REDIS_URI` to a dedicated Redis database with Search first. The checker
uses the active interpreter; source builds also need `REDIS_ADBC_LIBRARY` and
`PYTHONPATH=python` available to child processes and kernels.

[All integration guides](README.md) · [Upstream documentation status](../upstream-submissions/status.md#new-workflows-in-pr-248)
