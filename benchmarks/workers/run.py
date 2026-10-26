# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Single-query worker scaling on the disposable local Redis Software database.

Load go/examples/bench.py's fixture first. This harness never writes Redis or
changes QPF. It requires standard QPF and verifies effective server settings.
Run from the repository root; see the dated report for the complete command.
"""

import argparse
import base64
import hashlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import platform
import random
import resource
import ssl
import statistics
import subprocess
import sys
import time
import urllib.request
from urllib.parse import urlparse

import adbc_driver_manager.dbapi
import pyarrow as pa
import redis

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "go/examples"))
from common import DRIVER

WORKERS = ["1", "2", "4", "8", "auto"]
PREFIX = "adbc.redis."
# Default exact pushdown stays enabled. AVG(double) requires driver reduction.
QUERIES = {
    "driver_avg": {"sql": "SELECT AVG(discount) FROM sales", "rows": 1, "eligible": True},
    "driver_reduce": {"sql": "SELECT SUM(quantity), AVG(discount), MIN(unit_price), MAX(unit_price) FROM sales", "rows": 1, "eligible": True},
    "driver_filtered": {"sql": "SELECT SUM(quantity), AVG(discount), MIN(unit_price), MAX(unit_price) FROM sales WHERE quantity BETWEEN 5 AND 10", "rows": 1, "eligible": True},
    "index_count": {"sql": "SELECT COUNT(*) FROM sales WHERE quantity BETWEEN 5 AND 10", "rows": 1, "eligible": False},
    "index_group": {"sql": "SELECT country, COUNT(*), SUM(quantity) FROM sales GROUP BY country ORDER BY country", "rows": 6, "eligible": False},
    "driver_group": {"sql": "SELECT country, AVG(discount) FROM sales GROUP BY country ORDER BY country", "rows": 6, "eligible": False},
    "full_scan": {"sql": "SELECT * FROM sales", "rows": 100000, "eligible": False},
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def signature(table, name):
    if name != "full_scan":
        return json.loads(json.dumps(table.to_pylist(), default=str))
    table = table.sort_by([("order_id", "ascending")]).combine_chunks()
    sink = pa.BufferOutputStream()
    with pa.ipc.new_stream(sink, table.schema) as writer:
        writer.write_table(table)
    return {"sorted_arrow_sha256": hashlib.sha256(sink.getvalue()).hexdigest()}


def check_result(actual, expected):
    if isinstance(expected, float):
        require(isinstance(actual, (int, float)) and math.isclose(actual, expected, rel_tol=1e-12, abs_tol=1e-12), "Float result mismatch")
    elif isinstance(expected, list):
        require(isinstance(actual, list) and len(actual) == len(expected), "Result length mismatch")
        for a, e in zip(actual, expected):
            check_result(a, e)
    elif isinstance(expected, dict):
        require(isinstance(actual, dict) and actual.keys() == expected.keys(), "Result columns mismatch")
        for key in expected:
            check_result(actual[key], expected[key])
    else:
        require(actual == expected, "Result value mismatch")


def child(args):
    """A fresh client process isolates each block's memory high-water mark."""
    query = QUERIES[args.query]
    kwargs = {"uri": args.uri, PREFIX + "query_workers": args.workers,
              PREFIX + "query_worker_budget": "auto"}
    with adbc_driver_manager.dbapi.connect(driver=DRIVER, db_kwargs=kwargs, autocommit=True) as conn, conn.cursor() as cur:
        resolved = {key: int(conn.adbc_connection.get_option(PREFIX + key))
                    for key in ("query_workers", "query_worker_budget")}
        require(resolved["query_worker_budget"] == args.cpus, "Unexpected automatic budget")
        ceiling = args.cpus if args.workers == "auto" else int(args.workers)
        require(resolved["query_workers"] == ceiling, "Unexpected worker ceiling")
        want_grant = min(ceiling, args.cpus) if query["eligible"] else 0
        samples, table, reference = [], None, None
        for i in range(args.warmups + args.repeat):
            table = None  # Release the previous result outside the timed region.
            cpu_start = time.process_time()
            start = time.perf_counter()
            cur.execute(query["sql"])
            table = cur.fetch_arrow_table()
            wall_ms = (time.perf_counter() - start) * 1000
            cpu_ms = (time.process_time() - cpu_start) * 1000
            require(table.num_rows == query["rows"], "Unexpected row count; load the 100,000-row fixture")
            grant = int(conn.adbc_connection.get_option(PREFIX + "query_workers_last_grant"))
            require(grant == want_grant, f"Expected grant {want_grant}, observed {grant}")
            require(conn.adbc_connection.get_option(PREFIX + "query_workers_active") == "0", "Worker grant was not released")
            if args.query != "full_scan":
                current = signature(table, args.query)
                if reference is not None:
                    check_result(current, reference)
                reference = current
            if i >= args.warmups:
                samples.append({"wall_ms": wall_ms, "cpu_ms": cpu_ms, "grant": grant})
        # Includes interpreter, Arrow, Go runtime, warmups and all timed queries.
        # Read before full-scan sorting/serialization, which is validation only.
        rss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
        peak_rss_bytes = rss if sys.platform == "darwin" else rss * 1024
        result = signature(table, args.query)
        print(json.dumps({"query": args.query, "workers": args.workers, "resolved": resolved,
                          "samples": samples, "peak_rss_bytes": peak_rss_bytes, "result": result}))


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--output", type=Path)
    ap.add_argument("--cpus", type=int, default=10, help="fixed client GOMAXPROCS, not a host CPU limit")
    ap.add_argument("--rounds", type=int, choices=(1, 5), default=5)
    ap.add_argument("--repeat", type=int, default=3)
    ap.add_argument("--warmups", type=int, default=2)
    ap.add_argument("--seed", type=int, default=224)
    ap.add_argument("--child", action="store_true", help=argparse.SUPPRESS)
    ap.add_argument("--query", choices=QUERIES, help=argparse.SUPPRESS)
    ap.add_argument("--workers", choices=WORKERS, help=argparse.SUPPRESS)
    args = ap.parse_args()
    require(args.cpus > 0 and args.repeat > 0 and args.warmups > 0, "CPU, repeat and warmup counts must be positive")
    args.uri = os.getenv("REDIS_URI", "redis://localhost:6379/0")
    if args.child:
        child(args)
        return
    if args.output is None:
        ap.error("--output is required")
    endpoint = urlparse(args.uri)
    require(endpoint.scheme == "redis" and endpoint.hostname in ("localhost", "127.0.0.1")
            and (endpoint.port or 6379) == int(os.getenv("SOFTWARE_DB_PORT", "6379"))
            and endpoint.path in ("", "/", "/0"), "Use the local make redis-up database")
    args.output.mkdir(parents=True, exist_ok=True)
    require(not (args.output / "results.json").exists(), "Choose a new output directory to preserve previous results")
    auth = base64.b64encode((os.getenv("SOFTWARE_ADMIN_USER", "admin@example.com") + ":" +
                            os.getenv("SOFTWARE_ADMIN_PASSWORD", "adbc-software-test")).encode()).decode()
    api_port = int(os.getenv("SOFTWARE_API_PORT", "19443"))
    ctx = ssl._create_unverified_context()  # disposable localhost certificate

    def api(path):
        request = urllib.request.Request(f"https://localhost:{api_port}/v1/{path}", headers={"Authorization": "Basic " + auth})
        with urllib.request.urlopen(request, context=ctx, timeout=30) as response:
            return json.load(response)

    uid = next(b["uid"] for b in api("bdbs") if b["name"] == "adbc-test")

    def server_config():
        bdb = api(f"bdbs/{uid}?fields=query_performance_factor,sched_policy,conns,search,module_list,status")
        qpf = bdb["query_performance_factor"]
        require((not qpf["active"] or qpf["scaling_factor"] == 0) and bdb["status"] == "active"
                and bdb["search"]["search-workers"] == 0 and bdb["sched_policy"] == "cmp"
                and bdb["conns"] == 5, "QPF must remain standard, including effective worker/routing settings")
        return bdb

    def command(*cmd):
        return subprocess.check_output(cmd, cwd=ROOT, text=True).strip()

    client = redis.Redis.from_url(args.uri, decode_responses=True)
    environment = {**os.environ, "GOMAXPROCS": str(args.cpus)}

    def run_block(name, workers, repeat=args.repeat):
        output = subprocess.check_output([sys.executable, str(Path(__file__).resolve()), "--child",
                    "--query", name, "--workers", workers, "--cpus", str(args.cpus),
                    "--repeat", str(repeat), "--warmups", str(args.warmups)],
                    env=environment, text=True, timeout=180)
        return json.loads(output)

    rng = random.Random(args.seed)
    orders = {}
    for name in QUERIES:
        orders[name] = rng.sample(WORKERS, len(WORKERS))
    schedule = []
    for round_idx in range(args.rounds):
        for name in rng.sample(list(QUERIES), len(QUERIES)):
            order = orders[name][round_idx:] + orders[name][:round_idx]
            schedule.extend({"round": round_idx + 1, "query": name, "workers": w} for w in order)
    metadata = {"date_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "rows": 100000,
                "gomaxprocs": args.cpus, "budget": "auto", "pushdown": "exact (default)",
                "rounds": args.rounds, "repeat": args.repeat, "warmups_per_block": args.warmups,
                "seed": args.seed, "queries": QUERIES, "schedule": schedule,
                "commit": command("git", "rev-parse", "HEAD"),
                "driver_sha256": hashlib.sha256(Path(DRIVER).read_bytes()).hexdigest(),
                "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                "go_build": command("go", "version", "-m", DRIVER),
                "python": platform.python_version(), "platform": platform.platform(),
                "host_logical_cpus": os.cpu_count(),
                "packages": {p: importlib.metadata.version(p) for p in ("pyarrow", "adbc-driver-manager", "redis")},
                "docker": json.loads(command("docker", "info", "--format", '{"cpus":{{.NCPU}},"memory_bytes":{{.MemTotal}},"version":"{{.ServerVersion}}"}')),
                "image_digests": json.loads(command("docker", "image", "inspect", "redislabs/redis:8.2.0-78.18", "--format", "{{json .RepoDigests}}")),
                "server": client.info("server"), "modules": client.execute_command("MODULE", "LIST")}
    results = {"metadata": metadata, "references": {}, "server_checks": [], "blocks": [], "complete": False}

    def save():
        path = args.output / "results.json"
        temporary = path.with_suffix(".tmp")
        temporary.write_text(json.dumps(results, indent=2, default=str) + "\n")
        temporary.replace(path)

    try:
        results["server_checks"].append({"before_round": 0, "config": server_config(),
            "containers": command("docker", "ps", "--format", "{{.Names}}")})
        for name in QUERIES:
            reference = run_block(name, "1", repeat=1)
            results["references"][name] = reference["result"]
        save()
        current_round = 0
        for scheduled in schedule:
            if current_round != scheduled["round"]:
                current_round = scheduled["round"]
                results["server_checks"].append({"before_round": current_round, "config": server_config(),
                    "containers": command("docker", "ps", "--format", "{{.Names}}")})
            block = run_block(scheduled["query"], scheduled["workers"])
            check_result(block.pop("result"), results["references"][scheduled["query"]])
            block["round"] = current_round
            results["blocks"].append(block)
            save()
            print(json.dumps({**scheduled, "median_ms": statistics.median(s["wall_ms"] for s in block["samples"])}), flush=True)
        results["server_checks"].append({"after_round": current_round, "config": server_config(),
            "containers": command("docker", "ps", "--format", "{{.Names}}")})
        results["complete"] = True
    finally:
        save()


if __name__ == "__main__":
    main()
