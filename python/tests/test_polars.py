"""Exercise Polars' public ADBC APIs against a disposable Redis with Search."""

from datetime import date, datetime
from uuid import uuid4

import polars as pl
import pytest
from polars.testing import assert_frame_equal


@pytest.fixture
def polars_table(connection):
    name = f"it_polars_{uuid4().hex}"
    frame = pl.DataFrame(
        {
            "id": [1, 2, 3, 4],
            "category": ["east", "west", "east", None],
            "amount": [10.5, 20.0, None, -2.5],
            "active": [True, False, None, True],
            "day": [date(2026, 1, 1), None, date(2026, 1, 3), date(2026, 1, 4)],
            "created": [datetime(2026, 1, 1, 12, 0, 0, 123456), None, None, None],
        },
        schema_overrides={"created": pl.Datetime("us")},
    )
    try:
        assert frame.write_database(name, connection, engine="adbc") == frame.height
        yield name, frame
    finally:
        with connection.cursor() as cur:
            cur.execute(f"DROP TABLE IF EXISTS {name}")


@pytest.mark.parametrize("use_uri", [False, True], ids=["connection", "uri"])
def test_polars_roundtrip(uri, connection, polars_table, use_uri):
    name, expected = polars_table
    query = f"SELECT * FROM {name} ORDER BY id"
    actual = (
        pl.read_database_uri(query, uri, engine="adbc")
        if use_uri
        else pl.read_database(query, connection)
    )
    assert_frame_equal(actual, expected)


def test_polars_append_filter_and_aggregate(uri, connection, polars_table):
    name, frame = polars_table
    extra = frame.head(1).with_columns(pl.lit(5, dtype=pl.Int64).alias("id"))
    assert extra.write_database(
        name, connection, engine="adbc", if_table_exists="append"
    ) == 1
    source = pl.concat([frame, extra])
    actual = pl.read_database_uri(
        f"SELECT category, SUM(amount) AS total FROM {name} "
        "WHERE active = TRUE GROUP BY category ORDER BY category NULLS LAST",
        uri,
        engine="adbc",
    )
    expected = (
        source.filter(pl.col("active"))
        .group_by("category")
        .agg(pl.col("amount").sum().alias("total"))
        .sort("category", nulls_last=True)
    )
    assert_frame_equal(actual, expected)


def test_polars_empty_result_preserves_schema(connection, polars_table):
    name, frame = polars_table
    actual = pl.read_database(f"SELECT * FROM {name} WHERE id < 0", connection)
    assert_frame_equal(actual, frame.head(0))
