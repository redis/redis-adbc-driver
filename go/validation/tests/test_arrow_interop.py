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


def layout_inventory_inputs():
    i64 = pa.int64()
    inputs = {
        "fixed": pa.array([[91, 92], [7, None], None, [-3, 4]], pa.list_(i64, 2)),
        "nested_fixed": pa.array([[[1], []], None, [None, [2, None]]], pa.list_(pa.list_(i64), 2)),
        "view": pa.ListViewArray.from_arrays(
            pa.array([3, 0, 0, 0, 1], pa.int32()), pa.array([1, 2, 0, 0, 2], pa.int32()),
            pa.array([7, None, 8, 9], i64), mask=pa.array([False, False, True, False, False])),
        "large_view": pa.LargeListViewArray.from_arrays(
            pa.array([3, 0, 0, 0, 1], i64), pa.array([1, 2, 0, 0, 2], i64),
            pa.array([7, None, 8, 9], i64), mask=pa.array([False, False, True, False, False])),
    }
    for width in [16, 32, 64]:
        inputs[f"run_end_{width}"] = pa.RunEndEncodedArray.from_arrays(
            pa.array([2, 5, 7], getattr(pa, f"int{width}")()),
            pa.array(["long string that must own its bytes", None, "last"]))
    inputs["run_end_list"] = pa.RunEndEncodedArray.from_arrays(
        pa.array([2, 3, 5], pa.int16()), pa.array([[1, None], None, []], pa.list_(i64)))
    inputs["list_of_run_end"] = pa.ListArray.from_arrays(
        pa.array([0, 2, 5, 7], pa.int32()), inputs["run_end_16"])
    inputs["map"] = pa.array([[('duplicate', 1), ('duplicate', None)], None, []], pa.map_(pa.string(), i64))
    ids = pa.array([3, 7, 3], pa.int8())
    inputs["dense_union"] = pa.UnionArray.from_dense(
        ids, pa.array([0, 0, 1], pa.int32()), [pa.array([9, None], i64), pa.array(["text"])],
        field_names=["n", "s"], type_codes=[3, 7])
    inputs["sparse_union"] = pa.UnionArray.from_sparse(
        ids, [pa.array([9, None, None], i64), pa.array([None, "text", None], pa.string())],
        field_names=["n", "s"], type_codes=[3, 7])
    return inputs


@pytest.mark.parametrize("name", [
    "fixed", "nested_fixed", "view", "large_view", "run_end_16", "run_end_32", "run_end_64", "run_end_list",
    "list_of_run_end", "map", "dense_union", "sparse_union",
])
def test_layout_inventory_c_bindings(conn, name):
    source = layout_inventory_inputs()[name]
    canonical_type = pa.string() if name.startswith("run_end_") and name != "run_end_list" else pa.list_(pa.int64())
    if name == "nested_fixed":
        canonical_type = pa.list_(pa.list_(pa.int64()))
    elif name == "list_of_run_end":
        canonical_type = pa.list_(pa.string())
    elif name == "map":
        canonical_type = source.type
    elif name in {"dense_union", "sparse_union"}:
        canonical_type = pa.union(list(source.type), mode="dense", type_codes=source.type.type_codes)
    with conn.cursor() as cur:
        for start, length in [(0, len(source)), (1, len(source) - 1), (0, 0)]:
            sliced = source.slice(start, length)
            # RecordBatch uses C Bind; Table uses C BindStream. NULLs in Union
            # are selected-child NULLs because Union has no parent validity bitmap.
            for stream in [False, True]:
                table = f"it_c_inventory_{name}"
                cur.execute(f"DROP TABLE IF EXISTS {table}")
                batch = pa.RecordBatch.from_arrays([sliced], names=["v"])
                data = pa.Table.from_batches([batch]) if stream else batch
                try:
                    cur.adbc_ingest(table, data)
                    cur.execute(f"SELECT v FROM {table} ORDER BY __rowid")
                    actual = cur.fetch_arrow_table()
                    if name in {"dense_union", "sparse_union"}:
                        # Independent dense oracle preserves the selected tag on NULL children.
                        ids = [source.type_codes[i].as_py() for i in range(start, start + length)]
                        children, offsets = [[], []], []
                        for i, code in enumerate(ids):
                            child = source.type.type_codes.index(code)
                            offsets.append(len(children[child]))
                            children[child].append(source[start+i].as_py())
                        canonical = pa.UnionArray.from_dense(pa.array(ids, pa.int8()), pa.array(offsets, pa.int32()),
                            [pa.array(children[0], pa.int64()), pa.array(children[1], pa.string())],
                            field_names=["n", "s"], type_codes=[3, 7])
                    else:
                        canonical = pa.array(sliced.to_pylist(), canonical_type)
                    expected = pa.table({"v": canonical})
                    assert actual.equals(expected), (name, start, stream, actual, expected)
                    assert conn.adbc_get_table_schema(table).equals(expected.schema)
                finally:
                    cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_layout_expansion_rejected_before_rows(conn):
    sources = [
        pa.RunEndEncodedArray.from_arrays(pa.array([2**20 + 1], pa.int64()), pa.array([7], pa.int64())),
        pa.RunEndEncodedArray.from_arrays(pa.array([65], pa.int16()), pa.array(["x" * 2**20])),
    ]
    with conn.cursor() as cur:
        for source in sources:
            for stream in [False, True]:
                table = "it_c_layout_expansion_guard"
                cur.execute(f"DROP TABLE IF EXISTS {table}")
                batch = pa.RecordBatch.from_arrays([source], names=["v"])
                data = pa.Table.from_batches([batch]) if stream else batch
                try:
                    with pytest.raises(Exception, match="Arrow normalization exceeds"):
                        cur.adbc_ingest(table, data)
                    cur.execute(f"SELECT COUNT(*) FROM {table}")
                    assert cur.fetchall() == [(0,)]
                finally:
                    cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("name", ["map", "dense_union", "sparse_union"])
@pytest.mark.parametrize("stream", [False, True])
def test_map_union_append_schema_is_exact(conn, name, stream):
    source = layout_inventory_inputs()[name]
    if name == "map":
        changed = pa.array([], pa.map_(pa.string(), pa.string()))
    else:
        changed_type = pa.union([pa.field("n", pa.int64()), pa.field("s", pa.string())],
                                mode="dense", type_codes=[7, 3])
        changed = pa.UnionArray.from_dense(pa.array([], pa.int8()), pa.array([], pa.int32()),
            [pa.array([], pa.int64()), pa.array([], pa.string())],
            field_names=["n", "s"], type_codes=changed_type.type_codes)
    table = f"it_c_exact_{name}"
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            cur.adbc_ingest(table, pa.table({"v": source}))
            before = conn.adbc_get_table_schema(table)
            batch = pa.RecordBatch.from_arrays([changed], names=["v"])
            with pytest.raises(Exception, match="schema|match"):
                cur.adbc_ingest(table, pa.Table.from_batches([batch]) if stream else batch, mode="append")
            cur.execute(f"SELECT v FROM {table} ORDER BY __rowid")
            result = cur.fetch_arrow_table()
            assert result.column(0).to_pylist() == source.to_pylist()
            assert result.schema.equals(before)
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("bad", ["negative_tag", "unknown_tag", "negative_offset", "past_child", "map_offset"])
@pytest.mark.parametrize("stream", [False, True])
def test_map_union_malformed_buffers(conn, bad, stream):
    import ctypes

    source = layout_inventory_inputs()["map" if bad == "map_offset" else "dense_union"]
    if bad.endswith("tag"):
        cell = ctypes.c_int8.from_address(source.buffers()[1].address)
        replacement = -1 if bad == "negative_tag" else 4
    else:
        buffer = source.buffers()[1 if bad == "map_offset" else 2]
        cell = ctypes.c_int32.from_address(buffer.address + (4 if bad == "map_offset" else 0))
        replacement = -1 if bad == "negative_offset" else 2**30
    original = cell.value
    cell.value = replacement
    table = "it_c_malformed_container"
    try:
        batch = pa.RecordBatch.from_arrays([source], names=["v"])
        with conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            try:
                with pytest.raises(Exception, match="invalid|offset|type id|bounds"):
                    cur.adbc_ingest(table, pa.Table.from_batches([batch]) if stream else batch)
                # Creation may precede batch validation. No malformed rows may appear.
                try:
                    conn.adbc_get_table_schema(table)
                except Exception:
                    pass
                else:
                    cur.execute(f"SELECT COUNT(*) FROM {table}")
                    assert cur.fetchall() == [(0,)]
            finally:
                cur.execute(f"DROP TABLE IF EXISTS {table}")
    finally:
        cell.value = original


def test_nested_map_union_c_bindings(conn):
    i64 = pa.int64()
    map_type = pa.map_(pa.string(), pa.list_(i64))
    sources = [
        pa.array([[[("a", [1, None])], None], [], None], pa.list_(map_type)),
        pa.array([{"m": [("a", [1])]}, None, {"m": []}], pa.struct([pa.field("m", map_type)])),
        pa.UnionArray.from_dense(pa.array([11, 42, 11], pa.int8()), pa.array([0, 0, 1], pa.int32()),
            [pa.array([[("b", [2])], None], map_type), pa.array([[3, None]], pa.list_(i64))],
            field_names=["m", "xs"], type_codes=[11, 42]),
    ]
    with conn.cursor() as cur:
        for i, source in enumerate(sources):
            for start, length in [(0, len(source)), (1, len(source)-1), (0, 0)]:
                sliced = source.slice(start, length)
                for stream in [False, True]:
                    table = f"it_c_nested_container_{i}"
                    batch = pa.RecordBatch.from_arrays([sliced], names=["v"])
                    cur.execute(f"DROP TABLE IF EXISTS {table}")
                    try:
                        cur.adbc_ingest(table, pa.Table.from_batches([batch]) if stream else batch)
                        cur.execute(f"SELECT v FROM {table} ORDER BY __rowid")
                        actual = cur.fetch_arrow_table()
                        assert actual.equals(pa.Table.from_batches([batch]))
                        assert conn.adbc_get_table_schema(table).equals(actual.schema)
                    finally:
                        cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("stream", [False, True])
def test_map_null_key_c_import_does_not_poison_driver(conn, stream):
    import ctypes as ct
    import adbc_driver_manager as adm

    class ArrowArray(ct.Structure):
        pass
    ArrowArray._fields_ = [
        ("length", ct.c_int64), ("null_count", ct.c_int64), ("offset", ct.c_int64),
        ("n_buffers", ct.c_int64), ("n_children", ct.c_int64), ("buffers", ct.POINTER(ct.c_void_p)),
        ("children", ct.POINTER(ct.POINTER(ArrowArray))), ("dictionary", ct.POINTER(ArrowArray)),
        ("release", ct.c_void_p), ("private_data", ct.c_void_p),
    ]
    class ArrowStream(ct.Structure):
        _fields_ = [(name, ct.c_void_p) for name in ["get_schema", "get_next", "get_last_error", "release", "private_data"]]

    def corrupt_key(array):
        # A known NULL count is sufficient to reject before Arrow Go's Map
        # constructor. Keep the producer buffers/callbacks intact for cleanup.
        key = array.children[0].contents.children[0].contents.children[0].contents
        key.null_count = 1

    data = pa.record_batch([pa.array([[("key", 1)]], pa.map_(pa.string(), pa.int64()))], names=["v"])
    with conn.cursor() as cur:
        st = cur.adbc_statement
        st.set_options(**{"adbc.ingest.target_table": "it_c_null_map_key"})
        if stream:
            handle = adm.ArrowArrayStreamHandle()
            pa.RecordBatchReader.from_batches(data.schema, [data])._export_to_c(handle.address)
            header = ArrowStream.from_address(handle.address)
            callback_type = ct.CFUNCTYPE(ct.c_int, ct.POINTER(ArrowStream), ct.POINTER(ArrowArray))
            original = callback_type(header.get_next)
            @callback_type
            def checked_next(source, out):
                status = original(source, out)
                if status == 0 and out.contents.release:
                    corrupt_key(out.contents)
                return status
            header.get_next = ct.cast(checked_next, ct.c_void_p).value
            st.bind_stream(handle)
            with pytest.raises(Exception, match="Map.*NULL"):
                st.execute_update()
        else:
            array, schema = adm.ArrowArrayHandle(), adm.ArrowSchemaHandle()
            data._export_to_c(array.address, schema.address)
            corrupt_key(ArrowArray.from_address(array.address))
            with pytest.raises(Exception, match="Map.*NULL"):
                st.bind(array, schema)
        # Constructor panics poison every statement in the driver. Prove this
        # malformed producer caused a normal argument error and permits cleanup.
        cur.execute("DROP TABLE IF EXISTS it_c_null_map_key")
        cur.execute("SELECT 1")
        assert cur.fetchall() == [(1,)]


def test_map_item_nullability_is_canonical(conn):
    declared = pa.map_(pa.string(), pa.field("value", pa.int64(), nullable=False))
    source = pa.array([[("k", 2)], [], None], declared)
    with conn.cursor() as cur:
        table = "it_c_map_item_nullability"
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        try:
            cur.adbc_ingest(table, pa.table({"v": source}))
            cur.execute(f"SELECT v FROM {table} ORDER BY __rowid")
            expected = pa.table({"v": pa.array(source.to_pylist(), pa.map_(pa.string(), pa.int64()))})
            assert cur.fetch_arrow_table().equals(expected)
            assert conn.adbc_get_table_schema(table).equals(expected.schema)
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")


@pytest.mark.parametrize("nullable", ["entries", "key"])
def test_map_nullable_key_schema_c_rejected(conn, nullable):
    import ctypes as ct
    import adbc_driver_manager as adm

    class ArrowSchema(ct.Structure):
        pass
    ArrowSchema._fields_ = [
        ("format", ct.c_char_p), ("name", ct.c_char_p), ("metadata", ct.c_void_p),
        ("flags", ct.c_int64), ("n_children", ct.c_int64),
        ("children", ct.POINTER(ct.POINTER(ArrowSchema))), ("dictionary", ct.POINTER(ArrowSchema)),
        ("release", ct.c_void_p), ("private_data", ct.c_void_p),
    ]
    data = pa.record_batch([pa.array([], pa.map_(pa.string(), pa.int64()))], names=["v"])
    array, schema = adm.ArrowArrayHandle(), adm.ArrowSchemaHandle()
    data._export_to_c(array.address, schema.address)
    entry = ArrowSchema.from_address(schema.address).children[0].contents.children[0].contents
    child = entry if nullable == "entries" else entry.children[0].contents
    child.flags |= 2  # ARROW_FLAG_NULLABLE
    with conn.cursor() as cur:
        with pytest.raises(Exception, match="Map.*non-nullable"):
            cur.adbc_statement.bind(array, schema)
        cur.execute("SELECT 1")
        assert cur.fetchall() == [(1,)]
