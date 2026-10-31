# Redis integration guide for Keras

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install numpy tensorflow keras
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import os
os.environ['KERAS_BACKEND'] = 'tensorflow'
import numpy as np
import keras

with example_table() as name:
    table = read_rows(name)
values = np.nan_to_num(np.asarray(table['amount'].to_pylist(), dtype=np.float32)).reshape(-1, 1)
model = keras.Sequential([keras.layers.Input(shape=(1,)), keras.layers.Dense(1)])
model.layers[0].set_weights([np.array([[2.0]], dtype=np.float32), np.array([1.0], dtype=np.float32)])
expected = np.array([[6.0], [1.0], [16.0]], dtype=np.float32)
np.testing.assert_allclose(model(values, training=False).numpy(), expected)
model.save('redis-model.keras')
restored = keras.models.load_model('redis-model.keras')
np.testing.assert_allclose(restored(values, training=False).numpy(), expected)
print(expected.tolist())
```

## Expected result

`redis-model.keras` is written, reloaded, and checked against the three expected predictions.

## Limits and validation

This uses the TensorFlow backend and zero-fills NULL amounts. Other backends, training, accelerators and remote serving are outside this example.

The example follows the [saved workflow](../../python/tests/test_model_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py keras
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh keras` after installing its test extra.

[All integration guides](README.md)
