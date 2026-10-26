"""Native Arrow list ownership, ingestion and ANY parameter regression tests."""
import pyarrow as pa


def test_array_ingest_and_parameter(connection):
    table = pa.table({
        "id": pa.array([1, 2, 3], type=pa.int32()),
        "vals": pa.array([[1, None, 3], [], None], type=pa.list_(pa.int64())),
        "tags": pa.array([["a,b", "NULL", ""], [None], []], type=pa.list_(pa.string())),
    })
    with connection.cursor() as cur:
        cur.execute("DROP TABLE IF EXISTS it_python_arrays")
        try:
            cur.adbc_ingest("it_python_arrays", table, mode="create")
            cur.execute("SELECT id, vals, tags FROM it_python_arrays ORDER BY id")
            result = cur.fetch_arrow_table()
            assert result.equals(table, check_metadata=False)
            params = pa.record_batch(
                [pa.array([[1, None, 3]], type=pa.list_(pa.int32()))], names=["ids"]
            )
            cur.execute("SELECT id FROM it_python_arrays WHERE id=ANY(?) ORDER BY id", params)
            assert cur.fetchall() == [(1,), (3,)]
            cur.execute("SELECT vals FROM it_python_arrays WHERE id=2")
            assert cur.fetch_arrow_table().column(0).to_pylist() == [[]]
            assert result.equals(table, check_metadata=False)  # survives later executions
        finally:
            cur.execute("DROP TABLE IF EXISTS it_python_arrays")
