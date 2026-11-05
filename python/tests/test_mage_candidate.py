"""Run a real local Mage loader/transformer pipeline with native ADBC data."""

import json
import os
from pathlib import Path
import subprocess
import sys
import textwrap

import pytest


@pytest.mark.parametrize("empty", [False, True])
def test_mage_native_pipeline(uri, workflow_rows, tmp_path, empty):
    pytest.importorskip("mage_ai")
    name, expected = workflow_rows
    expected = expected.slice(0, 0) if empty else expected
    project = tmp_path / "project"
    executable = str(Path(sys.executable).parent / "mage")
    env = {**os.environ, "REDIS_URI": uri, "REDIS_ADBC_TEST_TABLE": name,
           "REDIS_ADBC_MINIMUM": "4" if empty else "1",
           "REDIS_ADBC_MAGE_OUTPUT": str(tmp_path / "checked.json"),
           "MAGE_DATA_DIR": str(tmp_path / "mage-data"), "DISABLE_NOTEBOOK_EDIT_ACCESS": "1",
           "MAGE_TELEMETRY_ENABLED": "0"}
    result = subprocess.run([executable, "init", str(project)], cwd=tmp_path, env=env,
                            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
    assert result.returncode == 0, result.stdout
    (project / "pipelines" / "native_adbc").mkdir(parents=True)
    (project / "pipelines" / "native_adbc" / "metadata.yaml").write_text(textwrap.dedent("""\
        uuid: native_adbc
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
        """))
    (project / "data_loaders" / "native_read.py").write_text(textwrap.dedent('''\
        import os
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
        '''))
    (project / "transformers" / "check_rows.py").write_text(textwrap.dedent('''\
        import json
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
            return frame
        '''))
    result = subprocess.run([executable, "run", str(project), "native_adbc"], cwd=tmp_path,
                            env=env, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=180)
    assert result.returncode == 0, result.stdout
    output = json.loads((tmp_path / "checked.json").read_text())
    assert output["rows"] == expected.to_pylist()
    assert output["columns"] == expected.column_names
    assert output["dtypes"] == {key: str(value) for key, value in expected.to_pandas().dtypes.items()}
