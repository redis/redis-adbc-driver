# Redis integration guide for MLflow

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install 'mlflow>=3.17.2,<4' pandas 'pyarrow>=18,<26'
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
from pathlib import Path
import hashlib
import pyarrow as pa
import mlflow

with example_table() as name:
    table = read_rows(name)
mlflow.set_tracking_uri('sqlite:///' + str(Path('mlflow-guide.db').resolve()))
mlflow.set_experiment('redis-guide')
# Explicit digest also handles empty data with MLflow 3.17.2 / pandas 3.1.
sink = pa.BufferOutputStream()
with pa.ipc.new_stream(sink, table.schema) as writer:
    writer.write_table(table)
digest = hashlib.sha256(sink.getvalue().to_pybytes()).hexdigest()[:32]
dataset = mlflow.data.from_pandas(table.to_pandas(), name='redis-rows', digest=digest)
with mlflow.start_run() as run:
    mlflow.log_input(dataset, context='example')
    mlflow.log_metric('rows', table.num_rows)
    mlflow.log_dict({'rows': table.to_pylist()}, 'input.json')
stored = mlflow.get_run(run.info.run_id)
assert stored.data.metrics['rows'] == 3
assert stored.inputs.dataset_inputs[0].dataset.digest == digest
print(run.info.run_id)
```

## Expected result

A local SQLite tracking database records a run, its input dataset, three rows, and `input.json`.

## Limits and validation

Redis supplies the dataset, not the MLflow tracking store. An explicit digest avoids the recorded empty-pandas digest incompatibility. Use the isolated analysis profile for the recorded dependency versions.

The example follows the [saved workflow](../../python/tests/test_analysis_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py mlflow
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh mlflow` after installing its test extra.

[All integration guides](README.md)
