#!/usr/bin/env python3
"""Audit the four frozen width/count diagnostic grids, retaining regressions."""
import csv
import json
import math
from pathlib import Path
import statistics
import sys


def summarize(directory, output):
    runs = {}
    for name in ("A1", "B1", "B2", "A2"):
        profile = json.loads((directory / (name + ".json")).read_text())
        assert len(profile) == 2 and {p["columns"] for p in profile} == {3, 24}
        for p in profile:
            actual = [(r["round"], r["pending_probes"]) for r in p["samples"]]
            expected = {(round, n) for round in range(8) for n in (1, 2, 3, 8)}
            assert len(actual) == len(expected) and set(actual) == expected
            assert p["real_segments"] == 3 and p["caller_retries"] == 0
            for row in p["samples"]:
                assert row["proof_plus_valid_milliseconds"] > 0
                assert all(not call["failed"] for call in row["requests"])
                assert sum(call["shape"].count("ft.info") for call in row["requests"]) == row["pending_probes"]
                assert sum(call["shape"].count("zcard") for call in row["requests"]) == row["pending_probes"]
                assert sum(call["shape"] == "control_valid" for call in row["requests"]) == 1
        runs[name] = profile
    writer = csv.writer(output, lineterminator="\n")
    writer.writerow(["public_columns", "pending_probes", "run", "samples", "p50_ms", "p95_nearest_rank_ms", "response_bytes_p50", "helper_requests"])
    for width in (3, 24):
        for probes in (1, 2, 3, 8):
            for name, profiles in runs.items():
                p = next(p for p in profiles if p["columns"] == width)
                rows = [r for r in p["samples"] if r["pending_probes"] == probes]
                times = sorted(r["proof_plus_valid_milliseconds"] for r in rows)
                writer.writerow([width, probes, name, len(rows), f"{statistics.median(times):.3f}",
                                 f"{times[math.ceil(len(times)*.95)-1]:.3f}",
                                 f"{statistics.median(r['connection_resp_read_bytes'] for r in rows):.1f}",
                                 "/".join(str(v) for v in sorted({len(r["requests"]) for r in rows}))])


if __name__ == "__main__":
    summarize(Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).with_name("rejected-max2"), sys.stdout)
