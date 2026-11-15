#!/usr/bin/env python3
"""Audit and summarize the complete opt-in unmanaged sparse retrieval matrix."""
import argparse
import collections
import json
import math
import statistics

SHAPES = ("dense", "sparse90", "all_null", "sparse90_wide")
MODES = ("native_hgetall", "projected_fields")


def audit(records):
    expected = {(shape, mode, clients, repeat) for shape in SHAPES for mode in MODES
                for clients in (1, 4) for repeat in (-1, 0, 1, 2)}
    seen = set()
    groups = collections.defaultdict(list)
    for row in records:
        identity = (row["shape"], row["mode"], row["clients"], row["repeat"])
        if identity not in expected or identity in seen:
            raise ValueError(f"unexpected or duplicate record {identity}")
        seen.add(identity)
        shape, mode, clients, repeat = identity
        non_null = 256 if shape == "dense" else 0 if shape == "all_null" else 26
        checks = {"validated": True, "warmup": repeat < 0, "operations": 8,
                  "hash_rows": 256, "non_null_selected_rows": non_null,
                  "wrong_type_keys": 1, "missing_keys": 1,
                  "wide_unselected_bytes": 16384 if shape == "sparse90_wide" else 0}
        if any(type(row.get(key)) is not type(value) or row.get(key) != value
               for key, value in checks.items()):
            raise ValueError(f"fixture or validation differs {identity}")
        samples = row["latency_ns"]
        if len(samples) != 8 or any(type(x) is not int or x <= 0 for x in samples):
            raise ValueError(f"invalid latency samples {identity}")
        if type(row["seconds"]) not in (float, int) or not math.isfinite(row["seconds"]) or row["seconds"] <= 0:
            raise ValueError(f"invalid wall time {identity}")
        want_hmget = 8 * 258 if mode == "projected_fields" else 0
        want_hgetall = 8 * (257 - non_null) if mode == "projected_fields" else 8 * 258
        if row["commands"].get("hmget", 0) != want_hmget or row["commands"].get("hgetall", 0) != want_hgetall:
            raise ValueError(f"source command counts differ {identity}")
        if not row["warmup"]:
            groups[(shape, clients, mode)].extend(samples)
    if seen != expected:
        raise ValueError(f"incomplete matrix: missing {len(expected - seen)} records")
    return [{"shape": shape, "clients": clients, "mode": mode, "samples": len(values),
             "p50_ms": statistics.median(values) / 1e6,
             "p95_ms": sorted(values)[math.ceil(.95 * len(values)) - 1] / 1e6}
            for (shape, clients, mode), values in sorted(groups.items())]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("records", help="JSONL, retaining all warmup and measured records")
    args = parser.parse_args()
    with open(args.records) as source:
        records = [json.loads(line) for line in source if line.strip()]
    print(json.dumps({"records": len(records), "total_reads": 512, "measured_reads": 384,
                      "scope": "native source retrieval only; no SQL/Search/Arrow/C bridge; finite trial timings, not capacity or server CPU",
                      "summary": audit(records)}, indent=2))


if __name__ == "__main__":
    main()
