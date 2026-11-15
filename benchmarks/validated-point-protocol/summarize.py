#!/usr/bin/env python3
"""Summarize sanitized Go attribution; deliberately computes no speedup ratio."""
import collections
import csv
import json
import math
from pathlib import Path
import statistics
import sys

path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).with_name("A1.json")
profile = json.loads(path.read_text())
assert profile["clients"] == 1 and profile["caller_retries"] == 0
assert profile["overlay_segments"] == 3
samples = profile["samples"]
assert len(samples) == 7 * profile["samples_per_case"]
groups = collections.defaultdict(list)
for sample in samples:
    groups[sample["case"]].append(sample)
assert len(groups) == 7


def percentile(values, quantile):
    values = sorted(values)
    return values[math.ceil(len(values) * quantile) - 1]


writer = csv.writer(sys.stdout, lineterminator="\n")
writer.writerow(["case", "samples", "p50_ms", "p95_ms_nearest_rank", "rtt_min", "rtt_max", "commands_min", "commands_max", "control_p50_ms", "standalone_valid_mean_ms"])
for name, records in groups.items():
    assert sorted(x["sample"] for x in records) == list(range(profile["samples_per_case"]))
    trips, commands, valid_ms = [], [], []
    for row in records:
        phase_name = f'{name}_{row["sample"]:02d}'
        phase = profile["foreground_calls"][phase_name]
        assert phase["steps"] == 1 and phase["step_milliseconds"] == row["milliseconds"]
        trips.append(sum(v["calls"] for v in phase["round_trips"].values()))
        commands.append(sum(v["commands"] for v in phase["round_trips"].values()))
        valid = phase["round_trips"]["control_valid"]
        assert valid["calls"] == 3
        valid_ms.append(valid["milliseconds"])
        if "pipeline_duration_by_shape" in profile:
            shapes = profile["pipeline_duration_by_shape"][phase_name]
            assert {k: v["calls"] for k, v in shapes.items()} == phase["pipeline_shapes"]
            assert sum(v["commands"] for v in shapes.values()) == sum(v["commands"] for k, v in phase["round_trips"].items() if k.startswith("pipeline_"))
            direct = profile["standalone_calls_in_order"][phase_name]
            assert len(direct) == sum(v["calls"] for k, v in phase["round_trips"].items() if not k.startswith("pipeline_"))
    values = [x["milliseconds"] for x in records]
    writer.writerow([name, len(records), f"{statistics.median(values):.3f}", f"{percentile(values, .95):.3f}", min(trips), max(trips), min(commands), max(commands), f'{statistics.median(x["control_milliseconds"] for x in records):.3f}', f"{statistics.mean(valid_ms):.3f}"])
