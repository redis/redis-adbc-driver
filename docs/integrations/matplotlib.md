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

[All integration guides](README.md)
