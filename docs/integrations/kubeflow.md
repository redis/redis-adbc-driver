# Redis integration guide for Kubeflow Pipelines

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install kfp
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
from kfp import dsl, local
import os

@dsl.component(base_image='python:3.12', install_kfp_package=False)
def read_redis(redis_uri: str, table_name: str) -> int:
    from adbc_driver_redis import dbapi
    with dbapi.connect(redis_uri) as conn, conn.cursor() as cur:
        cur.execute(f'SELECT id FROM "{table_name}" ORDER BY id')
        rows = cur.fetch_arrow_table()
    from integration_guide_data import write_result
    import pyarrow as pa
    total = sum(rows['id'].to_pylist())
    write_result(pa.table({'total': [total]}))
    return total

if __name__ == '__main__':
    local.init(runner=local.SubprocessRunner(use_venv=False), pipeline_root='kfp-output')
    with example_table() as name:
        task = read_redis(redis_uri=os.environ['REDIS_URI'], table_name=name)
        assert task.output == 6
        print(task.output)
```

## Expected result

The local component returns `6` and materializes its output under `kfp-output`.

## Limits and validation

The component runs in the active interpreter without a virtual environment. Kubernetes, container images, remote secrets and distributed execution require separate installation and validation.

The example follows the [saved workflow](../../python/tests/test_orchestrator_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py kubeflow
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh kubeflow` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
