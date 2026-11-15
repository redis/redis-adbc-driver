#!/usr/bin/env python3
"""Audit the coordinator's independent held-connection RedisPy control."""
import csv
import json
import math
from pathlib import Path
import statistics
import sys


def summarize(profile, output):
    assert profile["complete"] and profile["caller_retries"] == 0
    expected = {(r, n, commands, route) for r in range(8)
                for n in (4096, 8000, 8191, 8192, 8193, 16384, 32768)
                for commands in (1, 3) for route in ("same_connection", "different_connection")}
    actual = [(r["repeat"], r["payload_bytes"], r["get_commands"], r["route"]) for r in profile["samples"]]
    assert len(actual) == len(expected) and set(actual) == expected
    writer = csv.writer(output, lineterminator="\n")
    writer.writerow(["payload_bytes", "get_commands", "route", "samples", "first_ping_p50_ms", "first_ping_p95_nearest_rank_ms", "later_ping_p50_ms", "get_plus_three_pings_p50_ms"])
    for n, commands, route in sorted({a[1:] for a in expected}):
        rows = [r for r in profile["samples"] if (r["payload_bytes"], r["get_commands"], r["route"]) == (n, commands, route)]
        assert all(len(r["ping_ns"]) == 3 and r["sequence_ns"] > 0 and all(v > 0 for v in r["ping_ns"]) for r in rows)
        first = sorted(r["ping_ns"][0] for r in rows)
        writer.writerow([n, commands, route, len(rows), f"{statistics.median(first)/1e6:.3f}",
                         f"{first[math.ceil(len(first)*.95)-1]/1e6:.3f}",
                         f"{statistics.median(v for r in rows for v in r['ping_ns'][1:])/1e6:.3f}",
                         f"{statistics.median(r['sequence_ns'] for r in rows)/1e6:.3f}"])


if __name__ == "__main__":
    path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).with_name("independent-redispy.json")
    summarize(json.loads(path.read_text()), sys.stdout)
