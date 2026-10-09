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


"""Postgres 16 differential runner. Run via `make pgdiff` from go/."""
import argparse
from collections import Counter, defaultdict
from contextlib import contextmanager
from datetime import date, datetime, time, timezone
from decimal import Decimal
import json
import math
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
import time as clock
import uuid

import adbc_driver_manager.dbapi as adbc
import psycopg
from psycopg.types.string import TextLoader
import pyarrow as pa
import sqlparse

ROOT = Path(__file__).resolve().parent
IMAGE = "postgres:16@sha256:75adc2a65806c96bc0e59030230bfe3f1c1b0d6d8f8181a8913e2b8b53bdc166"
# Physical Arrow equivalents. Arrow strings cannot distinguish text/varchar/json;
# unconstrained Postgres numeric has no fixed precision/scale to compare.
PG_TYPES = {16: "bool", 17: "binary", 20: "int64", 21: "int16", 23: "int32",
            25: "string", 114: "string", 700: "float32", 701: "float64",
            1042: "string", 1043: "string", 1082: "date32", 1083: "time",
            1114: "timestamp", 1184: "timestamptz", 1186: "interval",
            1266: "string", 1700: "decimal", 3802: "string"}


def arrow_type(t):
    if pa.types.is_decimal(t):
        return "decimal"
    if pa.types.is_timestamp(t):
        return "timestamptz" if t.tz else "timestamp"
    if pa.types.is_time(t):
        return "time"
    if pa.types.is_interval(t):
        return "interval"
    return {"double": "float64", "float": "float32", "date32[day]": "date32"}.get(str(t), str(t))


def value(v, kind):
    if v is None:
        return None
    if kind == "interval":
        if hasattr(v, "months"):
            return [v.months, v.days, v.nanoseconds // 1000, v.nanoseconds % 1000]
        # SET intervalstyle=iso_8601 preserves months separately from days.
        m = re.fullmatch(r"P(?:(-?\d+)Y)?(?:(-?\d+)M)?(?:(-?\d+)D)?(?:T(?:(-?\d+)H)?(?:(-?\d+)M)?(?:(-?[\d.]+)S)?)?", v)
        if not m:
            raise ValueError(f"unhandled Postgres interval: {v!r}")
        y, mo, d, h, mi, s = [Decimal(x or 0) for x in m.groups()]
        return [int(y * 12 + mo), int(d), int((h * 3600 + mi * 60 + s) * 1000000), 0]
    if isinstance(v, float):
        if kind == "float32":
            v = struct.unpack("!f", struct.pack("!f", v))[0]
        return "NaN" if math.isnan(v) else v.hex()
    if isinstance(v, Decimal):
        text = format(v, "f")
        return text.rstrip("0").rstrip(".") if "." in text else text
    if isinstance(v, datetime):
        if v.tzinfo:
            v = v.astimezone(timezone.utc)
        return v.isoformat()
    if isinstance(v, (date, time)):
        return v.isoformat()
    if isinstance(v, (bytes, memoryview)):
        return bytes(v).hex()
    return v


def ordered(sql):
    """Only an outer ORDER BY constrains output order, not a window/subquery."""
    tokens = list(sqlparse.parse(sql)[0].flatten())
    depth = 0
    for t in tokens:
        if t.value == "(":
            depth += 1
        elif t.value == ")":
            depth -= 1
        elif depth == 0 and t.is_keyword and t.normalized == "ORDER BY":
            return True
    return False


def validate_sql(sql):
    if len(sqlparse.split(sql)) != 1:
        raise ValueError("each corpus statement must contain exactly one SQL statement")


def execute(conn, sql, postgres=False):
    validate_sql(sql)
    try:
        with conn.cursor() as cur:
            cur.execute(sql)
            if postgres:
                if cur.description is None:
                    return {"types": [], "rows": []}
                types = [PG_TYPES.get(c.type_code, f"pg_oid:{c.type_code}") for c in cur.description]
                raw_types = [str(c.type_code) for c in cur.description]
                rows = cur.fetchall()
            else:
                table = cur.fetch_arrow_table()
                raw_types = [str(f.type) for f in table.schema]
                types = [arrow_type(f.type) for f in table.schema]
                columns = [c.to_pylist() for c in table.columns]
                rows = zip(*columns) if columns else []
            return {"types": types, "raw_types": raw_types,
                    "rows": [[value(v, t) for v, t in zip(row, types)] for row in rows]}
    except (psycopg.Error, adbc.Error) as exc:
        state = getattr(exc, "sqlstate", None)
        if isinstance(state, bytes):
            state = state.decode()
        return {"sqlstate": state or "MISSING", "message": str(exc)}


def signature(result, keep_order):
    if "sqlstate" in result:
        return {"sqlstate": result["sqlstate"]}
    rows = result["rows"]
    if not keep_order:
        rows = sorted(rows, key=lambda r: json.dumps(r, sort_keys=True))
    return {"types": result["types"], "rows": rows}


def classify(pg, redis, case, rule=None):
    keep_order = case.get("ordered", ordered(case["sql"]))
    p, r = signature(pg, keep_order), signature(redis, keep_order)
    if p == r and p.get("sqlstate") != "MISSING":
        return "pass"
    # Exact snapshots stop an allowlist entry from masking a new regression.
    if rule and p == rule["postgres"] and r == rule["redis"]:
        return "allowed"
    unsupported = case.get("unsupported")
    if unsupported and "sqlstate" not in pg and redis.get("sqlstate") == unsupported["sqlstate"]:
        return "unsupported"
    return "fail"


@contextmanager
def reference():
    name = "redis-pgdiff-" + uuid.uuid4().hex[:12]
    subprocess.run(["docker", "run", "--detach", "--name", name,
                    "--platform", "linux/amd64", "-p", "127.0.0.1::5432",
                    "-e", "POSTGRES_PASSWORD=pgdiff", "-e", "POSTGRES_INITDB_ARGS=--locale=C --encoding=UTF8",
                    IMAGE], check=True, stdout=subprocess.DEVNULL)
    try:
        port = subprocess.check_output(["docker", "port", name, "5432/tcp"], text=True).strip().rsplit(":", 1)[1]
        uri = f"postgresql://postgres:pgdiff@127.0.0.1:{port}/postgres"
        for _ in range(120):
            try:
                with psycopg.connect(uri, connect_timeout=1):
                    break
            except psycopg.OperationalError:
                clock.sleep(0.5)
        else:
            raise RuntimeError("Postgres did not become ready in 60 seconds")
        yield uri
    finally:
        subprocess.run(["docker", "rm", "--force", name], check=True, stdout=subprocess.DEVNULL)


def run(args, uri):
    rules = json.loads(args.allowlist.read_text())
    for key, rule in rules.items():
        if not rule.get("reason") or not rule.get("issue"):
            raise ValueError(f"allowlist {key} needs reason and issue")
    files = sorted(args.corpus.glob("*.json"))
    if not files:
        raise ValueError("no corpus files found")
    results = []
    metadata = {}
    with psycopg.connect(uri, autocommit=True) as pg, adbc.connect(
            driver=str(args.driver.resolve()), db_kwargs={"uri": args.redis_uri}, autocommit=True) as redis:
        for oid in (114, 3802, 1186, 1266):
            pg.adapters.register_loader(oid, TextLoader)
        with pg.cursor() as cur:
            cur.execute("SELECT version(), datcollate FROM pg_database WHERE datname = current_database()")
            version, collation = cur.fetchone()
        if not version.startswith("PostgreSQL 16.") or collation not in ("C", "POSIX"):
            raise ValueError(f"reference must be Postgres 16 with C collation: {version}, {collation}")
        metadata = {"image": IMAGE, "postgres": version, "collation": collation,
                    "corpus": str(args.corpus.resolve())}
        for conn, is_pg in ((pg, True), (redis, False)):
            for sql in ("SET TIME ZONE 'UTC'", "SET datestyle = 'ISO, MDY'",
                        "SET intervalstyle = 'iso_8601'" if is_pg else "SET intervalstyle = 'postgres'"):
                outcome = execute(conn, sql, is_pg)
                if "sqlstate" in outcome:
                    raise RuntimeError(f"session setup failed: {outcome}")
        for path in files:
            corpus = json.loads(path.read_text())
            schema = "pgdiff_" + uuid.uuid4().hex
            for case in corpus["cases"]:
                if case.get("unsupported") and not case["unsupported"].get("reason"):
                    raise ValueError(f"unsupported case {case['id']} needs a documented reason")
            ids = [c["id"] for c in corpus["cases"]]
            if len(ids) != len(set(ids)):
                raise ValueError(f"duplicate case IDs in {path}")
            try:
                for conn, is_pg in ((pg, True), (redis, False)):
                    for sql in (f'CREATE SCHEMA "{schema}"', f'SET search_path TO "{schema}"'):
                        outcome = execute(conn, sql, is_pg)
                        if "sqlstate" in outcome:
                            raise RuntimeError(f"schema setup failed: {outcome}")
                setup_ok = True
                for index, sql in enumerate(corpus.get("setup", [])):
                    p, r = execute(pg, sql, True), execute(redis, sql)
                    if "sqlstate" in p or "sqlstate" in r:
                        results.append({"file": path.name, "area": corpus["area"], "id": f"setup-{index}",
                                        "sql": sql, "status": "fail", "postgres": p, "redis": r})
                        setup_ok = False
                        break
                for case in corpus["cases"]:
                    row = {"file": path.name, "area": corpus["area"], "source": corpus.get("source"), **case}
                    if not setup_ok:
                        row["status"] = "blocked"
                    else:
                        p, r = execute(pg, case["sql"], True), execute(redis, case["sql"])
                        rule = rules.get(f"{path.name}:{case['id']}")
                        row.update(status=classify(p, r, case, rule), postgres=p, redis=r)
                    results.append(row)
            finally:
                for conn, is_pg in ((pg, True), (redis, False)):
                    # A corpus can exercise transactions; close any open unit before cleanup.
                    execute(conn, "ROLLBACK", is_pg)
                    outcome = execute(conn, f'DROP SCHEMA IF EXISTS "{schema}" CASCADE', is_pg)
                    if "sqlstate" in outcome:
                        raise RuntimeError(f"schema cleanup failed: {outcome}")
    counts = Counter(row["status"] for row in results)
    denominator = counts["pass"] + counts["fail"]
    summary = {"counts": dict(counts), "scored": denominator,
               "pass_rate": counts["pass"] / denominator if denominator else None}
    report = {"reference": metadata, "summary": summary, "results": results}
    print(f"Total: {counts['pass']}/{denominator} passed; {dict(counts)}")
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + "\n")
    for label in ("file", "area"):
        groups = defaultdict(Counter)
        for row in results:
            groups[row[label]][row["status"]] += 1
        for name, counts in sorted(groups.items()):
            denominator = counts["pass"] + counts["fail"]
            rate = f"{100 * counts['pass'] / denominator:.1f}%" if denominator else "n/a"
            print(f"{label} {name}: {rate} ({counts['pass']}/{denominator}); {dict(counts)}")
    for row in results:
        if row["status"] == "fail":
            print(f"\nDIFF {row['file']}:{row['id']}\n{row['sql']}\nPostgres: {json.dumps(row['postgres'])}\nRedis:    {json.dumps(row['redis'])}")
    print(f"\nReport: {args.report}")
    return int(any(r["status"] in ("fail", "blocked") for r in results))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--redis-uri", default=os.getenv("REDIS_URI", "redis://localhost:6379/0"))
    ext = {"darwin": "dylib", "win32": "dll"}.get(sys.platform, "so")
    parser.add_argument("--driver", type=Path, default=ROOT.parent / f"build/libadbc_driver_redis.{ext}")
    parser.add_argument("--corpus", type=Path, default=ROOT / "corpus")
    parser.add_argument("--allowlist", type=Path, default=ROOT / "allowlist.json")
    parser.add_argument("--report", type=Path, default=ROOT.parent / "build/pgdiff-report.json")
    args = parser.parse_args()
    with reference() as uri:
        return run(args, uri)


if __name__ == "__main__":
    sys.exit(main())
