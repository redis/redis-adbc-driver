#!/usr/bin/env python3
# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Compare complete, independently validated reads on an owned Software fixture.

Run each driver revision in a separate process, with the same verified Software
fixture and parameters. This deliberately resets the database between revisions;
never point it at shared or production data. Results exclude setup and include
execution, full Arrow fetch and result validation. No credentials are emitted.
"""

import argparse
import json
import os
import platform
import statistics
import sys
import time
from pathlib import Path
from urllib.parse import urlsplit

import adbc_driver_manager.dbapi
import redis


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", type=Path, required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--software-api-port", type=int, required=True)
    parser.add_argument(
        "--software-data-port",
        type=int,
        help="Published loopback proxy port for a Docker fixture",
    )
    parser.add_argument("--reset-owned-database", action="store_true", required=True)
    parser.add_argument("--rows", type=int, default=20000)
    parser.add_argument("--unrelated-tables", type=int, default=32)
    parser.add_argument("--repeats", type=int, default=5)
    args = parser.parse_args()
    if (
        args.rows < 128
        or args.rows > 50000
        or args.repeats < 1
        or args.unrelated_tables < 0
    ):
        parser.error(
            "require 128..50000 rows, positive repeats, nonnegative table count"
        )
    sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
    from software_partition_tls import SoftwareAPI, qpf_4x_ready

    ca = Path(os.environ["SSL_CERT_FILE"])
    api = SoftwareAPI(args.software_api_port, ca)
    databases = api.call("GET", "bdbs")
    # The repository's disposable fixture has one database with this name.
    if len(databases) != 1 or databases[0]["name"] != "adbc-test":
        raise RuntimeError("expected the owned single-database test fixture")
    database = databases[0]
    if database.get("eviction_policy") != "noeviction":
        raise RuntimeError("benchmark requires verified noeviction")
    effective = api.call(
        "GET",
        f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns",
    )
    if not qpf_4x_ready(effective):
        raise RuntimeError("benchmark requires active verified 4x QPF")
    uri = os.environ["REDIS_SNAPSHOT_URI"]
    if not uri.startswith("rediss://"):
        raise RuntimeError("owned Software fixture must use verified TLS")
    target = urlsplit(uri)
    if target.hostname not in ("localhost", "127.0.0.1") or target.port != (
        args.software_data_port or database["port"]
    ):
        raise RuntimeError(
            "data endpoint must match the owned local management fixture"
        )
    with redis.Redis.from_url(uri, ssl_ca_certs=str(ca), decode_responses=True) as raw:
        for index in raw.execute_command("FT._LIST"):
            raw.execute_command("FT.DROPINDEX", index)
        raw.flushdb()
        server = raw.info("server")["redis_version"]
    with adbc_driver_manager.dbapi.connect(
        driver=str(args.driver.resolve()), db_kwargs={"uri": uri}, autocommit=True
    ) as conn:
        with conn.cursor() as cur:
            cur.execute(
                "CREATE TABLE t(id INTEGER,g INTEGER,v INTEGER,payload VARCHAR)"
            )
            cur.execute(
                f"INSERT INTO t SELECT i,i%64,i,'payload' FROM generate_series(1,{args.rows}) AS x(i)"
            )
            cur.execute(f"UPDATE t SET v={args.rows} WHERE id=1")
            cur.execute("DELETE FROM t WHERE id=2")
            for i in range(args.unrelated_tables):
                cur.execute(f"CREATE TABLE unrelated_{i}(v INTEGER)")
            logical = [
                (i, i % 64, args.rows if i == 1 else i, "payload")
                for i in range(1, args.rows + 1)
                if i != 2
            ]
            groups = {}
            for i, group, value, _ in logical:
                count, total = groups.get(group, (0, 0))
                groups[group] = (count + 1, total + value)
            expected_topk = [
                (i, v, p)
                for i, _, v, p in sorted(logical, key=lambda r: (-r[2], r[0]))[5:15]
            ]
            cases = [
                ("selective", "SELECT id,v FROM t WHERE v=1", []),
                (
                    "matching_selective",
                    f"SELECT id,v FROM t WHERE v={args.rows} ORDER BY id",
                    [(1, args.rows), (args.rows, args.rows)],
                ),
                (
                    "grouped",
                    "SELECT g,COUNT(*),SUM(v) FROM t GROUP BY g ORDER BY g",
                    [(g, *groups[g]) for g in sorted(groups)],
                ),
                ("distinct", "SELECT COUNT(DISTINCT g) FROM t", [(64,)]),
                (
                    "topk",
                    "SELECT id,v,payload FROM t ORDER BY v DESC,id ASC LIMIT 10 OFFSET 5",
                    expected_topk,
                ),
                (
                    "topk_nulls_last",
                    "SELECT id,v,payload FROM t ORDER BY v DESC NULLS LAST,id ASC NULLS LAST LIMIT 10 OFFSET 5",
                    expected_topk,
                ),
                ("point", "SELECT v FROM t WHERE __rowid=1", [(args.rows,)]),
            ]
            results = {}
            for name, sql, expected in cases:
                samples = []
                for iteration in range(args.repeats + 1):
                    start = time.perf_counter()
                    cur.execute(sql)
                    table = cur.fetch_arrow_table()
                    columns = [col.to_pylist() for col in table.columns]
                    actual = list(zip(*columns))
                    if actual != expected:
                        raise AssertionError(
                            f"{name}: independently calculated results differ"
                        )
                    elapsed = time.perf_counter() - start
                    if iteration:
                        samples.append(elapsed * 1000)
                results[name] = {
                    "median_ms": statistics.median(samples),
                    "samples_ms": samples,
                    "output_rows": len(expected),
                }
    print(
        json.dumps(
            {
                "source": args.source,
                "platform": platform.platform(),
                "machine": platform.machine(),
                "redis_version": server,
                "software_shards": database["shards_count"],
                "qpf": 4,
                "noeviction": True,
                "input_rows": args.rows,
                "visible_rows": len(logical),
                "segments": 3,
                "unrelated_tables": args.unrelated_tables,
                "repeats": args.repeats,
                "warmups": 1,
                "timing": "execute + complete Arrow fetch + independent validation",
                "cases": results,
            },
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
