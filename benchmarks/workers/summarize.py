# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Rebuild summary.json and Markdown tables from a completed worker benchmark."""

import argparse
import json
from pathlib import Path
import statistics

WORKERS = ("1", "2", "4", "8", "auto")
LABELS = {
    "driver_avg": "Ungrouped `AVG(discount)`",
    "driver_reduce": "Ungrouped `SUM, AVG, MIN, MAX`",
    "driver_filtered": "Filtered ungrouped `SUM, AVG, MIN, MAX`",
    "index_count": "Indexed range `COUNT` (control)",
    "index_group": "Indexed `GROUP BY, COUNT, SUM` (control)",
    "driver_group": "Driver grouped `AVG(discount)` (control)",
    "full_scan": "Full scan, all columns (control)",
}


def summarize(data):
    if not data["complete"]:
        raise ValueError("Benchmark is incomplete")
    metadata, blocks = data["metadata"], data["blocks"]
    rounds, repeat = metadata["rounds"], metadata["repeat"]
    if len(blocks) != len(metadata["queries"]) * len(WORKERS) * rounds:
        raise ValueError("Unexpected block count")
    summary = {}
    for name in metadata["queries"]:
        summary[name] = {}
        for workers in WORKERS:
            cells = [b for b in blocks if b["query"] == name and b["workers"] == workers]
            if len(cells) != rounds or {b["round"] for b in cells} != set(range(1, rounds + 1)):
                raise ValueError("Missing or duplicated round")
            if any(len(b["samples"]) != repeat for b in cells):
                raise ValueError("Unexpected sample count")
            samples = [s for b in cells for s in b["samples"]]
            medians = [statistics.median(s["wall_ms"] for s in b["samples"]) for b in cells]
            summary[name][workers] = {
                "latency_ms": statistics.median(medians),
                "block_medians_ms": medians,
                "min_block_median_ms": min(medians), "max_block_median_ms": max(medians),
                "cpu_ms": statistics.median(statistics.median(s["cpu_ms"] for s in b["samples"]) for b in cells),
                "mean_client_cpu_cores": sum(s["cpu_ms"] for s in samples) / sum(s["wall_ms"] for s in samples),
                "median_process_peak_rss_mib": statistics.median(b["peak_rss_bytes"] for b in cells) / 2**20,
                "max_process_peak_rss_mib": max(b["peak_rss_bytes"] for b in cells) / 2**20,
                "grants": sorted({s["grant"] for s in samples}), "samples": len(samples),
            }
    return summary


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("directory", type=Path)
    args = ap.parse_args()
    data = json.loads((args.directory / "results.json").read_text())
    summary = summarize(data)
    (args.directory / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    lines = [f"| Query | Rows out | 1 worker (ms) | 2 workers (ms) | 4 workers (ms) | 8 workers (ms) | Auto ({data['metadata']['gomaxprocs']}) (ms) | Auto vs 1 |",
             "|---|---:|---:|---:|---:|---:|---:|---:|"]
    for name, cells in summary.items():
        values = " | ".join(f"{cells[w]['latency_ms']:.1f}" for w in WORKERS)
        ratio = cells["1"]["latency_ms"] / cells["auto"]["latency_ms"]
        speedup = f"{ratio:.2f}×" if data["metadata"]["queries"][name]["eligible"] else "—"
        lines.append(f"| {LABELS[name]} | {data['metadata']['queries'][name]['rows']:,} | {values} | {speedup} |")
    lines += ["", "| Query | Setting | Actual grant | CPU ms/query | Mean client CPU cores | Median process peak RSS (MiB) | Max process peak RSS (MiB) | Block-median latency range (ms) |",
              "|---|---:|---:|---:|---:|---:|---:|---:|"]
    for name, cells in summary.items():
        for workers, s in cells.items():
            grants = ", ".join(str(v) for v in s["grants"])
            lines.append(f"| {LABELS[name]} | {workers} | {grants} | {s['cpu_ms']:.1f} | {s['mean_client_cpu_cores']:.2f} | {s['median_process_peak_rss_mib']:.1f} | {s['max_process_peak_rss_mib']:.1f} | {s['min_block_median_ms']:.1f}–{s['max_block_median_ms']:.1f} |")
    (args.directory / "tables.md").write_text("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
