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

"""Owned-fixture old-driver worker probe; never prints endpoints or row data."""
import argparse
import hashlib
import os
from pathlib import Path

import adbc_driver_manager.dbapi


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", type=Path, required=True)
    parser.add_argument("--sha256", required=True)
    args = parser.parse_args()
    if hashlib.sha256(args.driver.read_bytes()).hexdigest() != args.sha256:
        raise RuntimeError("older driver hash mismatch")
    initial = 9007199254740993
    with adbc_driver_manager.dbapi.connect(
        driver=str(args.driver.resolve()),
        db_kwargs={"uri": os.environ["REDIS_SNAPSHOT_URI"]},
        autocommit=True,
    ) as connection:
        with connection.cursor() as cursor:
            for operation in range(12):
                cursor.execute("UPDATE t SET v=v+1 WHERE id=1 RETURNING id,v")
                if cursor.fetchall() != [(1, initial + 6 + operation + 1)]:
                    raise AssertionError("old worker returned wrong or replayed values")
            cursor.execute("SELECT id,v FROM t ORDER BY id")
            expected = [
                (i, initial + (18 if i == 1 else 0))
                for i in range(1, 258) if i != 2
            ]
            if cursor.fetchall() != expected:
                raise AssertionError("old worker exposed a partial or resurrected result")
    print("old_snapshot_worker_ok rows=256 writes=12 caller_retries=0")


if __name__ == "__main__":
    main()
