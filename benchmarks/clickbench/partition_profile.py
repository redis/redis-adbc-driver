"""Local ExecuteQuery vs partition API pilot on official ClickBench source rows.

This does not relax the Cloud/systemd gates of the official benchmark runner.
Global ClickBench SQL remains unchanged and unsupported partition plans remain
visible. Supplemental row-local workloads are explicitly not leaderboard SQL.
"""
import argparse
import concurrent.futures
import hashlib
import importlib.metadata
import json
import multiprocessing
import os
from pathlib import Path
import platform
import resource
import statistics
import subprocess
import sys
import tempfile
import threading

import psutil
import time
import uuid

import adbc_driver_manager.dbapi as dbapi
import duckdb
import pyarrow as pa
import pyarrow.parquet as pq
import redis

from run import arrow_schema, convert_batch, commit


def read_task(payload):
    driver, options, sql, descriptor, output = payload
    with dbapi.connect(driver=driver, db_kwargs=options, autocommit=True) as conn:
        with conn.cursor() as cur:
            started = time.perf_counter()
            if descriptor is None:
                cur.execute(sql)
            else:
                cur.adbc_read_partition(descriptor)
            first_batch = None
            count = 0
            with cur.fetch_record_batch() as reader:
                with pa.OSFile(output, "wb") as target, pa.ipc.new_stream(target, reader.schema) as writer:
                    for batch in reader:
                        if first_batch is None:
                            first_batch = time.perf_counter() - started
                        writer.write_batch(batch)
                        count += batch.num_rows
            elapsed = time.perf_counter() - started
    return {"rows": count, "read_and_ipc_seconds": elapsed,
            "first_batch_seconds": first_batch,
            "process_peak_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == "darwin" else 1024)}


class TrialMonitor:
    """Sample total live process RSS and server memory; no hard memory limit."""
    interval = 0.05

    def __init__(self, client):
        self.client = client
        self.process = psutil.Process()
        self.stop = threading.Event()
        self.samples = 0
        self.client_baseline = self.process.memory_info().rss
        self.client_peak = self.client_baseline
        self.server_peak = 0
        self.before = client.info()
        self.commands_before = client.info("commandstats")
        self.thread = threading.Thread(target=self.run, daemon=True)

    def sample(self):
        rss = self.process.memory_info().rss
        for child in self.process.children(recursive=True):
            try:
                rss += child.memory_info().rss
            except psutil.NoSuchProcess:
                pass
        self.client_peak = max(self.client_peak, rss)
        self.server_peak = max(self.server_peak, self.client.info("memory")["used_memory"])
        self.samples += 1

    def run(self):
        while not self.stop.wait(self.interval):
            self.sample()

    def __enter__(self):
        self.sample()
        self.thread.start()
        return self

    def __exit__(self, *unused):
        self.stop.set()
        self.thread.join()
        self.sample()

    def result(self):
        after = self.client.info()
        commands = self.client.info("commandstats")
        return {"command_calls_delta": {name.removeprefix("cmdstat_"): value["calls"] - self.commands_before.get(name, {}).get("calls", 0)
                                       for name, value in commands.items()
                                       if value["calls"] != self.commands_before.get(name, {}).get("calls", 0)},
                "sample_interval_seconds": self.interval, "samples": self.samples,
                "client_process_tree_baseline_rss_bytes": self.client_baseline,
                "client_process_tree_sampled_peak_rss_bytes": self.client_peak,
                "server_baseline_used_memory_bytes": self.before["used_memory"],
                "server_sampled_peak_used_memory_bytes": self.server_peak,
                "server_after_used_memory_bytes": after["used_memory"],
                "server_net_input_bytes_delta": after["total_net_input_bytes"] - self.before["total_net_input_bytes"],
                "server_net_output_bytes_delta": after["total_net_output_bytes"] - self.before["total_net_output_bytes"]}


def wait_index_ready(client, index, rows, timeout=300):
    started = time.perf_counter()
    while True:
        raw = client.execute_command("FT.INFO", index)
        info = dict(zip(raw[::2], raw[1::2])) if isinstance(raw, list) else raw
        state = {key: info.get(key) for key in ["num_docs", "indexing", "percent_indexed"]}
        if int(info["num_docs"]) == rows and int(info["indexing"]) == 0:
            return {"wait_seconds": time.perf_counter() - started, "state": state}
        if time.perf_counter() - started >= timeout:
            raise TimeoutError(f"Search index did not become ready: {state}")
        time.sleep(0.05)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--driver", type=Path, required=True)
    ap.add_argument("--upstream", type=Path, required=True)
    ap.add_argument("--parquet", type=Path, required=True)
    ap.add_argument("--rows", type=int, default=100000)
    ap.add_argument("--repeats", type=int, default=3)
    ap.add_argument("--workers", type=int, nargs="+", default=[1, 4])
    ap.add_argument("--partitions", type=int, nargs="+", default=[1, 8])
    ap.add_argument("--keep-failure-table", action="store_true", help="Retain only this run's generated table after an exact-validation failure")
    ap.add_argument("--workloads", nargs="+", help="Optional workload names for focused reproduction")
    ap.add_argument("--strategies", nargs="+", choices=["rowid", "balanced"], default=["rowid"])
    ap.add_argument("--layout", choices=["dense", "sparse", "skewed"], default="dense")
    ap.add_argument("--partition-mode", choices=["live", "retained"], default="live")
    ap.add_argument("--execution-limits", help="Retained partial JSON execution limits; recorded in report")
    ap.add_argument("--output", type=Path, required=True)
    args = ap.parse_args()
    if args.output.exists() or args.rows < 1 or args.repeats < 1 or any(not 1 <= n <= 1024 for n in args.partitions) or min(args.workers) < 1:
        ap.error("choose a fresh output and positive row/repeat/worker counts")
    uri = os.environ["REDIS_URI"]
    driver = str(args.driver.resolve())
    ddl = (args.upstream / "postgresql/create.sql").read_text()
    queries = (args.upstream / "postgresql/queries.sql").read_text().splitlines()
    if len(queries) != 43:
        ap.error("expected 43 official PostgreSQL ClickBench queries")
    schema = arrow_schema(ddl)
    source = pq.ParquetFile(args.parquet)
    if source.metadata.num_rows < args.rows:
        ap.error("source prefix is shorter than requested")
    batches, remaining = [], args.rows
    for batch in source.iter_batches(batch_size=10000):
        batch = batch.slice(0, min(remaining, batch.num_rows))
        batches.append(convert_batch(batch, schema))
        remaining -= batch.num_rows
        if remaining == 0:
            break
    table = pa.Table.from_batches(batches)
    namespace = "partitionprofile_" + uuid.uuid4().hex[:12]
    options = {"uri": uri}
    if args.partition_mode == "retained":
        options["adbc.redis.partition_result_namespace"] = namespace
    worker_options = dict(options)
    if args.partition_mode == "retained":
        worker_options["adbc.redis.partition_reader_only"] = "true"
    oracle = duckdb.connect()
    source_table = table.add_column(0, "source_ordinal", pa.array(range(1, args.rows + 1), pa.int64()))
    oracle.register("all_hits", source_table)
    keep = {"dense": "true", "sparse": "source_ordinal % 10 = 0", "skewed": f"source_ordinal > {args.rows * 9 // 10}"}[args.layout]
    source_table = oracle.sql("SELECT * FROM all_hits WHERE " + keep).to_arrow_table()
    oracle.register("hits", source_table.drop(["source_ordinal"]))
    oracle.register("source_hits", source_table)
    control = redis.Redis.from_url(uri, decode_responses=True)
    report = {"kind": "local ClickBench API comparison pilot; not a leaderboard result",
              "cpu_count": os.cpu_count(), "host_memory_bytes": psutil.virtual_memory().total,
              "rows_ingested": args.rows, "rows_retained": source_table.num_rows, "layout": args.layout, "columns": len(schema), "platform": platform.platform(),
              "upstream_commit": commit(args.upstream),
              "driver_revision": commit(Path(__file__).resolve().parents[2]),
              "source_dirty": bool(subprocess.check_output(["git", "status", "--porcelain"], text=True).strip()),
              "driver_sha256": hashlib.sha256(args.driver.read_bytes()).hexdigest(),
              "source_prefix_sha256": hashlib.sha256(args.parquet.read_bytes()).hexdigest(),
              "redis_version": control.info("server")["redis_version"],
              "search_module": control.execute_command("MODULE", "LIST"),
              "aggregate_cap": control.config_get("search-max-aggregate-results").get("search-max-aggregate-results"),
              "partition_mode": args.partition_mode,
              "execution_limits": json.loads(args.execution_limits) if args.execution_limits else None,
              "packages": {n: importlib.metadata.version(n) for n in ["pyarrow", "adbc-driver-manager", "duckdb", "redis", "psutil"]},
              "measurement_notes": [
                  "End-to-end includes planning, fresh child process/connection startup, full fetch and Arrow IPC writes; parent validation is outside the timer.",
                  "Process-tree RSS sums shared pages and includes coordinator/source/oracle state; sampled peak can miss short transients. Worker rusage is lifetime peak including imports.",
                  "Network deltas include INFO telemetry and other traffic to this dedicated server; server memory is sampled, not isolated per query.",
                  "Local reduced-data pilot with no enforced process memory limit; not full-scale or Cloud ClickBench scores.",
                  "Ingest and expected field names fold to lowercase to match unquoted PostgreSQL identifiers; official SQL is unchanged apart from schema qualification."],
              "official_partition_coverage": [], "trials": [], "summary": [], "status": "running"}
    args.output.parent.mkdir(parents=True, exist_ok=True)

    def save():
        args.output.write_text(json.dumps(report, indent=2) + "\n")

    def qualify(sql):
        return sql.replace("FROM hits", f"FROM {namespace}.hits")

    def trial(cur, name, sql, expected, method, workers, partitions, strategy, repeat, ordinal):
        with tempfile.TemporaryDirectory(prefix="clickbench-partition-") as tmp:
            with TrialMonitor(control) as monitor:
                started = time.perf_counter()
                planning = 0
                descriptors = [None]
                if method == "partitions":
                    cur.adbc_statement.set_options(**{"adbc.redis.partition_count": partitions, "adbc.redis.partition_strategy": strategy})
                    pstart = time.perf_counter()
                    descriptors, planned_schema = cur.adbc_execute_partitions(qualify(sql))
                    planning = time.perf_counter() - pstart
                paths = [str(Path(tmp) / f"{i}.arrow") for i in range(len(descriptors))]
                with concurrent.futures.ProcessPoolExecutor(max_workers=workers, mp_context=multiprocessing.get_context("spawn")) as pool:
                    metrics = list(pool.map(read_task, [(driver, worker_options if descriptor is not None else options, qualify(sql), descriptor, path) for descriptor, path in zip(descriptors, paths)]))
                seconds = time.perf_counter() - started
            tables = [pa.ipc.open_stream(path).read_all() for path in paths]
            actual = pa.concat_tables(tables)
            schema_matches = method != "partitions" or actual.schema.equals(planned_schema)
            if ordinal:
                actual = actual.sort_by("source_ordinal")
            # Ordinary output supplies a physical Arrow schema baseline, while
            # all complete values independently match the typed source/DuckDB.
            actual_rows = actual.to_pylist()
            # Generated expression labels differ across engines (COUNT versus
            # DuckDB's count_star()). Compare the independent oracle by column
            # ordinal; the driver's full physical schema is compared separately
            # between ExecuteQuery and every partition trial below.
            if expected.num_columns != actual.num_columns:
                raise AssertionError("independent oracle column count differs")
            expected_rows = expected.rename_columns(actual.column_names).to_pylist()
            if not ordinal:
                actual_rows = sorted(actual_rows, key=lambda r: json.dumps(r, default=str, sort_keys=True))
                expected_rows = sorted(expected_rows, key=lambda r: json.dumps(r, default=str, sort_keys=True))
            if not schema_matches or actual_rows != expected_rows:
                failure = {"workload": name, "method": method, "workers": workers,
                           "partitions": partitions, "strategy": strategy, "repeat": repeat,
                           "actual_rows": len(actual_rows), "expected_rows": len(expected_rows),
                           "table": namespace + ".hits", "schema_matches_plan": schema_matches, "actual_schema": str(actual.schema),
                           "first_difference": next(({"actual": a, "expected": b} for a, b in zip(actual_rows, expected_rows) if a != b), None)}
                report["failure"] = json.loads(json.dumps(failure, default=str))
                report["status"] = "failed_validation"
                save()
                for label, value in [("actual", actual), ("expected", expected)]:
                    path = str(args.output) + "." + label + ".arrow"
                    with pa.OSFile(path, "wb") as sink, pa.ipc.new_stream(sink, value.schema) as writer:
                        writer.write_table(value)
                raise AssertionError(f"complete value/schema check failed: {failure}")
            record = {"workload": name, "sql": sql, "official": name.startswith("Q"), "method": method,
                      "workers": workers, "partitions": len(descriptors) if method == "partitions" else None,
                      "requested_partitions": partitions,
                      "strategy": strategy, "repeat": repeat, "rows": actual.num_rows, "end_to_end_seconds": seconds,
                      "planning_seconds": planning, "exact_values": True,
                      "arrow_schema": str(actual.schema), "worker_metrics": metrics, "telemetry": monitor.result()}
            report["trials"].append(record)
            if method == "partitions" and args.partition_mode == "retained":
                revoke(cur, descriptors)
            save()
            print(name, method, workers, partitions, strategy, repeat, round(seconds, 4), actual.num_rows, flush=True)

    def revoke(cur, descriptors):
        job = json.loads(descriptors[0])["result"]["job_id"]
        if len(job) != 32 or any(c not in "0123456789abcdef" for c in job):
            raise AssertionError("invalid retained job identity")
        cur.execute(f"DROP PARTITION RESULT '{job}'")

    with dbapi.connect(driver=driver, db_kwargs=options, autocommit=True) as conn:
        with conn.cursor() as cur:
            cur.execute("CREATE SCHEMA " + namespace)
            try:
                cur.execute("SET search_path TO " + namespace)
                cur.execute(ddl)
                started = time.perf_counter()
                for batch in batches:
                    cur.adbc_ingest("hits", batch.rename_columns([n.lower() for n in batch.schema.names]), mode="append", db_schema_name=namespace)
                report["load_index_readiness"] = wait_index_ready(control, "idx:" + namespace + ":hits", args.rows)
                report["load_seconds"] = time.perf_counter() - started
                cur.execute("SELECT COUNT(*) FROM hits")
                assert cur.fetchone()[0] == args.rows
                if args.layout != "dense":
                    delete = "__rowid % 10 <> 0" if args.layout == "sparse" else f"__rowid <= {args.rows * 9 // 10}"
                    cur.execute("DELETE FROM hits WHERE " + delete)
                report["final_index_readiness"] = wait_index_ready(control, "idx:" + namespace + ":hits", source_table.num_rows)
                cur.execute("SELECT COUNT(*) FROM hits")
                assert cur.fetchone()[0] == source_table.num_rows
                supported = []
                cur.adbc_statement.set_options(**{"adbc.redis.partition_result_mode": args.partition_mode})
                if args.execution_limits:
                    cur.adbc_statement.set_options(**{"adbc.redis.partition_execution_limits": args.execution_limits})
                for number, sql in enumerate(queries, 1):
                    try:
                        descriptors, _ = cur.adbc_execute_partitions(qualify(sql))
                        if args.partition_mode == "retained":
                            revoke(cur, descriptors)
                        report["official_partition_coverage"].append({"query": number, "sql": sql, "status": "supported"})
                        supported.append((f"Q{number:02}", sql, oracle.sql(sql).to_arrow_table(), False))
                    except dbapi.NotSupportedError as exc:
                        report["official_partition_coverage"].append({"query": number, "sql": sql, "status": "not_implemented", "reason": str(exc)})
                save()
                supplemental = [
                    ("rowid_point", f"SELECT __rowid AS source_ordinal, WatchID, UserID FROM hits WHERE __rowid={args.rows}", f"SELECT source_ordinal, WatchID, UserID FROM source_hits WHERE source_ordinal={args.rows}"),
                    ("rowid_in", f"SELECT __rowid AS source_ordinal, WatchID, UserID FROM hits WHERE __rowid IN ({args.rows},{args.rows-10},{args.rows},{args.rows-20})", f"SELECT source_ordinal, WatchID, UserID FROM source_hits WHERE source_ordinal IN ({args.rows},{args.rows-10},{args.rows-20})"),
                    ("rowid_range", f"SELECT __rowid AS source_ordinal, WatchID, UserID FROM hits WHERE __rowid BETWEEN {max(1,args.rows-1999)} AND {args.rows}", f"SELECT source_ordinal, WatchID, UserID FROM source_hits WHERE source_ordinal BETWEEN {max(1,args.rows-1999)} AND {args.rows}"),
                    ("scan_narrow", "SELECT __rowid AS source_ordinal, WatchID, UserID, AdvEngineID FROM hits", "SELECT source_ordinal, WatchID, UserID, AdvEngineID FROM source_hits"),
                    ("scan_wide", "SELECT __rowid AS source_ordinal, * FROM hits", "SELECT * FROM source_hits"),
                    ("filter_adv", "SELECT __rowid AS source_ordinal, WatchID, AdvEngineID, ResolutionWidth FROM hits WHERE AdvEngineID <> 0", "SELECT source_ordinal, WatchID, AdvEngineID, ResolutionWidth FROM source_hits WHERE AdvEngineID <> 0"),
                    ("expression_region", "SELECT __rowid AS source_ordinal, AdvEngineID + 1 AS engine, LENGTH(URL) AS url_length FROM hits WHERE RegionID = 229", "SELECT source_ordinal, CAST(AdvEngineID + 1 AS BIGINT) AS engine, LENGTH(URL) AS url_length FROM source_hits WHERE RegionID = 229"),
                ]
                workloads = supported + [(name, sql, oracle.sql(reference).to_arrow_table(), True) for name, sql, reference in supplemental]
                if args.workloads:
                    workloads = [w for w in workloads if w[0] in args.workloads]
                    if {w[0] for w in workloads} != set(args.workloads):
                        raise ValueError("unknown workload name")
                for name, sql, expected, ordinal in workloads:
                    for repeat in range(args.repeats):
                        methods = [("execute_query", 1, None, None)] + [("partitions", n, p, strategy) for p in args.partitions for n in args.workers for strategy in args.strategies]
                        shift = repeat % len(methods)
                        for method, workers, partitions, strategy in methods[shift:] + methods[:shift]:
                            trial(cur, name, sql, expected, method, workers, partitions, strategy, repeat, ordinal)
                for name, _, _, _ in workloads:
                    baseline = [r["end_to_end_seconds"] for r in report["trials"] if r["workload"] == name and r["method"] == "execute_query"]
                    baseline_schema = {r["arrow_schema"] for r in report["trials"] if r["workload"] == name and r["method"] == "execute_query"}
                    if len(baseline_schema) != 1:
                        raise AssertionError("ordinary schema changed between trials")
                    for method, workers, partitions, strategy in [("execute_query", 1, None, None)] + [("partitions", n, p, strategy) for p in args.partitions for n in args.workers for strategy in args.strategies]:
                        records = [r for r in report["trials"] if r["workload"] == name and r["method"] == method and r["workers"] == workers and r["requested_partitions"] == partitions and r["strategy"] == strategy]
                        if any(r["arrow_schema"] not in baseline_schema for r in records):
                            raise AssertionError("partition schema differs from ordinary execution")
                        median = statistics.median(r["end_to_end_seconds"] for r in records)
                        report["summary"].append({"workload": name, "method": method, "workers": workers,
                            "partitions": partitions, "strategy": strategy, "median_seconds": median, "median_rows_per_second": records[0]["rows"] / median, "ratio_to_execute_query": median / statistics.median(baseline),
                            "rows": records[0]["rows"], "all_exact_values_and_schema": True,
                            "median_first_batch_seconds": statistics.median(m["first_batch_seconds"] for r in records for m in r["worker_metrics"] if m["first_batch_seconds"] is not None) if records[0]["rows"] else None,
                            "max_sampled_client_rss_bytes": max(r["telemetry"]["client_process_tree_sampled_peak_rss_bytes"] for r in records),
                            "max_sampled_server_used_memory_bytes": max(r["telemetry"]["server_sampled_peak_used_memory_bytes"] for r in records)})
                report["status"] = "complete"
                save()
            finally:
                if args.keep_failure_table and report["status"] == "failed_validation":
                    print("Retained failed diagnostic table:", namespace + ".hits", flush=True)
                else:
                    cur.execute(f"DROP TABLE IF EXISTS {namespace}.hits")
                    cur.execute("DROP SCHEMA " + namespace)
                if args.partition_mode == "retained":
                    pattern = f"adbc:{{retained:{namespace}}}:*"
                    for key in control.scan_iter(match=pattern, count=100):
                        control.delete(key)
                    if list(control.scan_iter(match=pattern, count=100)):
                        raise AssertionError("retained benchmark namespace cleanup incomplete")
                    report["retained_namespace_cleaned"] = True
                    save()
    oracle.close()
    control.close()


if __name__ == "__main__":
    main()
