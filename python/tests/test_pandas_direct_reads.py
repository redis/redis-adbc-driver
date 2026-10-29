"""Exercise pandas SQL APIs with a real Redis ADBC connection."""

import datetime
import uuid
import warnings

import pandas as pd
import pyarrow as pa
import pytest

from adbc_driver_redis import DatabaseOptions, dbapi


@pytest.fixture
def pandas_table(connection):
    name = f"it_pandas_{uuid.uuid4().hex}"
    with connection.cursor() as cur:
        cur.execute(
            f"CREATE TABLE {name} (id bigint, label text, qty integer, "
            "active boolean, amount double precision, ts timestamp)"
        )
        try:
            cur.execute(
                f"INSERT INTO {name} VALUES "
                "(9007199254740993, 'café 東京', 2, TRUE, 1.25, "
                "TIMESTAMP '2026-10-29 01:02:03.123456'), "
                "(9007199254740995, NULL, NULL, NULL, NULL, NULL), "
                "(9007199254740997, 'O''Reilly', 5, FALSE, 2.5, "
                "TIMESTAMP '2026-10-30 04:05:06.654321')"
            )
            expected = pa.table(
                {
                    "id": pa.array(
                        [9007199254740993, 9007199254740995, 9007199254740997],
                        type=pa.int64(),
                    ),
                    "label": pa.array(["café 東京", None, "O'Reilly"]),
                    "qty": pa.array([2, None, 5], type=pa.int32()),
                    "active": pa.array([True, None, False]),
                    "amount": pa.array([1.25, None, 2.5]),
                    "ts": pa.array(
                        [
                            datetime.datetime(2026, 10, 29, 1, 2, 3, 123456),
                            None,
                            datetime.datetime(2026, 10, 30, 4, 5, 6, 654321),
                        ],
                        type=pa.timestamp("us"),
                    ),
                }
            )
            yield name, expected
        finally:
            cur.execute(f"DROP TABLE {name}")


@pytest.mark.parametrize("read", [pd.read_sql, pd.read_sql_query])
@pytest.mark.parametrize("backend", [None, "pyarrow"])
def test_pandas_sql_values(connection, pandas_table, read, backend):
    name, expected = pandas_table
    kwargs = {} if backend is None else {"dtype_backend": backend}
    expected_df = expected.to_pandas(
        types_mapper=pd.ArrowDtype if backend == "pyarrow" else None
    )
    with warnings.catch_warnings():
        warnings.simplefilter("error")
        actual = read(f"SELECT * FROM {name} ORDER BY id", connection, **kwargs)
    pd.testing.assert_frame_equal(actual, expected_df)


@pytest.mark.parametrize("read", [pd.read_sql, pd.read_sql_query])
def test_pandas_sql_filter_aggregate_and_empty(connection, pandas_table, read):
    name, expected = pandas_table
    actual = read(
        f"SELECT id, label FROM {name} WHERE qty >= 5 ORDER BY id",
        connection,
        index_col="id",
    )
    pd.testing.assert_frame_equal(
        actual, expected.slice(2).select(["id", "label"]).to_pandas().set_index("id")
    )
    aggregate = read(
        f"SELECT COUNT(*) AS n, SUM(qty) AS total FROM {name}", connection
    )
    assert aggregate.to_dict("records") == [{"n": 3, "total": 7}]
    empty = read(
        f"SELECT * FROM {name} WHERE id < 0", connection, dtype_backend="pyarrow"
    )
    pd.testing.assert_frame_equal(
        empty, expected.slice(0, 0).to_pandas(types_mapper=pd.ArrowDtype)
    )
    # pandas closes its cursor, but must leave the caller's connection usable.
    assert read("SELECT 42 AS answer", connection).to_dict("records") == [
        {"answer": 42}
    ]


def test_pandas_sql_streamed_batches(uri, pandas_table):
    name, expected = pandas_table
    with dbapi.connect(
        uri,
        db_kwargs={
            DatabaseOptions.STREAM_RESULTS.value: "true",
            DatabaseOptions.STREAM_BATCH_ROWS.value: "1",
        },
    ) as conn:
        # Prove the source has multiple batches before testing pandas' full fetch.
        query = f"SELECT * FROM {name} ORDER BY id"
        with conn.cursor() as cur:
            cur.execute(query)
            batches = list(cur.fetch_record_batch())
        assert len(batches) > 1
        actual = pd.read_sql_query(query, conn, dtype_backend="pyarrow")
        pd.testing.assert_frame_equal(
            actual, expected.to_pandas(types_mapper=pd.ArrowDtype)
        )
