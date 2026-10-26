"""Native Arrow IPC export and optional ingestion through standard ADBC calls."""

import argparse
from pathlib import Path

import adbc_driver_manager as adbc
from adbc_driver_redis import connect


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--uri", required=True)
    parser.add_argument("--query", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--format", choices=["stream", "file"], default="stream")
    parser.add_argument("--ingest-table")
    args = parser.parse_args()
    path = str(args.output.absolute())
    with connect(args.uri) as db, adbc.AdbcConnection(db) as conn:
        with adbc.AdbcStatement(conn) as stmt:
            stmt.set_sql_query(args.query)
            stmt.set_options(
                **{
                    "adbc.redis.ipc.output": path,
                    "adbc.redis.ipc.format": args.format,
                }
            )
            rows = stmt.execute_update()
            print(f"Exported {rows} rows to {path}")
        if args.ingest_table:
            with adbc.AdbcStatement(conn) as stmt:
                stmt.set_options(
                    **{
                        "adbc.ingest.target_table": args.ingest_table,
                        "adbc.ingest.mode": "adbc.ingest.mode.create",
                        "adbc.redis.ipc.input": path,
                        "adbc.redis.ipc.format": args.format,
                    }
                )
                ingested = stmt.execute_update()
                if ingested != rows:
                    raise RuntimeError(f"Expected {rows} rows, ingested {ingested}")
                print(f"Ingested {rows} rows into {args.ingest_table}")


if __name__ == "__main__":
    main()
