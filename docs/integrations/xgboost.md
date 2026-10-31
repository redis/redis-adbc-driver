# Redis integration guide for XGBoost

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run

On macOS, XGBoost may require the OpenMP runtime (`brew install libomp`).
```sh
python -m pip install numpy xgboost
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import numpy as np
import xgboost as xgb

with example_table() as name:
    table = read_rows(name)
values = np.asarray(table['amount'].to_pylist(), dtype=float).reshape(-1, 1)
labels = table['id'].to_numpy().astype(float)
data = xgb.DMatrix(values, label=labels, missing=np.nan)
model = xgb.train({'objective': 'reg:squarederror', 'max_depth': 2, 'eta': 1,
                   'min_child_weight': 0, 'lambda': 0, 'nthread': 1, 'seed': 0},
                  data, num_boost_round=4)
np.testing.assert_allclose(model.predict(data), labels, atol=0.01)
model.save_model('redis-model.json')
restored = xgb.Booster(model_file='redis-model.json')
np.testing.assert_array_equal(restored.predict(data), model.predict(data))
print(restored.predict(data).tolist())
```

## Expected result

Predictions are approximately `[1, 2, 3]`, including the missing-value row, and match after reloading `redis-model.json`.

## Limits and validation

This checks input conversion and persistence on three CPU examples. Training-set agreement is not a model-quality estimate or a scale benchmark.

The example follows the [saved workflow](../../python/tests/test_model_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py xgboost
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh xgboost` after installing its test extra.

[All integration guides](README.md)
