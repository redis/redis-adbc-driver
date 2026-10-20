"""Verify an extension through its matching DuckDB CLI (including 2.0 builds).

Requires REDIS_DUCKDB_CLI, REDIS_ADBC_EXTENSION, REDIS_ADBC_LIBRARY and REDIS_URI.
Uses a disposable Redis with Search and cleans up its uniquely named table.
"""

import csv
import datetime
import decimal
import io
import os
import subprocess
import uuid

import adbc_driver_manager.dbapi
import pyarrow as pa


def literal(value):
    return "'" + value.replace("'", "''") + "'"


def main():
    cli = os.environ["REDIS_DUCKDB_CLI"]
    uri = os.environ["REDIS_URI"]
    extension = os.environ["REDIS_ADBC_EXTENSION"]
    library = os.environ["REDIS_ADBC_LIBRARY"]
    name = "duckdb_cli_" + uuid.uuid4().hex

    def run(sql, error=None):
        result = subprocess.run(
            [cli, "-unsigned", "-csv", "-noheader", "-nullvalue", "<NULL>",
             "-c", f"LOAD {literal(extension)}; " + sql],
            capture_output=True, text=True, timeout=60,
        )
        if error:
            assert result.returncode and error in result.stderr, (result.stdout, result.stderr)
            return
        assert result.returncode == 0, (result.stdout, result.stderr)
        return list(csv.reader(io.StringIO(result.stdout)))

    with adbc_driver_manager.dbapi.connect(driver=library, db_kwargs={"uri": uri}, autocommit=True) as conn:
        with conn.cursor() as cur:
            fixture = pa.table({
                "id": pa.array(range(10003), type=pa.int64()),
                "label": [None if i % 17 == 0 else f"row-{i}-é" for i in range(10003)],
                "flag": [i % 2 == 0 for i in range(10003)],
                "amount": pa.array([decimal.Decimal("123.45")] * 10003, type=pa.decimal128(12, 2)),
                "day": pa.array([datetime.date(2026, 10, 20)] * 10003, type=pa.date32()),
            })
            cur.adbc_ingest(name, fixture, mode="create")
            source = f"redis_adbc_scan({literal(uri)}, 'SELECT * FROM public.{name}')"
            try:
                rows = run(f"SELECT * FROM {source} ORDER BY id")
                assert len(rows) == 10003
                for i, row in enumerate(rows):
                    assert row == [str(i), "<NULL>" if i % 17 == 0 else f"row-{i}-é",
                                   str(i % 2 == 0).lower(), "123.45", "2026-10-20"], (i, row)
                assert run(f"SELECT count(*),sum(id) FROM {source}") == [["10003", str(10003 * 10002 // 2)]]
                assert run(f"SELECT label,id FROM {source} WHERE id=42") == [["row-42-é", "42"]]
                assert run(f"SELECT s.id,x.label FROM {source} s JOIN (VALUES (42,'answer')) x(id,label) USING(id)") == [["42", "answer"]]
                assert run(f"SELECT * FROM {source} LIMIT 0") == []
                assert len(run(f"SELECT * FROM {source} LIMIT 1")) == 1
                assert run(f"SELECT * FROM redis_adbc_scan({literal(uri)}, 'WITH x AS (SELECT 7 AS n) SELECT n FROM x UNION ALL SELECT 8')") == [["7"], ["8"]]
                run(f"SELECT * FROM redis_adbc_scan({literal(uri)}, 'SELECT 1; DROP TABLE public.{name}')", error="exactly one SELECT")
                run(f"SET enable_external_access=false; SELECT * FROM {source}", error="enable_external_access")
                run(f"SELECT * FROM redis_adbc_scan({literal(uri)}, 'SELECT CASE WHEN id=3000 THEN 1/0 ELSE id END AS n FROM public.{name}')", error="division by zero")
                assert run(f"PREPARE q AS SELECT * FROM redis_adbc_scan({literal(uri)},'SELECT 42 AS n'); EXECUTE q; EXECUTE q") == [["42"], ["42"]]
                assert run(f"SELECT count(*) FROM {source}") == [["10003"]]
                print("12 CLI scenarios passed; every value in 10,003 rows verified")
            finally:
                cur.execute(f"DROP TABLE public.{name}")


if __name__ == "__main__":
    main()
