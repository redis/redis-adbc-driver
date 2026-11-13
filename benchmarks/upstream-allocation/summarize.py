#!/usr/bin/env python3
"""Audit and summarize every completed allocation microbenchmark sample."""

import argparse
from collections import defaultdict
import json
from pathlib import Path
import re
import statistics


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("work", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    metadata = json.loads((args.work / "metadata.json").read_text())
    assert metadata["mode"] == "bench"
    assert all(command["exit_code"] == 0 for command in metadata["commands"])
    samples, hardware = [], {}
    suites = ("redis",) if metadata.get("suite") == "redis-owned" else ("redis", "arrow")
    expected = metadata.get("expected_cases", 28)
    assert expected == (12 if metadata.get("suite") == "redis-owned" else 28)
    for suite in suites:
        text = (args.work / (suite + "-bench.log")).read_text()
        for line in text.splitlines():
            for field in ("goos", "goarch", "cpu"):
                if line.startswith(field + ": "):
                    value = line.split(": ", 1)[1]
                    assert hardware.get(field, value) == value
                    hardware[field] = value
            if not line.startswith("Benchmark"):
                continue
            tokens = line.split()
            assert len(tokens) >= 8
            metrics = {tokens[i + 1]: float(tokens[i]) for i in range(2, len(tokens), 2)}
            assert {"ns/op", "B/op", "allocs/op"}.issubset(metrics)
            assert all(value >= 0 for value in metrics.values())
            samples.append({"suite": suite, "name": re.sub(r"-\d+$", "", tokens[0]), "iterations": int(tokens[1]), "metrics": metrics})
        assert "PASS" in text and "FAIL" not in text
    groups = defaultdict(list)
    for sample in samples:
        groups[(sample["suite"], sample["name"])].append(sample)
    assert len(groups) == expected, len(groups)
    count = metadata["count"]
    assert len(samples) == expected * count
    summary = []
    for (suite, name), group in sorted(groups.items()):
        assert len(group) == count, (name, len(group))
        summary.append({"suite": suite, "name": name, "samples": count,
                        "median": {unit: statistics.median(item["metrics"][unit] for item in group)
                                   for unit in group[0]["metrics"]}})
    result = {"metadata": metadata, "hardware": hardware, "samples": samples, "summary": summary}
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(f"audited {len(samples)} samples across {len(groups)} cases")


if __name__ == "__main__":
    main()
