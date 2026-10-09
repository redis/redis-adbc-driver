"""A separate process per trial lets the supervisor retain OOM/timeout failures."""
import hashlib
import json
import os
from pathlib import Path
import sys
import time

import adbc_driver_manager.dbapi as dbapi


def main():
    job = json.loads(Path(sys.argv[1]).read_text())
    result = {"seconds": None}
    try:
        with dbapi.connect(driver=job["driver"], autocommit=True, db_kwargs={
            "uri": os.environ["REDIS_CLOUD_URI"],
            "adbc.redis.aggregate_pushdown": "exact",
            "adbc.redis.query_workers": str(job.get("query_workers") or "auto"),
            "adbc.redis.read_timeout": "20m", "adbc.redis.write_timeout": "20m",
        }) as conn, conn.cursor() as cur:
            result["query_workers"] = int(conn.adbc_connection.get_option("adbc.redis.query_workers"))
            cur.execute(f"SET search_path TO {job['namespace']}")
            started = time.perf_counter()
            cur.execute(job["sql"])
            table = cur.fetch_arrow_table()
            result["seconds"] = time.perf_counter() - started
            # Repeated aggregate column names must not collapse into a dict.
            values = [list(row) for row in zip(*(column.to_pylist() for column in table.columns))]
            result.update(rows=table.num_rows, values=values,
                          fingerprint=hashlib.sha256(repr(values).encode()).hexdigest())
    except Exception as error:
        result.update(error_type=type(error).__name__,
                      status=str(getattr(error, "status_code", "unknown")))
    Path(job["output"]).write_text(json.dumps(result, default=str) + "\n")


if __name__ == "__main__":
    main()
