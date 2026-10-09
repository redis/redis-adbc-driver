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


"""Build reproducible corpus cases from captured dbt adapter SQLQuery events."""

import csv
import hashlib
import json
import re
from pathlib import Path

from sqlscan import split_sql

ROOT = Path(__file__).resolve().parent
SOURCE = ROOT / "upstream/dbt"


def normalize(sql):
    sql = re.sub(r"test\d+_test_generated", "{{schema}}", sql)
    sql = re.sub(r"incremental__dbt_tmp\d+", "incremental__dbt_tmp", sql)
    # Catalog-qualified relation names and the union_relations source strings
    # refer to the same relation without their environment-specific catalog.
    return sql.replace('"postgres".', "")


def rebuild():
    raw = [json.loads(line) for line in (SOURCE / "sql.jsonl").read_text().splitlines()]
    manifest = json.loads((SOURCE / "manifest.json").read_text())
    parsed = [split_sql(normalize(r["sql"])) for r in raw]
    seed_create = "CREATE TABLE base (id integer, name text, some_date timestamp without time zone)"

    def insert_seed(name):
        data = list(csv.DictReader((SOURCE / f"{name}.csv").read_text().splitlines()))
        tuples = [f"({r['id']}, '{r['name']}', '{r['some_date']}')" for r in data]
        return f"INSERT INTO {name} VALUES " + ",".join(tuples)

    common = [seed_create, insert_seed("base")]
    groups = {
        "models": {"setup": common, "cases": []},
        "incremental": {
            "setup": common
            + [
                "CREATE TABLE added (id integer, name text, some_date timestamp without time zone)",
                insert_seed("added"),
                "CREATE TABLE incremental AS SELECT * FROM base",
            ],
            "cases": [],
        },
        "utils": {"setup": common, "cases": []},
        "metadata": {"setup": common, "cases": []},
    }
    selected = set()
    seen = set()
    for index, (record, queries) in enumerate(zip(raw, parsed)):
        for subindex, sql in enumerate(queries):
            q = sql.lower()
            target = None
            if (
                'node_id": "model.base.view_model' in record["sql"]
                or 'node_id": "model.base.table_model' in record["sql"]
            ):
                if q.startswith(("create", "alter", "drop")):
                    target = "models"
            elif 'node_id": "model.incremental.incremental' in record["sql"]:
                if q.startswith(("create temporary", "insert into")) or (
                    "information_schema.columns" in q
                    and "table_name = 'incremental'" in q
                ):
                    target = "incremental"
            elif (
                'node_id": "model.test.utils' in record["sql"]
                or 'node_id": "model.test.unioned' in record["sql"]
            ):
                if (
                    q.startswith(("create", "alter", "drop"))
                    or "information_schema.columns" in q
                ):
                    target = "utils"
            elif "information_schema.columns" in q and "table_name = 'base'" in q:
                target = "metadata"
            elif q.startswith("with diff_count") and "view_model" in q:
                target = "models"
            if not target or (target, sql) in seen:
                continue
            seen.add((target, sql))
            selected.add(index)
            groups[target]["cases"].append(
                {
                    "id": f"event-{index:04d}-{subindex}",
                    "sql": sql,
                    "expect_postgres": None,
                    "source_event": index,
                    "source_sql_sha256": hashlib.sha256(
                        record["sql"].encode()
                    ).hexdigest(),
                    "adaptation": "Strip query comments; substitute isolated schema and temporary suffix; remove the environment-specific catalog qualifier.",
                }
            )
    # Lazy views must actually be read, not scored only by successful creation.
    groups["models"]["cases"].append(
        {
            "id": "read-materialized",
            "sql": "SELECT * FROM table_model ORDER BY id",
            "expect_postgres": None,
        }
    )
    groups["incremental"]["cases"] += [
        {
            "id": "read-incremental",
            "sql": "SELECT * FROM incremental ORDER BY id",
            "expect_postgres": None,
        },
        {
            "id": "cleanup-temp",
            "sql": "DROP TABLE IF EXISTS incremental__dbt_tmp",
            "expect_postgres": None,
        },
    ]
    groups["utils"]["cases"].append(
        {
            "id": "read-unioned",
            "sql": "SELECT * FROM unioned ORDER BY _dbt_source_relation, id",
            "expect_postgres": None,
        }
    )
    target = ROOT / "corpus/dbt"
    target.mkdir(exist_ok=True, parents=True)
    for name, group in groups.items():
        corpus = {
            "area": name,
            "tier": "dbt",
            "source": manifest["source"],
            "provenance": manifest,
            **group,
        }
        (target / f"{name}.json").write_text(json.dumps(corpus, indent=2) + "\n")
    omitted = []
    for index, r in enumerate(raw):
        if index in selected:
            continue
        q = normalize(r["sql"])
        reason = (
            "Seed bind values come from pinned CSV fixture data."
            if "%s" in q
            else "Adapter connection lifecycle, global catalog discovery, repeated materialization, or test cleanup outside the isolated selected scenarios."
        )
        omitted.append(
            {
                "event": index,
                "sha256": hashlib.sha256(r["sql"].encode()).hexdigest(),
                "reason": reason,
            }
        )
    (ROOT / "dbt-coverage.json").write_text(
        json.dumps(
            {
                "captured": len(raw),
                "selected_events": sorted(selected),
                "corpus_cases": sum(len(g["cases"]) for g in groups.values()),
                "omitted": omitted,
            },
            indent=2,
        )
        + "\n"
    )
    print(
        "Imported",
        sum(len(g["cases"]) for g in groups.values()),
        "dbt cases from",
        len(raw),
        "events",
    )


if __name__ == "__main__":
    rebuild()
