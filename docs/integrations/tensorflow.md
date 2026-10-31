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

[All integration guides](README.md)
