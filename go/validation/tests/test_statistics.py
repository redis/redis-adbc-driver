"""ADBC 1.1 statistics through the driver manager and Arrow C stream."""

import pyarrow as pa


def statistics_schema():
    value = pa.dense_union([
        pa.field("int64", pa.int64()), pa.field("uint64", pa.uint64()),
        pa.field("float64", pa.float64()), pa.field("binary", pa.binary()),
    ], type_codes=[0, 1, 2, 3])
    statistic = pa.struct([
        pa.field("table_name", pa.string(), nullable=False),
        pa.field("column_name", pa.string()),
        pa.field("statistic_key", pa.int16(), nullable=False),
        pa.field("statistic_value", value, nullable=False),
        pa.field("statistic_is_approximate", pa.bool_(), nullable=False),
    ])
    schema = pa.struct([
        pa.field("db_schema_name", pa.string()),
        pa.field("db_schema_statistics", pa.list_(statistic), nullable=False),
    ])
    return pa.schema([
        pa.field("catalog_name", pa.string()),
        pa.field("catalog_db_schemas", pa.list_(schema), nullable=False),
    ])


def entries(conn, **filters):
    with conn.adbc_get_statistics(**filters) as reader:
        assert reader.schema == statistics_schema()
        rows = reader.read_all().to_pylist()
    return [stat for catalog in rows for schema in catalog["catalog_db_schemas"]
            for stat in schema["db_schema_statistics"]]


def test_statistics_schema_counts_and_filters(conn):
    table = "it_native_statistics"
    with conn.cursor() as cur:
        cur.execute(f"DROP TABLE IF EXISTS {table}")
        cur.execute(f"CREATE TABLE {table} (id BIGINT, label TEXT)")
        try:
            with conn.adbc_get_statistic_names() as reader:
                assert reader.schema == pa.schema([
                    pa.field("statistic_name", pa.string(), nullable=False),
                    pa.field("statistic_key", pa.int16(), nullable=False),
                ])
                assert reader.read_all().num_rows == 0
            for count in [0, 3]:
                if count:
                    cur.execute(f"INSERT INTO {table} VALUES (1,'a'),(1,NULL),(2,'a')")
                assert entries(conn, db_schema_filter="public", table_name_filter=table) == [{
                    "table_name": table, "column_name": None, "statistic_key": 6,
                    "statistic_value": count, "statistic_is_approximate": True,
                }]
            for filters in [
                {"catalog_filter": "does_not_exist"}, {"catalog_filter": ""},
                {"db_schema_filter": "does_not_exist"}, {"db_schema_filter": ""},
                {"table_name_filter": "does_not_exist"}, {"table_name_filter": ""},
            ]:
                assert entries(conn, **filters) == []
            assert entries(conn, db_schema_filter="pub%", table_name_filter="it_native_statistic_")
            exact = entries(conn, table_name_filter=table, approximate=False)
            assert exact == [{"table_name": table, "column_name": None,
                              "statistic_key": 6, "statistic_value": 3,
                              "statistic_is_approximate": True}]
        finally:
            cur.execute(f"DROP TABLE {table}")
