"""Run the official ClickBench queries through ADBC against Redis Cloud.

Dependencies: pyarrow, redis, adbc-driver-manager. Requires a separate
ClickBench checkout and the official hits_compatible/hits.parquet file.
"""
import argparse
import datetime
from contextlib import closing, contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from urllib.parse import urlsplit
import uuid

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import pyarrow.compute as pc
import pyarrow.parquet as pq
import redis

from loading import ingest_batches

FULL_ROWS = 99_997_497
HERE = Path(__file__).resolve().parent


def commit(path):
    marker = Path(path) / "SOURCE_COMMIT"
    if marker.exists():
        return marker.read_text().strip()
    return subprocess.check_output(
        ["git", "-C", str(path), "rev-parse", "HEAD"], text=True
    ).strip()


def arrow_schema(ddl):
    types = {"BIGINT": pa.int64(), "INTEGER": pa.int32(),
             "SMALLINT": pa.int16(), "TEXT": pa.string(),
             "VARCHAR": pa.string(), "CHAR": pa.string(),
             "TIMESTAMP": pa.timestamp("us"), "DATE": pa.date32()}
    fields = []
    for line in ddl.splitlines():
        match = re.fullmatch(
            r"\s*(\w+)\s+(\w+)(?:\(\d+\))?(\s+NOT NULL)?[,]?\s*", line
        )
        if match and match[2].upper() in types:
            fields.append(pa.field(match[1], types[match[2].upper()],
                                   nullable=not bool(match[3])))
    if len(fields) != 105:
        raise ValueError(f"Expected 105 ClickBench columns, found {len(fields)}")
    return pa.schema(fields)


def convert_batch(batch, schema):
    """Decode the official Parquet's epoch-day dates and epoch-second timestamps."""
    if set(batch.schema.names) != set(schema.names):
        raise ValueError("Parquet columns differ from the official ClickBench schema")
    columns = []
    for field in schema:
        values = batch.column(batch.schema.get_field_index(field.name))
        if pa.types.is_date(field.type) and pa.types.is_integer(values.type):
            values = pc.cast(pc.cast(values, pa.int32()), pa.date32())
        elif pa.types.is_timestamp(field.type) and pa.types.is_integer(values.type):
            values = pc.cast(pc.multiply_checked(pc.cast(values, pa.int64()),
                                                1_000_000), field.type)
        values = pc.cast(values, field.type, safe=True)
        if not field.nullable and values.null_count:
            raise ValueError(f"NULL values in required column {field.name}")
        columns.append(values)
    return pa.RecordBatch.from_arrays(columns, schema=schema)


def source_batches(parquet, schema, batch_size, rows, retained=0):
    """Read exactly the requested source prefix, skipping verified retained rows."""
    offset = 0
    for batch in parquet.iter_batches(batch_size=batch_size):
        start = max(0, retained - offset)
        end = min(batch.num_rows, rows - offset)
        offset += batch.num_rows
        if start < end:
            yield convert_batch(batch.slice(start, end - start), schema)
        if offset >= rows:
            break


def save(path, result):
    temp = path.with_suffix(".tmp")
    temp.write_text(json.dumps(result, indent=2) + "\n")
    temp.replace(path)


def validate_placement(placement):
    """Require verified Cloud placement for both the database and runner."""
    for key in ("provider", "region", "az"):
        database = placement["database"].get(key)
        runner = placement["runner"].get(key)
        if not database or database != runner:
            raise ValueError(f"Database and runner must share a verified {key}")
    for surface in ("database", "runner"):
        if not placement[surface].get("evidence"):
            raise ValueError(f"Missing placement evidence for {surface}")
    return placement


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--upstream", type=Path, required=True)
    ap.add_argument("--parquet", type=Path, required=True)
    ap.add_argument("--rows", type=int, default=100_000,
                    help="Pilot row limit; use 99997497 for the full benchmark")
    ap.add_argument("--batch-size", type=int, default=10_000)
    ap.add_argument("--load-workers", type=int, default=1,
                    help="Independent ADBC append connections (default 1); parallel workers prefetch batches")
    ap.add_argument("--load-prefetch", action="store_true",
                    help="Also overlap preparation and writes with a single worker")
    ap.add_argument("--machine", required=True,
                    help="Cloud provider/region, RAM, shards, QPF, client location")
    ap.add_argument("--placement", type=Path, required=True,
                    help="Verified database and runner provider/region/AZ JSON")
    ap.add_argument("--driver", type=Path, default=HERE.parents[1] / "go/build" /
                    ("libadbc_driver_redis.dylib" if sys.platform == "darwin"
                     else "libadbc_driver_redis.so"))
    ap.add_argument("--output", type=Path, default=HERE / "results.json")
    ap.add_argument("--resume-result", type=Path, help="Resume a interrupted load after prefix validation")
    ap.add_argument("--cleanup", action="store_true",
                    help="Drop only this run's generated table and schema on completion")
    ap.add_argument("--query-workers", type=int, default=None, choices=range(1, 33),
                    help="Override automatic driver workers for eligible individual aggregates")
    ap.add_argument("--query-timeout", type=float, default=1200)
    ap.add_argument("--deadline", type=float, default=float("inf"),
                    help="UTC epoch deadline for stopping within the deployment budget")
    args = ap.parse_args()
    prefetch = args.load_workers > 1 or args.load_prefetch
    placement = validate_placement(json.loads(args.placement.read_text()))
    uri = os.environ.get("REDIS_CLOUD_URI")
    if not uri:
        ap.error("Set REDIS_CLOUD_URI to the authenticated Redis Cloud database URI")
    host = urlsplit(uri).hostname or ""
    if not host.endswith((".redis-cloud.com", ".redislabs.com", ".db.redis.io", ".cloud.rlrcp.com")):
        ap.error("REDIS_CLOUD_URI must use a Redis Cloud endpoint")
    if not 0 < args.rows <= FULL_ROWS or args.batch_size <= 0 or args.load_workers <= 0:
        ap.error("Invalid row limit or batch size")
    if args.output.exists():
        ap.error("Output already exists; choose a new path to preserve previous results")
    ddl = (args.upstream / "postgresql/create.sql").read_text()
    queries = (args.upstream / "postgresql/queries.sql").read_text().splitlines()
    if len(queries) != 43 or any(not q.endswith(";") for q in queries):
        ap.error("Expected the 43 official, single-line PostgreSQL queries")
    schema = arrow_schema(ddl)
    parquet = pq.ParquetFile(args.parquet)
    if parquet.metadata.num_rows < args.rows:
        ap.error("Parquet file has fewer rows than requested")
    with args.driver.open("rb") as driver_file:
        driver_sha256 = hashlib.file_digest(driver_file, "sha256").hexdigest()
    previous = json.loads(args.resume_result.read_text()) if args.resume_result else None
    if previous:
        if args.load_workers != 1 or previous["metadata"].get("load_workers", 1) != 1:
            ap.error("Parallel loads cannot resume: row IDs do not preserve source order")
        if previous["trials"] or previous["metadata"]["requested_rows"] != args.rows:
            raise ValueError("Resume requires an interrupted load at the same scale")
        if previous["metadata"].get("driver_binary_sha256", driver_sha256) != driver_sha256:
            raise ValueError("Resume driver binary differs")
        if previous["metadata"]["driver_commit"] != commit(HERE.parents[1]):
            raise ValueError("Resume driver commit differs")
    namespace = previous["metadata"]["namespace"] if previous else "clickbench_" + uuid.uuid4().hex[:12]
    if not re.fullmatch(r"clickbench_[a-f0-9]{12}", namespace):
        raise ValueError("Invalid benchmark namespace")
    result = {
        "system": "Redis Cloud + Redis ADBC", "machine": args.machine,
        "date": datetime.date.today().isoformat(), "tags": ["managed", "no-cold"],
        "load_time": None, "data_size": None, "result": [[None] * 3 for _ in queries],
        "metadata": {"upstream_commit": commit(args.upstream),
                     "driver_commit": commit(HERE.parents[1]),
                     "driver_binary_sha256": driver_sha256,
                     "requested_rows": args.rows, "loaded_rows": 0,
                     "batch_size": args.batch_size, "load_workers": args.load_workers,
                     "load_prefetch_batches": 2 * args.load_workers if prefetch else 0,
                     "resume_safe_source_order": args.load_workers == 1,
                     "source_rows": parquet.metadata.num_rows,
                     "official_scale": args.rows == FULL_ROWS,
                     "namespace": namespace, "columns": 105,
                     "placement": placement,
                     "query_timeout_seconds": args.query_timeout,
                     "query_workers": args.query_workers,
                     "connection_protocol": "RESP3 (driver default); fresh connection per trial",
                     "aggregate_pushdown": "exact", "timing": "execute + Arrow fetch",
                     "correctness": "row count checked; query fingerprints need reference validation",
                     "queries_sha256": hashlib.sha256("\n".join(queries).encode()).hexdigest()},
        "queries": queries, "trials": [],
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    # No connection URI or raw server errors are written to the output.
    client = redis.Redis.from_url(uri, socket_connect_timeout=10, socket_timeout=30, protocol=2)
    client.ping()
    client.execute_command("FT._LIST")
    info = client.info("server")
    result["metadata"]["redis_version"] = info.get("redis_version")
    result["metadata"]["modules"] = str(client.execute_command("MODULE", "LIST"))
    save(args.output, result)
    with dbapi.connect(driver=str(args.driver.resolve()), autocommit=True,
                       db_kwargs={"uri": uri, "adbc.redis.aggregate_pushdown": "exact"}) as conn:
        with conn.cursor() as cur:
            # Unique schema; no existing tables, indexes, or Cloud settings are replaced.
            if not previous:
                cur.execute(f"CREATE SCHEMA {namespace}")
            cur.execute(f"SET search_path TO {namespace}")
            try:
                started = time.perf_counter()
                if not previous:
                    cur.execute(ddl)
                loaded = 0
                if previous:
                    # Exact __rowid bounds are safe here (<100M, far below 2^53).
                    reply = client.execute_command("FT.AGGREGATE", f"idx:{namespace}:hits", "*",
                        "GROUPBY", 0, "REDUCE", "COUNT", 0, "AS", "n",
                        "REDUCE", "MIN", 1, "@__rowid", "AS", "lo",
                        "REDUCE", "MAX", 1, "@__rowid", "AS", "hi", "DIALECT", 2)
                    if isinstance(reply, dict):
                        stats = reply["results"][0]["extra_attributes"] if "results" in reply else reply[b"results"][0][b"extra_attributes"]
                    else:
                        stats = dict(zip(reply[1][::2], reply[1][1::2]))
                    def stat(name):
                        return int(float(stats.get(name, stats.get(name.encode()))))
                    loaded = stat("n")
                    if not 0 < loaded <= args.rows or stat("lo") != 1 or stat("hi") != loaded:
                        raise ValueError("Retained rows are not a contiguous source prefix")
                    # Match full typed rows at the prefix start, middle and end.
                    for row_id in sorted({1, loaded // 2 or 1, loaded - 1 or 1, loaded}):
                        offset = row_id - 1
                        for group in range(parquet.num_row_groups):
                            size = parquet.metadata.row_group(group).num_rows
                            if offset < size:
                                sample = parquet.read_row_group(group).slice(offset, 1).to_batches()[0]
                                expected = pa.Table.from_batches([convert_batch(sample, schema)]).to_pylist()
                                break
                            offset -= size
                        cur.execute(f"SELECT * FROM hits WHERE __rowid = {row_id}")
                        if cur.fetch_arrow_table().to_pylist() != expected:
                            raise ValueError(f"Retained source prefix sample differs at row {row_id}")
                    result["metadata"].update(resumed_from_rows=loaded, interrupted_load=True,
                        resume_validation="contiguous row IDs plus full typed source samples at start/middle/end")
                    save(args.output, result)
                    print(f"Verified retained prefix of {loaded:,} rows; resuming", flush=True)
                retained = loaded

                def batches():
                    for batch in source_batches(parquet, schema, args.batch_size,
                                                args.rows, retained):
                        if time.time() >= args.deadline:
                            result["metadata"]["stopped_for_budget"] = True
                            save(args.output, result)
                            raise RuntimeError("Budget deadline reached during ingestion")
                        yield batch

                @contextmanager
                def writer():
                    if not prefetch:
                        yield lambda batch: cur.adbc_ingest("hits", batch, mode="append", db_schema_name=namespace)
                        return
                    with dbapi.connect(driver=str(args.driver.resolve()), autocommit=True,
                                       db_kwargs={"uri": uri, "adbc.redis.aggregate_pushdown": "exact"}) as worker_conn:
                        with worker_conn.cursor() as worker_cur:
                            def append(batch):
                                if time.time() >= args.deadline:
                                    raise RuntimeError("Budget deadline reached during ingestion")
                                worker_cur.adbc_ingest("hits", batch, mode="append", db_schema_name=namespace)
                            yield append

                last_checkpoint = loaded // 1_000_000
                with closing(ingest_batches(batches(), writer, args.load_workers, prefetch=prefetch)) as counts:
                    for count in counts:
                        loaded += count
                        if loaded // 1_000_000 > last_checkpoint or loaded == args.rows:
                            last_checkpoint = loaded // 1_000_000
                            result["metadata"]["loaded_rows"] = loaded
                            save(args.output, result)
                            print(f"Ingested {loaded:,}/{args.rows:,} rows", flush=True)
                        if time.time() >= args.deadline:
                            result["metadata"]["stopped_for_budget"] = True
                            save(args.output, result)
                            raise RuntimeError("Budget deadline reached during ingestion")
                # Cloud indexing can lag the completed writes. Include readiness
                # in load time rather than benchmarking incomplete indexes.
                deadline = time.monotonic() + 600
                while True:
                    reply = client.execute_command("FT.INFO", f"idx:{namespace}:hits")
                    index = reply if isinstance(reply, dict) else dict(zip(reply[::2], reply[1::2]))
                    num_docs = index.get(b"num_docs", index.get("num_docs"))
                    indexing = index.get(b"indexing", index.get("indexing"))
                    if int(num_docs) == loaded and int(indexing) == 0:
                        break
                    if time.monotonic() > deadline:
                        raise RuntimeError("ClickBench index not ready after 600 seconds")
                    time.sleep(1)
                cur.execute("SELECT COUNT(*) FROM hits")
                actual = cur.fetchone()[0]
                if actual != loaded:
                    raise RuntimeError(f"Ingest count mismatch: expected {loaded}, got {actual}")
                elapsed_load = time.perf_counter() - started
                result["load_time"] = None if previous else elapsed_load
                if previous:
                    result["metadata"]["resumed_remaining_load_seconds"] = elapsed_load
                result["metadata"]["loaded_rows"] = loaded
                save(args.output, result)
                print(f"Loaded {loaded:,} rows in {elapsed_load:.2f}s in this session; schema {namespace}", flush=True)
                for number, sql in enumerate(queries, 1):
                    for trial in range(3):
                        record = {"query": number, "trial": trial + 1}
                        raw = args.output.parent / (args.output.stem + "-raw")
                        raw.mkdir(exist_ok=True)
                        job_path = raw / f"q{number:02}-{trial + 1}-job.json"
                        output_path = raw / f"q{number:02}-{trial + 1}.json"
                        remaining = args.deadline - time.time()
                        if remaining <= 0:
                            record.update(seconds=None, error_type="BudgetDeadline")
                            result["trials"].append(record)
                            result["metadata"]["stopped_for_budget"] = True
                            save(args.output, result)
                            continue
                        job_path.write_text(json.dumps({"driver": str(args.driver.resolve()),
                            "namespace": namespace, "sql": sql, "query_workers": args.query_workers, "output": str(output_path.resolve())}))
                        try:
                            process = subprocess.run([sys.executable, str(HERE / "trial.py"), str(job_path)],
                                timeout=min(args.query_timeout, remaining), capture_output=True)
                            if process.returncode != 0 or not output_path.exists():
                                record.update(seconds=None, error_type="WorkerExit", exit_code=process.returncode)
                            else:
                                measured = json.loads(output_path.read_text())
                                record.update({k:v for k,v in measured.items() if k != "values"})
                                result["result"][number - 1][trial] = measured["seconds"]
                        except subprocess.TimeoutExpired:
                            record.update(seconds=None, error_type="QueryDeadline")
                        except Exception as error:
                            # Exception type/status aid diagnosis without leaking credentials.
                            record.update(seconds=None, error_type=type(error).__name__,
                                          status=str(getattr(error, "status_code", "unknown")))
                        result["trials"].append(record)
                        save(args.output, result)
                    print(f"Q{number:02}: {result['result'][number - 1]}", flush=True)
            finally:
                if args.cleanup:
                    cur.execute("DROP TABLE IF EXISTS hits")
                    cur.execute("SET search_path TO public")
                    cur.execute(f"DROP SCHEMA {namespace}")
                    result["metadata"]["cleaned_up"] = True
                    save(args.output, result)
    client.close()


if __name__ == "__main__":
    main()
