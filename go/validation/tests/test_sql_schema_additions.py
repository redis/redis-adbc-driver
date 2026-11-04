# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.

"""Generated keys and schema SQL, including Arrow metadata across the C ABI."""

import uuid

import pyarrow as pa


def test_generated_keys_uuid_cte_indexes_and_materialized_view(conn):
    name = "it_schema_" + uuid.uuid4().hex[:12]
    view = name + "_mv"
    index = name + "_v_idx"
    with conn.cursor() as cur:
        try:
            cur.execute(
                f"CREATE TABLE {name}(id SERIAL, event_id UUID DEFAULT gen_random_uuid(),"
                " v INTEGER NOINDEX)"
            )
            cur.execute(f"INSERT INTO {name}(v) VALUES (10) RETURNING id, event_id")
            result = cur.fetch_arrow_table()
            assert result.schema.field("id").type == pa.int32()
            assert result.schema.field("event_id").type == pa.string()
            assert result.schema.field("event_id").metadata[
                b"ARROW:FLIGHT:SQL:TYPE_NAME"
            ] == b"UUID"
            row = result.to_pylist()[0]
            assert row["id"] == 1
            parsed_uuid = uuid.UUID(row["event_id"])
            assert parsed_uuid.version == 4
            assert str(parsed_uuid) == row["event_id"]

            cur.execute(
                f"WITH staged AS (SELECT 20 AS v) INSERT INTO {name}(v)"
                " SELECT v FROM staged RETURNING id"
            )
            assert cur.fetchall() == [(2,)]
            cur.execute(
                f"WITH removed AS (DELETE FROM {name} WHERE id=1 RETURNING *)"
                " SELECT id FROM removed"
            )
            assert cur.fetchall() == [(1,)]

            cur.execute(f"CREATE INDEX IF NOT EXISTS {index} ON {name}(v)")
            cur.execute(f"SELECT id FROM {name} WHERE v=20")
            assert cur.fetchall() == [(2,)]
            cur.execute(f"CREATE MATERIALIZED VIEW {view} AS SELECT count(*) AS n FROM {name}")
            cur.execute(f"INSERT INTO {name}(v) VALUES (30)")
            cur.execute(f"SELECT n FROM {view}")
            assert cur.fetchall() == [(1,)]
            cur.execute(f"REFRESH MATERIALIZED VIEW {view}")
            cur.execute(f"SELECT n FROM {view}")
            assert cur.fetchall() == [(2,)]
            cur.execute(f"DROP INDEX {index}")
            cur.execute(f"SELECT id FROM {name} WHERE v=20")
            assert cur.fetchall() == [(2,)]
        finally:
            cur.execute(f"DROP MATERIALIZED VIEW IF EXISTS {view}")
            cur.execute(f"DROP TABLE IF EXISTS {name}")
