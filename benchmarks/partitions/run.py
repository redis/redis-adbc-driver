"""Fixed-data native partition benchmark. Credentials stay in process memory."""
import argparse
import concurrent.futures
import datetime
import importlib.metadata
import json
import multiprocessing
import os
from pathlib import Path
import platform
import resource
import subprocess
import sys
import tempfile
import time
import uuid

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import redis


def read_partition(args):
    driver, options, descriptor, path = args
    started = time.perf_counter()
    with dbapi.connect(driver=driver, db_kwargs=options, autocommit=True) as conn:
        with conn.cursor() as cur:
            cur.adbc_read_partition(descriptor)
            with cur.fetch_record_batch() as reader:
                rows = 0
                with pa.OSFile(path, "wb") as out, pa.ipc.new_stream(out, reader.schema) as writer:
                    for batch in reader:
                        writer.write_batch(batch)
                        rows += batch.num_rows
    return {"rows": rows, "seconds": time.perf_counter() - started,
            "process_peak_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
            * (1 if sys.platform == "darwin" else 1024)}


def telemetry(client):
    try:
        info = client.info()
        return {k: info[k] for k in ("used_memory", "used_memory_peak",
                "total_net_input_bytes", "total_net_output_bytes") if k in info}
    except redis.RedisError:
        return None  # restricted workers need no INFO permission


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", required=True)
    parser.add_argument("--rows", type=int, default=10000)
    parser.add_argument("--width", type=int, default=64)
    parser.add_argument("--layout", choices=["dense", "sparse", "skewed"], default="dense")
    parser.add_argument("--partitions", type=int, nargs="+", default=[1, 4, 8])
    parser.add_argument("--workers", type=int, nargs="+", default=[1, 4])
    parser.add_argument("--scan-cap", type=int, default=0)
    parser.add_argument("--mode", choices=["live", "retained"], default="live")
    parser.add_argument("--execution-limits", help="Retained coordinator partial JSON limits; recorded in output")
    parser.add_argument("--tls-ca-file", help="Explicit verification roots for all clients")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.rows < 1 or args.width < 0 or any(n < 1 or n > 1024 for n in args.partitions) or any(n < 1 for n in args.workers):
        parser.error("positive rows/workers, nonnegative width, and partitions in 1..1024 required")
    uri = os.environ["REDIS_URI"]
    client = redis.Redis.from_url(uri, protocol=3, **({"ssl_ca_certs": args.tls_ca_file} if args.tls_ca_file else {}))
    table = "partition_bench_" + uuid.uuid4().hex
    namespace = "bench_" + uuid.uuid4().hex
    options = {"uri": uri}
    if args.tls_ca_file:
        options["adbc.redis.tls_ca_file"] = args.tls_ca_file
    if args.mode == "retained":
        options["adbc.redis.partition_result_namespace"] = namespace
    worker_options = dict(options)
    if args.mode == "retained":
        worker_options["adbc.redis.partition_reader_only"] = "true"
    report = {"started_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "platform": platform.platform(), "python": sys.version,
              "packages": {n: importlib.metadata.version(n) for n in ["pyarrow", "adbc-driver-manager", "redis"]},
              "revision": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
              "driver_sha256": __import__("hashlib").sha256(Path(args.driver).read_bytes()).hexdigest(),
              "source_dirty": bool(subprocess.check_output(["git", "status", "--porcelain"], text=True).strip()),
              "rows_ingested": args.rows, "payload_width": args.width, "layout": args.layout,
              "scan_cap": args.scan_cap, "mode": args.mode,
              "execution_limits": json.loads(args.execution_limits) if args.execution_limits else None,
              "measurements": []}
    try:
        report["redis_version"] = client.info("server").get("redis_version")
    except redis.RedisError:
        report["redis_version"] = None
    with dbapi.connect(driver=args.driver, db_kwargs=options, autocommit=True) as conn:
        with conn.cursor() as cur:
            try:
                cur.adbc_ingest(table, pa.table({"id": range(args.rows), "payload": ["x" * args.width] * args.rows}))
                if args.layout == "sparse":
                    cur.execute(f"DELETE FROM {table} WHERE id % 10 != 0")
                elif args.layout == "skewed":
                    cur.execute(f"DELETE FROM {table} WHERE id < {args.rows * 9 // 10}")
                before = telemetry(client)
                started = time.perf_counter()
                cur.execute(f"SELECT * FROM {table}")
                expected = cur.fetch_arrow_table()
                seconds = time.perf_counter() - started
                report["ordinary"] = {"seconds": seconds, "rows": expected.num_rows,
                                      "rows_per_second": expected.num_rows / seconds,
                                      "server_before": before, "server_after": telemetry(client)}
                expected = expected.sort_by("id")
                for partitions in args.partitions:
                    for workers in args.workers:
                        before = telemetry(client)
                        started = time.perf_counter()
                        cur.adbc_statement.set_options(**{"adbc.redis.partition_count": partitions,
                                                         "adbc.redis.partition_result_mode": args.mode,
                                                         "adbc.redis.partition_scan_cap": args.scan_cap})
                        if args.execution_limits:
                            cur.adbc_statement.set_options(**{"adbc.redis.partition_execution_limits": args.execution_limits})
                        descriptors, schema = cur.adbc_execute_partitions(f"SELECT * FROM {table}")
                        planning_seconds = time.perf_counter() - started
                        with tempfile.TemporaryDirectory(prefix="adbc-partitions-") as tmp:
                            paths = [str(Path(tmp) / f"{i}.arrow") for i in range(len(descriptors))]
                            with concurrent.futures.ProcessPoolExecutor(max_workers=workers, mp_context=multiprocessing.get_context("spawn")) as pool:
                                metrics = list(pool.map(read_partition, [(args.driver, worker_options, d, p) for d, p in zip(descriptors, paths)]))
                            seconds = time.perf_counter() - started
                            after = telemetry(client)
                            tables = [pa.ipc.open_stream(p).read_all() for p in paths]
                            actual = pa.concat_tables(tables).sort_by("id")
                            if not actual.schema.equals(schema) or not actual.equals(expected):
                                raise AssertionError("partition result/schema differs from ordinary execution")
                        report["measurements"].append({"partitions": len(descriptors), "requested_partitions": partitions, "workers": workers,
                            "planning_seconds": planning_seconds, "end_to_end_seconds": seconds,
                            "rows": actual.num_rows, "rows_per_second": actual.num_rows / seconds,
                            "exact_result_and_schema": True, "worker_metrics": metrics,
                            "server_before": before, "server_after": after,
                            "coordinator_process_peak_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == "darwin" else 1024)})
                        if args.mode == "retained":
                            job = json.loads(descriptors[0])["result"]["job_id"]
                            if len(job) != 32 or any(c not in "0123456789abcdef" for c in job):
                                raise AssertionError("invalid retained job identity")
                            cur.execute(f"DROP PARTITION RESULT '{job}'")
            finally:
                cur.execute(f"DROP TABLE IF EXISTS {table}")
                if args.mode == "retained":
                    # This fresh UUID namespace belongs only to this run.
                    # Scan every primary on Cluster; never clean source keys.
                    clustered = bool(client.info("cluster").get("cluster_enabled"))
                    cluster = redis.RedisCluster.from_url(uri, protocol=3,
                        **({"ssl_ca_certs": args.tls_ca_file} if args.tls_ca_file else {})) if clustered else None
                    try:
                        clients = [cluster.get_redis_connection(n) for n in cluster.get_primaries()] if cluster else [client]
                        pattern = f"adbc:{{retained:{namespace}}}:*"
                        for node in clients:
                            for key in node.scan_iter(match=pattern, count=100):
                                (cluster or client).delete(key)
                            if list(node.scan_iter(match=pattern, count=100)):
                                raise AssertionError("owned retained namespace cleanup incomplete")
                        report["retained_namespace_cleaned"] = True
                    finally:
                        if cluster:
                            cluster.close()
    client.close()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
