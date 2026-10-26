"""Incremental UNION ALL through the shared ADBC C driver and native IPC."""

import uuid

import adbc_driver_manager as adbc
import pyarrow as pa
import pytest
from adbc_driver_redis import connect


@pytest.fixture
def union_table(connection):
    name = f"it_union_c_{uuid.uuid4().hex}"
    rows = 25001
    data = pa.table(
        {
            "id": list(range(rows)),
            "label": [None if i % 7 == 0 else f"row-{i}" for i in range(rows)],
        }
    )
    with connection.cursor() as cur:
        cur.adbc_ingest(name, data)
    try:
        yield name, data
    finally:
        with connection.cursor() as cur:
            cur.execute(f'DROP TABLE "{name}"')


def test_union_c_stream_and_cancel(connection, union_table):
    name, data = union_table
    sql = f'SELECT id,label FROM "{name}" UNION ALL SELECT id,label FROM "{name}"'
    with connection.cursor() as cur:
        cur.adbc_statement.set_options(
            **{
                "adbc.redis.stream_batch_rows": "127",
                "adbc.redis.stream_batch_bytes": "4096",
            }
        )
        cur.execute(sql)
        assert cur.rowcount == -1
        with cur.fetch_record_batch() as reader:
            at = 0
            for batch in reader:
                assert 0 < batch.num_rows <= 127
                expected = [
                    {
                        "id": i % data.num_rows,
                        "label": None
                        if (i % data.num_rows) % 7 == 0
                        else f"row-{i % data.num_rows}",
                    }
                    for i in range(at, at + batch.num_rows)
                ]
                assert batch.to_pylist() == expected
                cost = sum(
                    512 + 2 * len((row["label"] or "").encode()) for row in expected
                )
                assert batch.num_rows == 1 or cost <= 4096
                at += batch.num_rows
            assert at == 2 * data.num_rows
        cur.execute(sql)
        with cur.fetch_record_batch() as reader:
            held = reader.read_next_batch()
            expected = held.to_pylist()
            cur.adbc_cancel()
            with pytest.raises(OSError, match="cancelled"):
                reader.read_next_batch()
            assert held.to_pylist() == expected
        cur.execute("SELECT 1")
        assert cur.fetchone() == (1,)


@pytest.mark.parametrize("format", ["stream", "file"])
def test_union_native_ipc(uri, union_table, tmp_path, format):
    name, data = union_table
    output = tmp_path / "union.arrow"
    sql = f'SELECT id,label FROM "{name}" UNION ALL SELECT id,label FROM "{name}"'
    with (
        connect(uri) as db,
        adbc.AdbcConnection(db) as conn,
        adbc.AdbcStatement(conn) as st,
    ):
        st.set_sql_query(sql)
        st.set_options(
            **{
                "adbc.redis.ipc.output": str(output),
                "adbc.redis.ipc.format": format,
                "adbc.redis.stream_batch_rows": "127",
            }
        )
        assert st.execute_update() == 2 * data.num_rows
        failed = tmp_path / "failed.arrow"
        st.set_sql_query(
            f'SELECT id,label FROM "{name}" UNION ALL SELECT 1/(id-id),label FROM "{name}"'
        )
        st.set_options(**{"adbc.redis.ipc.output": str(failed)})
        with pytest.raises(adbc.Error) as error:
            st.execute_update()
        assert error.value.sqlstate == "22012"
        assert not failed.exists()
    with pa.memory_map(str(output), "r") as source:
        if format == "file":
            reader = pa.ipc.open_file(source)
            batches = [reader.get_batch(i) for i in range(reader.num_record_batches)]
        else:
            batches = list(pa.ipc.open_stream(source))
        assert all(0 < batch.num_rows <= 127 for batch in batches)
        result = pa.Table.from_batches(batches)
        assert result.equals(pa.concat_tables([data, data]), check_metadata=False)
