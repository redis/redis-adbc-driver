import datetime
import warnings

import pandas as pd
import pyarrow as pa
import pytest
from sqlalchemy import create_engine, text


@pytest.fixture
def pandas_table(connection):
    with connection.cursor() as cur:
        cur.execute("DROP TABLE IF EXISTS it_python_pandas")
        cur.execute(
            "CREATE TABLE it_python_pandas "
            "(id integer PRIMARY KEY, label text, amount double precision, ts timestamp)"
        )
        try:
            cur.execute(
                "INSERT INTO it_python_pandas VALUES "
                "(1, 'café', 10.25, TIMESTAMP '2026-01-01 12:30:00'), "
                "(2, NULL, NULL, NULL), "
                "(3, '東京', 5.5, TIMESTAMP '2026-01-03 00:00:00')"
            )
            yield "it_python_pandas"
        finally:
            cur.execute("DROP TABLE it_python_pandas")


def test_read_sql_adbc(connection, pandas_table):
    # Treat pandas' unsupported-DBAPI fallback warning as an error: this must
    # exercise pandas' native ADBC path and consume the complete Arrow result.
    with warnings.catch_warnings():
        warnings.simplefilter("error")
        frame = pd.read_sql(
            f"SELECT * FROM {pandas_table} ORDER BY id",
            connection,
            index_col="id",
            dtype_backend="pyarrow",
        )
    assert frame.index.tolist() == [1, 2, 3]
    assert list(frame.columns) == ["label", "amount", "ts"]
    assert frame.loc[1, "label"] == "café"
    assert frame.loc[3, "label"] == "東京"
    assert frame.loc[1, "amount"] == 10.25
    assert frame.loc[3, "amount"] == 5.5
    assert frame.loc[1, "ts"] == datetime.datetime(2026, 1, 1, 12, 30)
    assert frame.loc[2].isna().all()
    assert frame["amount"].dtype == pd.ArrowDtype(pa.float64())
    assert isinstance(frame["ts"].dtype, pd.ArrowDtype)
    assert pa.types.is_timestamp(frame["ts"].dtype.pyarrow_dtype)

    default_frame = pd.read_sql(
        f"SELECT id, amount FROM {pandas_table} ORDER BY id", connection
    )
    assert default_frame["id"].tolist() == [1, 2, 3]
    assert default_frame.loc[0, "amount"] == 10.25
    assert pd.isna(default_frame.loc[1, "amount"])
    assert default_frame.loc[2, "amount"] == 5.5

    empty = pd.read_sql(
        f"SELECT id, label FROM {pandas_table} WHERE id < 0", connection
    )
    assert empty.empty
    assert list(empty.columns) == ["id", "label"]


def test_read_sql_sqlalchemy(uri, pandas_table):
    engine = create_engine(uri.replace("://", "+adbc://", 1), future=True)
    try:
        with engine.connect() as conn:
            frame = pd.read_sql(
                text(f"SELECT id, label FROM {pandas_table} WHERE id >= :minimum ORDER BY id"),
                conn,
                params={"minimum": 2},
            )
            assert frame["id"].tolist() == [2, 3]
            assert pd.isna(frame.loc[0, "label"])
            assert frame.loc[1, "label"] == "東京"

            chunks = list(
                pd.read_sql(
                    f"SELECT id FROM {pandas_table} ORDER BY id", conn, chunksize=2
                )
            )
            assert [len(chunk) for chunk in chunks] == [2, 1]
            assert pd.concat(chunks)["id"].tolist() == [1, 2, 3]
    finally:
        engine.dispose()
