# Redis integration guide for Ibis

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run

The saved analysis profile uses PyArrow below 26; install `./python[analysis-tests]` from the repository root for that isolated dependency set.
```sh
python -m pip install 'ibis-framework[duckdb]>=12' 'pyarrow>=18,<26'
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import ibis

with example_table() as name:
    table = read_rows(name)
backend = ibis.duckdb.connect()
try:
    expression = ibis.memtable(table)
    result = backend.to_pyarrow(expression.filter(expression.id >= 2).order_by('id'))
    assert result['id'].to_pylist() == [2, 3]
    stats = backend.to_pyarrow(expression.aggregate(rows=expression.count(), total=expression.amount.sum()))
    assert stats.to_pylist() == [{'rows': 3, 'total': 10.0}]
    print(stats.to_pylist())
finally:
    backend.disconnect()

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(stats)
```

## Expected result

Ibis produces `rows=3` and `total=10.0` and filters to IDs 2 and 3.

## Limits and validation

Expressions run on a local DuckDB backend after fetching Arrow data. This does not provide a Redis Ibis backend or SQL pushdown into Redis.

The example follows the [saved workflow](../../python/tests/test_analysis_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py ibis
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh ibis` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
