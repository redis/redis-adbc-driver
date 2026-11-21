#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["redis>=6.4,<8"]
# ///
# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Native Search binary-key controls for #559; no SQL or ADBC dependency.

Use an owned, disposable Software RAM or actual Flex database after management
verification of active/effective 4x QPF. REDIS_URI and SSL_CERT_FILE stay private.
The script refuses existing app:* keys, creates only its enumerated fixture keys
and indexes, and cleans them up without DD. JSON output contains synthetic keys
as hex, counts and settings, never endpoint/authentication material.
"""
import itertools
import json
import os
import time

import redis


def field(mapping, key, default=None):
    return mapping.get(key, mapping.get(key.encode(), default))


def search(client, index, *args):
    reply = client.execute_command("FT.SEARCH", index, "*", *args, "DIALECT", 2)
    if not isinstance(reply, dict):
        raise RuntimeError("expected RESP3 Search map")
    if field(reply, "warning", []) or field(reply, "warnings", []):
        raise RuntimeError("native Search warning")
    return int(field(reply, "total_results")), {
        field(row, "id") for row in field(reply, "results", [])
    }


def ready(client, index):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        reply = client.execute_command("FT.INFO", index)
        if float(field(reply, "indexing", 1)) == 0:
            return
        time.sleep(.05)
    raise RuntimeError("initial indexing did not complete")


def matrix(client):
    # Both prefixes are used, even in the non-overlapping-index control.
    keys = {
        b"app:ascii": (1000, b"ascii", 1),
        b"app:a" + bytes([0]) + b"b": (1002, b"nul-key", 1),
        b"app:a" + bytes([255]) + b"b": (1004, b"non-utf8", 1),
        br"app:a\x00b": (1006, b"printable-escape", 1),
        b"app:sub:nested": (1008, b"nested", 1),
        b"app:excluded": (1010, b"excluded", 0),
    }
    assert any(0 in key for key in keys)
    if next(client.scan_iter(match=b"app:*", count=100), None) is not None:
        raise RuntimeError("refusing pre-existing app:* keys")
    index = "binary_key_membership_559"
    if index.encode() in client.execute_command("FT._LIST"):
        raise RuntimeError("refusing pre-existing fixture index")
    info = client.info("server")
    modules = [{"name": field(module, "name").decode("ascii"),
                "version": field(module, "ver")} for module in client.module_list()]
    print(json.dumps({"redis_version": info["redis_version"], "protocol": 3,
                      "modules": modules}))
    for initial, filtered, overlapping, ttl in itertools.product(
            [True, False], [True, False], [True, False], ["none", "before", "after"]):
        created = False
        owned = []
        try:
            def populate():
                for key, (n, text, active) in keys.items():
                    if client.exists(key):
                        raise RuntimeError("fixture key collision")
                    owned.append(key)
                    client.hset(key, mapping={b"n": n, b"v": 9007199254740993+n,
                        b"s": text, b"active": active, b"unknown": b"preserve"})
                    if ttl == "before":
                        client.pexpire(key, 3600000)
            if initial:
                populate()
            args = ["FT.CREATE", index, "ON", "HASH", "PREFIX"]
            args += [2, "app:", "app:sub:"] if overlapping else [1, "app:"]
            if filtered:
                args += ["FILTER", "@active==1"]
            args += ["SCHEMA", "n", "NUMERIC", "v", "NUMERIC", "s", "TAG", "active", "NUMERIC"]
            client.execute_command(*args)
            created = True
            if not initial:
                populate()
            ready(client, index)
            if ttl == "after":
                for key in keys:
                    client.pexpire(key, 3600000)
            before = {key: (client.dump(key), client.pexpiretime(key)) for key in keys}
            total, members = search(client, index, "NOCONTENT", "LIMIT", 0, 100)
            if len(members) != total:
                raise RuntimeError("incomplete native membership enumeration")
            observations = []
            for key, (_, _, active) in keys.items():
                count, _ = search(client, index, "INKEYS", 1, key, "LIMIT", 0, 0)
                exists = client.exists(key)
                if not exists or before[key] != (client.dump(key), client.pexpiretime(key)):
                    raise RuntimeError("source image/absolute TTL changed during controls")
                if count not in (0, 1):
                    raise RuntimeError("invalid singleton count")
                # Non-NUL controls must be ordinary native members. Record NUL
                # behavior without inventing membership or assuming its cause.
                expected = not filtered or active == 1
                if 0 not in key and (count != int(expected) or (key in members) != expected):
                    raise RuntimeError("non-NUL native control failed")
                observations.append({"key_hex": key.hex(), "exists": exists,
                    "full_member": key in members, "inkeys_count": count})
            print(json.dumps({"initial_scan": initial, "filtered": filtered,
                "overlapping_prefixes": overlapping, "ttl": ttl, "total": total,
                "rows": observations}), flush=True)
            # Separate deliberate expiry from the fixed-source membership oracle.
            for key in keys:
                client.pexpireat(key, 1)
            total, members = search(client, index, "NOCONTENT", "LIMIT", 0, 100)
            if total or members or any(client.exists(key) for key in keys):
                raise RuntimeError("expired source remains visible")
        finally:
            if created:
                client.execute_command("FT.DROPINDEX", index)
            if owned:
                client.delete(*owned)


def main():
    options = {"protocol": 3, "decode_responses": False, "socket_timeout": 30,
               "retry": redis.retry.Retry(redis.backoff.NoBackoff(), 0)}
    if os.environ["REDIS_URI"].startswith("rediss:"):
        options["ssl_ca_certs"] = os.environ["SSL_CERT_FILE"]
    with redis.Redis.from_url(os.environ["REDIS_URI"], **options) as client:
        matrix(client)


if __name__ == "__main__":
    main()
