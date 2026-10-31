"""DirectRunner subprocesses read Redis through native ADBC, without JDBC/Flight."""

import json
import os
import subprocess
import sys
import textwrap

import pytest


@pytest.mark.parametrize("empty", [False, True])
def test_beam_native_worker(uri, workflow_rows, tmp_path, empty):
    pytest.importorskip("apache_beam")
    name, expected = workflow_rows
    expected = expected.slice(0, 0) if empty else expected
    script = tmp_path / "beam_native.py"
    script.write_text(textwrap.dedent('''\
        import json
        import os
        import apache_beam as beam
        from apache_beam.options.pipeline_options import PipelineOptions
        from apache_beam.runners.portability.fn_api_runner import FnApiRunner

        class NativeRead(beam.DoFn):
            def process(self, minimum):
                from adbc_driver_redis import dbapi
                with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
                    name = os.environ["REDIS_ADBC_TEST_TABLE"]
                    cur.execute(f'SELECT * FROM "{name}" WHERE id >= ? ORDER BY id', (minimum,))
                    table = cur.fetch_arrow_table()
                # Include schema and a worker PID even when the result is empty.
                yield json.dumps({"pid": os.getpid(), "rows": table.to_pylist(),
                                  "schema": str(table.schema)}, ensure_ascii=False)

        if __name__ == "__main__":
            options = PipelineOptions([
                "--direct_running_mode=multi_processing",
                "--direct_num_workers=2", "--save_main_session",
            ])
            # DirectRunner can select Prism and ignore the multi-processing
            # option. Select its local FnApi runner explicitly and verify PID.
            with beam.Pipeline(runner=FnApiRunner(), options=options) as pipeline:
                (pipeline | beam.Create([int(os.environ["REDIS_ADBC_MINIMUM"])])
                 | beam.ParDo(NativeRead())
                 | beam.io.WriteToText(os.environ["REDIS_ADBC_BEAM_OUTPUT"], num_shards=1))
            print("BEAM_DRIVER_PID=" + str(os.getpid()))
        '''))
    env = {**os.environ, "REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name,
           "REDIS_ADBC_MINIMUM": "4" if empty else "1",
           "REDIS_ADBC_BEAM_OUTPUT": str(tmp_path / "rows")}
    result = subprocess.run(
        [sys.executable, str(script)], cwd=tmp_path, env=env, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180,
    )
    assert result.returncode == 0, result.stdout
    parent_pid = int(result.stdout.split("BEAM_DRIVER_PID=")[-1].splitlines()[0])
    lines = [line for path in tmp_path.glob("rows-*") for line in path.read_text().splitlines()]
    assert len(lines) == 1
    output = json.loads(lines[0])
    assert output["pid"] != parent_pid
    assert output["rows"] == expected.to_pylist()
    assert output["schema"] == str(expected.schema)
