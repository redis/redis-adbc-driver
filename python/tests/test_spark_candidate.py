"""Custom Python ADBC -> Arrow -> local Spark data pipeline (no JDBC/Flight)."""

import sys

import pytest
from adbc_driver_redis import dbapi


def test_spark_from_redis_arrow(uri, workflow_rows, tmp_path, monkeypatch):
    pytest.importorskip("pyspark")
    from pyspark.sql import SparkSession

    monkeypatch.setenv("SPARK_LOCAL_IP", "127.0.0.1")
    monkeypatch.setenv("PYSPARK_PYTHON", sys.executable)
    name, expected = workflow_rows
    with dbapi.connect(uri) as conn, conn.cursor() as cur:
        cur.execute(f'SELECT * FROM "{name}" ORDER BY id')
        table = cur.fetch_arrow_table()
    spark = (
        SparkSession.builder.master("local[1]").appName("redis-adbc-candidate")
        .config("spark.ui.enabled", "false")
        .config("spark.driver.bindAddress", "127.0.0.1")
        .config("spark.driver.host", "127.0.0.1")
        .config("spark.sql.warehouse.dir", str(tmp_path / "warehouse"))
        .config("spark.sql.shuffle.partitions", "1")
        .getOrCreate()
    )
    try:
        frame = spark.createDataFrame(table)
        assert [row.asDict() for row in frame.orderBy("id").collect()] == expected.to_pylist()
        frame.createOrReplaceTempView("redis_rows")
        assert spark.sql("SELECT count(*), sum(amount) FROM redis_rows").collect()[0][:] == (3, 10.0)
        assert [(r.id, r.label) for r in frame.filter("id >= 2").orderBy("id").collect()] == [(2, None), (3, "雪")]
        empty = spark.createDataFrame(table.slice(0, 0))
        assert empty.schema == frame.schema
        assert empty.count() == 0
    finally:
        spark.stop()
