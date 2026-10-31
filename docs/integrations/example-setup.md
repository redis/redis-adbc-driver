# Shared setup for the integration examples

These instructions support the additional guides linked from the README. Use
Python 3.12 for the recorded model, Spark and orchestration environments; choose
a tool-compatible interpreter when installing another release. Keep each tool's
dependencies in a separate environment (especially Airflow, Prefect and Superset).
Choose a different virtual-environment path for each tool instead of reusing
the example path below across incompatible dependencies.
The dedicated guides for existing integrations have their own installation steps.

## Build and connect

Start from the repository root. You need Git, Docker Compose, curl, jq, a C
compiler and the Go toolchain declared in `go/go.mod`. The Redis Software setup
verifies Search and active 4x QPF, including when reusing an existing fixture.

```sh
export REDIS_ADBC_REPO="$PWD"
python3.12 -m venv /tmp/redis-guide-venv
. /tmp/redis-guide-venv/bin/activate
python -m pip install -e './python' pyarrow
make -C go build
make -C go redis-up
export REDIS_URI=redis://localhost:6379/0
export REDIS_ADBC_LIBRARY="$REDIS_ADBC_REPO/go/build/libadbc_driver_redis.so"
# On macOS use this instead:
# export REDIS_ADBC_LIBRARY="$REDIS_ADBC_REPO/go/build/libadbc_driver_redis.dylib"
export PYTHONPATH="$REDIS_ADBC_REPO/python/examples:$REDIS_ADBC_REPO/python${PYTHONPATH:+:$PYTHONPATH}"
```

For an existing dedicated Redis Software database, set its URI instead. The
database must allow the driver's Search, metadata and table commands. Use
`rediss://` and trusted certificates for TLS; these local examples do not test
TLS, clusters or remote deployment. See [server requirements](../driver-reference.md#server-requirements).
Writes autocommit and cannot be rolled back.

## Sample data and working directory

Create a fresh working directory, then follow one tool's guide:

```sh
mkdir -p /tmp/redis-guide-work
cd /tmp/redis-guide-work
```

The [shared Python helper](../../python/examples/integration_guide_data.py)
provides `example_table()` and `read_rows(name)`. Each context creates a unique
table, ingests the following Arrow rows, and drops only that table on exit:

| id | label | amount | active |
|---|---|---|---|
| 1 | alpha | 2.5 | true |
| 2 | NULL | NULL | NULL |
| 3 | 雪 | 7.5 | false |

`read_rows` returns an ordered Arrow table, materialized in client memory. The
connection and cursor close before returning it. Model examples explicitly
choose how to handle missing values; that choice is part of the example.
If a process is killed before cleanup, remove only its `guide_<UUID>` table
after identifying it in `information_schema.tables`.

Keep these environment variables when launching kernels, workers, R or Quarto.
For a remote worker, installing this helper alone is insufficient: the matching
driver, dependencies, Redis reachability and credentials must exist there too.

## Check the environment

```sh
python -c 'from integration_guide_data import example_table, read_rows; exec("with example_table() as name:\n    assert read_rows(name).num_rows == 3"); print("Redis guide setup OK")'
```

The driver is built from the checkout to make the examples reproducible; this
does not validate the contents of a release wheel. See each guide's recorded
versions and [actual guide test results](../sdlc/changes/integration-guides-262/validation.md).
