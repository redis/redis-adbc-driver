"""Fresh engine/validation processes and private, disk-backed reference exchange.

Pickle files are created only by our own child processes in a mode-0700 temporary
directory. Never point this module at untrusted result files.
"""

import hashlib
import heapq
import itertools
import json
import os
from pathlib import Path
import pickle
import shutil
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


class ArtifactError(ValueError):
    """A private result file did not read back as its writer recorded it.

    This is a failure of the file exchange between processes, never evidence
    about the query result itself, so it is reported as validation_failed and
    never as a mismatch.
    """

    def __init__(self, message, detail):
        super().__init__(message)
        self.detail = detail


class _DigestWriter:
    """Count and hash the exact bytes handed to an artifact file."""

    def __init__(self, out):
        self.out, self.size, self.sha256 = out, 0, hashlib.sha256()

    def write(self, data):
        view = memoryview(data)
        self.sha256.update(view)
        self.size += view.nbytes
        return self.out.write(view)


class _DigestReader:
    """Hash exactly the bytes the unpickler consumes (no read-ahead), so the
    stream position is the start of the next record."""

    def __init__(self, raw):
        self.raw, self.size, self.sha256 = raw, 0, hashlib.sha256()

    def _seen(self, data):
        self.sha256.update(data)
        self.size += memoryview(data).nbytes
        return data

    def read(self, n=-1):
        return self._seen(self.raw.read(n))

    def readline(self, n=-1):
        return self._seen(self.raw.readline(n))

    def readinto(self, buffer):
        count = self.raw.readinto(buffer)
        self._seen(memoryview(buffer).cast("B")[:count])
        return count


def dump_objects(path, values, sync=True):
    """Write one pickle per value and return the size/SHA-256 a reader must see."""
    with path.open("wb") as raw:
        out = _DigestWriter(raw)
        for value in values:
            pickle.dump(value, out)
        if sync:
            # The validator is another process; publish the data, not only the size.
            raw.flush()
            os.fsync(raw.fileno())
    return {"bytes": out.size, "sha256": out.sha256.hexdigest()}


def dump_rows(path, columns, rows):
    return dump_objects(path, itertools.chain([(columns, len(rows))], rows))


def _inspect_artifact(path, offset=None):
    """Size, digest, first all-zero 4 KiB page and the zero run at offset."""
    digest, size, zero_page, zeros = hashlib.sha256(), 0, None, None
    with path.open("rb") as source:
        while page := source.read(4096):
            digest.update(page)
            if zero_page is None and page.count(0) == len(page):
                zero_page = size
            size += len(page)
        if offset is not None:
            source.seek(offset)
            zeros = 0
            while chunk := source.read(1 << 16):
                rest = chunk.lstrip(b"\x00")
                zeros += len(chunk) - len(rest)
                if rest:
                    break
    return size, digest.hexdigest(), zero_page, zeros


def _artifact_error(path, role, expected, objects, offset=None, cause=None):
    size, digest, zero_page, zeros = _inspect_artifact(path, offset)
    detail = {"role": role, "file": path.name, "objects_decoded": objects,
              "bytes_read_back": size, "sha256_read_back": digest[:16]}
    parts = []
    if offset is not None:
        detail["decode_offset"] = offset
        detail["decode_error"] = f"{type(cause).__name__}: {cause}"
        parts.append(f"decode failed at byte {offset} after {objects} objects ({detail['decode_error']})")
        detail["zero_bytes_at_offset"] = zeros
        if zeros:
            parts.append(f"{zeros} zero bytes start at byte {offset}")
    else:
        parts.append(f"bytes differ from the writer's record after {objects} objects")
    if zero_page is not None:
        detail["first_zero_page_offset"] = zero_page
        parts.append(f"first all-zero 4 KiB page at byte {zero_page}")
    if expected is not None:
        detail.update(bytes_recorded=expected["bytes"], sha256_recorded=expected["sha256"][:16])
        parts.append(f"recorded {expected['bytes']} bytes sha256 {expected['sha256'][:16]}")
    parts.append(f"read back {size} bytes sha256 {digest[:16]}")
    return ArtifactError(f"{role} {path.name} did not read back as written: " + "; ".join(parts), detail)


def verified_objects(path, expected, role):
    """Yield each pickle in path, then prove the bytes match the writer's record.

    expected=None only skips the final digest comparison (unit fixtures); decode
    failures still report their location."""
    count = 0
    with path.open("rb") as raw:
        stream = _DigestReader(raw)
        while True:
            offset = stream.size
            try:
                value = pickle.load(stream)
            except EOFError as cause:
                if stream.size == offset:
                    break
                raise _artifact_error(path, role, expected, count, offset, cause) from cause
            except MemoryError:
                raise
            except Exception as cause:
                raise _artifact_error(path, role, expected, count, offset, cause) from cause
            count += 1
            yield value
    if expected is not None and (stream.size, stream.sha256.hexdigest()) != (expected["bytes"], expected["sha256"]):
        raise _artifact_error(path, role, expected, count)


def merge_runs(runs, target):
    """Merge sorted (path, digest) runs into target; return target's digest."""
    return dump_objects(target, heapq.merge(*(verified_objects(p, d, "validator scratch") for p, d in runs)),
                        sync=False)


def sorted_rows(path, scratch, chunk_size=4096, fan_in=32, expected=None, role="result"):
    # Sort normalized rows externally: bounded chunks and bounded open files.
    # This retains the old comparator's multiset/tolerance semantics, duplicates
    # included, without loading both complete reference/result sets in memory.
    # Yields the (columns, count) header first, after the whole source has been
    # read and verified, then the sorted rows.
    from run import cell

    runs = []
    source = verified_objects(path, expected, role)
    header = next(source, None)
    if not (isinstance(header, tuple) and len(header) == 2):
        raise ValueError("result artifact has no header")
    columns, count = header
    observed = 0
    while chunk := list(itertools.islice(source, chunk_size)):
        if any(len(row) != columns for row in chunk):
            raise ValueError("result row width does not match header")
        observed += len(chunk)
        normalized = sorted(tuple(cell(v) for v in row) for row in chunk)
        target = scratch / f"sort-{len(runs)}"
        runs.append((target, dump_objects(target, normalized, sync=False)))
    if observed != count:
        raise ValueError("truncated result artifact")
    generation = 0
    while len(runs) > 1:
        merged = []
        for offset in range(0, len(runs), fan_in):
            group = runs[offset:offset + fan_in]
            target = scratch / f"merge-{generation}-{offset}"
            merged.append((target, merge_runs(group, target)))
            for old, _ in group:
                old.unlink()
        runs = merged
        generation += 1
    yield header
    if runs:
        yield from verified_objects(*runs[0], "validator scratch")


def validate_files(reference, actual, scratch, digests=(None, None)):
    from run import equal_cells

    with tempfile.TemporaryDirectory(dir=scratch) as a, tempfile.TemporaryDirectory(dir=scratch) as b:
        left = sorted_rows(reference, Path(a), expected=digests[0], role="reference")
        right = sorted_rows(actual, Path(b), expected=digests[1], role="result")
        try:
            if next(left) != next(right):
                raise ValueError("result shape mismatch against DuckDB warm-up")
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


def client_threads(private):
    profile = Path(private) / "connection.json"
    config = json.loads(profile.read_text()) if profile.exists() else {}
    threads = config.get("client_threads", 4)
    if type(threads) is not int or not 1 <= threads <= 32:
        raise ValueError("client_threads must be an integer from 1 to 32")
    return threads


def duckdb_adbc_scan_threads(private):
    profile = Path(private) / "connection.json"
    config = json.loads(profile.read_text()) if profile.exists() else {}
    threads = config.get("duckdb_adbc_scan_threads", 1)
    if type(threads) is not int or not 1 <= threads <= 1024:
        raise ValueError("duckdb_adbc_scan_threads must be an integer from 1 to 1024")
    return threads


def duckdb_adbc_global_symbols(private):
    profile = Path(private) / "connection.json"
    config = json.loads(profile.read_text()) if profile.exists() else {}
    enabled = config.get("duckdb_adbc_global_symbols", False)
    if type(enabled) is not bool:
        raise ValueError("duckdb_adbc_global_symbols must be a boolean")
    return enabled


def connect_engine(args):
    """Import/open only the selected engine; no local source DB in ADBC mode."""
    if args.engine == "redis":
        from adbc_driver_redis import dbapi
        config = json.loads((args.private / "connection.json").read_text())
        conn = dbapi.connect(config["uri"], db_kwargs=redis_options(config))
        with conn.cursor() as cur:
            cur.execute("SET statement_timeout = '60s'")
        return conn, {}

    threads = client_threads(args.private)
    scan_threads = duckdb_adbc_scan_threads(args.private) if args.engine == "duckdb_adbc" else 1
    global_symbols = duckdb_adbc_global_symbols(args.private) if args.engine == "duckdb_adbc" else False
    scanner = os.environ.get("ADBC_SCANNER_EXTENSION") if args.engine == "duckdb_adbc" else None
    if global_symbols and not scanner:
        raise ValueError("duckdb_adbc_global_symbols requires ADBC_SCANNER_EXTENSION")
    import duckdb

    if args.engine == "duckdb":
        conn = duckdb.connect(str(args.private / f"{args.suite}-sf1.duckdb"), read_only=True)
        conn.execute(f"SET threads={threads}; SET memory_limit='8GB'")
        return conn, {"client_threads": threads}

    # Use the exact same installed native Redis library as the direct route.
    from adbc_driver_redis import _driver_path
    os.environ["QUERY_FARM_TELEMETRY_OPT_OUT"] = "1"
    config = json.loads((args.private / "connection.json").read_text())
    conn = duckdb.connect(config={"allow_unsigned_extensions": bool(scanner)})
    try:
        conn.execute(f"SET threads={threads}; SET memory_limit='8GB'")
        if global_symbols:
            # Promote the already imported DuckDB runtime, never an arbitrary library.
            import ctypes
            import _duckdb
            ctypes.CDLL(_duckdb.__file__, mode=ctypes.RTLD_GLOBAL)
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
        scan_option = f", scan_threads {scan_threads}" if scan_threads > 1 else ""
        conn.execute(f"ATTACH {literal(config['uri'])} AS r "
                     f"(TYPE adbc, driver {literal(library)}, READ_ONLY, query_pushdown false{scan_option}, {extra})")
        schema = options.get("adbc.redis.default_schema", "public")
        conn.execute(f"USE r.{identifier(schema)}")
        # Public provenance excludes library paths, attach SQL and credentials.
        extension = conn.execute("SELECT extension_version FROM duckdb_extensions() "
                                 "WHERE extension_name='adbc_scanner' AND loaded").fetchone()
        return conn, {"client_threads": threads, "requested_scan_threads": scan_threads, "custom_scanner_global_symbols": global_symbols, "execution": "DuckDB planner over Redis ADBC attached catalog",
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
            # Published with the next status write; the validator requires it.
            status.setdefault("artifacts", {})[str(repeat)] = dump_rows(
                args.artifacts / f"{args.engine}-{repeat}.pickle", columns, data)
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


def recorded_digest(artifacts, engine, repeat, role="result"):
    """The size/SHA-256 the engine worker recorded while writing one artifact."""
    status, problem = read_status(artifacts / f"{engine}.json")
    entry = status.get("artifacts", {}).get(str(repeat)) if isinstance(status.get("artifacts"), dict) else None
    if (problem or not isinstance(entry, dict) or type(entry.get("bytes")) is not int
            or not isinstance(entry.get("sha256"), str) or len(entry["sha256"]) != 64):
        raise ArtifactError(f"{engine} worker status has no recorded digest for repeat {repeat}",
                            {"role": role, "file": f"{engine}-{repeat}.pickle"})
    return entry


def validation_worker(args):
    from run import safe_error

    try:
        reference = recorded_digest(args.artifacts, "duckdb", 0, role="reference")
        for repeat in range(4):
            validate_files(args.artifacts / "duckdb-0.pickle",
                           args.artifacts / f"{args.engine}-{repeat}.pickle", args.artifacts,
                           digests=(reference, recorded_digest(args.artifacts, args.engine, repeat)))
    except Exception as exc:
        message = safe_error(str(exc))
        artifact = isinstance(exc, ArtifactError)
        mismatch = not artifact and ("result mismatch" in message or "result shape mismatch" in message)
        detail = {"status": "mismatch" if mismatch else "validation_failed", "error": message}
        if artifact:
            detail["artifact"] = exc.detail
        atomic_json(args.artifacts / f"{args.engine}-validation.json", detail)
        return 1
    return 0


ARTIFACT_PROBLEM_FIELDS = ("role", "file", "objects_decoded", "decode_offset", "decode_error",
                           "zero_bytes_at_offset", "first_zero_page_offset", "bytes_recorded",
                           "sha256_recorded", "bytes_read_back", "sha256_read_back")


def artifact_problem(detail):
    """Public, bounded copy of a validator's artifact diagnosis."""
    from run import safe_error

    value = detail.get("artifact")
    if not isinstance(value, dict):
        return None
    problem = {}
    for key in ARTIFACT_PROBLEM_FIELDS:
        item = value.get(key)
        if type(item) is int:
            problem[key] = item
        elif isinstance(item, str):
            problem[key] = safe_error(item)[:200]
    return problem


def retain_artifacts(private, artifacts, suite, query, engine):
    """Keep a failed validation's inputs; the per-query directory is removed.

    Engine files move; the shared DuckDB reference is copied because later
    engines still validate against it. The result stays in the private profile
    (mode 0700) and is named relative to it, never by absolute path."""
    root = private / "failed-artifacts"
    root.mkdir(mode=0o700, exist_ok=True)
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    target = Path(tempfile.mkdtemp(prefix=f"{suite}-q{query}-{engine}-{stamp}-", dir=root))
    for name in ("duckdb.json", "duckdb-0.pickle"):
        if (artifacts / name).exists():
            shutil.copy2(artifacts / name, target / name)
    moved = [f"{engine}.json", f"{engine}-validation.json"]
    moved += sorted(p.name for p in artifacts.glob(f"{engine}-*.pickle") if p.name != "duckdb-0.pickle")
    for name in moved:
        if (artifacts / name).exists():
            os.replace(artifacts / name, target / name)
    return f"failed-artifacts/{target.name}"


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
                        problem = artifact_problem(detail)
                        if problem:
                            record[f"{engine}_artifact_problem"] = problem
                    else:
                        record[f"{engine}_status"] = "ok"
                except subprocess.TimeoutExpired:
                    record[f"{engine}_status"] = "validation_failed"
                    record[f"{engine}_error"] = "validator exceeded 120-second process limit"
                if record[f"{engine}_status"] != "ok":
                    try:
                        record[f"{engine}_retained_artifacts"] = retain_artifacts(
                            args.private, artifacts, args.suite, args.query, engine)
                    except OSError as exc:
                        record[f"{engine}_retention_error"] = f"{type(exc).__name__}: {exc.strerror or 'retention failed'}"
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
