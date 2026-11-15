#!/usr/bin/env python3
"""Audit complete helper-sequence grids; report no SQL speedup ratio."""
import csv
import gzip
import json
import math
from pathlib import Path
import statistics
import sys


def summarize(profile, output):
    cases = [c["name"] for c in profile["cases"]]
    expected = {(r, case, response) for r in range(profile["rounds"])
                for case in cases for response in ("valid", "ping")}
    samples = profile["samples"]
    actual = [(s["round"], s["case"], s["response"]) for s in samples]
    assert len(actual) == len(expected) and set(actual) == expected
    writer = csv.writer(output, lineterminator="\n")
    writer.writerow(["case", "response", "samples", "before_first_p50_ms",
                     "after_first_p50_ms", "after_first_p95_nearest_rank_ms",
                     "after_later_p50_ms", "after_socket_p50_ms",
                     "proof_plus_first_p50_ms", "complete_sequence_p50_ms",
                     "proof_reply_bytes_p50"])
    for case in cases:
        for response in ("valid", "ping"):
            rows = [s for s in samples if s["case"] == case and s["response"] == response]
            for row in rows:
                assert len(row["before_proof"]) == len(row["after_proof"]) == 3
                assert all(not r["failed"] for r in row["requests"])
                for call in row["before_proof"] + row["after_proof"]:
                    assert call["maintenance_main_pool_delta"]["wait_count"] == 0
                    assert call["maintenance_main_pool_delta"]["wait_milliseconds"] == 0
                for counters in row.get("proof_pool_deltas", {}).values():
                    assert counters["wait_count"] == counters["wait_milliseconds"] == 0
            after = sorted(r["after_proof"][0]["wall_milliseconds"] for r in rows)
            med = lambda values: f"{statistics.median(values):.3f}"
            optional = lambda key: med(r[key] for r in rows) if key in rows[0] else "not_recorded"
            writer.writerow([
                case, response, len(rows),
                med(r["before_proof"][0]["wall_milliseconds"] for r in rows),
                med(after), f"{after[math.ceil(len(after)*.95)-1]:.3f}",
                med(c["wall_milliseconds"] for r in rows for c in r["after_proof"][1:]),
                med(r["after_proof"][0]["connection_socket_read_milliseconds"] for r in rows),
                optional("proof_and_first_response_milliseconds"),
                optional("complete_sequence_milliseconds"),
                optional("proof_connection_resp_read_bytes"),
            ])


if __name__ == "__main__":
    path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).with_name("v2-profile.json.gz")
    summarize(json.loads(gzip.decompress(path.read_bytes()) if path.suffix == ".gz" else path.read_bytes()), sys.stdout)
