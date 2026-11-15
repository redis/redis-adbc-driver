#!/usr/bin/env python3
"""Summarize the bounded native first-valid diagnostic; no SQL speedup ratio."""
import csv
import json
from pathlib import Path
import statistics
import sys

profile = json.loads(Path(__file__).with_name("profile.json").read_text())
assert len(profile["samples"]) == 32 and profile["segments"] == 3
writer = csv.writer(sys.stdout, lineterminator="\n")
writer.writerow(["case", "side", "valid_position", "samples", "wall_p50_ms", "wall_max_ms", "socket_read_p50_ms", "pool_wait_total_ms", "resp_read_bytes", "resp_written_bytes"])
for case in dict.fromkeys(x["case"] for x in profile["samples"]):
    sequences = [x for x in profile["samples"] if x["case"] == case]
    assert sorted(x["round"] for x in sequences) == list(range(8))
    for side in ["before_proof", "after_proof"]:
        for position in range(3):
            calls = [x[side][position] for x in sequences]
            assert all(not request["failed"] for x in sequences for request in x["requests"])
            reads = sorted(set(x["connection_resp_read_bytes"] for x in calls))
            writes = sorted(set(x["connection_resp_written_bytes"] for x in calls))
            writer.writerow([case, side, position+1, len(calls), f'{statistics.median(x["wall_milliseconds"] for x in calls):.6f}', f'{max(x["wall_milliseconds"] for x in calls):.6f}', f'{statistics.median(x["connection_socket_read_milliseconds"] for x in calls):.6f}', f'{sum(x["maintenance_main_pool_delta"]["wait_milliseconds"] for x in calls):.6f}', ";".join(map(str, reads)), ";".join(map(str, writes))])
