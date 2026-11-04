#!/usr/bin/env python3
"""Summarize public synthetic Go benchmark artifacts, never Redis diagnostics."""
import argparse
import json
import re
import statistics
from pathlib import Path


def summarize(directory):
    measurements = {}
    for filename, expected in (("kernels.txt", 72), ("conversion.txt", 108)):
        groups = {}
        for line in (directory / filename).read_text().splitlines():
            match = re.match(r"^(Benchmark\S+)-\d+\s+\d+\s+([\d.]+) ns/op.*?\s+(\d+) B/op\s+(\d+) allocs/op$", line)
            if match:
                groups.setdefault(match[1], []).append(tuple(map(float, match.groups()[1:])))
        if len(groups) != expected or any(len(trials) != 3 for trials in groups.values()):
            raise ValueError(f"Incomplete three-trial matrix in {filename}")
        measurements[filename.removesuffix(".txt")] = {
            name: dict(zip(("median_ns", "bytes_per_op", "allocs_per_op"),
                           (statistics.median(values) for values in zip(*trials))))
            for name, trials in groups.items()
        }
    cpu = (directory / "cpu.txt").read_text()
    return {
        "source": (directory / "source.txt").read_text().strip(),
        "toolchain": (directory / "toolchain.txt").read_text().strip(),
        "cpu": re.search(r"^Model name:\s*(.+)$", cpu, re.MULTILINE)[1].strip(),
        "logical_cpus": int(re.search(r"^CPU\(s\):\s*(\d+)$", cpu, re.MULTILINE)[1]),
        **measurements,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("artifact_root", type=Path)
    args = parser.parse_args()
    print(json.dumps({p.name: summarize(p) for p in sorted(args.artifact_root.iterdir()) if p.is_dir()}, indent=2))
