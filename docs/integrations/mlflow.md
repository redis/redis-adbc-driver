# Redis integration guide for MLflow

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install 'mlflow>=3.18.0,<4' pandas 'pyarrow>=26.0.1'
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
from pathlib import Path
import mlflow

with example_table() as name:
    table = read_rows(name)
mlflow.set_tracking_uri('sqlite:///' + str(Path('mlflow-guide.db').resolve()))
mlflow.set_experiment('redis-guide')
# MLflow 3.18.0 computes the default digest for empty results as well.
dataset = mlflow.data.from_pandas(table.to_pandas(), name='redis-rows')
with mlflow.start_run() as run:
    mlflow.log_input(dataset, context='example')
    mlflow.log_metric('rows', table.num_rows)
    mlflow.log_dict({'rows': table.to_pylist()}, 'input.json')
stored = mlflow.get_run(run.info.run_id)
assert stored.data.metrics['rows'] == 3
assert stored.inputs.dataset_inputs[0].dataset.digest == dataset.digest
print(run.info.run_id)

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(pa.table({'run_id': [stored.info.run_id], 'rows': [int(stored.data.metrics['rows'])]}))
```

## Expected result

A local SQLite tracking database records a run, its input dataset, three rows, and `input.json`.

## Limits and validation

Redis supplies the dataset, not the MLflow tracking store. The guide requires MLflow 3.18.0: MLflow 3.17.2 could not compute the default digest of an empty result's pandas string columns ([mlflow#27358](https://github.com/mlflow/mlflow/issues/27358)), which needed an explicit `digest`. Use the isolated analysis profile for the recorded dependency versions.

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


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
