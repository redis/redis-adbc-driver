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
import json
import time
from pathlib import Path

import adbc_driver_manager.dbapi


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", type=Path, required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--preindexed-job")
    parser.add_argument("--root-before")
    args = parser.parse_args()
    if hashlib.sha256(args.driver.read_bytes()).hexdigest() != args.sha256:
        raise RuntimeError("older driver hash mismatch")
    initial = 9007199254740993
    with adbc_driver_manager.dbapi.connect(
        driver=str(args.driver.resolve()),
        db_kwargs={"uri": os.environ["REDIS_SNAPSHOT_URI"]},
        autocommit=True,
    ) as connection:
        if bool(args.preindexed_job) != bool(args.root_before):
            raise ValueError("preindexed handoff requires its initial root")
        if args.preindexed_job:
            import redis

            with redis.Redis.from_url(
                os.environ["REDIS_SNAPSHOT_URI"], protocol=3,
                ssl_ca_certs=os.environ["SSL_CERT_FILE"], decode_responses=True,
            ) as control:
                deadline = time.monotonic() + 20
                while True:
                    raw = control.hget("adbc:{meta}:snapshot:catalog", "compaction")
                    job = json.loads(raw) if raw else None
                    root = control.hget("adbc:{meta}:snapshot:catalog", "root")
                    if (job is None or job["id"] != args.preindexed_job) and root != args.root_before:
                        break
                    if time.monotonic() >= deadline:
                        raise AssertionError("older worker did not publish the preindexed job")
                    time.sleep(0.01)
        with connection.cursor() as cursor:
            for operation in range(12):
                cursor.execute("UPDATE t SET v=v+1 WHERE id=1 RETURNING id,v")
                baseline = 3 if args.preindexed_job else 6
                if cursor.fetchall() != [(1, initial + baseline + operation + 1)]:
                    raise AssertionError("old worker returned wrong or replayed values")
            cursor.execute("SELECT id,v FROM t ORDER BY id")
            expected = (
                [(i, initial + (15 if i == 1 else 3)) for i in range(1, 258)]
                if args.preindexed_job else
                [(i, initial + (18 if i == 1 else 0)) for i in range(1, 258) if i != 2]
            )
            if cursor.fetchall() != expected:
                raise AssertionError("old worker exposed a partial or resurrected result")
    count = 257 if args.preindexed_job else 256
    print(f"old_snapshot_worker_ok rows={count} writes=12 caller_retries=0")


if __name__ == "__main__":
    main()
