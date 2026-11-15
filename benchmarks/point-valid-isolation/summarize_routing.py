#!/usr/bin/env python3
"""Audit complete native routing grids and summarize every case; no SQL ratios."""
import csv
import gzip
import json
import math
from pathlib import Path
import statistics
import sys


def load(path):
    data = path.read_bytes()
    return json.loads(gzip.decompress(data) if path.suffix == ".gz" else data)


def audit(profile):
    rows = profile["samples"]
    if "cases" in profile:
        expected = {(r, c["name"], route) for r in range(profile["rounds"])
                    for c in profile["cases"] for route in profile["routes"]}
        actual = [(r["round"], r["case"], r["response_route"]) for r in rows]
        for row in rows:
            assert len(row["requests"]) == 7
            assert all(not r["failed"] for r in row["requests"])
            for key in ("before_proof", "after_proof"):
                assert len(row[key]) == 3
                assert all(math.isfinite(c["wall_milliseconds"]) and c["wall_milliseconds"] > 0
                           for c in row[key])
            assert row["complete_sequence_milliseconds"] >= row["proof_and_first_response_milliseconds"] > 0
    else:
        assert profile["validated"]
        policies = [p["policy"] for p in profile["pool_topologies"]]
        expected = {(policy, r, size, route) for policy in policies
                    for r in range(profile["rounds"])
                    for size in (4096, 8000, 8191, 8192, 8193, 16384, 32768)
                    for route in ("scalar_main", "scalar_proof", "pipeline_main", "pipeline_proof")}
        actual = [(r["pool_policy"], r["round"], r["payload_bytes"], r["ping_route"]) for r in rows]
        for row in rows:
            for key in ("before_get_milliseconds", "after_get_milliseconds"):
                assert len(row[key]) == 3
                assert all(math.isfinite(v) and v > 0 for v in row[key])
            assert row["get_plus_first_ping_milliseconds"] > 0
    assert len(actual) == len(expected) and set(actual) == expected
    assert profile["caller_retries"] == 0
    return rows


def summarize(profile, output):
    rows = audit(profile)
    routing = "cases" in profile
    if routing:
        keys = [(profile["pool_policy"], c["name"], route) for c in profile["cases"] for route in profile["routes"]]
    else:
        keys = sorted({(r["pool_policy"], str(r["payload_bytes"]), r["ping_route"]) for r in rows})
    writer = csv.writer(output, lineterminator="\n")
    writer.writerow(["pool_policy", "case_or_payload_bytes", "ping_route", "samples",
                     "before_first_p50_ms", "after_first_p50_ms", "after_first_p95_nearest_rank_ms",
                     "after_later_p50_ms", "proof_plus_first_p50_ms", "proof_resp_bytes_p50"])
    for policy, case, route in keys:
        if routing:
            selected = [r for r in rows if r["case"] == case and r["response_route"] == route]
            before = [r["before_proof"][0]["wall_milliseconds"] for r in selected]
            after = sorted(r["after_proof"][0]["wall_milliseconds"] for r in selected)
            later = [c["wall_milliseconds"] for r in selected for c in r["after_proof"][1:]]
            combined = [r["proof_and_first_response_milliseconds"] for r in selected]
            byte_width = f"{statistics.median(r['proof_connection_resp_read_bytes'] for r in selected):.1f}"
        else:
            selected = [r for r in rows if r["pool_policy"] == policy and str(r["payload_bytes"]) == case and r["ping_route"] == route]
            before = [r["before_get_milliseconds"][0] for r in selected]
            after = sorted(r["after_get_milliseconds"][0] for r in selected)
            later = [v for r in selected for v in r["after_get_milliseconds"][1:]]
            combined = [r["get_plus_first_ping_milliseconds"] for r in selected]
            byte_width = "not_instrumented"
        assert len(selected) == profile["rounds"]
        writer.writerow([policy, case, route, len(selected), f"{statistics.median(before):.3f}",
                         f"{statistics.median(after):.3f}", f"{after[math.ceil(len(after)*.95)-1]:.3f}",
                         f"{statistics.median(later):.3f}", f"{statistics.median(combined):.3f}", byte_width])


if __name__ == "__main__":
    summarize(load(Path(sys.argv[1])), sys.stdout)
