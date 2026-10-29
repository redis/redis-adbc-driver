"""Exercise pandas' native ADBC path against a disposable Redis with Search."""

import uuid
import warnings

import pandas as pd
import pyarrow as pa
import pytest
from pandas.testing import assert_frame_equal


@pytest.fixture
def pandas_table(connection):
    name = "it_pandas_" + uuid.uuid4().hex
    try:
        yield name
    finally:
        with connection.cursor() as cur:
            cur.execute(f'DROP TABLE IF EXISTS "{name}"')


def test_nullable_round_trip(connection, pandas_table):
    source = pd.DataFrame(
        {
            "id": pd.Series([1, 2, 3], dtype="int64[pyarrow]"),
            "label": pd.Series(["café", None, "東京"], dtype=pd.ArrowDtype(pa.string())),
            "amount": pd.Series([10.5, None, -2.25], dtype="float64[pyarrow]"),
            "active": pd.Series([True, False, None], dtype="bool[pyarrow]"),
            "quantity": pd.Series([2**53 + 1, None, -7], dtype="int64[pyarrow]"),
            "created_at": pd.Series(
                pd.to_datetime([
                    "2026-10-01 12:34:56.123456", None, "2026-10-03 00:00:00.000000"
                ]),
                dtype="timestamp[us][pyarrow]",
            ),
        }
    )
    with warnings.catch_warnings():
        warnings.simplefilter("error")
        assert source.to_sql(pandas_table, connection, index=False) == 3
        result = pd.read_sql_query(
            f'SELECT * FROM "{pandas_table}" ORDER BY id',
            connection,
            dtype_backend="pyarrow",
        )
    assert_frame_equal(result, source)


def test_append_filter_and_aggregate(connection, pandas_table):
    source = pd.DataFrame(
        {"id": [1, 2, 3, 4], "region": ["west", "east", "west", "east"],
         "amount": [10.5, 20.0, 4.5, 7.0]}
    )
    assert source.iloc[:2].to_sql(pandas_table, connection, index=False) == 2
    assert source.iloc[2:].to_sql(
        pandas_table, connection, index=False, if_exists="append"
    ) == 2
    result = pd.read_sql(
        f'SELECT region, SUM(amount) AS total FROM "{pandas_table}" '
        "WHERE amount >= 10 GROUP BY region ORDER BY region",
        connection,
    )
    expected = pd.DataFrame({"region": ["east", "west"], "total": [20.0, 10.5]})
    assert_frame_equal(result, expected)
    assert_frame_equal(
        pd.read_sql(f'SELECT * FROM "{pandas_table}" ORDER BY id', connection),
        source,
    )


def test_existing_table_modes(connection, pandas_table):
    original = pd.DataFrame({"id": [1, 2]})
    replacement = pd.DataFrame({"id": [42]})
    original.to_sql(pandas_table, connection, index=False)
    with pytest.raises(ValueError, match="already exists"):
        replacement.to_sql(pandas_table, connection, index=False)
    assert_frame_equal(
        pd.read_sql(f'SELECT * FROM "{pandas_table}" ORDER BY id', connection),
        original,
    )
    assert replacement.to_sql(
        pandas_table, connection, index=False, if_exists="replace"
    ) == 1
    assert_frame_equal(
        pd.read_sql(f'SELECT * FROM "{pandas_table}"', connection), replacement
    )
