# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Arrow normalization/layouts across the native C driver-manager boundary."""
import json

import pyarrow as pa
import pytest


def test_normalized_nested_and_unsigned(conn):
    table = "it_c_arrow_interop"
    source = pa.table({
        "u": pa.array([0, 2**63-1, 2**63, 2**64-1, None], pa.uint64()),
        "nested": pa.array([[[1, None], [], None], None, [], [[2]], [[3, 4]]], pa.large_list(pa.list_(pa.int64()))),
        "record": pa.array([{"name": None}, None, {"name": "a"}, {"name": "b"}, {"name": "c"}], pa.struct([pa.field("name", pa.string())])),
    })
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            cur.adbc_ingest(table, source)
            cur.execute(f"SELECT * FROM {table}")
            actual = cur.fetch_arrow_table()
            expected = source.set_column(0, "u", source["u"].cast(pa.decimal128(20, 0)))
            expected = expected.set_column(1, "nested", source["nested"].cast(pa.list_(pa.list_(pa.int64()))))
            assert actual.equals(expected)
            cur.execute(f"SELECT u FROM {table} WHERE u >= 9223372036854775808 ORDER BY u")
            assert [int(x[0]) for x in cur.fetchall()] == [2**63, 2**64-1]
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("layout", ["large_string", "string_view", "dictionary"])
def test_output_layout_retained_batches(conn, layout):
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{
            "adbc.redis.stream_batch_rows": "1",
            "adbc.redis.result_layouts": json.dumps({"s": layout}),
        })
        cur.execute("SELECT 'one' AS s UNION ALL SELECT 'two' AS s UNION ALL SELECT NULL AS s")
        reader = cur.fetch_record_batch()
        first = reader.read_next_batch()
        snapshot = first.column(0).to_pylist()
        rest = reader.read_all()
        reader.close()
        assert first.column(0).to_pylist() == snapshot
        assert snapshot + rest.column(0).to_pylist() == ["one", "two", None]
        if layout == "dictionary":
            assert pa.types.is_dictionary(first.schema.field(0).type)
        else:
            assert first.schema.field(0).type == getattr(pa, layout)()


def test_decimal_and_temporal_layouts(conn):
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{"adbc.redis.result_layouts": json.dumps({
            "d": "decimal32", "date": "date64", "iv": "duration:ms", "ts": "timestamp:America/New_York",
        })})
        cur.execute("SELECT CAST(-123.45 AS NUMERIC(9,2)) AS d, DATE '1969-12-31' AS date, INTERVAL '0.002 seconds' AS iv, TIMESTAMPTZ '2024-11-03 01:30:00-04' AS ts")
        result = cur.fetch_arrow_table()
        assert result.schema.field("d").type == pa.decimal32(9, 2)
        assert str(result["d"][0].as_py()) == "-123.45"
        assert result.schema.field("date").type == pa.date64()
        assert result["date"].cast(pa.int64()).to_pylist() == [-86400000]
        assert result["iv"].cast(pa.int64()).to_pylist() == [2]
        assert result.schema.field("ts").type.tz == "America/New_York"


def test_unknown_extension_storage_metadata_rejected(conn):
    schema = pa.schema([pa.field("x", pa.string(), metadata={b"ARROW:extension:name": b"test.unknown"})])
    data = pa.Table.from_arrays([pa.array(["opaque"])], schema=schema)
    with conn.cursor() as cur:
        with pytest.raises(Exception, match="extension"):
            cur.adbc_ingest("it_unknown_extension", data)


def test_normalized_layout_inputs(conn):
    arrays = [
        pa.array([[1, 2], None, [3, 4]], pa.list_(pa.int64(), 2)),
        pa.ListViewArray.from_arrays(pa.array([2, 0, 1], pa.int32()), pa.array([1, 2, 0], pa.int32()), pa.array([1, None, 3], pa.int64())),
        pa.RunEndEncodedArray.from_arrays(pa.array([2, 5, 7], pa.int16()), pa.array(["a", None, "c"])).slice(1, 5),
    ]
    with conn.cursor() as cur:
        for i, array in enumerate(arrays):
            table = f"it_c_arrow_layout_{i}"
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            try:
                cur.adbc_ingest(table, pa.table({"v": array}))
                cur.execute(f"SELECT v FROM {table}")
                assert cur.fetch_arrow_table().column(0).to_pylist() == array.to_pylist()
            finally:
                cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_registered_extension_rejected(conn):
    class Custom(pa.ExtensionType):
        def __init__(self):
            super().__init__(pa.string(), "redis.test.custom")
        def __arrow_ext_serialize__(self):
            return b"v1"
        @classmethod
        def __arrow_ext_deserialize__(cls, storage_type, serialized):
            return cls()
    for ext in [Custom(), pa.uuid()]:
        storage = pa.array([None], ext.storage_type)
        data = pa.table({"x": pa.ExtensionArray.from_storage(ext, storage)})
        with conn.cursor() as cur:
            with pytest.raises(Exception, match="extension|unsupported Arrow"):
                cur.adbc_ingest("it_registered_extension", data)
