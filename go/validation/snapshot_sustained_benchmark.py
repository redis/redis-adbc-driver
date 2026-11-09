#!/usr/bin/env python3
# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Sustained native writes on a disposable, verified Software 4x QPF fixture.

No caller retries. Each timed interval contains only complete SQL/RETURNING
operations and local expected-value checks. Between intervals, fully validate
both tables and sample approximate native storage/backlog. Read-only probes can
observe different maintenance instants; they never repair or control the worker.
Do not run builds/tests/other benchmark processes against the fixture concurrently.
"""

import argparse
import hashlib
import json
import math
import os
import platform
from pathlib import Path
import statistics
import sys
import time
from urllib.parse import urlsplit

import adbc_driver_manager.dbapi
import redis

META = "adbc:{meta}:"
PREFIX = META + "snapshot:17:"
CATALOG = META + "snapshot:catalog"


def quantiles(samples):
    ordered = sorted(samples)
    return {"median_ms": statistics.median(samples),
            "p95_ms": ordered[math.ceil(.95 * len(samples)) - 1],
            "p99_ms": ordered[math.ceil(.99 * len(samples)) - 1],
            "max_ms": max(samples)}


def slot(suffix):
    value = 2166136261
    for byte in suffix.encode():
        value = ((value ^ byte) * 16777619) & 0xffffffff
    return f"{value & 255:02x}"


# This atomic, read-only observer samples the bounded catalog and the two known
# descriptors without racing retirement of a root between separate commands.
# It is test infrastructure, not a driver protocol or correctness proof.
OBSERVE = """
local result={segments={},works=0,pins=0,queued=0,buckets=0,values=0,data=0}
local root=redis.call('HGET',KEYS[1],'root')
if not root then return cjson.encode({storage_model='mutable'}) end
result.storage_model='snapshot'
for _,field in ipairs(redis.call('HKEYS',KEYS[1])) do
 if string.sub(field,1,2)=='w:' then result.works=result.works+1 end
 if string.sub(field,1,2)=='p:' then result.pins=result.pins+1 end
end
for i=1,#ARGV,2 do
 local bucket=redis.call('HGET',KEYS[2]..'metadata:root:'..root,ARGV[i])
 if not bucket then return redis.error_reply('observer descriptor bucket missing') end
 local owner=redis.call('HGET',KEYS[2]..'metadata:bucket:'..bucket..':'..ARGV[i],ARGV[i+1])
 if not owner then return redis.error_reply('observer descriptor owner missing') end
 local raw=redis.call('GET',KEYS[2]..'workspace:'..owner..':'..ARGV[i+1])
 if not raw then return redis.error_reply('observer descriptor missing') end
 local m=cjson.decode(raw)
 table.insert(result.segments,1+#(m.snapshot_segments or {}))
end
result.queued=redis.call('ZCARD',KEYS[2]..'compaction:queue')
result.buckets=redis.call('ZCARD',KEYS[2]..'metadata:gc:buckets')
result.values=redis.call('ZCARD',KEYS[2]..'metadata:gc:values')
result.data=redis.call('ZCARD',KEYS[2]..'metadata:gc:data')
local job=redis.call('HGET',KEYS[1],'compaction')
if job then
 local j=cjson.decode(job)
 result.phase=j.phase
 result.copy_rows=redis.call('ZCARD',KEYS[2]..'members:'..j.data)
end
return cjson.encode(result)
"""


def observe(raw, names, storage=False):
    started = time.perf_counter()
    args = []
    for name in names:
        suffix = "table:public:" + name
        args.extend((slot(suffix), suffix))
    result = json.loads(raw.eval(OBSERVE, 2, CATALOG, PREFIX, *args))
    if storage:
        # Approximate native allocations across the owned database while the
        # worker remains active. Deduplicate transient SCAN repeats. This is
        # neither allocator RSS nor a synchronized retained-memory snapshot.
        keys = set()
        for key in raw.scan_iter(count=256):
            keys.add(key)
            if len(keys) > 100000:
                raise RuntimeError("owned fixture exceeds storage observation budget")
        used = 0
        keys = sorted(keys)
        for first in range(0, len(keys), 256):
            with raw.pipeline(transaction=False) as pipe:
                for key in keys[first:first+256]:
                    pipe.memory_usage(key, samples=0)
                used += sum(value or 0 for value in pipe.execute())
        result["native_key_bytes_approx"] = used
        result["native_key_count_observed"] = len(keys)
        indexes = raw.execute_command("FT._LIST")
        index_fields = {}
        for index in indexes:
            # A worker may legitimately drop a retired index between LIST/INFO.
            try:
                info = raw.execute_command("FT.INFO", index)
            except redis.ResponseError as error:
                if ("unknown index" in str(error).lower() or "no such index" in str(error).lower()
                        or str(error).startswith("SEARCH_INDEX_NOT_FOUND ")):
                    continue
                raise
            if isinstance(info, list):
                info = dict(zip(info[::2], info[1::2]))
            for key, value in info.items():
                if str(key).endswith("_mb") and isinstance(value, (str, int, float)):
                    index_fields[key] = index_fields.get(key, 0.0) + float(value)
        result["native_index_count_observed"] = len(indexes)
        # Fields overlap; do not sum them into a supposed total.
        result["native_index_memory_mb_fields_approx"] = index_fields
    result["probe_ms"] = (time.perf_counter() - started) * 1000
    return result


def validate(cur, expected):
    for name, values in expected.items():
        cur.execute(f"SELECT id,v,payload FROM {name} ORDER BY id")
        actual = cur.fetchall()
        if actual != [(i, value, payload) for i, (value, payload) in sorted(values.items())]:
            raise AssertionError("complete independent row/value/multiplicity check failed")


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--driver", type=Path, required=True)
    p.add_argument("--source", required=True)
    p.add_argument("--software-api-port", type=int, required=True)
    p.add_argument("--software-data-port", type=int, required=True)
    p.add_argument("--reset-owned-database", action="store_true", required=True)
    p.add_argument("--workload", choices=("sparse", "mixed", "dense"), required=True)
    p.add_argument("--rows", type=int, default=20000)
    p.add_argument("--windows", type=int, default=4)
    p.add_argument("--window-seconds", type=float, default=20)
    p.add_argument("--drain-seconds", type=float, default=30)
    p.add_argument("--max-starts-per-second", type=float, default=0,
                   help="Explicit common offered-load ceiling; zero means unpaced")
    p.add_argument("--output", type=Path, required=True)
    a = p.parse_args()
    if not (128 <= a.rows <= 50000 and a.windows >= 2 and a.window_seconds >= 1
            and 1 <= a.drain_seconds <= 120
            and math.isfinite(a.max_starts_per_second) and 0 <= a.max_starts_per_second <= 10000):
        p.error("require 128..50000 rows, >=2 windows, >=1 second/window, 1..120s drain")
    sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
    from software_partition_tls import SoftwareAPI, qpf_4x_ready
    ca = Path(os.environ["SSL_CERT_FILE"])
    api = SoftwareAPI(a.software_api_port, ca)
    databases = api.call("GET", "bdbs")
    if len(databases) != 1 or databases[0]["name"] != "adbc-test":
        raise RuntimeError("requires the owned single-database Software fixture")
    database = databases[0]
    effective = api.call("GET", f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns")
    if not qpf_4x_ready(effective) or database.get("eviction_policy") != "noeviction":
        raise RuntimeError("requires verified effective 4x QPF and noeviction")
    uri = os.environ["REDIS_SNAPSHOT_URI"]
    target = urlsplit(uri)
    if target.scheme != "rediss" or target.hostname not in ("localhost", "127.0.0.1") or target.port != a.software_data_port:
        raise RuntimeError("requires the owned loopback TLS endpoint")
    names = ("sustained_a", "sustained_b")
    initial = 9007199254740993
    expected = {name: {i: (initial, None if i % 7 == 0 else f"row-{i}")
                       for i in range(1, a.rows+1)} for name in names}
    result = {"source": a.source, "driver_sha256": hashlib.sha256(a.driver.read_bytes()).hexdigest(),
              "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              "workload": a.workload, "rows_per_table": a.rows, "qpf": 4,
              "noeviction": True, "software_shards": database["shards_count"],
              "platform": platform.platform(), "caller_retries": 0,
              "max_statement_starts_per_second": a.max_starts_per_second,
              "timing": "complete SQL/publication/RETURNING; probes and full validation between windows excluded",
              "windows": [], "drain": [], "status": "started"}
    def save():
        a.output.write_text(json.dumps(result, indent=2) + "\n")
    save()
    try:
        with redis.Redis.from_url(uri, ssl_ca_certs=str(ca), decode_responses=True, protocol=3) as raw:
            for index in raw.execute_command("FT._LIST"):
                raw.execute_command("FT.DROPINDEX", index)
            raw.flushdb()
            result["redis_version"] = raw.info("server")["redis_version"]
            with adbc_driver_manager.dbapi.connect(driver=str(a.driver.resolve()), db_kwargs={"uri": uri}, autocommit=True) as conn:
                with conn.cursor() as cur:
                    for name in names:
                        cur.execute(f"CREATE TABLE {name}(id BIGINT,v BIGINT,payload VARCHAR)")
                        cur.execute(f"INSERT INTO {name} SELECT i,{initial},CASE WHEN i%7=0 THEN NULL ELSE 'row-'||i END FROM generate_series(1,{a.rows}) AS g(i)")
                    validate(cur, expected)
                    result["initial"] = observe(raw, names, storage=True)
                    operation = 0
                    load_started = time.perf_counter()
                    for window in range(a.windows):
                        samples = []
                        row_changes = 0
                        pacing_seconds = 0
                        next_start = 0
                        started = time.perf_counter()
                        while time.perf_counter()-started < a.window_seconds:
                            if a.max_starts_per_second and next_start:
                                wait = min(next_start-time.perf_counter(),
                                           a.window_seconds-(time.perf_counter()-started))
                                if wait > 0:
                                    before_wait = time.perf_counter()
                                    time.sleep(wait)
                                    pacing_seconds += time.perf_counter()-before_wait
                                if time.perf_counter()-started >= a.window_seconds:
                                    break
                            name = names[operation % 2]
                            values = expected[name]
                            # Mixed mode deletes then reinserts a rotating ID;
                            # it exercises retained tombstones and native NULLs.
                            row_id = 1 if a.workload == "sparse" else 1 + (operation//6) % a.rows
                            if a.workload == "mixed" and operation % 6 in (2, 3):
                                kind = "delete"
                                sql = f"DELETE FROM {name} WHERE id={row_id} RETURNING id,v"
                                want = [(row_id, values[row_id][0])]
                            elif a.workload == "mixed" and row_id not in values:
                                kind = "insert"
                                sql = f"INSERT INTO {name} VALUES ({row_id},{initial},NULL) RETURNING id,v"
                                want = [(row_id, initial)]
                            else:
                                kind = "update"
                                where = "" if a.workload == "dense" else f" WHERE id={row_id}"
                                sql = f"UPDATE {name} SET v=v+1{where} RETURNING id,v"
                                want = [(i, v+1) for i, (v, _) in values.items() if a.workload == "dense" or i == row_id]
                            tick = time.perf_counter()
                            if a.max_starts_per_second:
                                next_start = tick+1/a.max_starts_per_second
                            cur.execute(sql)
                            actual = cur.fetchall()
                            elapsed = (time.perf_counter()-tick)*1000
                            if sorted(actual) != sorted(want):
                                raise AssertionError("incorrect or replayed RETURNING rows")
                            if kind == "delete":
                                del values[row_id]
                            elif kind == "insert":
                                values[row_id] = (initial, None)
                            else:
                                for i, v in want:
                                    values[i] = (v, values[i][1])
                            operation += 1
                            row_changes += len(want)
                            samples.append(elapsed)
                        load_finished_at = time.perf_counter()
                        seconds = load_finished_at-started
                        # Observe pending work before validation/storage traffic
                        # can give the background worker time to catch up.
                        end_of_load = observe(raw, names)
                        check = time.perf_counter()
                        validate(cur, expected)
                        observation = observe(raw, names, storage=True)
                        result["windows"].append({"window": window, "operations": len(samples),
                            "seconds": seconds, "operations_per_second": len(samples)/seconds,
                            "logical_row_changes": row_changes,
                            "offered_load_pacing_seconds": pacing_seconds,
                            "start_seconds": started-load_started,
                            "end_seconds": load_finished_at-load_started,
                            "end_of_load_backlog": end_of_load,
                            "samples_ms": samples, **quantiles(samples), "observation": observation,
                            "validation_and_probe_seconds": time.perf_counter()-check})
                        save()
                    # Keep the connection's ordinary worker alive while waiting;
                    # never call private compaction/GC or retry SQL to drain it.
                    # Include final validation/storage observation time in the
                    # elapsed cleanup bound; never restart the clock afterward.
                    start = load_finished_at
                    while True:
                        observation = observe(raw, names)
                        elapsed = time.perf_counter()-start
                        result["drain"].append({"seconds": elapsed, **observation})
                        empty = observation["storage_model"] == "mutable" or (
                            not observation.get("phase") and not any(observation[k] for k in ("queued", "buckets", "values", "data"))
                            and observation["works"] == 1)
                        if empty or elapsed >= a.drain_seconds:
                            result["drain_complete"] = empty
                            result["drain_seconds"] = elapsed
                            break
                        time.sleep(.5)
                    validate(cur, expected)
                    result["final"] = observe(raw, names, storage=True)
                    result["validated_final_rows"] = sum(map(len, expected.values()))
            result["status"] = "passed"
    except BaseException as error:
        # Keep prior windows on failure without exporting exception text/SQL,
        # endpoints, physical identities, values or operational diagnostics.
        result["status"] = "failed"
        result["error_type"] = type(error).__name__
        save()
        raise
    save()
    print(json.dumps({"source": a.source, "workload": a.workload, "status": result["status"],
                      "operations": sum(w["operations"] for w in result["windows"]),
                      "drain_complete": result["drain_complete"]}))


if __name__ == "__main__":
    main()
