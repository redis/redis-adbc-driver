#!/usr/bin/env python3
# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Compare independently validated reads and small writes on an owned Software fixture.

Run each driver revision in a separate process, with the same verified Software
fixture and parameters. This deliberately resets the database between revisions;
never point it at shared or production data. Read timings include execution, complete Arrow fetch and validation. Write timings
include publication and complete RETURNING fetch; independent post-write checks
are excluded from the timer. All samples require correct complete results. Setup
is excluded, and no credentials are emitted.
"""

import argparse
import hashlib
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
import pyarrow as pa


def write_cases(cur, repeats, logical):
    results = {}
    value = 1
    for name in ("metadata_comment", "point_update", "arrow_append"):
        samples = []
        for iteration in range(repeats + 1):
            if name == "arrow_append":
                next_id = 1000 + iteration
                batch = pa.table(
                    {
                        "id": pa.array([next_id], pa.int32()),
                        "g": pa.array([next_id % 64], pa.int32()),
                        "v": pa.array([next_id], pa.int32()),
                        "payload": ["payload"],
                    }
                )
            start = time.perf_counter()
            if name == "metadata_comment":
                cur.execute("COMMENT ON TABLE t IS 'metadata benchmark'")
            elif name == "point_update":
                value += 1
                cur.execute("UPDATE t SET v=v+1 WHERE id=1 RETURNING v")
                if cur.fetchall() != [(value,)]:
                    raise AssertionError(
                        "UPDATE returned an incorrect or replayed value"
                    )
                logical[0] = (1, 1, value, "payload")
            else:
                if cur.adbc_ingest("t", batch, mode="append") != 1:
                    raise AssertionError("append returned the wrong row count")
                logical.append((next_id, next_id % 64, next_id, "payload"))
            elapsed = time.perf_counter() - start
            # Writes have completed publication. Independent validation is
            # outside the write timer and must pass before retaining a sample.
            cur.execute("SELECT id,g,v,payload FROM t ORDER BY id")
            if cur.fetchall() != sorted(logical):
                raise AssertionError(f"{name}: independently calculated rows differ")
            if iteration:
                samples.append(elapsed * 1000)
        results[name] = {"median_ms": statistics.median(samples), "samples_ms": samples}
    return results


def wide_cases(cur, repeats, rows):
    """Wide exact-value reads over a baseline plus replacements/tombstones."""
    ids = list(range(1, rows + 1))
    labels = [None if i % 3 == 0 else f"Redis → {i}\x00value" for i in ids]
    data = {"id": pa.array(ids, pa.int64()), "label": pa.array(labels, pa.string())}
    for column in range(62):
        data[f"p{column}"] = pa.array([9007199254740993] * rows, pa.int64())
    if cur.adbc_ingest("wide", pa.table(data), mode="create") != rows:
        raise AssertionError("wide setup ingest returned an incorrect row count")
    cur.execute("UPDATE wide SET p0=NULL WHERE id%4=0")
    cur.execute("DELETE FROM wide WHERE id%7=0")
    visible = [i for i in ids if i % 7]
    expected = {
        "id": pa.array(visible, pa.int64()),
        "label": pa.array([labels[i - 1] for i in visible], pa.string()),
    }
    for column in range(62):
        expected[f"p{column}"] = pa.array(
            [None if column == 0 and i % 4 == 0 else 9007199254740993 for i in visible],
            pa.int64(),
        )
    complete = pa.table(expected)
    cases = [("wide_directory", "SELECT * FROM wide", complete)]
    selected = [j for j, i in enumerate(visible) if 100 <= i <= 200]
    cases.append(
        (
            "wide_candidates",
            "SELECT * FROM wide WHERE id>=100 AND id<=200",
            complete.take(pa.array(selected, pa.int64())),
        )
    )
    results = {}
    for name, sql, expected in cases:
        samples = []
        for iteration in range(repeats + 1):
            start = time.perf_counter()
            cur.execute(sql)
            actual = cur.fetch_arrow_table()
            if not actual.equals(expected, check_metadata=True):
                raise AssertionError(f"{name}: full typed Arrow result differs")
            elapsed = (time.perf_counter() - start) * 1000
            if iteration:
                samples.append(elapsed)
        results[name] = {
            "median_ms": statistics.median(samples),
            "samples_ms": samples,
            "output_rows": expected.num_rows,
        }
    return results, visible


def batch_write_cases(cur, repeats, rows, rewrites=False):
    results = {}
    expected = [(i, 9007199254740993) for i in range(1, rows + 1)]
    batch = pa.table({"id": pa.array(range(1, rows + 1), pa.int64()),
                      "v": pa.array([9007199254740993] * rows, pa.int64())})
    modes = ("update_batch", "delete_batch", "alter_batch") if rewrites else ("sql_insert_batch", "arrow_append_batch")
    for mode in modes:
        original = [(i, 9007199254740993) for i in range(1, rows + 1)]
        expected = ([(i, 9007199254740994) for i in range(1, rows + 1)] if mode == "update_batch"
                    else [] if mode == "delete_batch"
                    else [(i, "9007199254740993") for i in range(1, rows + 1)] if mode == "alter_batch"
                    else original)
        samples = []
        for iteration in range(repeats + 1):
            cur.execute("CREATE TABLE batch_write(id BIGINT,v BIGINT)")
            if rewrites:
                cur.execute(f"INSERT INTO batch_write SELECT i,9007199254740993 FROM generate_series(1,{rows}) AS g(i)")
            start = time.perf_counter()
            if mode == "sql_insert_batch":
                cur.execute(f"INSERT INTO batch_write SELECT i,9007199254740993 FROM generate_series(1,{rows}) AS g(i)")
                affected = cur.rowcount
            elif mode == "arrow_append_batch":
                affected = cur.adbc_ingest("batch_write", batch, mode="append")
            else:
                sql = {"update_batch": "UPDATE batch_write SET v=v+1",
                       "delete_batch": "DELETE FROM batch_write",
                       "alter_batch": "ALTER TABLE batch_write ALTER COLUMN v TYPE VARCHAR"}[mode]
                cur.execute(sql)
                affected = cur.rowcount
            elapsed = time.perf_counter() - start
            if mode != "alter_batch" and affected != rows:
                raise AssertionError(f"{mode}: incorrect affected row count")
            cur.execute("SELECT id,v FROM batch_write ORDER BY id")
            if cur.fetchall() != expected:
                raise AssertionError(f"{mode}: complete independent row validation failed")
            cur.execute("DROP TABLE batch_write")
            if iteration:
                samples.append(elapsed * 1000)
        results[mode] = {"median_ms": statistics.median(samples), "samples_ms": samples,
                         "output_rows": len(expected)}
    return results, expected


def foreign_import_cases(conn, cur, uri, ca, repeats, rows):
    """Time explicit recapture/publication; check every source and copied value."""
    with redis.Redis.from_url(uri, ssl_ca_certs=str(ca), decode_responses=True) as raw:
        expected = []
        with raw.pipeline(transaction=False) as pipe:
            for i in range(rows):
                key = f"app:import:{i:08d}"
                payload = f"Redis → {i}"
                pipe.hset(key, mapping={"n": str(i), "payload": payload})
                expected.append((key, i, payload))
                if (i + 1) % 256 == 0:
                    pipe.execute()
            pipe.execute()
        raw.execute_command("FT.CREATE", "app_import", "ON", "HASH", "PREFIX", 1,
                            "app:import:", "SCHEMA", "n", "NUMERIC", "payload", "TEXT")
        deadline = time.monotonic() + 60
        while True:
            info = raw.execute_command("FT.INFO", "app_import")
            if not isinstance(info, dict):
                info = dict(zip(info[::2], info[1::2]))
            if int(info["num_docs"]) == rows and not int(info.get("indexing", 0)):
                break
            if time.monotonic() >= deadline:
                raise RuntimeError("source index did not finish indexing")
            time.sleep(0.05)
        conn.adbc_connection.set_options(**{"adbc.redis.foreign_indexes": "true"})
        samples = []
        for iteration in range(repeats + 1):
            # Change the input outside the timer so a stale copy cannot pass.
            expected[0] = (expected[0][0], iteration + rows, expected[0][2])
            raw.hset(expected[0][0], "n", str(iteration + rows))
            start = time.perf_counter()
            conn.adbc_connection.set_options(**{"adbc.redis.foreign_indexes_refresh": "true"})
            elapsed = time.perf_counter() - start
            cur.execute("SELECT __key,n,payload FROM redis_foreign.app_import ORDER BY __key")
            if cur.fetchall() != expected:
                raise AssertionError("foreign refresh: complete copied rows differ")
            for start_at in range(0, rows, 256):
                page = expected[start_at:start_at + 256]
                with raw.pipeline(transaction=False) as pipe:
                    for key, _, _ in page:
                        pipe.hgetall(key)
                    actual = pipe.execute()
                if actual != [{"n": str(n), "payload": payload} for _, n, payload in page]:
                    raise AssertionError("foreign refresh modified application HASHes")
            if iteration:
                samples.append(elapsed * 1000)
    return {"foreign_refresh": {"median_ms": statistics.median(samples),
                                "samples_ms": samples, "output_rows": rows}}, expected


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
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument(
        "--wide", action="store_true", help="64-column exact-value read comparison"
    )
    modes.add_argument(
        "--writes",
        action="store_true",
        help="Small-write metadata benchmark; requires --rows=1",
    )
    modes.add_argument("--write-batches", action="store_true",
                       help="SQL insert and Arrow append batches with full post-write validation")
    modes.add_argument("--rewrite-batches", action="store_true",
                       help="UPDATE, DELETE and ALTER with complete post-write validation")
    modes.add_argument("--foreign-imports", action="store_true",
                       help="Explicit foreign refresh with complete source/copy validation")
    parser.add_argument("--unrelated-tables", type=int, default=32)
    parser.add_argument("--repeats", type=int, default=5)
    args = parser.parse_args()
    if (
        (args.rows != 1 if args.writes else not 128 <= args.rows <= 50000)
        or args.repeats < 1
        or args.unrelated_tables < 0
    ):
        parser.error(
            "require 128..50000 rows (1 for --writes), positive repeats, nonnegative table count"
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
            if args.foreign_imports:
                results, logical = foreign_import_cases(conn, cur, uri, ca, args.repeats, args.rows)
            elif args.write_batches or args.rewrite_batches:
                results, logical = batch_write_cases(cur, args.repeats, args.rows, args.rewrite_batches)
            elif args.wide:
                results, logical = wide_cases(cur, args.repeats, args.rows)
            else:
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
                if args.writes:
                    results = write_cases(cur, args.repeats, logical)
                else:
                    groups = {}
                    for i, group, value, _ in logical:
                        count, total = groups.get(group, (0, 0))
                        groups[group] = (count + 1, total + value)
                    expected_topk = [
                        (i, v, p)
                        for i, _, v, p in sorted(logical, key=lambda r: (-r[2], r[0]))[
                            5:15
                        ]
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

    # Identify the selected library's actual storage model after closing its
    # workers. The benchmark resets this owned database before each revision,
    # so a pre-snapshot baseline must not be labeled as a segmented catalog.
    with redis.Redis.from_url(uri, ssl_ca_certs=str(ca), decode_responses=True) as raw:
        catalog = "adbc:{meta}:snapshot:catalog"
        storage_format = raw.hget(catalog, "format")
        if storage_format is not None:
            if (
                not storage_format.isascii()
                or not storage_format.isdecimal()
                or not raw.hget(catalog, "root")
            ):
                raise RuntimeError("invalid snapshot benchmark catalog")
            storage_model, snapshot_protocol = "snapshot", int(storage_format)
        elif raw.exists(catalog):
            raise RuntimeError("snapshot benchmark catalog has no format")
        else:
            storage_model, snapshot_protocol = "mutable", None

    print(
        json.dumps(
            {
                "source": args.source,
                "driver_sha256": hashlib.sha256(args.driver.read_bytes()).hexdigest(),
                "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                "platform": platform.platform(),
                "machine": platform.machine(),
                "redis_version": server,
                "software_shards": database["shards_count"],
                "qpf": 4,
                "noeviction": True,
                "input_rows": args.rows,
                "visible_rows": len(logical),
                "storage_model": storage_model,
                "snapshot_protocol": snapshot_protocol,
                "initial_segments": None,
                "segment_count_note": (
                    "not sampled; maintenance enabled during workload"
                    if snapshot_protocol is not None else "not applicable to mutable storage"
                ),
                "unrelated_tables": 0 if args.foreign_imports or args.wide or args.write_batches or args.rewrite_batches else args.unrelated_tables,
                "wide_columns": 64 if args.wide else None,
                "repeats": args.repeats,
                "warmups": 1,
                "timing": (
                    "write through atomic publication; complete independent post-write validation excluded"
                    if args.foreign_imports or args.write_batches or args.rewrite_batches else
                    "write/publication + complete RETURNING fetch; independent post-write validation excluded"
                    if args.writes
                    else "execute + complete Arrow fetch + independent validation"
                ),
                "cases": results,
            },
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
