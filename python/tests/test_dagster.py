import pandas as pd
import polars as pl
import pyarrow as pa
import pytest
from adbc_driver_redis import dbapi
from adbc_driver_redis.dagster import RedisADBCIOManager
from dagster import (
    AssetIn,
    AssetKey,
    DailyPartitionsDefinition,
    Definitions,
    StaticPartitionsDefinition,
    asset,
    build_input_context,
    build_output_context,
    materialize,
)
from dagster._core.types.dagster_type import resolve_dagster_type


@pytest.fixture
def io(uri):
    manager = RedisADBCIOManager(uri=uri, schema_name="it_python_dagster")
    yield manager
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute("DROP SCHEMA IF EXISTS it_python_dagster CASCADE")


def output(name, **kwargs):
    return build_output_context(asset_key=AssetKey([name]), **kwargs)


@pytest.mark.parametrize("kind", ["arrow", "reader", "pandas", "polars"])
def test_roundtrip(io, kind):
    table = pa.table({"id": [1, 2], "name": ["a", "b"]})
    obj = {
        "arrow": table,
        "reader": table.to_reader(),
        "pandas": table.to_pandas(),
        "polars": pl.from_arrow(table),
    }[kind]
    expected_type = {
        "arrow": pa.Table,
        "reader": pa.RecordBatchReader,
        "pandas": pd.DataFrame,
        "polars": pl.DataFrame,
    }[kind]
    with output("roundtrip") as context:
        io.handle_output(context, obj)
        with build_input_context(
            asset_key=context.asset_key,
            upstream_output=context,
            dagster_type=resolve_dagster_type(expected_type),
        ) as read:
            result = io.load_input(read)
        assert isinstance(result, expected_type)
        if kind == "reader":
            result = result.read_all()
        elif kind == "pandas":
            result = pa.Table.from_pandas(result, preserve_index=False)
        elif kind == "polars":
            result = result.to_arrow()
        assert (
            sorted(result.to_pylist(), key=lambda row: row["id"]) == table.to_pylist()
        )
        assert context.get_logged_metadata()["row_count"].value == 2


def test_materialize_dependencies_and_projection(io):
    @asset(io_manager_key="redis")
    def source() -> pd.DataFrame:
        return pd.DataFrame({"id": [1, 2], "unused": [3, 4]})

    @asset(
        io_manager_key="redis", ins={"source": AssetIn(metadata={"columns": ["id"]})}
    )
    def downstream(source: pl.DataFrame) -> pa.Table:
        assert source.columns == ["id"]
        return source.to_arrow()

    definitions = Definitions(assets=[source, downstream], resources={"redis": io})
    assert definitions.get_repository_def()
    assert materialize([source, downstream], resources={"redis": io}).success


def test_static_partition_retries(io):
    partitions = StaticPartitionsDefinition(["a", "b"])

    @asset(
        io_manager_key="redis",
        partitions_def=partitions,
        metadata={"partition_expr": "part"},
    )
    def static_asset(context) -> pa.Table:
        return pa.table({"part": [context.partition_key], "n": [1]})

    for key in ["b", "a", "a"]:
        assert materialize(
            [static_asset], resources={"redis": io}, partition_key=key
        ).success
    with dbapi.connect(io.uri) as conn, conn.cursor() as cur:
        cur.execute(
            "SELECT part, count(*) FROM it_python_dagster.static_asset GROUP BY part ORDER BY part"
        )
        assert cur.fetchall() == [("a", 1), ("b", 1)]


def test_daily_partition_retries(io):
    partitions = DailyPartitionsDefinition(start_date="2026-01-01")

    @asset(
        io_manager_key="redis",
        partitions_def=partitions,
        metadata={"partition_expr": "day"},
    )
    def daily_asset(context) -> pa.Table:
        return pa.table({"day": [context.partition_time_window.start], "n": [1]})

    for key in ["2026-01-02", "2026-01-01", "2026-01-01"]:
        assert materialize(
            [daily_asset], resources={"redis": io}, partition_key=key
        ).success
    with dbapi.connect(io.uri) as conn, conn.cursor() as cur:
        cur.execute("SELECT count(*) FROM it_python_dagster.daily_asset")
        assert cur.fetchone() == (2,)


def test_failed_stream_preserves_previous(io):
    with output("safe") as context:
        io.handle_output(context, pa.table({"id": [1]}))
    schema = pa.schema([("id", pa.int64())])

    def broken():
        yield pa.record_batch([[2]], schema=schema)
        raise RuntimeError("injected stream failure")

    with (
        output("safe") as context,
        pytest.raises(Exception, match="injected stream failure"),
    ):
        io.handle_output(context, pa.RecordBatchReader.from_batches(schema, broken()))
    with build_input_context(
        asset_key=AssetKey(["safe"]), dagster_type=resolve_dagster_type(pa.Table)
    ) as context:
        result = io.load_input(context)
    assert result.to_pylist() == [{"id": 1}]
    with dbapi.connect(io.uri) as conn, conn.cursor() as cur:
        cur.execute(
            "SELECT table_name FROM information_schema.tables WHERE table_schema='it_python_dagster'"
        )
        assert cur.fetchall() == [("safe",)]
