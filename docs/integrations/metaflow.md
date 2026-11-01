# Redis integration guide for Metaflow

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install metaflow
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

flow_source = """
import os
from metaflow import FlowSpec, step

class RedisGuideFlow(FlowSpec):
    @step
    def start(self):
        from integration_guide_data import read_rows
        self.rows = read_rows(os.environ['REDIS_ADBC_TEST_TABLE']).to_pylist()
        self.next(self.end)

    @step
    def end(self):
        assert [r['label'] for r in self.rows] == ['alpha', None, '雪']
        assert sum(r['id'] for r in self.rows) == 6
        print('REDIS_METAFLOW_OK')

        # Persist the tool's computed output, then read it through a fresh connection.
        from integration_guide_data import write_result
        import pyarrow as pa
        write_result(pa.Table.from_pylist(self.rows))

if __name__ == '__main__':
    RedisGuideFlow()
"""
Path('redis_flow.py').write_text(textwrap.dedent(flow_source))
with example_table() as name:
    env = {**os.environ, 'REDIS_ADBC_TEST_TABLE': name,
           'METAFLOW_USER': 'redis_guide', 'METAFLOW_DEFAULT_METADATA': 'local',
           'METAFLOW_DEFAULT_DATASTORE': 'local'}
    subprocess.run([sys.executable, 'redis_flow.py', '--no-pylint', 'run'], env=env, check=True)
print('REDIS_METAFLOW_OK')
```

## Expected result

Both local steps succeed, persist the fetched rows between steps, and print `REDIS_METAFLOW_OK`.

## Limits and validation

This uses local metadata and data storage. Remote execution, retries with writes, production secrets and shared storage are not validated. Keep the helper and driver available to each subprocess.

The example follows the [saved workflow](../../python/tests/test_orchestrator_candidates.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py metaflow
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh metaflow` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
