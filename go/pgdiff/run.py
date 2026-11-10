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


"""PostgreSQL 18 SQL dialect comparison through ADBC. Run via `make pgdiff`."""

import argparse
import json
import math
import os
import re
import struct
import subprocess
import sys
import time as clock
import uuid
from collections import Counter
from contextlib import ExitStack, contextmanager
from datetime import UTC, date, datetime, time, timedelta
from decimal import Decimal
from pathlib import Path

import adbc_driver_manager as adm
import adbc_driver_manager.dbapi as adbc
import psycopg
import pyarrow as pa
import sqlparse
from psycopg.types.string import TextLoader
from sqlscan import split_sql
from composites import Composite, register as register_composites, resolve_catalog_names

ROOT = Path(__file__).resolve().parent
# Official postgres:18 linux/amd64 manifest, resolved 2026-11-09 (18.6).
IMAGE = "postgres:18@sha256:e923a22ac074261dd73970c57edf659475bf0497cb9456f17d98ed7cdd3ca913"
# Physical Arrow equivalents. Arrow strings cannot distinguish text/varchar/json;
# unconstrained Postgres numeric has no fixed precision/scale to compare.
PG_TYPES = {
    2249: "record",
    18: "string",
    19: "string",
    2205: "string",
    2206: "string",
    16: "bool",
    17: "binary",
    20: "int64",
    21: "int16",
    23: "int32",
    25: "string",
    114: "string",
    700: "float32",
    701: "float64",
    1042: "string",
    1043: "string",
    1082: "date32",
    1083: "time",
    1114: "timestamp",
    1184: "timestamptz",
    1186: "interval",
    1266: "string",
    1700: "decimal",
    3802: "string",
    2950: "string",  # UUID has UTF-8 storage in this ADBC driver.
    1000: "list<bool>", 1001: "list<binary>", 1005: "list<int16>",
    1007: "list<int32>", 1016: "list<int64>", 1009: "list<string>",
    1015: "list<string>", 1021: "list<float32>", 1022: "list<float64>",
    1231: "list<decimal>", 1115: "list<timestamp>", 1185: "list<timestamptz>",
    1182: "list<date32>", 1183: "list<time>", 1187: "list<interval>",
}


def arrow_type(t):
    if isinstance(t, pa.BaseExtensionType) and t.extension_name == "arrow.json":
        return "string"
    if pa.types.is_struct(t):
        return "record"
    if pa.types.is_list(t) or pa.types.is_large_list(t):
        return f"list<{arrow_type(t.value_type)}>"
    if pa.types.is_decimal(t):
        return "decimal"
    if pa.types.is_timestamp(t):
        return "timestamptz" if t.tz else "timestamp"
    if pa.types.is_time(t):
        return "time"
    if pa.types.is_interval(t):
        return "interval"
    return {"double": "float64", "float": "float32", "date32[day]": "date32"}.get(
        str(t), str(t)
    )


def value(v, kind):
    if v is None:
        return None
    if kind == "record":
        if not isinstance(v, Composite):
            raise ValueError("record value lacks independent field types")
        types = v.types if v.types is not None else [PG_TYPES.get(oid, f"pg_oid:{oid}") for oid in v.oids]
        return {"types": types, "fields": [value(x, t) for x, t in zip(v.fields, types)]}
    if kind.startswith("list<"):
        return [value(x, kind[5:-1]) for x in v]
    if kind == "interval":
        if hasattr(v, "months"):
            return [v.months, v.days, v.nanoseconds // 1000, v.nanoseconds % 1000]
        if v.startswith("P"):
            m = re.fullmatch(
                r"P(?:(-?\d+)Y)?(?:(-?\d+)M)?(?:(-?\d+)D)?(?:T(?:(-?\d+)H)?(?:(-?\d+)M)?(?:(-?[\d.]+)S)?)?",
                v,
            )
            if not m:
                raise ValueError(f"unhandled Postgres interval: {v!r}")
            y, mo, d, h, mi, sec = [Decimal(x or 0) for x in m.groups()]
            return [
                int(y * 12 + mo),
                int(d),
                int((h * 3600 + mi * 60 + sec) * 1000000),
                0,
            ]
        months = days = 0
        for n, unit in re.findall(r"([+-]?\d+)\s+(years?|mons?|months?|days?)", v):
            if unit.startswith("year"):
                months += int(n) * 12
            elif unit.startswith(("mon", "month")):
                months += int(n)
            else:
                days += int(n)
        clockpart = re.search(r"([+-]?)(\d+):(\d+):([\d.]+)", v)
        micros = 0
        if clockpart:
            sign, h, m, sec = clockpart.groups()
            micros = int((Decimal(h) * 3600 + Decimal(m) * 60 + Decimal(sec)) * 1000000)
            if sign == "-":
                micros = -micros
        if not clockpart and not re.search(r"\d+\s+(year|mon|day)", v):
            raise ValueError(f"unhandled Postgres interval: {v!r}")
        return [months, days, micros, 0]
    if isinstance(v, str) and kind in ("timestamp", "timestamptz"):
        if re.search(r"\.\d{7,9}(?=[+-]|$)", v):
            return v
        try:
            v = datetime.fromisoformat(v)
        except ValueError:
            # Normalize only ISO text formatting; preserve the era and every digit.
            m = re.fullmatch(r"(\d{4,}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2})(\.\d+)?(\+00(?::00)?)?( BC)?", v)
            if m:
                d, clock, frac, zone, era = m.groups()
                frac = (frac or "").rstrip("0").rstrip(".")
                return d + "T" + clock + frac + ("+00:00" if zone else "") + (era or "")
            return v
    if isinstance(v, uuid.UUID):
        return str(v)
    if isinstance(v, float):
        if kind == "float32":
            v = struct.unpack("!f", struct.pack("!f", v))[0]
        return "NaN" if math.isnan(v) else v.hex()
    if isinstance(v, Decimal):
        text = format(v, "f")
        return text.rstrip("0").rstrip(".") if "." in text else text
    if isinstance(v, datetime):
        if v.tzinfo:
            v = v.astimezone(UTC)
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
    if len(split_sql(sql)) != 1:
        raise ValueError("each corpus statement must contain exactly one SQL statement")


def parameter_sql(sql, params, postgres):
    """Translate only placeholder tokens, preserving literals/comments and repeats."""
    values = []
    parts = []
    for token in sqlparse.parse(sql)[0].flatten():
        if token.ttype in sqlparse.tokens.Name.Placeholder and re.fullmatch(
            r"\$[1-9]\d*", token.value
        ):
            index = int(token.value[1:]) - 1
            if index >= len(params):
                raise ValueError(f"unbound parameter {token.value}")
            values.append(params[index])
            parts.append("%s" if postgres else "?")
        else:
            parts.append(token.value.replace("%", "%%") if postgres else token.value)
    if not values:
        raise ValueError("params supplied without $n placeholders")
    return "".join(parts), values


def render_sql(sql, schema, postgres):
    return sql.replace("{{schema}}", schema).replace(
        "{{catalog}}", "postgres" if postgres else "redis"
    )


def type_details(cur, postgres, table=None):
    if postgres:
        details = []
        for i, col in enumerate(cur.description):
            typmod = cur.pgresult.fmod(i)
            detail = {"oid": col.type_code, "typmod": typmod}
            if col.type_code == 1700 and typmod >= 4:
                detail.update(
                    precision=(typmod - 4) >> 16,
                    scale=(((typmod - 4) & 2047) ^ 1024) - 1024,
                )
            if col.type_code in (1083, 1114, 1184):
                declared = min(typmod, 6) if typmod >= 0 else 6
                resolution = ((declared + 2) // 3) * 3
                detail.update(
                    declared_precision=declared,
                    precision=resolution,
                    unit={0: "s", 3: "ms", 6: "us"}[resolution],
                )
            details.append(detail)
        return details
    details = []
    for field in table.schema:
        detail = {"arrow": str(field.type)}
        if pa.types.is_decimal(field.type):
            detail.update(precision=field.type.precision, scale=field.type.scale)
        if pa.types.is_timestamp(field.type) or pa.types.is_time(field.type):
            detail.update(
                unit=field.type.unit,
                precision={"s": 0, "ms": 3, "us": 6, "ns": 9}[field.type.unit],
            )
        details.append(detail)
    return details


def civil_date(days):
    # Proleptic Gregorian civil date from epoch days, without datetime's 1..9999 limit.
    z = days + 719468
    era, doe = divmod(z, 146097)
    yoe = (doe - doe // 1460 + doe // 36524 - doe // 146096) // 365
    year = yoe + era * 400
    doy = doe - (365 * yoe + yoe // 4 - yoe // 100)
    mp = (5 * doy + 2) // 153
    day = doy - (153 * mp + 2) // 5 + 1
    month = mp + (3 if mp < 10 else -9)
    year += month <= 2
    bc = year <= 0
    return f"{1-year if bc else year:04d}-{month:02d}-{day:02d}", bc


def arrow_values(column, datatype):
    if isinstance(datatype, pa.BaseExtensionType) and datatype.extension_name == "arrow.json":
        column = column.combine_chunks() if isinstance(column, pa.ChunkedArray) else column
        return arrow_values(column.storage, datatype.storage_type)
    if pa.types.is_struct(datatype):
        # Positional children retain duplicate names and distinguish a NULL
        # struct from a present struct whose children are all NULL.
        column = column.combine_chunks() if isinstance(column, pa.ChunkedArray) else column
        children = [arrow_values(column.field(i), f.type) for i, f in enumerate(datatype)]
        types = [arrow_type(f.type) for f in datatype]
        return [None if column.is_null()[i].as_py() else Composite(None, types, [c[i] for c in children])
                for i in range(len(column))]
    if pa.types.is_date32(datatype):
        out = []
        for raw in column.cast(pa.int32()).to_pylist():
            if raw is None:
                out.append(None)
            else:
                text, bc = civil_date(raw)
                out.append(text + (" BC" if bc else ""))
        return out
    if not pa.types.is_timestamp(datatype):
        return column.to_pylist()
    out = []
    per_second = {"s": 1, "ms": 1000, "us": 1000000, "ns": 1000000000}[datatype.unit]
    precision = {"s": 0, "ms": 3, "us": 6, "ns": 9}[datatype.unit]
    for raw in column.cast(pa.int64()).to_pylist():
        if raw is None:
            out.append(None)
            continue
        seconds, fraction = divmod(raw, per_second)
        days, clock = divmod(seconds, 86400)
        text, bc = civil_date(days)
        hour, clock = divmod(clock, 3600)
        minute, second = divmod(clock, 60)
        text += f"T{hour:02d}:{minute:02d}:{second:02d}"
        if fraction:
            text += "." + f"{fraction:0{precision}d}".rstrip("0")
        if datatype.tz:
            text += "+00:00"
        if bc:
            text += " BC"
        out.append(text)
    return out


def execute(conn, sql, postgres=False, params=None):
    validate_sql(sql)
    try:
        with conn.cursor() as cur:
            if params is not None:
                sql, bindings = parameter_sql(sql, params, postgres)
                cur.execute(sql, bindings, **({"binary": True} if postgres else {}))
            else:
                cur.execute(sql, **({"binary": True} if postgres else {}))
            if postgres:
                if cur.description is None:
                    return {"types": [], "rows": []}
                types = [
                    PG_TYPES.get(c.type_code, f"pg_oid:{c.type_code}")
                    for c in cur.description
                ]
                raw_types = [str(c.type_code) for c in cur.description]
                details = type_details(cur, True)
                rows = cur.fetchall()
                cache = {}
                rows = [resolve_catalog_names(conn, row, cache) for row in rows]
            else:
                table = cur.fetch_arrow_table()
                raw_types = [str(f.type) for f in table.schema]
                types = [arrow_type(f.type) for f in table.schema]
                details = type_details(cur, False, table)
                columns = [
                    arrow_values(c, f.type) for c, f in zip(table.columns, table.schema)
                ]
                rows = zip(*columns) if columns else []
            return {
                "types": types,
                "raw_types": raw_types,
                "type_details": details,
                "rows": [[value(v, t) for v, t in zip(row, types)] for row in rows],
            }
    except (OverflowError, UnicodeDecodeError) as exc:
        return {
            "decode_error": str(exc),
            "types": locals().get("types", []),
            "raw_types": locals().get("raw_types", []),
        }
    except (psycopg.Error, adbc.Error) as exc:
        if (
            isinstance(exc, psycopg.OperationalError)
            and (not exc.sqlstate or exc.sqlstate.startswith(("08", "57")))
        ) or (
            isinstance(exc, adbc.Error)
            and getattr(exc, "status_code", None)
            in (
                adm.AdbcStatusCode.IO,
                adm.AdbcStatusCode.TIMEOUT,
                adm.AdbcStatusCode.CANCELLED,
            )
        ):
            raise RuntimeError(f"transport failure: {exc}") from exc
        state = getattr(exc, "sqlstate", None)
        if isinstance(state, bytes):
            state = state.decode()
        return {"sqlstate": state or "MISSING", "message": str(exc)}


def signature(result, keep_order, checks=None, context=None, engine=None):
    if "decode_error" in result:
        return {"decode_error": result["decode_error"]}
    if "sqlstate" in result:
        return {"sqlstate": result["sqlstate"]}
    rows = [list(row) for row in result["rows"]]
    for index, expected in (context or {}).items():
        for row in rows:
            if row[int(index)] == expected[engine]:
                row[int(index)] = {"context_column": index}
    if not keep_order:
        rows = sorted(rows, key=lambda r: json.dumps(r, sort_keys=True))
    out = {"types": result["types"], "rows": rows}
    if checks:
        out["type_checks"] = {
            index: {
                key: result.get("type_details", [{}] * len(result["types"]))[
                    int(index)
                ].get(key)
                for key in keys
            }
            for index, keys in checks.items()
        }
    return out


def classify(pg, redis, case, rule=None):
    keep_order = case.get("ordered", ordered(case["sql"]))
    p, r = (
        signature(
            pg,
            keep_order,
            case.get("type_checks"),
            case.get("context_values"),
            "postgres",
        ),
        signature(
            redis,
            keep_order,
            case.get("type_checks"),
            case.get("context_values"),
            "redis",
        ),
    )
    if (
        "decode_error" not in p
        and "decode_error" not in r
        and p == r
        and p.get("sqlstate") != "MISSING"
    ):
        return "pass"
    # Exact snapshots stop an allowlist entry from masking a new regression.
    if (
        rule
        and p.get("sqlstate") != "MISSING"
        and r.get("sqlstate") != "MISSING"
        and p == rule["postgres"]
        and r == rule["redis"]
    ):
        return "allowed"
    unsupported = case.get("unsupported")
    if unsupported and "sqlstate" not in pg and "decode_error" not in pg:
        expected = unsupported.get("expected_redis")
        exact = expected is not None and redis == expected
        state = unsupported.get("sqlstate")
        if exact or (state and state != "MISSING" and redis.get("sqlstate") == state):
            return "unsupported"
    return "fail"


@contextmanager
def reference():
    name = "redis-pgdiff-" + uuid.uuid4().hex[:12]
    subprocess.run(
        [
            "docker",
            "run",
            "--detach",
            "--name",
            name,
            "--platform",
            "linux/amd64",
            "-p",
            "127.0.0.1::5432",
            "-e",
            "POSTGRES_PASSWORD=pgdiff",
            "-e",
            "POSTGRES_INITDB_ARGS=--locale=C --encoding=UTF8",
            IMAGE,
        ],
        check=True,
        stdout=subprocess.DEVNULL,
    )
    try:
        port = (
            subprocess.check_output(["docker", "port", name, "5432/tcp"], text=True)
            .strip()
            .rsplit(":", 1)[1]
        )
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
        subprocess.run(
            ["docker", "rm", "--force", name], check=True, stdout=subprocess.DEVNULL
        )


def configure(conn, postgres, schema=None):
    statements = [
        "SET TIME ZONE 'UTC'",
        "SET datestyle = 'ISO, MDY'",
        "SET intervalstyle = 'postgres'",
    ]
    if postgres:
        statements.append("SET statement_timeout = '5s'")
    if schema:
        statements.append(f'SET search_path TO "{schema}", pg_catalog')
    for sql in statements:
        outcome = execute(conn, sql, postgres)
        if "sqlstate" in outcome:
            raise RuntimeError(f"session setup failed: {outcome}")


def summarize(results):
    counts = Counter(row["status"] for row in results)
    denominator = counts["pass"] + counts["fail"]
    return {
        "counts": dict(counts),
        "scored": denominator,
        "pass_rate": counts["pass"] / denominator if denominator else None,
    }


def write_report(args, metadata, results, errors):
    summary = summarize(results)
    report = {
        "reference": metadata,
        "summary": summary,
        "infrastructure_errors": errors,
        "groups": {
            key: {
                name: summarize([r for r in results if r[key] == name])
                for name in sorted({r[key] for r in results})
            }
            for key in ("file", "area", "tier")
        },
        "results": results,
    }
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + "\n")
    print(
        f"Total: {summary['counts'].get('pass', 0)}/{summary['scored']} passed; {summary['counts']}"
    )
    for key, groups in report["groups"].items():
        for name, group in groups.items():
            rate = (
                f"{group['pass_rate'] * 100:.1f}%"
                if group["pass_rate"] is not None
                else "n/a"
            )
            print(f"{key} {name}: {rate}; {group['counts']}")
    for row in results:
        if row["status"] in ("fail", "fixture-error"):
            print(f"\nDIFF {row['file']}:{row['id']}\n{json.dumps(row, indent=2)}")
    for error in errors:
        print(f"INFRASTRUCTURE: {error}")
    print(f"\nReport: {args.report}")
    if errors or any(r["status"] in ("fixture-error", "blocked") for r in results):
        return 2
    return int(any(r["status"] == "fail" for r in results))


def validate_reference(version, collation):
    if not version.startswith("PostgreSQL 18.") or collation not in ("C", "POSIX"):
        raise ValueError(
            f"reference must be PostgreSQL 18 with C collation: {version}, {collation}"
        )


def run(args, uri):
    rules = json.loads(args.allowlist.read_text())
    for key, rule in rules.items():
        if not rule.get("reason") or not rule.get("issue"):
            raise ValueError(f"allowlist {key} needs reason and issue")
    files = sorted(args.corpus.rglob("*.json"))
    if not files:
        raise ValueError("no corpus files found")
    results, errors = [], []
    metadata = {"image": IMAGE}
    with ExitStack() as stack:

        def connect(postgres, manager=stack):
            conn = (
                psycopg.connect(uri, autocommit=True)
                if postgres
                else adbc.connect(
                    driver=str(args.driver.resolve()),
                    db_kwargs={"uri": args.redis_uri},
                    autocommit=True,
                )
            )
            manager.enter_context(conn)
            if postgres:
                register_composites(conn)
                for oid in (114, 3802, 1186, 1266, 1082, 1114, 1184):
                    conn.adapters.register_loader(oid, TextLoader)
            configure(conn, postgres)
            return conn

        pg = connect(True)
        with pg.cursor() as cur:
            cur.execute(
                "SELECT version(), datcollate FROM pg_database WHERE datname = current_database()"
            )
            version, collation = cur.fetchone()
        validate_reference(version, collation)
        metadata.update(
            postgres=version, collation=collation, corpus=str(args.corpus.resolve())
        )
        for path in files:
            corpus = json.loads(path.read_text())
            name = path.relative_to(args.corpus).as_posix()
            tier = corpus.get(
                "tier", "issues" if name.startswith("issue-") else "smoke"
            )
            schema = "pgdiff_" + uuid.uuid4().hex
            file_stack = ExitStack()
            pairs = {"default": (connect(True, file_stack), connect(False, file_stack))}
            ids = [c["id"] for c in corpus["cases"]]
            if len(ids) != len(set(ids)):
                raise ValueError(f"duplicate case IDs in {path}")
            base = {
                "file": name,
                "area": corpus["area"],
                "tier": tier,
                "source": corpus.get("source"),
                "provenance": corpus.get("provenance"),
            }

            def pair(label, pairs=pairs, file_stack=file_stack, schema=schema):
                if label not in pairs:
                    pairs[label] = connect(True, file_stack), connect(False, file_stack)
                    for conn, is_pg in zip(pairs[label], (True, False)):
                        configure(conn, is_pg, schema)
                return pairs[label]

            def outcomes(step, pair=pair, schema=schema):
                conns = pair(step.get("connection", "default"))
                return tuple(
                    execute(
                        conn,
                        render_sql(step["sql"], schema, is_pg),
                        is_pg,
                        step.get("params"),
                    )
                    for conn, is_pg in zip(conns, (True, False))
                )

            try:
                for conn, is_pg in zip(pairs["default"], (True, False)):
                    outcome = execute(conn, f'CREATE SCHEMA "{schema}"', is_pg)
                    if "sqlstate" in outcome:
                        raise RuntimeError(f"schema setup failed: {outcome}")
                    configure(conn, is_pg, schema)
                setup_ok = True
                for index, sql in enumerate(corpus.get("setup", [])):
                    p, r = outcomes({"sql": sql})
                    if "sqlstate" in p or "sqlstate" in r:
                        results.append(
                            {
                                **base,
                                "id": f"setup-{index}",
                                "sql": sql,
                                "status": "fixture-error",
                                "postgres": p,
                                "redis": r,
                            }
                        )
                        setup_ok = False
                        break
                for case in corpus["cases"]:
                    row = {**base, **case}
                    if not setup_ok:
                        row["status"] = "blocked"
                    else:
                        steps = case.get("steps", [case])
                        outcomes_list = []
                        for step in steps:
                            if step.get("unsupported") and not step["unsupported"].get(
                                "reason"
                            ):
                                raise ValueError(
                                    f"unsupported case {case['id']} needs a documented reason"
                                )
                            p, r = outcomes(step)
                            if (
                                "expect_postgres" in step
                                and p.get("sqlstate") != step["expect_postgres"]
                            ):
                                status = "fixture-error"
                            else:
                                rule = (
                                    rules.get(f"{name}:{case['id']}")
                                    if len(steps) == 1
                                    else None
                                )
                                status = classify(p, r, step, rule)
                            outcomes_list.append(
                                {
                                    "sql": step["sql"],
                                    "connection": step.get("connection", "default"),
                                    "status": status,
                                    "postgres": p,
                                    "redis": r,
                                }
                            )
                        if "steps" in case:
                            row["outcomes"] = outcomes_list
                            statuses = {s["status"] for s in outcomes_list}
                            row["status"] = next(
                                (
                                    s
                                    for s in (
                                        "fixture-error",
                                        "fail",
                                        "unsupported",
                                        "allowed",
                                    )
                                    if s in statuses
                                ),
                                "pass",
                            )
                            for conns in pairs.values():
                                for conn, is_pg in zip(conns, (True, False)):
                                    execute(conn, "ROLLBACK", is_pg)
                        else:
                            row.update(
                                {
                                    k: v
                                    for k, v in outcomes_list[0].items()
                                    if k != "sql"
                                }
                            )
                    results.append(row)
            except Exception as exc:  # noqa: BLE001 - preserve failures in the JSON report
                errors.append({"file": name, "phase": "execution", "error": str(exc)})
            finally:
                # Release all transaction/row locks before dropping the shared schema.
                for conns in pairs.values():
                    for conn, is_pg in zip(conns, (True, False)):
                        execute(conn, "ROLLBACK", is_pg)
                for conn, is_pg in zip(pairs["default"], (True, False)):
                    outcome = execute(
                        conn, f'DROP SCHEMA IF EXISTS "{schema}" CASCADE', is_pg
                    )
                    if "sqlstate" in outcome:
                        errors.append(
                            {"file": name, "phase": "cleanup", "outcome": outcome}
                        )
                file_stack.close()
    return write_report(args, metadata, results, errors)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--redis-uri", default=os.getenv("REDIS_URI")
    )
    ext = {"darwin": "dylib", "win32": "dll"}.get(sys.platform, "so")
    parser.add_argument(
        "--driver", type=Path, default=ROOT.parent / f"build/libadbc_driver_redis.{ext}"
    )
    parser.add_argument("--corpus", type=Path, default=ROOT / "corpus")
    parser.add_argument("--allowlist", type=Path, default=ROOT / "allowlist.json")
    parser.add_argument(
        "--report", type=Path, default=ROOT.parent / "build/pgdiff-report.json"
    )
    args = parser.parse_args()
    from urllib.parse import urlsplit
    target = urlsplit(args.redis_uri or "")
    if target.scheme != "rediss" or not target.username or not target.password:
        parser.error("provide an authenticated rediss:// Redis Enterprise target via --redis-uri or REDIS_URI")
    try:
        with reference() as uri:
            return run(args, uri)
    except Exception as exc:  # noqa: BLE001 - preserve failures in the JSON report
        return write_report(
            args, {"image": IMAGE}, [], [{"phase": "startup", "error": str(exc)}]
        )


if __name__ == "__main__":
    sys.exit(main())
