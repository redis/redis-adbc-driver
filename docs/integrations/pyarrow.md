# Redis integration guide for PyArrow

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install pyarrow
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import pyarrow as pa

with example_table() as name:
    table = read_rows(name)
assert table['label'].to_pylist() == ['alpha', None, '雪']
assert table.schema.field('amount').type == pa.float64()
with pa.OSFile('rows.arrow', 'wb') as sink:
    with pa.ipc.new_file(sink, table.schema) as writer:
        writer.write_table(table)
with pa.memory_map('rows.arrow', 'r') as source:
    restored = pa.ipc.open_file(source).read_all()
assert restored.equals(table)
print(restored.to_pylist())
```

## Expected result

Three rows are printed and `rows.arrow` contains an Arrow IPC file that round-trips exactly.

## Limits and validation

This example materializes the full result. Batch readers and ingestion are separate APIs; it does not validate every Arrow type.

The example follows the [saved workflow](../../python/tests/test_ipc.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py pyarrow
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh arrow` after installing its test extra.

[All integration guides](README.md)
