"""Experimental host-staged Arrow/cuDF query comparison; no driver GPU backend."""

import argparse
import importlib.metadata
import json
import os
import platform
import random
import sys
import time
from contextlib import nullcontext

import pyarrow as pa
import pyarrow.compute as pc


NAMES = ["bucket", "n", "lo", "hi", "total"]
MAX_INT64 = 2**63 - 1


def identifier(value):
    if not value or "\x00" in value:
        raise ValueError("invalid SQL identifier")
    return '"' + value.replace('"', '""') + '"'


def queries(schema, table):
    source = identifier(schema) + "." + identifier(table)
    return (
        f'SELECT "bucket", "value" FROM {source}',
        'SELECT "bucket", COUNT("value") AS "n", MIN("value") AS "lo", '
        'MAX("value") AS "hi", SUM("value") AS "total" '
        f'FROM {source} WHERE "value" >= 0 GROUP BY "bucket"',
    )


def fixture(rows, groups):
    rng = random.Random(215)
    return pa.table({
        "bucket": pa.array(
            [None if i % 29 == 0 else i % groups for i in range(rows)], pa.int64()
        ),
        "value": pa.array(
            [None if i % 13 == 0 else rng.randrange(-100, 1000) for i in range(rows)],
            pa.int64(),
        ),
    })


def reference(table):
    """Independent exact Python integer oracle and conservative GPU sum gate."""
    if table.column_names != ["bucket", "value"] or any(
        field.type != pa.int64() for field in table.schema
    ):
        raise ValueError("experiment requires nullable int64 bucket and value columns")
    groups = {}
    for row in table.to_pylist():
        value, bucket = row["value"], row["bucket"]
        if value is None or value < 0:
            continue
        state = groups.setdefault(bucket, [0, value, value, 0])
        state[0] += 1
        state[1] = min(state[1], value)
        state[2] = max(state[2], value)
        state[3] += value
        # All included values are nonnegative, so every partial sum is bounded.
        if state[3] > MAX_INT64:
            raise ValueError("SUM exceeds int64; GPU experiment refuses overflow")
    return groups


def validate(table, expected):
    if table.column_names != NAMES:
        raise ValueError("unexpected aggregate output columns")
    if any(field.type != pa.int64() for field in table.schema):
        raise ValueError("aggregate output must preserve int64 physical types")
    actual = {}
    for row in table.to_pylist():
        bucket = row["bucket"]
        if bucket in actual:
            raise ValueError("duplicate aggregate group")
        actual[bucket] = [row[name] for name in NAMES[1:]]
    if actual != expected:
        raise ValueError("aggregate results differ from exact reference")


def cpu_query(table):
    filtered = table.filter(pc.greater_equal(table["value"], 0))
    result = filtered.group_by("bucket").aggregate([
        ("value", "count"), ("value", "min"),
        ("value", "max"), ("value", "sum"),
    ])
    return result.rename_columns(NAMES)


def gpu_query(table, cudf, cupy):
    """Synchronized wall times include upload, allocation and result download."""
    sync = cupy.cuda.runtime.deviceSynchronize
    sync()
    start = time.perf_counter()
    frame = cudf.DataFrame.from_arrow(table)
    sync()
    uploaded = time.perf_counter()
    selected = frame[frame["value"] >= 0]
    result = selected.groupby("bucket", dropna=False).agg(
        {"value": ["count", "min", "max", "sum"]}
    )
    result.columns = NAMES[1:]
    result = result.reset_index()
    sync()
    computed = time.perf_counter()
    host = result.to_arrow()
    sync()
    downloaded = time.perf_counter()
    return host, {
        "upload_seconds": uploaded - start,
        "compute_seconds": computed - uploaded,
        "download_seconds": downloaded - computed,
        "host_to_host_seconds": downloaded - start,
    }


def fetch(connection, sql):
    start = time.perf_counter()
    with connection.cursor() as cursor:
        cursor.execute(sql)
        result = cursor.fetch_arrow_table()  # Execute alone is not a completed query.
    return result, time.perf_counter() - start


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--backend", choices=["cpu", "cuda"], default="cpu")
    parser.add_argument("--rows", type=int, default=100_000)
    parser.add_argument("--groups", type=int, default=1000)
    parser.add_argument("--trials", type=int, default=3)
    parser.add_argument("--warmups", type=int, default=1)
    parser.add_argument("--driver", help="shared library; enables read-only Redis comparison")
    parser.add_argument("--schema", default="public")
    parser.add_argument("--table", help="stable existing table with int64 bucket/value")
    args = parser.parse_args(argv)
    if min(args.rows, args.groups, args.trials) <= 0 or args.warmups < 0:
        parser.error("rows, groups and trials must be positive; warmups nonnegative")
    if bool(args.driver) != bool(args.table):
        parser.error("--driver and --table must be supplied together")
    if args.driver and not os.environ.get("REDIS_URI"):
        parser.error("REDIS_URI is required for Redis mode")

    versions = {"pyarrow": pa.__version__}
    cudf = cupy = None
    device = None
    if args.backend == "cuda":
        # A requested GPU failure is fatal; never silently substitute CPU execution.
        import cudf
        import cupy

        props = cupy.cuda.runtime.getDeviceProperties(cupy.cuda.runtime.getDevice())
        name = props["name"]
        device = {
            "name": name.decode() if isinstance(name, bytes) else str(name),
            "memory_bytes": props["totalGlobalMem"],
            "cuda_runtime": cupy.cuda.runtime.runtimeGetVersion(),
            "cuda_driver": cupy.cuda.runtime.driverGetVersion(),
        }
        versions.update(cudf=cudf.__version__, cupy=cupy.__version__)

    connection = nullcontext(None)
    source = None
    if args.driver:
        import adbc_driver_manager.dbapi as dbapi

        versions["adbc_driver_manager"] = importlib.metadata.version("adbc-driver-manager")
        connection = dbapi.connect(
            driver=os.path.abspath(args.driver), autocommit=True,
            db_kwargs={"uri": os.environ["REDIS_URI"]},
        )
    else:
        source = fixture(args.rows, args.groups)

    scan_sql, aggregate_sql = queries(args.schema, args.table or "unused")
    records = []
    with connection as conn:
        for trial in range(-args.warmups, args.trials):
            # Alternate the direct SQL comparison around the scan to reduce order bias.
            direct = direct_seconds = None
            if conn is not None and trial % 2 == 0:
                direct, direct_seconds = fetch(conn, aggregate_sql)
            if conn is not None:
                table, scan_seconds = fetch(conn, scan_sql)
            else:
                table, scan_seconds = source, None
            expected = reference(table)  # Outside timed execution; domain preflight.
            timings = {}
            backends = ["cpu", "cuda"] if cudf is not None else ["cpu"]
            if trial % 2:
                backends.reverse()
            for backend in backends:
                if backend == "cpu":
                    start = time.perf_counter()
                    result = cpu_query(table)
                    stages = {"host_to_host_seconds": time.perf_counter() - start}
                else:
                    result, stages = gpu_query(table, cudf, cupy)
                validate(result, expected)
                # This sum is a model using the shared scan, not a separately timed query.
                if scan_seconds is not None:
                    stages["estimated_scan_plus_backend_seconds"] = (
                        scan_seconds + stages["host_to_host_seconds"]
                    )
                timings[backend] = stages
            if conn is not None:
                if direct is None:
                    direct, direct_seconds = fetch(conn, aggregate_sql)
                validate(direct, expected)
            if trial >= 0:
                records.append({
                    "trial": trial, "rows": table.num_rows,
                    "arrow_bytes": table.nbytes, "result_groups": len(expected),
                    "scan_fetch_seconds": scan_seconds,
                    "driver_sql_seconds": direct_seconds,
                    "backends": timings, "validated": True,
                })
    print(json.dumps({
        "experiment": "host_staged_filter_groupby_int64", "format_version": 1,
        "source": "redis" if args.driver else "synthetic",
        "platform": platform.platform(), "versions": versions, "device": device,
        "warmups": args.warmups, "trials": records,
    }, indent=2))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Driver errors can contain URIs or SQL data. Keep diagnostics out of artifacts.
        print(f"GPU experiment failed ({type(error).__name__}); no valid report produced.",
              file=sys.stderr)
        sys.exit(1)
