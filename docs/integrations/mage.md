# Redis integration guide for Mage

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run

The example writes two ordinary Mage Python blocks and pipeline metadata; edit those files to adapt the query and transformation.

```sh
python -m pip install 'mage-ai==0.9.80'
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import os
import json
from pathlib import Path
import subprocess

project = Path('mage-guide').resolve()
subprocess.run(['mage', 'init', str(project)], check=True)
(project / 'pipelines/native_adbc').mkdir(parents=True)
(project / 'pipelines/native_adbc/metadata.yaml').write_text("""uuid: native_adbc
name: native_adbc
type: python
blocks:
- uuid: native_read
  name: native_read
  type: data_loader
  language: python
  upstream_blocks: []
  downstream_blocks: [check_rows]
- uuid: check_rows
  name: check_rows
  type: transformer
  language: python
  upstream_blocks: [native_read]
  downstream_blocks: []
""")
(project / 'data_loaders/native_read.py').write_text("""import os
from adbc_driver_redis import dbapi
if 'data_loader' not in globals():
    from mage_ai.data_preparation.decorators import data_loader

@data_loader
def load_native(*args, **kwargs):
    with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
        name = os.environ["REDIS_ADBC_TEST_TABLE"]
        cur.execute(f'SELECT * FROM "{name}" WHERE id >= ? ORDER BY id',
                    (int(os.environ["REDIS_ADBC_MINIMUM"]),))
        table = cur.fetch_arrow_table()
    return table.to_pandas()
""")
(project / 'transformers/check_rows.py').write_text("""import json
import os
import pyarrow as pa
if 'transformer' not in globals():
    from mage_ai.data_preparation.decorators import transformer

@transformer
def check_rows(frame, *args, **kwargs):
    table = pa.Table.from_pandas(frame, preserve_index=False)
    output = {"rows": table.to_pylist(), "columns": list(frame.columns),
              "dtypes": {key: str(value) for key, value in frame.dtypes.items()}}
    with open(os.environ["REDIS_ADBC_MAGE_OUTPUT"], "w") as file:
        json.dump(output, file, ensure_ascii=False)

    # Persist the tool's computed output, then read it through a fresh connection.
    from integration_guide_data import write_result
    write_result(table)
    return frame
""")
with example_table() as name:
    expected = read_rows(name).to_pylist()
    output = Path('mage-checked.json').resolve()
    env = {**os.environ, 'REDIS_ADBC_TEST_TABLE': name, 'REDIS_ADBC_MINIMUM': '1',
           'REDIS_ADBC_MAGE_OUTPUT': str(output), 'MAGE_TELEMETRY_ENABLED': '0'}
    subprocess.run(['mage', 'run', str(project), 'native_adbc'], env=env, check=True)
    assert json.loads(output.read_text())['rows'] == expected
print('Mage loader and transformer verified')
```

## Expected result

The local `native_adbc` pipeline hands a DataFrame from its loader to its transformer. `mage-checked.json` contains the three expected rows.

## Limits and validation

Use a fresh working directory. The example creates a local Mage project and uses native ADBC in custom Python blocks; hosted Mage deployment is outside its scope. The pinned Mage release needs an isolated dependency environment.

The example follows the [saved workflow](../../python/tests/test_mage_candidate.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py mage
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh mage` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
