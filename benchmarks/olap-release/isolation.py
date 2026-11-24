"""Fresh engine/validation processes and private, disk-backed reference exchange.

Pickle files are created only by our own child processes in a mode-0700 temporary
directory. Never point this module at untrusted result files.
"""

from contextlib import ExitStack
import hashlib
import heapq
import itertools
import json
import os
from pathlib import Path
import pickle
import statistics
import subprocess
import sys
import tempfile
import threading
import time


ENGINES = ("duckdb", "redis", "duckdb_adbc")


def atomic_json(path, value):
    """Publish a complete status document; a killed writer leaves the old one."""
    fd, name = tempfile.mkstemp(prefix=f'.{path.name}.', suffix='.tmp', dir=path.parent)
    temporary = Path(name)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as out:
            json.dump(value, out)
            out.flush()
            os.fsync(out.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def read_status(path):
    """Do not turn missing/corrupt crash evidence into a coordinator failure."""
    try:
        value = json.loads(path.read_text())
    except FileNotFoundError:
        return {}, 'missing'
    except (json.JSONDecodeError, UnicodeError):
        return {}, 'malformed'
    except OSError:
        return {}, 'unreadable'
    if not isinstance(value, dict):
        return {}, 'invalid_json_object'
    return value, None


def dump_rows(path, columns, rows):
    with path.open("wb") as out:
        pickle.dump((columns, len(rows)), out)
        for row in rows:
            pickle.dump(row, out)


def objects(stream):
    while True:
        try:
            yield pickle.load(stream)
        except EOFError:
            return


def merge_runs(paths, target):
    with ExitStack() as stack, target.open("wb") as out:
        streams = [stack.enter_context(p.open("rb")) for p in paths]
        for row in heapq.merge(*(objects(s) for s in streams)):
            pickle.dump(row, out)


def sorted_rows(path, scratch, chunk_size=4096, fan_in=32):
    # Sort normalized rows externally: bounded chunks and bounded open files.
    # This retains the old comparator's multiset/tolerance semantics, duplicates
    # included, without loading both complete reference/result sets in memory.
    from run import cell

    runs = []
    with path.open("rb") as source:
        columns, count = pickle.load(source)
        rows = objects(source)
        observed = 0
        while chunk := list(itertools.islice(rows, chunk_size)):
            if any(len(row) != columns for row in chunk):
                raise ValueError("result row width does not match header")
            observed += len(chunk)
            normalized = sorted(tuple(cell(v) for v in row) for row in chunk)
            target = scratch / f"sort-{len(runs)}"
            with target.open("wb") as out:
                for row in normalized:
                    pickle.dump(row, out)
            runs.append(target)
        if observed != count:
            raise ValueError("truncated result artifact")
    generation = 0
    while len(runs) > 1:
        merged = []
        for offset in range(0, len(runs), fan_in):
            group = runs[offset:offset + fan_in]
            target = scratch / f"merge-{generation}-{offset}"
            merge_runs(group, target)
            for old in group:
                old.unlink()
            merged.append(target)
        runs = merged
        generation += 1
    if runs:
        with runs[0].open("rb") as source:
            yield from objects(source)


def validate_files(reference, actual, scratch):
    from run import equal_cells

    with reference.open("rb") as left, actual.open("rb") as right:
        if pickle.load(left) != pickle.load(right):
            raise ValueError("result shape mismatch against DuckDB warm-up")
    with tempfile.TemporaryDirectory(dir=scratch) as a, tempfile.TemporaryDirectory(dir=scratch) as b:
        left = sorted_rows(reference, Path(a))
        right = sorted_rows(actual, Path(b))
        try:
            sentinel = object()
            for x, y in itertools.zip_longest(left, right, fillvalue=sentinel):
                if x is sentinel or y is sentinel or not equal_cells(x, y):
                    raise ValueError("result mismatch against DuckDB warm-up")
        finally:
            left.close()
            right.close()


def literal(value):
    return "'" + str(value).replace("'", "''") + "'"


def identifier(value):
    return '"' + str(value).replace('"', '""') + '"'


def redis_options(config):
    return {**config.get("db_kwargs", {}),
            "adbc.redis.statement_timeout": "60s",
            "adbc.redis.telemetry.enabled": "false"}


def connect_engine(args):
    """Import/open only the selected engine; no local source DB in ADBC mode."""
    if args.engine == "redis":
        from adbc_driver_redis import dbapi
        config = json.loads((args.private / "connection.json").read_text())
        conn = dbapi.connect(config["uri"], db_kwargs=redis_options(config))
        with conn.cursor() as cur:
            cur.execute("SET statement_timeout = '60s'")
        return conn, {}

    import duckdb

    if args.engine == "duckdb":
        conn = duckdb.connect(str(args.private / f"{args.suite}-sf1.duckdb"), read_only=True)
        conn.execute("SET threads=4; SET memory_limit='8GB'")
        return conn, {}

    # Use the exact same installed native Redis library as the direct route.
    from adbc_driver_redis import _driver_path
    os.environ["QUERY_FARM_TELEMETRY_OPT_OUT"] = "1"
    config = json.loads((args.private / "connection.json").read_text())
    scanner = os.environ.get("ADBC_SCANNER_EXTENSION")
    conn = duckdb.connect(config={"allow_unsigned_extensions": bool(scanner)})
    try:
        conn.execute("SET threads=4; SET memory_limit='8GB'")
        if scanner:
            conn.execute(f"LOAD {literal(scanner)}")
        else:
            conn.execute("SET allow_extension_repositories = 'allowed'")
            conn.execute("CREATE EXTENSION REPOSITORY IF NOT EXISTS query_farm "
                         "WITH PREFIX 'https://duckdb-extensions.query.farm'")
            conn.execute("INSTALL adbc_scanner FROM query_farm")
            conn.execute("LOAD adbc_scanner FROM query_farm")
        options = redis_options(config)
        extra = ", ".join(f"{identifier(k)} {literal(v)}" for k, v in options.items())
        library = Path(_driver_path())
        conn.execute(f"ATTACH {literal(config['uri'])} AS r "
                     f"(TYPE adbc, driver {literal(library)}, READ_ONLY, query_pushdown false, {extra})")
        schema = options.get("adbc.redis.default_schema", "public")
        conn.execute(f"USE r.{identifier(schema)}")
        # Public provenance excludes library paths, attach SQL and credentials.
        extension = conn.execute("SELECT extension_version FROM duckdb_extensions() "
                                 "WHERE extension_name='adbc_scanner' AND loaded").fetchone()
        return conn, {"execution": "DuckDB planner over Redis ADBC attached catalog",
                      "query_pushdown": False, "extension": "adbc_scanner",
                      "extension_version": extension[0] if extension else None,
                      "redis_library_sha256": hashlib.sha256(library.read_bytes()).hexdigest()}
    except BaseException:
        conn.close()
        raise


def engine_worker(args):
    from run import safe_error

    status = {f"{args.engine}_seconds": [], "phase": "setup", "pid": os.getpid()}
    path = args.artifacts / f"{args.engine}.json"
    conn = None
    deadline_fired = None
    try:
        conn, provenance = connect_engine(args)
        status["provenance"] = provenance
        sql = (args.output / args.suite / "queries" / f"{args.query}.sql").read_text()
        for repeat in range(4):
            status["phase"] = "execute"
            status["repeat"] = repeat
            atomic_json(path, status)
            timer = None
            deadline_fired = threading.Event()
            if args.engine != "redis":
                def interrupt_statement():
                    deadline_fired.set()
                    conn.interrupt()
                timer = threading.Timer(60, interrupt_statement)
                timer.start()
            try:
                start = time.perf_counter()
                if args.engine == "redis":
                    with conn.cursor() as cur:
                        cur.execute(sql)
                        columns = len(cur.description)
                        data = cur.fetchall()
                else:
                    data = conn.execute(sql).fetchall()
                    columns = len(conn.description)
                seconds = time.perf_counter() - start
            finally:
                if timer:
                    timer.cancel()
                    timer.join()
            # The timer may fire as fetchall returns. Do not admit that sample
            # as successful just because the engine returned without raising.
            if deadline_fired.is_set():
                raise TimeoutError("60-second statement deadline exceeded: DuckDB interrupt timer fired")
            if repeat:
                status[f"{args.engine}_seconds"].append(seconds)
            else:
                status[f"{args.engine}_warmup_seconds"] = seconds
                status["columns"], status["rows"] = columns, len(data)
            status["phase"] = "export"
            atomic_json(path, status)
            dump_rows(args.artifacts / f"{args.engine}-{repeat}.pickle", columns, data)
            del data
        status[f"{args.engine}_median_seconds"] = statistics.median(status[f"{args.engine}_seconds"])
        status[f"{args.engine}_status"] = "executed"
    except Exception as exc:
        if deadline_fired is not None and deadline_fired.is_set():
            status[f"{args.engine}_status"] = "timeout"
            status[f"{args.engine}_timeout_cause"] = "duckdb_interrupt_timer"
            status[f"{args.engine}_timeout_seconds"] = 60
            status[f"{args.engine}_error"] = safe_error(f"60-second statement deadline exceeded: {exc}")
        else:
            status[f"{args.engine}_status"] = "error"
            status[f"{args.engine}_error"] = safe_error(str(exc))
    finally:
        try:
            if conn is not None:
                conn.close()
        finally:
            atomic_json(path, status)


def validation_worker(args):
    from run import safe_error

    try:
        for repeat in range(4):
            validate_files(args.artifacts / "duckdb-0.pickle",
                           args.artifacts / f"{args.engine}-{repeat}.pickle", args.artifacts)
    except Exception as exc:
        message = safe_error(str(exc))
        mismatch = "result mismatch" in message or "result shape mismatch" in message
        atomic_json(args.artifacts / f"{args.engine}-validation.json", {
            "status": "mismatch" if mismatch else "validation_failed", "error": message})
        return 1
    return 0


def oom_kills():
    try:
        values = dict(line.split() for line in Path('/sys/fs/cgroup/memory.events').read_text().splitlines())
        return int(values['oom_kill'])
    except FileNotFoundError:
        return None


def run_query(args, runner=None):
    """The parent imports neither engine; waiting for exit releases native caches."""
    from run import emit, safe_error

    runner = runner or subprocess.run
    folder = args.output / args.suite
    existing = folder / "queries.jsonl"
    if existing.exists():
        for line in existing.read_text().splitlines():
            previous = json.loads(line)
            if "engine_isolation" not in previous:
                raise ValueError("use a new output directory; historical shared-worker results must remain separate")
            if previous["query"] == args.query:
                raise ValueError("query already recorded; use a new output directory for a rerun")
    checkpoint = folder / f"checkpoint-{args.query}.json"
    record = {"suite": args.suite, "query": args.query,
              "engine_isolation": "sequential fresh processes; separate disk-backed validation"}
    engines = ["duckdb"] if args.duckdb_only else ["duckdb", "redis"]
    if args.include_duckdb_adbc:
        engines.append("duckdb_adbc")
    with tempfile.TemporaryDirectory(prefix="query-", dir=args.private) as tmp:
        artifacts = Path(tmp)
        base = [sys.executable, "-u", str(Path(__file__).with_name("run.py")), args.suite,
                "--private", str(args.private), "--output", str(args.output),
                "--query", args.query, "--artifacts", str(artifacts)]
        for engine in engines:
            record.update(active_engine=engine, active_phase="engine")
            atomic_json(checkpoint, record)
            before, started = oom_kills(), time.perf_counter()
            failure = None
            returncode = None
            try:
                child = runner(base + ["--phase", "engine", "--engine", engine], timeout=270,
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                returncode = child.returncode
                if child.returncode:
                    failure = f"{engine} worker exited with code {child.returncode}"
            except subprocess.TimeoutExpired:
                failure = f"{engine} worker exceeded 270-second process limit"
            path = artifacts / f"{engine}.json"
            status, status_problem = read_status(path)
            for key, value in status.items():
                if key.startswith(engine + "_"):
                    record[key] = value
            record[f"{engine}_worker"] = {k: status[k] for k in ["pid", "phase", "repeat", "provenance"] if k in status}
            record[f"{engine}_worker"].update(engine=engine, returncode=returncode,
                                              status_artifact=status_problem or "ok")
            # If the last status cannot be read, preserve the known coordinator
            # phase without inventing a finer setup/execute/export stage.
            record[f"{engine}_worker"].setdefault("phase", record["active_phase"])
            record[f"{engine}_worker"]["wall_seconds"] = time.perf_counter() - started
            after = oom_kills()
            if before is not None and after is not None:
                record[f"{engine}_worker"]["oom_kills_delta"] = after - before
            if failure:
                record[f"{engine}_status"] = "worker_failed"
                record[f"{engine}_error"] = failure
            elif status_problem:
                record[f"{engine}_status"] = "worker_failed"
                record[f"{engine}_error"] = f"{engine} worker returned with {status_problem} status artifact"
            elif record.get(f"{engine}_status") == "executed":
                record["active_phase"] = "validation"
                atomic_json(checkpoint, record)
                try:
                    check = runner(base + ["--phase", "validate", "--engine", engine], timeout=120,
                                   capture_output=True, text=True)
                    if check.returncode:
                        detail_path = artifacts / f"{engine}-validation.json"
                        detail, detail_problem = read_status(detail_path)
                        if (detail_problem or detail.get("status") not in ("mismatch", "validation_failed")
                                or not isinstance(detail.get("error"), str)):
                            detail = {"status": "validation_failed",
                                      "error": f"validator exited with code {check.returncode}; {detail_problem or 'invalid'} status artifact"}
                        record[f"{engine}_status"] = detail["status"]
                        record[f"{engine}_error"] = safe_error(detail["error"])
                    else:
                        record[f"{engine}_status"] = "ok"
                except subprocess.TimeoutExpired:
                    record[f"{engine}_status"] = "validation_failed"
                    record[f"{engine}_error"] = "validator exceeded 120-second process limit"
            elif f"{engine}_status" not in record:
                record[f"{engine}_status"] = "worker_failed"
                record[f"{engine}_error"] = "worker returned without a status artifact"
            if engine == "duckdb":
                record.update({k: status[k] for k in ["columns", "rows"] if k in status})
            if record.get(f"{engine}_status") == "ok" and engine != "duckdb":
                record[f"{engine}_over_duckdb"] = record[f"{engine}_median_seconds"] / record["duckdb_median_seconds"]
            atomic_json(checkpoint, record)
            # Keep only the reference warm-up after validation. Redis never
            # imports/loads it; the validator runs after the engine has exited.
            for artifact in artifacts.glob(f"{engine}-*.pickle"):
                if artifact.name != "duckdb-0.pickle":
                    artifact.unlink()
            if engine == "duckdb" and record.get("duckdb_status") != "ok":
                for skipped in engines[1:]:
                    record[f"{skipped}_status"] = "not_run_no_reference"
                break
    record.pop("active_engine", None)
    record.pop("active_phase", None)
    emit(folder / "queries.jsonl", record)
    checkpoint.unlink(missing_ok=True)
    return record
