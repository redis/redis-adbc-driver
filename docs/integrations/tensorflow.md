# Redis integration guide for TensorFlow

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install numpy tensorflow
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import numpy as np
import tensorflow as tf

with example_table() as name:
    table = read_rows(name)
raw = np.asarray(table['amount'].to_pylist(), dtype=np.float32)
dataset = tf.data.Dataset.from_tensor_slices({
    'id': table['id'].to_numpy(), 'value': np.nan_to_num(raw), 'valid': np.isfinite(raw)
}).batch(2)
options = tf.data.Options()
options.threading.private_threadpool_size = 1
batches = list(dataset.with_options(options).as_numpy_iterator())
np.testing.assert_allclose(np.concatenate([b['value'] for b in batches]), [2.5, 0, 7.5])
np.testing.assert_array_equal(np.concatenate([b['valid'] for b in batches]), [True, False, True])
print([len(b['id']) for b in batches])

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(pa.table({'id': np.concatenate([b['id'] for b in batches]), 'value': np.concatenate([b['value'] for b in batches]).astype('float64'), 'valid': np.concatenate([b['valid'] for b in batches])}))
```

## Expected result

The dataset has two batches with sizes `[2, 1]`; numeric values and the NULL mask are checked.

## Limits and validation

Data is fetched before building the dataset. The example does not implement streaming from Redis during training or validate GPU execution.

The example follows the [saved workflow](../../python/tests/test_model_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py tensorflow
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh tensorflow` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
