"""Native IPC serialization through the standard ADBC C statement interface."""

import datetime as dt
import decimal
import uuid

import adbc_driver_manager as adbc
import pyarrow as pa
import pytest
from adbc_driver_redis import connect


@pytest.mark.parametrize("format", ["stream", "file"])
@pytest.mark.parametrize("empty", [False, True])
def test_native_ipc_roundtrip(uri, tmp_path, format, empty):
    name = f"it_ipc_{uuid.uuid4().hex}"
    copied = name + "_copy"
    table = pa.table(
        {
            "id": pa.array([2**53 + 1, None, -3, 4, 5, 6], type=pa.int64()),
            "label": pa.array(["a\x00b", "", None, "é", "x", "y"]),
            "flag": pa.array([True, None, False, True, False, True]),
            "value": pa.array([1.5, None, -2.25, 0, 3, 4], type=pa.float64()),
            "payload": pa.array([b"\x00\xff", None, b"", b"x", b"y", b"z"]),
            "amount": pa.array(
                [
                    decimal.Decimal("12.34"),
                    None,
                    decimal.Decimal("-0.01"),
                    decimal.Decimal(0),
                    decimal.Decimal(2),
                    decimal.Decimal(3),
                ],
                type=pa.decimal128(20, 2),
            ),
            "day": pa.array(
                [
                    dt.date(2026, 10, 22),
                    None,
                    dt.date(1970, 1, 1),
                    dt.date(2000, 1, 1),
                    dt.date(2001, 1, 1),
                    dt.date(2002, 1, 1),
                ],
                type=pa.date32(),
            ),
            "at": pa.array(
                [
                    dt.datetime(2026, 10, 22, 1, 2, 3),
                    None,
                    dt.datetime(1970, 1, 1),
                    dt.datetime(2000, 1, 1),
                    dt.datetime(2001, 1, 1),
                    dt.datetime(2002, 1, 1),
                ],
                type=pa.timestamp("us"),
            ),
        }
    )
    if empty:
        table = table.slice(0, 0)
    source = tmp_path / "input.arrow"
    output = tmp_path / "output.arrow"
    new_writer = pa.ipc.new_file if format == "file" else pa.ipc.new_stream
    with pa.OSFile(str(source), "wb") as sink, new_writer(sink, table.schema) as writer:
        for batch in table.to_batches(max_chunksize=2):
            writer.write_batch(batch)

    with connect(uri) as db, adbc.AdbcConnection(db) as conn:
        try:
            with adbc.AdbcStatement(conn) as st:
                st.set_options(
                    **{
                        "adbc.ingest.target_table": name,
                        "adbc.redis.ipc.input": str(source),
                        "adbc.redis.ipc.format": format,
                    }
                )
                assert st.execute_update() == table.num_rows
            with adbc.AdbcStatement(conn) as st:
                st.set_sql_query(f'SELECT * FROM "{name}"')
                st.set_options(
                    **{
                        "adbc.redis.ipc.output": str(output),
                        "adbc.redis.ipc.format": format,
                        "adbc.redis.stream_batch_rows": "2",
                    }
                )
                assert st.get_option("adbc.redis.ipc.format") == format
                assert st.execute_update() == table.num_rows
                with pytest.raises(adbc.Error):
                    st.execute_update()  # Existing output must survive unchanged.
            with pa.memory_map(str(output), "r") as file:
                if format == "file":
                    reader = pa.ipc.open_file(file)
                    assert reader.num_record_batches == (1 if empty else 3)
                    result = reader.read_all()
                else:
                    reader = pa.ipc.open_stream(file)
                    schema = reader.schema
                    batches = list(reader)
                    assert len(batches) == (1 if empty else 3)
                    result = pa.Table.from_batches(batches, schema=schema)
            assert result.to_pylist() == table.to_pylist()
            with adbc.AdbcStatement(conn) as st:
                st.set_options(
                    **{
                        "adbc.ingest.target_table": copied,
                        "adbc.redis.ipc.input": str(output),
                        "adbc.redis.ipc.format": format,
                    }
                )
                assert st.execute_update() == table.num_rows
            with adbc.AdbcStatement(conn) as st:
                st.set_sql_query(f'SELECT * FROM "{copied}"')
                stream, _ = st.execute_query()
                with pa.RecordBatchReader.from_stream(stream) as reader:
                    assert reader.read_all().equals(result)
        finally:
            with adbc.AdbcStatement(conn) as st:
                for target in [copied, name]:
                    st.set_sql_query(f'DROP TABLE IF EXISTS "{target}"')
                    st.execute_update()


def test_native_ipc_rejects_invalid_execution(uri, tmp_path):
    with (
        connect(uri) as db,
        adbc.AdbcConnection(db) as conn,
        adbc.AdbcStatement(conn) as st,
    ):
        path = tmp_path / "rejected.arrow"
        st.set_options(**{"adbc.redis.ipc.output": str(path)})
        for query in [
            "CREATE TABLE it_ipc_must_not_exist (id int)",
            "SELECT 1; SELECT 2",
        ]:
            st.set_sql_query(query)
            with pytest.raises(adbc.Error):
                st.execute_update()
            assert not path.exists()
        st.set_sql_query("SELECT 1")
        with pytest.raises(adbc.Error):
            st.execute_query()  # Export must explicitly use the NULL-output path.
        st.set_options(**{"adbc.redis.ipc.input": str(path)})
        with pytest.raises(adbc.Error):
            st.execute_update()
        assert not path.exists()


def test_native_ipc_late_input_error(uri, tmp_path):
    name = f"it_ipc_bad_{uuid.uuid4().hex}"
    path = tmp_path / "truncated.arrow"
    schema = pa.schema([("id", pa.int64())])
    with pa.OSFile(str(path), "wb") as sink, pa.ipc.new_stream(sink, schema) as writer:
        for values in [[1, 2, 3], [4, 5, 6]]:
            writer.write_batch(pa.record_batch([pa.array(values)], schema=schema))
    # Truncate inside the second batch body, not just its optional EOS marker.
    data = path.read_bytes()
    path.write_bytes(data[:-24])
    with connect(uri) as db, adbc.AdbcConnection(db) as conn:
        try:
            with adbc.AdbcStatement(conn) as st:
                st.set_options(
                    **{
                        "adbc.ingest.target_table": name,
                        "adbc.redis.ipc.input": str(path),
                    }
                )
                with pytest.raises(adbc.Error):
                    st.execute_update()
            with adbc.AdbcStatement(conn) as st:
                st.set_sql_query(f'SELECT id FROM "{name}"')
                stream, _ = st.execute_query()
                with pa.RecordBatchReader.from_stream(stream) as reader:
                    assert reader.read_all().column("id").to_pylist() == [1, 2, 3]
        finally:
            with adbc.AdbcStatement(conn) as st:
                st.set_sql_query(f'DROP TABLE IF EXISTS "{name}"')
                st.execute_update()


def test_native_ipc_cancel_and_reuse(uri, tmp_path):
    import threading

    path = tmp_path / "cancelled.arrow"
    with (
        connect(uri) as db,
        adbc.AdbcConnection(db) as conn,
        adbc.AdbcStatement(conn) as st,
    ):
        st.set_sql_query(
            "SELECT sum(length(repeat(cast(g AS varchar), 2000))) FROM generate_series(1, 1000000) AS s(g)"
        )
        st.set_options(**{"adbc.redis.ipc.output": str(path)})
        errors = []

        def cancel():
            try:
                st.cancel()
            except adbc.Error as error:
                errors.append(error)

        timer = threading.Timer(0.1, cancel)
        timer.start()
        try:
            with pytest.raises(adbc.Error) as error:
                st.execute_update()
            assert error.value.status_code == adbc.AdbcStatusCode.CANCELLED
        finally:
            timer.cancel()
            timer.join()
        assert not errors
        assert not path.exists()
        st.set_sql_query("SELECT 1 AS one")
        assert st.execute_update() == 1
        with pa.ipc.open_stream(str(path)) as reader:
            assert reader.read_all().to_pylist() == [{"one": 1}]


def test_native_ipc_import_cancel(uri, tmp_path):
    import threading

    name = f"it_ipc_cancel_{uuid.uuid4().hex}"
    path = tmp_path / "large.arrow"
    batch = pa.record_batch({"id": list(range(1000))})
    with (
        pa.OSFile(str(path), "wb") as sink,
        pa.ipc.new_stream(sink, batch.schema) as writer,
    ):
        for _ in range(1000):
            writer.write_batch(batch)
    with connect(uri) as db, adbc.AdbcConnection(db) as conn:
        try:
            with adbc.AdbcStatement(conn) as st:
                st.set_options(
                    **{
                        "adbc.ingest.target_table": name,
                        "adbc.redis.ipc.input": str(path),
                    }
                )
                errors = []

                def cancel():
                    try:
                        st.cancel()
                    except adbc.Error as error:
                        errors.append(error)

                timer = threading.Timer(0.1, cancel)
                timer.start()
                try:
                    with pytest.raises(adbc.Error) as error:
                        st.execute_update()
                    assert error.value.status_code == adbc.AdbcStatusCode.CANCELLED
                finally:
                    timer.cancel()
                    timer.join()
                assert not errors
        finally:
            with adbc.AdbcStatement(conn) as st:
                st.set_sql_query(f'DROP TABLE IF EXISTS "{name}"')
                st.execute_update()
