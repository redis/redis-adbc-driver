"""SF1 query comparison using unmodified released engines and upstream SQL.

Run inside a Linux client sharing an owned Software server's network namespace.
Credentials, generated data and server evidence live outside this directory.
The caller supplies a fresh dedicated database for each suite. No reset here.
"""

import argparse
import datetime
from decimal import Decimal
import importlib.metadata
import json
import math
import os
from pathlib import Path
import platform
import re
import time
import subprocess
import sys



def emit(path, value):
    with path.open("a") as out:
        out.write(json.dumps(value, default=str) + "\n")
    print(json.dumps(value, default=str), flush=True)


def cell(value):
    if value is None:
        return (0, "")
    if isinstance(value, (int, float, Decimal)):
        return (1, Decimal(str(value)))
    if isinstance(value, (datetime.date, datetime.datetime)):
        return (2, value.isoformat())
    return (3, str(value))


def equal(actual, expected):
    """Compare complete row multisets, retaining duplicates; tolerate FP rounding.

    SQL ordering with ties need not have a deterministic row order across engines.
    Numeric tolerance is 1e-7 relative and 1e-6 absolute, including decimal values.
    """
    if len(actual) != len(expected):
        return False
    left = sorted(tuple(cell(v) for v in row) for row in actual)
    right = sorted(tuple(cell(v) for v in row) for row in expected)
    return all(equal_cells(a, b) for a, b in zip(left, right))


def equal_cells(a, b):
    if len(a) != len(b):
        return False
    for x, y in zip(a, b):
        if x[0] != y[0]:
            return False
        if x[0] == 1:
            if not math.isclose(x[1], y[1], rel_tol=1e-7, abs_tol=1e-6):
                return False
        elif x != y:
            return False
    return True


def safe_error(value):
    # Engine errors can contain ATTACH SQL, including credentials and options.
    value = re.sub(r"rediss?://[^\s'\"]+", "[redacted URI]", value)
    value = re.sub(r"(?is)(?:ATTACH|CREATE\s+SECRET)\b.*", "[redacted connection SQL]", value)
    value = re.sub(r"\b\d{1,3}(?:\.\d{1,3}){3}:\d+\b", "[endpoint]", value)
    value = re.sub(r"\[[0-9a-fA-F:]+\]:\d+", "[endpoint]", value)
    value = re.sub(r"\b[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\b", "[identifier]", value)
    value = re.sub(r"\b[0-9a-fA-F]{32}\b", "[identifier]", value)
    return value


def prepare(suite, private, query_dir):
    import duckdb

    source = private / f"{suite}-sf1.duckdb"
    c = duckdb.connect(str(source))
    c.execute("SET threads=4; SET memory_limit='8GB'")
    if not c.execute("SHOW TABLES").fetchall():
        if suite in ("tpch", "tpcds"):
            c.execute(f"INSTALL {suite}; LOAD {suite}")
            gen = "dbgen" if suite == "tpch" else "dsdgen"
            c.execute(f"CALL {gen}(sf=1)")
        else:
            ddl = (private / "ssb-tools/other_queries/create_schema_without_constraints.sql").read_text()
            c.execute(ddl)
            for (table,) in c.execute("SHOW TABLES").fetchall():
                data = private / "ssb-dbgen" / f"{table.rstrip('_')}.tbl"
                c.execute(f"COPY \"{table}\" FROM '{data}' (DELIMITER '|', HEADER false)")
        c.execute("CHECKPOINT")
    query_dir.mkdir(parents=True, exist_ok=True)
    if suite == "ssb":
        queries = [(p.stem, p.read_text()) for p in sorted((private / "ssb-tools/benchmark_queries").glob("*.sql"))]
    else:
        c.execute(f"LOAD {suite}")
        queries = [(str(n), sql) for n, sql in c.execute(f"SELECT * FROM {suite}_queries() ORDER BY query_nr").fetchall()]
    for n, sql in queries:
        (query_dir / f"{n}.sql").write_text(sql)
    return c, queries


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument("suite", choices=["ssb", "tpch", "tpcds"])
    p.add_argument("--private", type=Path, default=Path("/private"))
    p.add_argument("--output", type=Path, default=Path("/work/results"))
    p.add_argument("--prepare-only", action="store_true")
    p.add_argument("--load-only", action="store_true")
    p.add_argument("--reuse-loaded", action="store_true")
    p.add_argument("--query")
    p.add_argument("--duckdb-only", action="store_true")
    p.add_argument("--include-duckdb-adbc", action="store_true",
                   help="also run DuckDB SQL over the Redis ADBC attached catalog")
    p.add_argument("--phase", choices=["setup", "engine", "validate"], help=argparse.SUPPRESS)
    p.add_argument("--engine", choices=["duckdb", "redis", "duckdb_adbc"], help=argparse.SUPPRESS)
    p.add_argument("--artifacts", type=Path, help=argparse.SUPPRESS)
    args = p.parse_args()
    if args.duckdb_only and args.include_duckdb_adbc:
        p.error("--duckdb-only cannot include the Redis-backed third engine")
    if args.phase in ("engine", "validate") and not (args.query and args.engine and args.artifacts):
        p.error("engine/validation worker requires query, engine and private artifacts")
    if args.prepare_only and args.load_only:
        p.error("choose prepare-only or load-only")
    return args


def setup(args):
    # This process always exits before any timed worker starts.
    from isolation import cgroup_limit, client_threads, redis_options, worker_memory_limit
    threads = client_threads(args.private)
    worker_limit = worker_memory_limit(args.private)
    out = args.output / args.suite
    out.mkdir(parents=True, exist_ok=True)
    events = out / "events.jsonl"
    c, queries = prepare(args.suite, args.private, out / "queries")
    if not args.query:
        emit(events, {"event": "prepared", "suite": args.suite, "scale_factor": 1, "queries": len(queries)})
    if args.prepare_only:
        c.close()
        return
    metadata = {
        "suite": args.suite, "scale_factor": 1, "warmups": 1, "repeats": 3,
        "statement_timeout_seconds": 60, "platform": platform.platform(),
        "architecture": platform.machine(), "client_cpus": threads, "client_threads": threads,
        "client_memory_limit_bytes": cgroup_limit(Path('/sys/fs/cgroup/memory.max')),
        "worker_memory_limit_bytes": worker_limit,
        "worker_memory_isolation": ("per-child cgroup v2 leaf; coordinator and supervisor outside the worker limit"
                                    if worker_limit else "shared client cgroup: coordinator, supervisor and workers"),
        "duckdb_memory_limit": "8GB",
        "route": "Linux client in Redis Software server network namespace; TLS localhost",
        "table_mode": None if args.duckdb_only else "managed", "driver": importlib.metadata.version("adbc-driver-redis"),
        "duckdb": importlib.metadata.version("duckdb"), "pyarrow": importlib.metadata.version("pyarrow"),
        "adbc_driver_manager": importlib.metadata.version("adbc-driver-manager"),
        "server": None if args.duckdb_only else json.loads((args.private / "server.json").read_text()),
        "engine_isolation": "sequential fresh processes; separate disk-backed validation",
        "engines": ["duckdb"] if args.duckdb_only else (["duckdb", "redis", "duckdb_adbc"] if args.include_duckdb_adbc else ["duckdb", "redis"]),
        "numeric_tolerance": {"relative": 1e-7, "absolute": 1e-6},
        "ordering_validation": "row multiset (duplicates preserved); output ordering not validated",
        "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    }
    if not args.query:
        (out / "metadata.json").write_text(json.dumps(metadata, indent=2))
    r = None
    if not args.duckdb_only:
        from adbc_driver_redis import dbapi
        config = json.loads((args.private / "connection.json").read_text())
        r = dbapi.connect(config["uri"], db_kwargs={**redis_options(config),
                          "adbc.redis.statement_timeout": "1h"})
    tables = c.execute("SHOW TABLES").fetchall()
    try:
        for (table,) in ([] if args.query or args.duckdb_only else tables):
            start = time.perf_counter()
            count = None
            if not args.reuse_loaded:
                emit(events, {"event": "load_start", "table": table})
                reader = c.sql(f'SELECT * FROM "{table}"').to_arrow_reader(batch_size=4096)
                with r.cursor() as cur:
                    count = cur.adbc_ingest(table, reader, mode="create")
            expected = c.execute(f'SELECT COUNT(*) FROM "{table}"').fetchall()[0][0]
            with r.cursor() as cur:
                cur.execute(f'SELECT COUNT(*) FROM "{table}"')
                observed = cur.fetchone()[0]
            if observed != expected:
                raise ValueError(f"load cardinality mismatch for {table}: {observed} != {expected}")
            emit(events, {"event": "load_done", "table": table, "rows": expected,
                          "ingest_returned_rows": count, "seconds": time.perf_counter() - start})
    except Exception as exc:
        emit(events, {"event": "suite_error", "error": safe_error(str(exc))})
        raise
    finally:
        if r is not None:
            r.close()
        c.close()


def main():
    args = parse_args()
    if args.phase == "setup":
        return setup(args)
    if args.phase == "engine":
        from isolation import engine_worker
        return engine_worker(args)
    if args.phase == "validate":
        from isolation import validation_worker
        raise SystemExit(validation_worker(args))
    if not args.query:
        if (args.output / args.suite / "queries.jsonl").exists():
            raise ValueError("use a fresh output directory; do not overwrite historical results or metadata")
        from isolation import worker_leaf, worker_memory
        with worker_leaf(worker_memory(args.private), "setup") as leaf:
            subprocess.run(leaf.wrap([sys.executable, "-u", str(Path(__file__).resolve()),
                                      *sys.argv[1:], "--phase", "setup"]), check=True)
    if args.prepare_only or args.load_only:
        return
    from isolation import run_query
    folder = args.output / args.suite
    queries = sorted((folder / "queries").glob("*.sql"),
                     key=lambda p: [int(n) for n in p.stem.split('.')])
    if args.query and not (folder / "queries" / f"{args.query}.sql").is_file():
        raise FileNotFoundError("query SQL must be exported by preparation before isolated execution")
    selected = args.query
    for query in queries:
        if selected and query.stem != selected:
            continue
        args.query = query.stem
        run_query(args)
    if not selected:
        emit(folder / "events.jsonl", {"event": "complete", "suite": args.suite})


if __name__ == "__main__":
    main()
