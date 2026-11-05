# Redis integration guide for Fivetran Connector SDK

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install fivetran-connector-sdk duckdb
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import os
from pathlib import Path
import subprocess
import sys
import textwrap
import duckdb

source = """
import os
from fivetran_connector_sdk import Connector, Operations as op
from integration_guide_data import read_rows

def schema(configuration):
    return [{'table': 'rows', 'primary_key': ['id'], 'columns': {
        'id': 'LONG', 'label': 'STRING', 'amount': 'DOUBLE', 'active': 'BOOLEAN'}}]

def update(configuration, state):
    for row in read_rows(os.environ['REDIS_ADBC_TEST_TABLE']).to_pylist():
        op.upsert(table='rows', data=row)
    op.checkpoint(state={'complete': True})

connector = Connector(update=update, schema=schema)
if __name__ == '__main__':
    connector.debug(project_path=os.path.dirname(os.path.abspath(__file__)), configuration={})
"""
Path('connector.py').write_text(textwrap.dedent(source))
with example_table() as name:
    expected = read_rows(name).to_pylist()
    env = {**os.environ, 'REDIS_ADBC_TEST_TABLE': name}
    subprocess.run([sys.executable, 'connector.py'], env=env, check=True)
with duckdb.connect('files/warehouse.db', read_only=True) as warehouse:
    schemas = warehouse.execute("SELECT table_schema FROM information_schema.tables WHERE table_name='rows'").fetchall()
    assert len(schemas) == 1
    schema_name = schemas[0][0].replace('"', '""')
    actual = warehouse.execute(f'SELECT id, label, amount, active FROM "{schema_name}"."rows" ORDER BY id').fetchall()
    assert actual == [tuple(row.values()) for row in expected]
print('Three local warehouse rows verified')
```

## Expected result

The SDK reports a successful local sync. `files/warehouse.db` contains the three checked rows.

## Limits and validation

The first SDK debug run downloads its local tester. Run in a fresh directory to avoid reusing state or warehouse data. This full-refresh example does not implement incremental extraction or deletes. Hosted Fivetran packaging and deployment remain unverified.

The example follows the [saved workflow](../../python/tests/test_orchestrator_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py fivetran
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh fivetran` after installing its test extra.

[All integration guides](README.md)
