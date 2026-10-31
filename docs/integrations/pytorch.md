# Redis integration guide for PyTorch

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install numpy torch
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import numpy as np
import torch
from torch.utils.data import DataLoader, TensorDataset

with example_table() as name:
    table = read_rows(name)
raw = torch.tensor(np.asarray(table['amount'].to_pylist(), dtype=np.float32))
valid = torch.isfinite(raw)
values = torch.nan_to_num(raw).reshape(-1, 1)
loader = DataLoader(TensorDataset(values, valid), batch_size=2, shuffle=False)
assert torch.cat([mask for _, mask in loader]).tolist() == [True, False, True]
model = torch.nn.Linear(1, 1)
with torch.no_grad():
    model.weight.fill_(2)
    model.bias.fill_(1)
    output = model(values)
torch.testing.assert_close(output, torch.tensor([[6.0], [1.0], [16.0]]))
print(output.tolist())
```

## Expected result

The CPU forward pass prints `[[6.0], [1.0], [16.0]]`; the NULL mask distinguishes the filled value.

## Limits and validation

NULL amounts are filled with zero explicitly. This is a tiny CPU input pipeline, without GPU, distributed training or performance validation.

The example follows the [saved workflow](../../python/tests/test_model_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py pytorch
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh pytorch` after installing its test extra.

[All integration guides](README.md)
