# Redis integration guide for Apache Beam

Complete the [shared example setup](example-setup.md) first. It builds this
checkout's driver, verifies Redis Software with Search and active 4x QPF, and
sets the environment used below. Use a separate environment for this tool.

## Install and run


```sh
python -m pip install 'apache-beam>=2.77,<3'
```

Save the following as `example.py` in your working directory and run
`python example.py`. The shared helper creates a unique Redis table with three
rows, including NULLs and Unicode, and removes it when the example finishes.

```python
from integration_guide_data import example_table, read_rows
import json
import os
from pathlib import Path
import apache_beam as beam
from apache_beam.options.pipeline_options import PipelineOptions
from apache_beam.runners.portability.fn_api_runner import FnApiRunner

class ReadRedis(beam.DoFn):
    def process(self, name):
        from integration_guide_data import read_rows
        yield json.dumps(read_rows(name).to_pylist(), ensure_ascii=False)

if __name__ == '__main__':
    options = PipelineOptions(['--direct_running_mode=multi_processing',
                               '--direct_num_workers=2', '--save_main_session'])
    with example_table() as name:
        expected = read_rows(name).to_pylist()
        with beam.Pipeline(runner=FnApiRunner(), options=options) as pipeline:
            (pipeline | beam.Create([name]) | beam.ParDo(ReadRedis())
             | beam.io.WriteToText('beam-rows', num_shards=1))
    lines = [line for path in Path('.').glob('beam-rows-*') for line in path.read_text().splitlines()]
    assert len(lines) == 1 and json.loads(lines[0]) == expected
    print('Three Beam worker rows verified')

# Persist the tool's computed output, then read it through a fresh connection.
from integration_guide_data import write_result
import pyarrow as pa
write_result(pa.Table.from_pylist(json.loads(lines[0])))
```

## Expected result

A local worker subprocess reads Redis and writes `beam-rows-*`; its three rows match the expected values.

## Limits and validation

This is a custom DoFn and local FnApi runner, not a packaged Beam I/O connector. Remote runners need the helper, native library and environment installed separately. Each query materializes its result.

The example follows the [saved workflow](../../python/tests/test_beam_candidate.py). Historical execution is
recorded in the [native sweep](../sdlc/changes/candidate-test-runner/native-sweep.md)
or [follow-up](../sdlc/changes/native-candidate-followup/validation.md).
The [guide validation record](../sdlc/changes/integration-guides-262/validation.md)
reports the checks of these actual instructions.

From the repository root, with this tool installed, rerun the actual guide block:

```sh
python python/scripts/check_integration_guide.py beam
```

The broader workflow can also be checked with
`python/scripts/run-candidate-tests.sh beam` after installing its test extra.


## Write results back to Redis

The runnable example writes the tool's computed output through the shared
`write_result` Arrow sink and checks exact rows and types on a fresh connection.
This is custom application code and fully materializes the output; it does not
add a native connector, distributed sink, model/artifact store, or transactions.
The helper creates a unique output table and drops it after verification. In a
production workflow, choose an owned destination and explicit retry policy;
failed autocommit writes can partially apply and blind append retries duplicate rows.

[All integration guides](README.md)
