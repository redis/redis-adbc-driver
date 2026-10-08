"""End-to-end SQL/Arrow QPF benchmark on the disposable `make redis-up` database.

First load the sample using go/examples/bench.py. This script changes QPF on
adbc-test and restores its original setting even if a trial fails. No writes
to the fixture occur. See the dated report for reproduction commands.
"""
import argparse
import base64
import concurrent.futures
import hashlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import platform
import ssl
import statistics
import subprocess
import sys
import threading
import time
import urllib.request
from urllib.parse import urlparse
import warnings

import redis

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "go/examples"))
from common import connect, DRIVER

# Small results exercise server compute at 16 clients; larger driver/transfer
# workloads use four to keep the laptop memory footprint bounded.
QUERIES = {
    "index_group": ("SELECT country, COUNT(*), SUM(quantity) FROM sales GROUP BY country ORDER BY country", [1, 16]),
    "index_count": ("SELECT COUNT(*) FROM sales WHERE quantity BETWEEN 5 AND 10", [1, 16]),
    "driver_group": ("SELECT country, AVG(discount) FROM sales GROUP BY country ORDER BY country", [1, 4]),
    "driver_reduce": ("SELECT SUM(quantity), AVG(discount), MIN(unit_price), MAX(unit_price) FROM sales", [1, 4]),
    "full_scan": ("SELECT * FROM sales", [1, 4]),
}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--output", type=Path, required=True)
    ap.add_argument("--seconds", type=float, default=5)
    ap.add_argument("--rounds", type=int, choices=[1, 3], default=3)
    ap.add_argument("--latency-bench", action="store_true", help="also run all 20 README queries at each factor, without reloading")
    args = ap.parse_args()
    if args.seconds <= 0:
        ap.error("--seconds must be positive")
    uri = os.getenv("REDIS_URI", "redis://localhost:6379/0")
    endpoint = urlparse(uri)
    if (endpoint.scheme != "redis" or endpoint.hostname not in ("localhost", "127.0.0.1")
            or (endpoint.port or 6379) != int(os.getenv("SOFTWARE_DB_PORT", "6379"))
            or endpoint.path not in ("", "/", "/0")):
        ap.error("REDIS_URI must target the local make redis-up database; match SOFTWARE_DB_PORT")
    args.output.mkdir(parents=True, exist_ok=True)
    auth = base64.b64encode((os.getenv("SOFTWARE_ADMIN_USER", "admin@example.com") + ":" +
                            os.getenv("SOFTWARE_ADMIN_PASSWORD", "adbc-software-test")).encode()).decode()
    api_port = int(os.getenv("SOFTWARE_API_PORT", "19443"))
    ctx = ssl._create_unverified_context()  # local test container certificate

    def api(path, body=None):
        request = urllib.request.Request(f"https://localhost:{api_port}/v1/{path}",
                    data=json.dumps(body).encode() if body is not None else None,
                    method="PUT" if body is not None else "GET",
                    headers={"Authorization": "Basic " + auth, "Content-Type": "application/json"})
        with urllib.request.urlopen(request, context=ctx, timeout=30) as response:
            return json.load(response)

    bdb = next(b for b in api("bdbs") if b["name"] == "adbc-test")
    uid = bdb["uid"]
    fields = "query_performance_factor,sched_policy,conns,search,module_list,status"
    bdb = api(f"bdbs/{uid}?fields={fields}")
    original = bdb["query_performance_factor"]
    if not original["active"] and (bdb["sched_policy"] != "cmp" or bdb["conns"] != 5
                                   or bdb["search"]["search-workers"] != 0):
        ap.error("Inactive QPF retains nonstandard settings; recreate the disposable database first")
    configs, trials, samples = [], [], []
    active_trial = {}
    stop_monitor = threading.Event()

    def set_qpf(setting):
        # active=false stops managing QPF but retains the existing worker and
        # routing settings. Standard is active=true, scaling_factor=0. Reset
        # effective settings before restoring an originally inactive baseline.
        if not setting["active"]:
            set_qpf({"active": True, "scaling_factor": 0})
        factor = setting["scaling_factor"] if setting["active"] else 0
        workers = math.ceil(factor * 1.5)
        policy, conns = ("mnp", 32) if factor else ("cmp", 5)
        api(f"bdbs/{uid}", {"query_performance_factor": setting})
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            observed = api(f"bdbs/{uid}?fields={fields}")
            if (observed["status"] == "active" and observed["query_performance_factor"] == setting
                    and observed["search"]["search-workers"] == workers
                    and observed["sched_policy"] == policy and observed["conns"] == conns):
                time.sleep(2)
                return {k: observed.get(k) for k in ("query_performance_factor", "sched_policy", "conns", "search", "module_list")}
            time.sleep(1)
        raise RuntimeError("QPF did not become active")

    def monitor():
        while not stop_monitor.is_set():
            try:
                output = subprocess.check_output(["docker", "stats", "--no-stream", "--format", "{{json .}}"], text=True, timeout=15)
                samples.append({"time": time.time(), "trial": dict(active_trial),
                                "containers": [json.loads(line) for line in output.splitlines()]})
            except Exception as error:
                samples.append({"time": time.time(), "error": str(error)})
            stop_monitor.wait(8)

    client = redis.Redis.from_url(uri, decode_responses=True)
    meta = {"date": time.strftime("%Y-%m-%d"), "rows": 100000, "seconds_per_trial": args.seconds,
            "rounds": args.rounds, "query_concurrency": QUERIES, "driver_sha256": hashlib.sha256(Path(DRIVER).read_bytes()).hexdigest(),
            "commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
            "python": platform.python_version(), "platform": platform.platform(),
            "packages": {p: importlib.metadata.version(p) for p in ("pyarrow", "adbc-driver-manager", "redis")},
            "server": client.info("server"), "modules": client.execute_command("MODULE", "LIST"),
            "original_qpf": original, "configs": configs}
    results = {"metadata": meta, "trials": trials, "docker_samples": samples}

    def save():
        (args.output / "results.json").write_text(json.dumps(results, indent=2, default=str) + "\n")

    def canonical(table, name):
        if name == "full_scan":
            table = table.sort_by([("order_id", "ascending")])
        return table.to_pylist()

    def assert_equal(actual, expected):
        assert len(actual) == len(expected), "Row count mismatch"
        for row, reference in zip(actual, expected):
            assert row.keys() == reference.keys()
            for key, value in row.items():
                want = reference[key]
                if isinstance(value, float):
                    assert math.isclose(value, want, rel_tol=1e-12, abs_tol=1e-12), (key, value, want)
                else:
                    assert value == want, (key, value, want)

    watcher = threading.Thread(target=monitor, daemon=True)
    watcher.start()
    try:
        # Clone connections share one database handle and its automatic worker
        # budget. Suppress the driver's expected no-transactions clone warning.
        warnings.filterwarnings("ignore", message="Cannot disable autocommit.*")
        with connect() as owner:
            meta["worker_options"] = {key: owner.adbc_connection.get_option(key) for key in
                                      ("adbc.redis.query_workers", "adbc.redis.query_worker_budget")}
            with owner.cursor() as cur:
                expected = {}
                for name, (sql, _) in QUERIES.items():
                    cur.execute(sql)
                    expected[name] = canonical(cur.fetch_arrow_table(), name)
            assert len(expected["full_scan"]) == meta["rows"]
            meta["result_hashes"] = {name: hashlib.sha256(repr(rows).encode()).hexdigest() for name, rows in expected.items()}
            for round_idx, factors in enumerate([[0, 2, 4], [4, 0, 2], [2, 4, 0]][:args.rounds], 1):
                for factor in factors:
                    config = set_qpf({"active": True, "scaling_factor": factor})
                    configs.append({"round": round_idx, "qpf": factor, "observed": config})
                    with owner.cursor() as cur:
                        for name, (sql, _) in QUERIES.items():
                            cur.execute(sql)
                            assert_equal(canonical(cur.fetch_arrow_table(), name), expected[name])
                    if args.latency_bench and round_idx == 1:
                        active_trial.clear()
                        active_trial.update(round=round_idx, qpf=factor, query="readme")
                        with (args.output / f"latency-qpf-{factor}.txt").open("w") as log:
                            subprocess.run([sys.executable, str(ROOT / "go/examples/bench.py"), "--skip-load", "--rows", "100000", "--repeat", "5"], stdout=log, stderr=subprocess.STDOUT, check=True)
                    for name, (sql, concurrencies) in QUERIES.items():
                        for concurrency in concurrencies:
                            active_trial.update(round=round_idx, qpf=factor, query=name, concurrency=concurrency)
                            start = [0.0]
                            ready = threading.Barrier(concurrency + 1, timeout=180,
                                                      action=lambda: start.__setitem__(0, time.perf_counter()))
                            def worker():
                                try:
                                    with owner.adbc_clone() as conn, conn.cursor() as cur:
                                        for _ in range(2):
                                            cur.execute(sql)
                                            assert cur.fetch_arrow_table().num_rows == len(expected[name])
                                        ready.wait()
                                        latencies = []
                                        while time.perf_counter() < start[0] + args.seconds:
                                            begin = time.perf_counter()
                                            cur.execute(sql)
                                            table = cur.fetch_arrow_table()
                                            elapsed = (time.perf_counter() - begin) * 1000
                                            assert table.num_rows == len(expected[name])
                                            latencies.append(elapsed)
                                        return latencies
                                except BaseException:
                                    ready.abort()
                                    raise
                            before = client.info()
                            with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
                                tasks = [pool.submit(worker) for _ in range(concurrency)]
                                # Action sets the deadline after all connections warm up.
                                ready.wait()
                                latencies = sorted(v for task in tasks for v in task.result())
                                elapsed = time.perf_counter() - start[0]
                            after = client.info()
                            record = dict(active_trial, requests=len(latencies), elapsed_s=elapsed,
                                          qps=len(latencies) / elapsed, p50_ms=statistics.median(latencies),
                                          p95_ms=latencies[math.ceil(.95 * len(latencies)) - 1], errors=0,
                                          redis_delta={k: after[k] - before[k] for k in ("used_cpu_sys", "used_cpu_user", "total_net_input_bytes", "total_net_output_bytes") if k in before and k in after})
                            trials.append(record)
                            print(json.dumps(record), flush=True)
                            save()
    finally:
        try:
            meta["restored_qpf"] = set_qpf(original)
        finally:
            stop_monitor.set()
            watcher.join(timeout=20)
            save()
            client.close()


if __name__ == "__main__":
    main()
