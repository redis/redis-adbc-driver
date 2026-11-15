#!/usr/bin/env python3
"""Summarize managed-tail Go test output without discarding failed trials."""
import argparse
import collections
import json
import math
import re


def percentile(values, p):
    values = sorted(values)
    return values[max(0, math.ceil(len(values) * p) - 1)] if values else None


def describe(values, scale=1):
    return {"n": len(values), "p50": percentile(values, .5) / scale if values else None,
            "p95": percentile(values, .95) / scale if values else None,
            "max": max(values) / scale if values else None}


def summarize(text):
    trials = []
    validated = set()
    for line in text.splitlines():
        marker = "MANAGED_WRITER_TAIL_PROFILE "
        if marker in line:
            trials.append(json.loads(line.split(marker, 1)[1]))
        match = re.search(r"MANAGED_WRITER_TAIL_VALIDATED trial=(-?\d+) writers=(\d+) writes=(\d+)", line)
        if match:
            validated.add(tuple(map(int, match.groups())))
    output = []
    seen = collections.Counter((t["trial"], t["writers"]) for t in trials)
    expected = {(trial, writers) for trial in (-1, 0, 1, 2) for writers in (1, 4, 8)}
    for trial in trials:
        samples = [s for s in trial["samples"] if s is not None]
        writes = trial["writers"] * trial["operations_per_writer"]
        complete = (trial["trial"], trial["writers"], writes) in validated
        report = {"trial": trial["trial"], "warmup": trial["warmup"], "writers": trial["writers"],
                  "expected_writes": writes, "observed_writes": len(samples), "validated": complete,
                  "failed_writes": sum(s["error"] for s in samples), "missing_admission_observations": sum(not s["error"] and s["admission_ns"] <= 0 for s in samples), "go_gc_cycles": trial["go_gc_cycles"],
                  "go_gc_pause_ring_complete": trial["go_gc_pause_ring_complete"],
                  "seconds": trial["elapsed_ns"] / 1e9, "operations": {}}
        for kind in ("insert", "update", "delete"):
            selected = [s for s in samples if s["kind"] == kind]
            result = {"latency_ms": describe([s["elapsed_ns"] for s in selected], 1e6),
                      "admission_ms": describe([s["admission_ns"] for s in selected], 1e6),
                      "admission_command_ms": describe([s["admission_command_ns"] for s in selected], 1e6),
                      "admission_off_transport_ms": describe([s["admission_off_transport_ns"] for s in selected], 1e6),
                      "acquire_attempts": describe([s["acquire_attempts"] for s in selected]),
                      "gc_completed_pause_ms": describe([s["go_gc_completed_pause_ns"] for s in selected], 1e6)}
            threshold = percentile([s["elapsed_ns"] for s in selected], .95)
            tail = []
            for sample in selected:
                if threshold is None or sample["elapsed_ns"] < threshold:
                    continue
                overlapping = collections.Counter()
                for call in trial["maintenance_calls"]:
                    overlap = min(call["start_ns"] + call["elapsed_ns"], sample["start_ns"] + sample["elapsed_ns"]) - max(call["start_ns"], sample["start_ns"])
                    if overlap > 0:
                        overlapping[call["category"]] += overlap / 1e6
                foreground = collections.Counter()
                for call in sample["calls"]:
                    foreground[call["category"]] += call["elapsed_ns"] / 1e6
                tail.append({"writer": sample["writer"], "operation": sample["operation"],
                             "latency_ms": sample["elapsed_ns"] / 1e6,
                             "admission_ms": sample["admission_ns"] / 1e6,
                             "foreground_command_ms": dict(foreground.most_common()),
                             "summed_overlapping_maintenance_client_ms": dict(overlapping.most_common())})
            result["p95_tail_samples"] = tail
            report["operations"][kind] = result
        output.append(report)
    return {"complete_validated_matrix": set(seen) == expected and all(n == 1 for n in seen.values()) and all(t["validated"] and t["observed_writes"] == t["expected_writes"] and t["failed_writes"] == 0 and t["missing_admission_observations"] == 0 for t in output), "complete_trial_grid": set(seen) == expected and all(n == 1 for n in seen.values()), "missing_trial_pairs": sorted(expected - set(seen)), "duplicate_trial_pairs": sorted(pair for pair, n in seen.items() if n > 1), "scope": "Diagnostic Go SQL client timing; maintenance overlap is correlation and concurrent client durations can sum above wall time. Warmup is retained and labeled. Missing final-state validation remains visible.", "trials": output}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log")
    args = parser.parse_args()
    with open(args.log) as source:
        print(json.dumps(summarize(source.read()), indent=2))
