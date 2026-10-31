# Redis integration guide for Plotly

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install plotly
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import plotly.graph_objects as go
import plotly.io as pio

with example_table() as name:
    table = read_rows(name)
figure = go.Figure(go.Scatter(x=table['id'].to_pylist(), y=table['amount'].to_pylist(),
                             customdata=table['label'].to_pylist(), mode='lines+markers'))
figure.write_json('redis-plot.json')
restored = pio.from_json(open('redis-plot.json').read())
assert list(restored.data[0].y) == [2.5, None, 7.5]
restored.write_html('redis-plot.html', include_plotlyjs=True, full_html=True)
print('Wrote redis-plot.html and redis-plot.json')
```

## Expected result

Open `redis-plot.html` in a browser. The serialized figure retains the NULL gap and Unicode label.

## Limits and validation

Figure serialization is checked; browser interactions and hosted dashboards require separate validation. All plotted rows are fetched into client memory.

The example follows the [saved workflow](../../python/tests/test_plotting_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py plotly
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh plotly` after installing its test extra.

[All integration guides](README.md)
