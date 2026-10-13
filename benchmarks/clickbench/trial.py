"""A separate process per trial lets the supervisor retain OOM/timeout failures."""
import hashlib
import json
import os
import resource
from pathlib import Path
import sys
import time

import adbc_driver_manager.dbapi as dbapi

from progress import TrialProgress


def main():
    job = json.loads(Path(sys.argv[1]).read_text())
    if "--credentials-stdin" in sys.argv[2:]:
        os.environ["REDIS_CLOUD_URI"] = json.load(sys.stdin)["uri"]
    result = {"seconds": None, "stage_seconds": {}}
    trial_started = time.perf_counter()
    cpu_before = resource.getrusage(resource.RUSAGE_SELF)
    progress = TrialProgress(job["output"], trial_started, cpu_before)
    progress.begin()
    try:
        with dbapi.connect(driver=job["driver"], autocommit=True, db_kwargs={
            "uri": os.environ["REDIS_CLOUD_URI"],
            "adbc.redis.aggregate_pushdown": "exact",
            "adbc.redis.query_workers": str(job.get("query_workers") or "auto"),
            "adbc.redis.read_timeout": "20m", "adbc.redis.write_timeout": "20m",
        }) as conn, conn.cursor() as cur:
            result["stage_seconds"]["connect"] = time.perf_counter() - trial_started
            result["query_workers"] = int(conn.adbc_connection.get_option("adbc.redis.query_workers"))
            progress.enter("session_setup", result["stage_seconds"])
            setup_started = time.perf_counter()
            cur.execute(f"SET search_path TO {job['namespace']}")
            result["stage_seconds"]["session_setup"] = time.perf_counter() - setup_started
            progress.enter("execute", result["stage_seconds"])
            started = time.perf_counter()
            try:
                cur.execute(job["sql"])
            finally:
                result["stage_seconds"]["execute"] = time.perf_counter() - started
                # Diagnostics must not replace the original query failure.
                try:
                    result["worker_grant"] = int(conn.adbc_connection.get_option(
                        "adbc.redis.query_workers_last_grant"))
                    result["aggregate_metrics"] = json.loads(conn.adbc_connection.get_option(
                        "adbc.redis.aggregate_metrics"))
                except Exception as diagnostic_error:
                    result["diagnostic_error_type"] = type(diagnostic_error).__name__
            progress.enter("arrow_fetch", result["stage_seconds"])
            fetch_started = time.perf_counter()
            table = cur.fetch_arrow_table()
            result["stage_seconds"]["arrow_fetch"] = time.perf_counter() - fetch_started
            result["seconds"] = time.perf_counter() - started
            # Repeated aggregate column names must not collapse into a dict.
            progress.enter("output_fingerprint", result["stage_seconds"])
            output_started = time.perf_counter()
            values = [list(row) for row in zip(*(column.to_pylist() for column in table.columns))]
            result.update(rows=table.num_rows, values=values,
                          fingerprint=hashlib.sha256(repr(values).encode()).hexdigest())
            result["stage_seconds"]["output_fingerprint"] = time.perf_counter() - output_started
    except Exception as error:
        result.update(seconds=None, error_type=type(error).__name__,
                      status=str(getattr(error, "status_code", "unknown")))
    finally:
        progress.close("failed" if result["seconds"] is None else "complete", result["stage_seconds"])
    usage = resource.getrusage(resource.RUSAGE_SELF)
    result.update(trial_wall_seconds=time.perf_counter() - trial_started,
                  process_user_cpu_seconds=usage.ru_utime - cpu_before.ru_utime,
                  process_system_cpu_seconds=usage.ru_stime - cpu_before.ru_stime,
                  # Trials require Linux, where ru_maxrss is in KiB.
                  process_peak_rss_bytes=usage.ru_maxrss * 1024)
    Path(job["output"]).write_text(json.dumps(result, default=str) + "\n")


if __name__ == "__main__":
    main()
