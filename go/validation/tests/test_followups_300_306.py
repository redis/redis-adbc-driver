# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Native driver-manager acceptance for the bounded #300–#306 extensions."""
import decimal
import json
import os

import pyarrow as pa
import pytest

from .test_partitions import connect, worker_kwargs
from .test_partitions_retained import retained_namespace
from .test_statistics import entries


def layout_cases():
    for layout, dtype in [("large_string", pa.large_string()), ("string_view", pa.string_view())]:
        yield layout, pa.array(["é🐉", None, "", "é🐉"]), dtype
    for layout, dtype in [("large_binary", pa.large_binary()), ("binary_view", pa.binary_view()), ("fixed_size_binary:3", pa.binary(3))]:
        yield layout, pa.array([b"\x00\xff\x00", None, b"abc", b"abc"], pa.binary()), dtype
    for layout, precision in [("decimal32", 9), ("decimal64", 18), ("decimal128", 30), ("decimal256", 50)]:
        for scale in [-2, 0, 2]:
            values = [decimal.Decimal("-100"), None, decimal.Decimal("0"), decimal.Decimal("100")]
            yield layout, pa.array(values, pa.decimal128(precision, scale) if precision <= 38 else pa.decimal256(precision, scale)), getattr(pa, layout)(precision, scale)
    yield "date64", pa.array([-1, None, 0, 1], pa.date32()), pa.date64()
    yield "timestamp:America/New_York", pa.array([-1, None, 0, 1730611800000000], pa.timestamp("us", "UTC")), pa.timestamp("us", "America/New_York")
    for unit in ["s", "ms", "us", "ns"]:
        yield "duration:" + unit, pa.array([-1, None, 0, 1], pa.duration(unit)), pa.duration(unit)


@pytest.mark.parametrize("layout,source,target", list(layout_cases()))
@pytest.mark.parametrize("mode", ["live", "retained"])
def test_partition_layout_independent_replay(driver_path, db_kwargs, layout, source, target, mode):
    table = "it_followup_layouts"
    with retained_namespace(db_kwargs) as options:
        with connect(driver_path, options) as producer:
            with producer.cursor() as cur:
                cur.execute(f"DROP TABLE IF EXISTS {table}")
                cur.adbc_ingest(table, pa.table({"id": pa.array(range(len(source)), pa.int64()), "v": source}))
                cur.adbc_statement.set_options(**{"adbc.redis.result_layouts": json.dumps({"v": layout}), "adbc.redis.stream_batch_rows": "1", "adbc.redis.partition_result_mode": mode, "adbc.redis.partition_scan_cap": int(os.environ.get("REDIS_PARTITION_SCAN_CAP", "0"))})
                query = f"SELECT id,v FROM {table}"
                cur.execute(query)
                expected = cur.fetch_arrow_table()
                assert expected.schema.field("v").type == target
                parts, schema = cur.adbc_execute_partitions(query)
                assert schema == expected.schema
        try:
            if mode == "retained":
                with connect(driver_path, options) as cleaner:
                    with cleaner.cursor() as cur:
                        cur.execute(f"TRUNCATE TABLE {table}")
            reader_options = worker_kwargs(options)
            if mode == "retained":
                reader_options["adbc.redis.partition_reader_only"] = "true"
            with connect(driver_path, reader_options) as reader:
                with reader.cursor() as cur:
                    chunks = []
                    for part in parts:
                        cur.adbc_read_partition(part)
                        result = cur.fetch_arrow_table()
                        assert result.schema == schema
                        chunks.extend(result.to_batches())
                    actual = pa.Table.from_batches(chunks, schema=schema)
                    def values(table):
                        ids = table.column("id").to_pylist()
                        column = table.column("v")
                        if pa.types.is_duration(target):
                            column = column.cast(pa.int64())
                        return sorted(zip(ids, column.to_pylist()))
                    assert values(actual) == values(expected)
        finally:
            with connect(driver_path, options) as cleaner:
                with cleaner.cursor() as cur:
                    cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_nested_extension_normalization(conn, tmp_path):
    uuid_values = [bytes(range(16)), b"0123456789abcdef", None, bytes(reversed(range(16)))]
    json_values = ['{"é":[1,null]}', 'not JSON under a NULL parent', None, '"tail"']
    uuid = pa.ExtensionArray.from_storage(pa.uuid(), pa.array(uuid_values, pa.binary(16)))
    js = pa.ExtensionArray.from_storage(pa.json_(), pa.array(json_values))
    source = pa.StructArray.from_arrays([uuid, js], names=["u", "j"], mask=pa.array([False, True, False, False]))
    expected = pa.StructArray.from_arrays([pa.array(uuid_values, pa.binary()), pa.array(json_values)], names=["u", "j"], mask=pa.array([False, True, False, False]))
    # A second STRUCT level and nonzero parent offsets exercise child offsets.
    source = pa.StructArray.from_arrays([source], names=["inner"])
    expected = pa.StructArray.from_arrays([expected], names=["inner"])
    want = pa.table({"v": pa.chunked_array([expected, expected.slice(1)])})
    table = "it_followup_extensions"
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        cur.adbc_statement.set_options(**{"adbc.redis.extension_policy": "storage"})
        try:
            cur.adbc_ingest(table, pa.RecordBatch.from_arrays([source], names=["v"]))
            cur.adbc_ingest(table, pa.table({"v": source.slice(1)}), mode="append")
            query = f"SELECT v FROM {table} ORDER BY __rowid"
            cur.execute(query)
            assert cur.fetch_arrow_table().equals(want)
            cur.execute("SELECT ? AS v", pa.RecordBatch.from_arrays([source.slice(1)], names=["v"]))
            assert cur.fetch_arrow_table().equals(pa.table({"v": expected.slice(1)}))
            for format in ["stream", "file"]:
                path = tmp_path / f"nested_{format}.arrow"
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


def test_column_null_counts_defaults_and_nan(conn):
    table = "it_followup_null_counts"
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        cur.execute(f"CREATE TABLE {table} (id BIGINT, v DOUBLE PRECISION, text_value TEXT)")
        try:
            cur.execute(f"INSERT INTO {table} VALUES (1,'NaN',''), (2,NULL,NULL), (3,0,'é')")
            cur.execute(f"ALTER TABLE {table} ADD COLUMN added INTEGER DEFAULT 42")
            cur.execute(f"UPDATE {table} SET added=NULL WHERE id=2")
            values = entries(conn, table_name_filter=table, approximate=False)
            assert {(v["column_name"], v["statistic_key"]): v["statistic_value"] for v in values} == {(None, 6): 3, ("id", 5): 0, ("v", 5): 1, ("text_value", 5): 1, ("added", 5): 1}
            assert all(v["statistic_is_approximate"] for v in values)
            cur.execute(f"TRUNCATE TABLE {table}")
            values = entries(conn, table_name_filter=table, approximate=False)
            assert len(values) == 5 and all(v["statistic_value"] == 0 for v in values)
        finally:
            cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_stream_distinct_matches_ordinary(conn):
    table = "it_followup_distinct"
    data = pa.table({"v": pa.array([None, "é", "é", "", None, "a\x00b", ""] * 100, pa.string()), "n": pa.array([None, 9007199254740993, 9007199254740993, 0, None, -1, 0] * 100, pa.int64())})
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        cur.adbc_ingest(table, data)
        try:
            for suffix in ["", " LIMIT 2", " LIMIT 2 OFFSET 1", " OFFSET 2", " WHERE v IS NULL"]:
                query = f"SELECT DISTINCT v,n FROM {table}" + suffix
                cur.adbc_statement.set_options(**{"adbc.redis.stream_required": "false", "adbc.redis.stream_results": "false"})
                cur.execute(query)
                expected = cur.fetch_arrow_table()
                cur.adbc_statement.set_options(**{"adbc.redis.stream_results": "true", "adbc.redis.stream_required": "true", "adbc.redis.stream_batch_rows": "1"})
                cur.execute(query)
                with cur.fetch_record_batch() as reader:
                    actual = reader.read_all()
                assert actual.schema == expected.schema
                unique = list(dict.fromkeys(zip(data.column("v").to_pylist(), data.column("n").to_pylist())))
                if suffix == " WHERE v IS NULL": unique = [row for row in unique if row[0] is None]
                if suffix == " LIMIT 2": unique = unique[:2]
                if suffix == " LIMIT 2 OFFSET 1": unique = unique[1:3]
                if suffix == " OFFSET 2": unique = unique[2:]
                assert list(zip(actual.column("v").to_pylist(), actual.column("n").to_pylist())) == unique
                # Neither path promises order without ORDER BY. Ordinary DISTINCT
                # may use a grouped plan, so LIMIT can select different valid rows.
                assert actual.num_rows == expected.num_rows
                if suffix in ["", " WHERE v IS NULL"]:
                    assert sorted(actual.to_pylist(), key=repr) == sorted(expected.to_pylist(), key=repr)
        finally:
            cur.adbc_statement.set_options(**{"adbc.redis.stream_required": "false"})
            cur.execute(f"DROP TABLE IF EXISTS {table}")


def test_retained_layout_loss_rejects_before_publication(driver_path, db_kwargs):
    with retained_namespace(db_kwargs) as options:
        with connect(driver_path, options) as conn, conn.cursor() as cur:
            cur.adbc_statement.set_options(**{"adbc.redis.partition_result_mode": "retained", "adbc.redis.result_layouts": json.dumps({"v": "duration:s"})})
            with pytest.raises(Exception, match="lose precision"):
                cur.adbc_execute_partitions("SELECT INTERVAL '0.000001 second' AS v")


def test_nested_extension_unknown_semantics_reject(conn):
    field = pa.field("j", pa.string(), metadata={b"ARROW:extension:name": b"custom.unknown"})
    source = pa.StructArray.from_arrays([pa.array(["{}", None])], fields=[field])
    with conn.cursor() as cur:
        cur.adbc_statement.set_options(**{"adbc.redis.extension_policy": "storage"})
        with pytest.raises(Exception, match="allowlisted"):
            cur.execute("SELECT ? AS v", pa.RecordBatch.from_arrays([source], names=["v"]))


def test_partition_byte_length_argument_errors(driver_path, db_kwargs):
    table = "it_followup_byte_args"
    with retained_namespace(db_kwargs) as options:
        with connect(driver_path, options) as conn, conn.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {table}")
            cur.execute(f"CREATE TABLE {table} (v TEXT)")
            try:
                for call in ["OCTET_LENGTH()", "BIT_LENGTH('a','b')", "OCTET_LENGTH(42)", "BIT_LENGTH(CAST('a' AS CHAR(3)))"]:
                    for mode in ["ordinary", "live", "retained"]:
                        query = f"SELECT {call} FROM {table}"
                        try:
                            if mode == "ordinary": cur.execute(query)
                            else:
                                cur.adbc_statement.set_options(**{"adbc.redis.partition_result_mode": mode})
                                cur.adbc_execute_partitions(query)
                        except Exception as error:
                            assert "expects" in str(error) or "does not exist" in str(error)
                        else:
                            pytest.fail(f"{mode}: {call} accepted invalid arguments")
            finally:
                cur.execute(f"DROP TABLE IF EXISTS {table}")
