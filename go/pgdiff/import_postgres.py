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


"""Rebuild curated Postgres cases and the exclusion inventory from pinned sources."""

import hashlib
import json
import re
from collections import defaultdict
from pathlib import Path

from sqlscan import split_sql

ROOT = Path(__file__).resolve().parent
SOURCE = ROOT / "upstream/postgres"


def statements(text):
    return split_sql(text)


def spans(numbers):
    out = []
    for n in numbers:
        if out and out[-1][1] == n - 1:
            out[-1][1] = n
        else:
            out.append([n, n])
    return out


def rebuild():
    manifest = json.loads((SOURCE / "manifest.json").read_text())
    oracle = json.loads((SOURCE / "oracle.json").read_text())["expectations"]
    inventory = []
    for entry in manifest["files"]:
        if not entry["path"].endswith(".sql"):
            continue
        data = (SOURCE / entry["path"]).read_bytes()
        if hashlib.sha256(data).hexdigest() != entry["sha256"]:
            raise ValueError(f"source changed: {entry['path']}")
        area = Path(entry["path"]).stem
        sql = statements(data.decode())
        setup, candidates = [], []
        # Preserve each chosen query; fixtures are reduced, deterministic, and
        # independent of Postgres's external regression database and psql COPY.
        if area == "join":
            setup = [
                q
                for q in sql
                if re.match(
                    r"(CREATE TABLE J[12]_TBL|INSERT INTO J[12]_TBL)", q, re.IGNORECASE
                )
            ]
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT") and "J1_TBL" in q.upper()
            ][:30]
        elif area == "case":
            setup = [
                q
                for q in sql
                if re.match(
                    r"(CREATE TABLE CASE[2]?_TBL|INSERT INTO CASE[2]?_TBL)",
                    q,
                    re.IGNORECASE,
                )
            ]
            candidates = [
                i for i, q in enumerate(sql) if q.upper().startswith("SELECT")
            ][:18]
            candidates = [
                i
                for i in candidates
                if not re.search(r"\brandom\s*\(", sql[i], re.IGNORECASE)
            ]
        elif area == "window":
            setup = [q.replace("TEMPORARY ", "") for q in sql[:2]]
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT")
                and "empsalary" in q
                and "tenk1" not in q
            ][:10]
        elif area == "aggregates":
            setup = [
                "CREATE TABLE aggtest (a smallint, b real)",
                "INSERT INTO aggtest VALUES (1,1.5),(2,2.5),(NULL,NULL),(3,3.5)",
            ]
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT")
                and ("FROM aggtest" in q or ("FROM" not in q.upper() and i < 80))
            ][:24]
        elif area == "int4":
            setup = [
                "CREATE TABLE INT4_TBL (f1 integer)",
                "INSERT INTO INT4_TBL VALUES (0,123456,-123456,2147483647,-2147483647)".replace(
                    "(0,123456,-123456,2147483647,-2147483647)",
                    "(0),(123456),(-123456),(2147483647),(-2147483647)",
                ),
            ]
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT")
                and (
                    re.search(r"\bINT4_TBL\b", q, re.IGNORECASE)
                    or "FROM" not in q.upper()
                )
            ][:25]
        elif area == "int8":
            setup = [
                "CREATE TABLE INT8_TBL (q1 bigint, q2 bigint)",
                "INSERT INTO INT8_TBL VALUES (123,456),(123,4567890123456789),(4567890123456789,123),(-4567890123456789,4567890123456789),(4567890123456789,-4567890123456789)",
            ]
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT")
                and (
                    re.search(r"\bINT8_TBL\b", q, re.IGNORECASE)
                    or "FROM" not in q.upper()
                )
            ][:28]
        elif area == "float8":
            setup = ["CREATE TABLE FLOAT8_TBL (f1 double precision)"] + [
                q for q in sql[1:6]
            ]
            candidates = [
                i for i, q in enumerate(sql) if q.upper().startswith("SELECT")
            ][:32]
        elif area == "select":
            candidates = [
                i for i, q in enumerate(sql) if q.lower().startswith("select foo from")
            ][:3]
        elif area == "with":
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("WITH")
                and i < 20
                and not re.search(r"\b(money|random|pg_typeof)\b", q, re.IGNORECASE)
            ]
        elif area == "union":
            candidates = list(range(17))
        elif area == "date":
            candidates = [
                i
                for i, q in enumerate(sql)
                if re.match(r"SELECT date ", q, re.IGNORECASE)
            ][:24]
        elif area in ("timestamp", "timestamptz"):
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT")
                and not re.search(
                    r"\b(FROM|now|today|tomorrow|yesterday|pg_sleep)\b",
                    q,
                    re.IGNORECASE,
                )
            ][:20]
        elif area == "interval":
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT") and "FROM" not in q.upper()
            ][:20]
        elif area == "numeric":
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT") and "FROM" not in q.upper()
            ][:25]
        elif area == "strings":
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT")
                and not re.search(r"\b(FROM|repeat)\b", q, re.IGNORECASE)
            ][:28]
        elif area == "json":
            candidates = [
                i
                for i, q in enumerate(sql)
                if q.upper().startswith("SELECT")
                and len(q) < 600
                and "repeat(" not in q
            ][:40]
        cases = []
        selected = set(candidates)
        for i in candidates:
            query = sql[i]
            case = {
                "id": f"statement-{i:04d}",
                "sql": query,
                "source_statement": i,
                "expect_postgres": None,
            }
            if area == "window" and "ORDER BY depname, salary" in query:
                case["sql"] = query.rstrip(";") + ", empno;"
                case["adaptation"] = (
                    "Outer empno tie-breaker makes tied salary rows deterministic."
                )
            if area == "with" and re.search(r"LIMIT\s+10", query, re.IGNORECASE):
                # Recursive generators here deliberately stop after ten rows;
                # they emit a deterministic prefix by their single successor.
                case["ordered"] = False
            expected = oracle.get(f"postgres/{area}.json:{case['id']}")
            if expected:
                if (
                    expected["sql_sha256"]
                    != hashlib.sha256(case["sql"].encode()).hexdigest()
                ):
                    raise ValueError(f"oracle SQL changed: {area}:{case['id']}")
                case["expect_postgres"] = expected["sqlstate"]
            cases.append(case)
        corpus = {
            "area": area,
            "tier": "postgres",
            "source": entry["source"],
            "provenance": {
                "version": manifest["version"],
                "commit": manifest["commit"],
                "sha256": entry["sha256"],
                "fixture": "Curated independent subset; reduced fixtures, not the full upstream regression database.",
            },
            "setup": setup,
            "cases": cases,
        }
        target = ROOT / "corpus/postgres" / f"{area}.json"
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps(corpus, indent=2) + "\n")
        omissions = []
        for i, q in enumerate(sql):
            if i in selected or q in setup:
                continue
            if not q:
                continue
            if re.search(
                r"\b(CREATE FUNCTION|CREATE PROCEDURE|CREATE TRIGGER|DO\s*\$|money|xml|INHERITS|PARTITION BY RANGE)\b",
                q,
                re.IGNORECASE,
            ):
                reason = "Explicit driver scope exclusion: procedural SQL or unsupported storage/type feature."
            elif re.search(
                r"\b(now|today|tomorrow|yesterday|random|pg_sleep)\b", q, re.IGNORECASE
            ):
                reason = "Requires time/volatility fixture; excluded from this deterministic subset."
            elif q.startswith("COPY ") or "FROM :'filename'" in q:
                reason = "psql/external regression dataset dependency; reduced fixture supplied instead."
            else:
                reason = "Not selected: requires additional fixture dependencies, or later regression coverage beyond this curated subset."
            omissions.append(
                {
                    "statement": i,
                    "sha256": hashlib.sha256(q.encode()).hexdigest(),
                    "reason": reason,
                }
            )
        grouped = defaultdict(list)
        for omission in omissions:
            grouped[omission["reason"]].append(omission["statement"])
        omissions = [
            {"reason": reason, "statement_ranges": spans(numbers)}
            for reason, numbers in grouped.items()
        ]
        inventory.append(
            {
                "area": area,
                "source": entry["source"],
                "source_statements": len(sql),
                "selected": [c["id"] for c in cases],
                "omitted": omissions,
            }
        )
    (ROOT / "postgres-coverage.json").write_text(json.dumps(inventory, indent=2) + "\n")
    print(
        "Imported",
        sum(len(x["selected"]) for x in inventory),
        "queries across",
        len(inventory),
        "areas",
    )


if __name__ == "__main__":
    rebuild()
