#!/usr/bin/env python3
"""Compare complete managed-tail baseline/candidate logs, retaining failures."""
import argparse
import collections
import gzip
import json
import math
from pathlib import Path
import re


def load(paths):
    trials, validated = [], set()
    for path in paths:
        text = Path(path).read_text()
        for line in text.splitlines():
            if "MANAGED_WRITER_TAIL_PROFILE " in line:
                trials.append(json.loads(line.split("MANAGED_WRITER_TAIL_PROFILE ", 1)[1]))
            m = re.search(r"MANAGED_WRITER_TAIL_VALIDATED trial=(-?\d+) writers=(\d+) writes=(\d+)", line)
            if m:
                validated.add(tuple(map(int, m.groups())))
    seen = collections.Counter((t["trial"], t["writers"]) for t in trials)
    expected = {(t, w) for t in (-1, 0, 1, 2) for w in (1, 4, 8)}
    valid = set(seen) == expected and all(n == 1 for n in seen.values())
    for trial in trials:
        count = trial["writers"] * trial["operations_per_writer"]
        trial["complete_final_oracle"] = (trial["trial"], trial["writers"], count) in validated
        trial["missing_operations"] = sum(s is None for s in trial["samples"])
        trial["failed_operations"] = sum(s is not None and s["error"] for s in trial["samples"])
        trial["missing_admission"] = sum(s is not None and not s["error"] and s["admission_ns"] <= 0 for s in trial["samples"])
        valid = valid and trial["complete_final_oracle"] and not any(trial[k] for k in ("missing_operations", "failed_operations", "missing_admission")) and len(trial["samples"]) == count
    return {"complete_validated_matrix": valid, "missing_trial_pairs": sorted(expected - set(seen)),
            "duplicate_trial_pairs": sorted(pair for pair, n in seen.items() if n > 1), "trials": trials}


def stats(values, scale=1):
    values = sorted(values)
    if not values:
        return {"n": 0, "p50": None, "p95": None, "max": None}
    return {"n": len(values), "p50": values[math.ceil(len(values) * .5) - 1] / scale,
            "p95": values[math.ceil(len(values) * .95) - 1] / scale, "max": values[-1] / scale}


def summarize(dataset):
    out = {"complete_validated_matrix": dataset["complete_validated_matrix"],
           "trial_outcomes": [{k: t[k] for k in ("trial", "writers", "warmup", "complete_final_oracle", "missing_operations", "failed_operations", "missing_admission")} for t in dataset["trials"]], "writers": {}}
    for writers in (1, 4, 8):
        trials = [t for t in dataset["trials"] if t["writers"] == writers and not t["warmup"]]
        samples = [s for t in trials for s in t["samples"] if s is not None]
        seconds = sum(t["elapsed_ns"] for t in trials) / 1e9
        commands = {role: sum(sum(t[role + "_command_counts"].values()) for t in trials) for role in ("foreground", "maintenance")}
        result = {"operations_per_second": len(samples) / seconds if seconds else None, "seconds": seconds,
                  "foreground_commands": commands["foreground"], "maintenance_commands": commands["maintenance"],
                  "attributed_foreground_client_calls_per_operation": sum(len(s["calls"]) for s in samples)/len(samples) if samples else None, "acquire_attempts_per_operation": sum(s["acquire_attempts"] for s in samples)/len(samples) if samples else None,
                  "commands_per_operation": {role: n / len(samples) if samples else None for role, n in commands.items()},
                  "trial_seconds": [{"trial": t["trial"], "seconds": t["elapsed_ns"] / 1e9} for t in trials], "operations": {}}
        for kind in ("insert", "update", "delete"):
            selected = [s for s in samples if s["kind"] == kind]
            result["operations"][kind] = {metric: stats([s[key] for s in selected], scale) for metric, key, scale in (
                ("latency_ms", "elapsed_ns", 1e6), ("admission_ms", "admission_ns", 1e6),
                ("admission_off_transport_ms", "admission_off_transport_ns", 1e6), ("acquire_attempts", "acquire_attempts", 1))}
        out["writers"][str(writers)] = result
    return out


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--baseline", nargs="+", required=True)
    p.add_argument("--candidate", nargs="+", required=True)
    p.add_argument("--output", type=Path, required=True)
    args = p.parse_args()
    datasets = {"baseline": load(args.baseline), "candidate": load(args.candidate)}
    write_counts = {mode: {t["writers"] * t["operations_per_writer"] for t in d["trials"]} for mode, d in datasets.items()}
    matched = write_counts["baseline"] == write_counts["candidate"] and len(write_counts["baseline"]) == 1
    raw = json.dumps(datasets, separators=(",", ":")).encode()
    assert all(s not in raw for s in (b"rediss://", b"snapshot:17:rows:", b"127.0.0.1", b"localhost")), "private identity"
    args.output.mkdir(parents=True, exist_ok=True)
    (args.output / "observations.json.gz").write_bytes(gzip.compress(raw, mtime=0))
    report = {"matched_total_writes_per_trial": matched, "complete_validated_comparison": matched and all(d["complete_validated_matrix"] for d in datasets.values()),
              "scope": "Go SQL execute/ack diagnostic, not native C-boundary performance or server CPU; all trial outcomes retained", **{mode: summarize(d) for mode, d in datasets.items()}}
    (args.output / "comparison.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({k: report[k] for k in ("matched_total_writes_per_trial", "complete_validated_comparison")}))
