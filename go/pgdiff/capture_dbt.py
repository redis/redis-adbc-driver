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


"""Run pinned dbt adapter functional tests and retain their generated SQL logs.

uv run --no-project --python 3.12 --with dbt-postgres==1.9.1 --with dbt-core==1.9.1 \
  --with dbt-tests-adapter==1.9.1 --with pytest --with psycopg[binary] \
  --with adbc-driver-manager --with pyarrow --with sqlparse python go/pgdiff/capture_dbt.py
"""

import json
import os
import shutil
import subprocess
import tempfile
import urllib.request
import zipfile
from pathlib import Path
from urllib.parse import urlparse

from run import reference

ROOT = Path(__file__).resolve().parent
UTILS_COMMIT = "e164c645238ca98f21b2ecc558912b94b3d75a9f"


def main():
    with tempfile.TemporaryDirectory(prefix="pgdiff-dbt-") as work, reference() as uri:
        work = Path(work)
        archive = work / "utils.zip"
        with urllib.request.urlopen(
            f"https://codeload.github.com/dbt-labs/dbt-utils/zip/{UTILS_COMMIT}"
        ) as response:
            archive.write_bytes(response.read())
        with zipfile.ZipFile(archive) as z:
            z.extractall(work)
        utils = work / f"dbt-utils-{UTILS_COMMIT}"
        tests = work / "tests"
        shutil.copytree(ROOT / "capture", tests)
        url = urlparse(uri)
        env = {
            **os.environ,
            "POSTGRES_TEST_PORT": str(url.port),
            "POSTGRES_TEST_USER": "postgres",
            "POSTGRES_TEST_PASS": "pgdiff",
            "POSTGRES_TEST_DATABASE": "postgres",
            "PGDIFF_DBT_UTILS": str(utils),
            "DBT_LOG_FORMAT_FILE": "json",
        }
        subprocess.run(
            [str(Path(os.sys.executable)), "-m", "pytest", "-q", str(tests)],
            cwd=work,
            env=env,
            check=True,
        )
        records = []
        for path in sorted(work.rglob("dbt.log")):
            for line in path.read_text().splitlines():
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if event.get("info", {}).get("name") == "SQLQuery":
                    data = event["data"]
                    records.append(
                        {
                            "log": path.parent.name,
                            "sql": data["sql"],
                            "connection": data.get("conn_name"),
                        }
                    )
        if not records:
            raise RuntimeError("no SQLQuery events captured")
        target = ROOT / "upstream/dbt"
        target.mkdir(parents=True, exist_ok=True)
        (target / "sql.jsonl").write_text(
            "".join(json.dumps(r) + "\n" for r in records)
        )
        (target / "dbt-core-LICENSE.md").write_bytes(
            urllib.request.urlopen(
                "https://raw.githubusercontent.com/dbt-labs/dbt-core/v1.9.1/License.md"
            ).read()
        )
        versions = {}
        import importlib.metadata

        for package in ("dbt-core", "dbt-postgres", "dbt-tests-adapter"):
            versions[package] = importlib.metadata.version(package)
        from dbt.tests.adapter.basic import files

        (target / "base.csv").write_text(files.seeds_base_csv)
        (target / "added.csv").write_text(files.seeds_added_csv)
        for name in ("LICENSE", "LICENSE.md"):
            if (utils / name).exists():
                shutil.copyfile(utils / name, target / "dbt-utils-LICENSE")
        (target / "manifest.json").write_text(
            json.dumps(
                {
                    "versions": versions,
                    "dbt-utils": {"version": "1.3.0", "commit": UTILS_COMMIT},
                    "adapter_tests": [
                        "dbt.tests.adapter.basic.test_base.BaseSimpleMaterializations.test_base",
                        "dbt.tests.adapter.basic.test_incremental.BaseIncremental.test_incremental",
                    ],
                    "utils_test": "go/pgdiff/capture/test_generated.py:TestUtils.test_generated",
                    "source": "https://github.com/dbt-labs/dbt-core/tree/v1.9.1/tests/adapter/basic",
                    "record_count": len(records),
                },
                indent=2,
            )
            + "\n"
        )
        print("Captured", len(records), "SQLQuery events")


if __name__ == "__main__":
    main()
