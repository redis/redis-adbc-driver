# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Pinned binary Substrait execution, feature reporting and C admission."""
from pathlib import Path
import subprocess

import adbc_driver_manager
import pyarrow as pa
import pytest

FIXTURE = Path(__file__).resolve().parents[2] / "testdata/substrait/read.bin"


def _read(stmt):
    handle, _ = stmt.execute_query()
    with pa.RecordBatchReader._import_from_c(handle.address) as reader:
        return reader.read_all()


@pytest.mark.parametrize("materialized", [False, True])
def test_substrait_values_and_state(conn, materialized):
    with conn.cursor() as cur:
        cur.execute("DROP TABLE IF EXISTS it_substrait")
        cur.execute("CREATE TABLE it_substrait(id BIGINT NOT NULL, value TEXT)")
        try:
            cur.execute("INSERT INTO it_substrait VALUES (9007199254740993,'exact'),(9007199254740992,NULL)")
            stmt = cur.adbc_statement
            stmt.set_options(**{"adbc.redis.stream_results": "false" if materialized else "true"})
            stmt.set_substrait_plan(FIXTURE.read_bytes())
            stmt.prepare()
            schema_handle = stmt.execute_schema()
            schema = pa.Schema._import_from_c(schema_handle.address)
            assert schema == pa.schema([pa.field("id", pa.int64(), nullable=False), pa.field("value", pa.string())])
            with pytest.raises(adbc_driver_manager.Error):
                stmt.set_substrait_plan(b"\xff")
            result = _read(stmt).sort_by("id")
            expected = pa.Table.from_pylist([
                {"id": 9007199254740992, "value": None},
                {"id": 9007199254740993, "value": "exact"},
            ], schema=schema)
            assert result.equals(expected)
            stmt.set_sql_query("SELECT id,value FROM it_substrait ORDER BY id")
            sql = _read(stmt)
            assert sql.to_pylist() == result.to_pylist()
            assert [f.type for f in sql.schema] == [f.type for f in schema]
        finally:
            cur.execute("DROP TABLE IF EXISTS it_substrait")


def test_substrait_get_info(conn):
    # Requested and all-code queries must both be fully drained/released.
    for requested in ([4, 5, 6], None):
        handle = conn.adbc_connection.get_info(requested)
        with pa.RecordBatchReader._import_from_c(handle.address) as reader:
            rows = reader.read_all().to_pylist()
        values = {row["info_name"]: row["info_value"] for row in rows}
        assert values[4] is True
        assert values[5] == values[6] == "0.108.0"


def test_substrait_c_admission(driver_path, tmp_path):
    root = Path(__file__).resolve().parents[2]
    exe = tmp_path / "substrait_preflight"
    subprocess.run([
        "cc", str(Path(__file__).with_name("substrait_preflight.c")),
        "-I", str(root / "pkg"), "-I", str(Path(driver_path).parent),
        str(driver_path), "-o", str(exe),
        "-Wl,-rpath," + str(Path(driver_path).parent),
    ], check=True, capture_output=True, text=True)
    result = subprocess.run([str(exe)], cwd=Path(driver_path).parent, capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
