# Redis integration guide for Matplotlib

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install numpy matplotlib pillow
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import numpy as np
from matplotlib.backends.backend_agg import FigureCanvasAgg
from matplotlib.figure import Figure
from PIL import Image

with example_table() as name:
    table = read_rows(name)
figure = Figure(figsize=(4, 3), dpi=100)
FigureCanvasAgg(figure)
axes = figure.subplots()
line, = axes.plot(table['id'].to_pylist(), table['amount'].to_pylist(), 'o-')
np.testing.assert_allclose(line.get_ydata(orig=False), [2.5, np.nan, 7.5], equal_nan=True)
figure.savefig('redis-plot.png')
with Image.open('redis-plot.png') as image:
    assert image.size == (400, 300)
    assert len(image.convert('RGB').getcolors(maxcolors=400 * 300)) > 1
print('Wrote redis-plot.png')

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(pa.table({'id': line.get_xdata(), 'amount': pa.array(line.get_ydata(orig=False), type=pa.float64(), from_pandas=True)}))
```

## Expected result

`redis-plot.png` is a nonblank 400×300 image. The missing amount remains a gap.

## Limits and validation

The Agg backend writes files without a GUI. Interactive backends and large-result plotting are not tested by this example.

The example follows the [saved workflow](../../python/tests/test_plotting_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py matplotlib
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh matplotlib` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
