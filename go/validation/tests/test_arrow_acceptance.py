# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Complete scalar/nested values and schemas across the native ADBC boundary."""
import json
import decimal

import pyarrow as pa
import pytest


def acceptance_inputs():
    cases = {}
    for bits, target in [(8, pa.int16()), (16, pa.int32()), (32, pa.int64()), (64, pa.decimal128(20, 0))]:
        values = [0, 2**(bits-1)-1, 2**(bits-1), 2**bits-1, None]
        source = pa.array(values, getattr(pa, f"uint{bits}")())
        cases[f"uint{bits}"] = source, source.cast(target)
    for bits, precision in [(32, 9), (64, 18), (128, 38), (256, 76)]:
        for scale in [-2, 0, 2]:
            coefficient = 10**precision - 1
            def from_coefficients(width):
                dtype = getattr(pa, f"decimal{width}")(precision, scale)
                raw = b"".join(v.to_bytes(width // 8, "little", signed=True) for v in [-coefficient, 0, coefficient, 0])
                return pa.Array.from_buffers(dtype, 4, [pa.py_buffer(bytes([7])), pa.py_buffer(raw)])
            source = from_coefficients(bits)
            cases[f"decimal{bits}_{scale}"] = source, from_coefficients(128 if precision <= 38 else 256)
    cases["date64"] = (
        pa.array([-86400001, -86400000, -1, 0, 1, 86400001, None], pa.date64()),
        pa.array([-2, -1, -1, 0, 0, 1, None], pa.date32()))
    for unit, factor in [("s", 1), ("ms", 1000), ("us", 10**6), ("ns", 10**9)]:
        time_type = pa.time32(unit) if unit in {"s", "ms"} else pa.time64(unit)
        time = pa.array([0, 1, 86400 * factor - 1, None], time_type)
        cases[f"time_{unit}"] = time, time
        for zone in [None, "America/New_York", "+05:30"]:
            ticks = [-1, 0, 1730611800 * factor, 1730615400 * factor, None]
            source = pa.array(ticks, pa.timestamp(unit, zone))
            expected = pa.array(ticks, pa.timestamp(unit, "UTC" if zone else None))
            cases[f"timestamp_{unit}_{zone}"] = source, expected
        values = [-1, 0, 1, None]
        source = pa.array(values, pa.duration(unit))
        expected = pa.array([(0, 0, v * (10**9 // factor)) if v is not None else None for v in values], pa.month_day_nano_interval())
        cases[f"duration_{unit}"] = source, expected
    interval = pa.array([(1, -2, 3), (-1, 2, -3), (0, 0, -(2**63)), (0, 0, 2**63-1), None], pa.month_day_nano_interval())
    cases["calendar_interval"] = interval, interval
    for name, dtype in [("large_string", pa.large_string()), ("string_view", pa.string_view())]:
        values = ["", None, "a\x00b", "view payload longer than twelve bytes"]
        cases[name] = pa.array(values, dtype), pa.array(values, pa.string())
    for name, dtype in [("large_binary", pa.large_binary()), ("binary_view", pa.binary_view()), ("fixed_binary", pa.binary(3))]:
        values = [b"\x00\xff\x00", None, b"a\x00b"]
        cases[name] = pa.array(values, dtype), pa.array(values, pa.binary())
    for name, dtype in [("list", pa.list_(pa.list_(pa.int64()))), ("large_list", pa.large_list(pa.list_(pa.int64())))]:
        values = [[[1, None], [], None], None, [], [[2], [3, 4]]]
        cases[name] = pa.array(values, dtype), pa.array(values, pa.list_(pa.list_(pa.int64())))
    struct_type = pa.struct([pa.field("Required", pa.int64(), nullable=False), pa.field("nested", pa.list_(pa.struct([("text", pa.string())])))])
    struct = pa.array([{"Required": 1, "nested": [{"text": "a"}, None, {"text": None}]}, None, {"Required": 2, "nested": []}], struct_type)
    cases["named_struct"] = struct, struct
    for binary in [False, True]:
        dictionary = pa.array([None, b"x" if binary else "x", b"unused" if binary else "unused"])
        source = pa.DictionaryArray.from_arrays(pa.array([1, 0, None, 1], pa.int16()), dictionary)
        cases[f"dictionary_{binary}"] = source, pa.array([b"x" if binary else "x", None, None, b"x" if binary else "x"], dictionary.type)
    return cases


@pytest.mark.parametrize("name", list(acceptance_inputs()))
def test_complete_interchange(conn, name, tmp_path):
    source, expected = acceptance_inputs()[name]
    table = "it_c_complete_interchange"
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            # Full Bind create, sliced BindStream append, and empty input.
            cur.adbc_ingest(table, pa.RecordBatch.from_arrays([source], names=["v"]))
            cur.adbc_ingest(table, pa.table({"v": source.slice(1)}), mode="append")
            cur.adbc_ingest(table, pa.table({"v": source.slice(0, 0)}), mode="append")
            want = pa.table({"v": pa.chunked_array([expected, expected.slice(1)])})
            query = f"SELECT v FROM {table} ORDER BY __rowid"
            schema = cur.adbc_execute_schema(query)
            cur.execute(query)
            assert cur.fetch_arrow_table().equals(want), name
            assert schema.equals(want.schema), name
            assert conn.adbc_get_table_schema(table).equals(want.schema), name
            # Also bind sliced parameters; this verifies the non-ingest path.
            cur.execute("SELECT ? AS v", pa.RecordBatch.from_arrays([source.slice(1)], names=["v"]))
            assert cur.fetch_arrow_table().equals(pa.table({"v": expected.slice(1)})), name
            for format in ["stream", "file"]:
                path = tmp_path / f"{format}.arrow"
                cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": str(path), "adbc.redis.ipc.format": format})
                cur.adbc_statement.set_sql_query(query)
                assert cur.adbc_statement.execute_update() == want.num_rows
                cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": ""})
                with pa.memory_map(str(path), "r") as data:
                    reader = pa.ipc.open_stream(data) if format == "stream" else pa.ipc.open_file(data)
                    assert reader.read_all().equals(want), name
        finally:
            cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": ""})
            cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("kind", ["uuid", "json"])
def test_allowlisted_extension_storage(conn, kind, tmp_path):
    if kind == "uuid":
        source = pa.ExtensionArray.from_storage(pa.uuid(), pa.array([b"0123456789abcdef", None, bytes(range(16))], pa.binary(16)))
        expected = pa.array([b"0123456789abcdef", None, bytes(range(16))], pa.binary())
    else:
        source = pa.ExtensionArray.from_storage(pa.json_(), pa.array(['{"a":[1,null]}', None, '"a string"']))
        expected = pa.array(['{"a":[1,null]}', None, '"a string"'])
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{"adbc.redis.extension_policy": "storage"})
        table = "it_c_extension_storage"
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            cur.adbc_ingest(table, pa.RecordBatch.from_arrays([source], names=["v"]))
            cur.adbc_ingest(table, pa.table({"v": source.slice(1)}), mode="append")
            want = pa.table({"v": pa.chunked_array([expected, expected.slice(1)])})
            query = f"SELECT v FROM {table} ORDER BY __rowid"
            cur.execute(query)
            assert cur.fetch_arrow_table().equals(want)
            assert conn.adbc_get_table_schema(table).equals(want.schema)
            cur.execute("SELECT ? AS v", pa.RecordBatch.from_arrays([source], names=["v"]))
            assert cur.fetch_arrow_table().equals(pa.table({"v": expected}))
            assert cur.adbc_execute_schema("SELECT ? AS v", pa.RecordBatch.from_arrays([source], names=["v"])).equals(pa.schema([("v", expected.type)]))
            for format in ["stream", "file"]:
                path = tmp_path / f"extension_{format}.arrow"
                cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": str(path), "adbc.redis.ipc.format": format})
                cur.adbc_statement.set_sql_query(query)
                assert cur.adbc_statement.execute_update() == want.num_rows
                cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": ""})
                with pa.memory_map(str(path), "r") as data:
                    reader = pa.ipc.open_stream(data) if format == "stream" else pa.ipc.open_file(data)
                    assert reader.read_all().equals(want)
        finally:
            cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": ""})
            cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_extension_unknown_and_invalid_semantics(conn):
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{"adbc.redis.extension_policy": "storage"})
        for name, metadata, dtype, value in [
            ("custom.semantic", b"v1", pa.string(), "opaque"),
            ("arrow.uuid", b"", pa.binary(15), bytes(15)),
            ("arrow.uuid", b'{"version":2}', pa.binary(16), bytes(16)),
            ("arrow.json", b"null", pa.string(), "{}"),
            ("arrow.json", b"x" * 16385, pa.string(), "{}"),
            ("arrow.json", b"", pa.string(), "invalid json"),
        ]:
            schema = pa.schema([pa.field("v", dtype, metadata={b"ARROW:extension:name": name.encode(), b"ARROW:extension:metadata": metadata})])
            data = pa.Table.from_arrays([pa.array([value], dtype)], schema=schema)
            with pytest.raises(Exception, match="extension|arrow.uuid|arrow.json|metadata"):
                cur.adbc_ingest("it_c_bad_extension", data)
            cur.execute("DROP TABLE IF EXISTS it_c_bad_extension")


def test_named_struct_append_schema_identity(conn):
    target_type = pa.struct([pa.field("x", pa.int64(), nullable=False)])
    with conn.cursor() as cur:
        table = "it_c_struct_append_schema"
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            cur.adbc_ingest(table, pa.table({"v": pa.array([{"x": 1}], target_type)}))
            for dtype in [
                pa.struct([pa.field("renamed", pa.int64(), nullable=False)]),
                pa.struct([pa.field("x", pa.int32(), nullable=False)]),
                pa.struct([pa.field("x", pa.int64(), nullable=True)]),
            ]:
                for values in [[], [None]]:
                    with pytest.raises(Exception, match="struct schema"):
                        cur.adbc_ingest(table, pa.table({"v": pa.array(values, dtype)}), mode="append")
            with pytest.raises(Exception, match="must not be NULL"):
                cur.adbc_ingest(table, pa.table({"v": pa.array([{"x": None}], target_type)}), mode="append")
            cur.execute(f"SELECT v FROM {table}")
            assert cur.fetch_arrow_table().equals(pa.table({"v": pa.array([{"x": 1}], target_type)}))
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("bits", [32, 64, 128, 256])
@pytest.mark.parametrize("scale", [-2, 0, 2])
def test_explicit_decimal_physical_schema(conn, bits, scale, tmp_path):
    source, _ = acceptance_inputs()[f"decimal{bits}_{scale}"]
    batch = pa.RecordBatch.from_arrays([source], names=["v"])
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{"adbc.redis.result_layouts": json.dumps({"v": f"decimal{bits}"})})
        assert cur.adbc_execute_schema("SELECT ? AS v", batch).equals(batch.schema)
        cur.execute("SELECT ? AS v", batch)
        assert cur.fetch_arrow_table().equals(pa.Table.from_batches([batch]))
        for mode in ["live", "retained"]:
            cur.adbc_statement.set_options(**{"adbc.redis.partition_result_mode": mode})
            with pytest.raises(Exception, match="result_layouts"):
                cur.adbc_execute_partitions("SELECT ? AS v", batch.slice(0, 1))


@pytest.mark.parametrize("layout", ["large_string", "string_view", "large_binary", "binary_view", "fixed_size_binary:3", "dictionary"])
def test_result_layout_buffers_and_ipc(conn, layout, tmp_path):
    binary = "binary" in layout
    values = [b"a\x00b", None, b"\x00\xff\x00", b"a\x00b"] if binary else ["", None, "payload longer than twelve bytes", ""]
    source = pa.table({"v": pa.array(values, pa.binary() if binary else pa.string())})
    target = {
        "large_string": pa.large_string(), "string_view": pa.string_view(),
        "large_binary": pa.large_binary(), "binary_view": pa.binary_view(),
        "fixed_size_binary:3": pa.binary(3), "dictionary": pa.dictionary(pa.int32(), pa.string()),
    }[layout]
    with conn.cursor() as cur:
        table = "it_c_result_layout_acceptance"
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            cur.adbc_ingest(table, source)
            cur.adbc_statement.set_options(**{"adbc.redis.result_layouts": json.dumps({"v": layout}), "adbc.redis.stream_batch_rows": "1"})
            query = f"SELECT v FROM {table} ORDER BY __rowid"
            assert cur.adbc_execute_schema(query).equals(pa.schema([("v", target)]))
            cur.execute(query)
            reader = cur.fetch_record_batch()
            first = reader.read_next_batch()
            rest = reader.read_all()
            reader.close()
            assert first.column(0).to_pylist() + rest.column(0).to_pylist() == values
            assert first.schema.field("v").type == target
            for format in ["stream", "file"]:
                path = tmp_path / f"layout_{format}.arrow"
                cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": str(path), "adbc.redis.ipc.format": format})
                cur.adbc_statement.set_sql_query(query)
                if layout == "dictionary" and format == "file":
                    with pytest.raises(Exception, match="dictionary.*IPC stream|per-batch dictionary"):
                        cur.adbc_statement.execute_update()
                    assert not path.exists()
                    continue
                assert cur.adbc_statement.execute_update() == len(values)
                with pa.memory_map(str(path), "r") as data:
                    result = (pa.ipc.open_stream(data) if format == "stream" else pa.ipc.open_file(data)).read_all()
                    assert result.column(0).to_pylist() == values
                    assert result.schema.field("v").type == target
            cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": ""})
            for mode in ["live", "retained"]:
                cur.adbc_statement.set_options(**{"adbc.redis.partition_result_mode": mode})
                with pytest.raises(Exception, match="result_layouts"):
                    cur.adbc_execute_partitions(query)
        finally:
            cur.adbc_statement.set_options(**{"adbc.redis.ipc.output": "", "adbc.redis.result_layouts": "{}"})
            cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_nested_array_indexing_and_element_overflow(conn):
    with conn.cursor() as cur:
        table = "it_c_nested_array_semantics"
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            cur.adbc_ingest(table, pa.table({"v": pa.array([[[1, None], [], None], None, []], pa.large_list(pa.list_(pa.int64())))}))
            cur.execute(f"SELECT v[1][1],v[1][2],v[2] FROM {table} ORDER BY __rowid")
            assert cur.fetchall() == [(1, None, []), (None, None, None), (None, None, None)]
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")

        cur.execute(f"CREATE TABLE {table} (v SMALLINT[])")
        try:
            with pytest.raises(Exception, match="range|smallint|SMALLINT"):
                cur.adbc_ingest(table, pa.table({"v": pa.array([[32768]], pa.list_(pa.int64()))}), mode="append")
            cur.execute(f"SELECT COUNT(*) FROM {table}")
            assert cur.fetchall() == [(0,)]
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_wide_decimal_arithmetic_and_schema(conn):
    largest = "9" * 38
    query = f"SELECT CAST({largest} AS NUMERIC(38,0)) + 1 AS v"
    with conn.cursor() as cur:
        schema = cur.adbc_execute_schema(query)
        cur.execute(query)
        result = cur.fetch_arrow_table()
        assert result.schema.equals(schema)
        assert pa.types.is_decimal256(schema.field("v").type)
        assert result["v"][0].as_py() == decimal.Decimal("1" + "0" * 38)
        query = f"SELECT SUM(v) AS v FROM (VALUES (CAST({largest} AS NUMERIC(38,0))),(CAST({largest} AS NUMERIC(38,0)))) t(v)"
        schema = cur.adbc_execute_schema(query)
        cur.execute(query)
        result = cur.fetch_arrow_table()
        assert result.schema.equals(schema)
        assert pa.types.is_decimal256(schema.field("v").type)
        assert result["v"][0].as_py() == decimal.Decimal(str(2 * (10**38 - 1)))
        with pytest.raises(Exception, match="numeric field overflow"):
            cur.execute(f"SELECT CAST({'9' * 76} AS NUMERIC(76,0)) + 1")
