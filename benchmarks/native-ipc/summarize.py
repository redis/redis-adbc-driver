"""Check a completed IPC matrix and emit reproducible comparison summaries."""

import argparse
import json
import statistics
from collections import Counter
from pathlib import Path


def summarize(folder):
    results = json.loads((folder / "results.json").read_text())
    manifest_path = folder / "manifest.json"
    manifest = (
        json.loads(manifest_path.read_text())
        if manifest_path.exists()
        else {
            "matrix": {
                "fresh_processes": 144,
                "export_workloads": [
                    "scan-narrow",
                    "scan-wide",
                    "union-wide",
                    "aggregate-narrow",
                ],
                "import_workloads": ["import-narrow", "import-wide"],
                "formats": ["stream", "file"],
                "methods": ["native", "pyarrow"],
            }
        }
    )
    assert len(results) == manifest["matrix"]["fresh_processes"] == 144
    assert all(row["validated"] for row in results)
    assert sorted(row["sequence"] for row in results) == list(range(1, 145))
    count = Counter(
        (r["direction"], r["workload"], r["format"], r["method"]) for r in results
    )
    expected_cases = {
        (direction, workload, format, method)
        for direction in ["export", "import"]
        for workload in manifest["matrix"][f"{direction}_workloads"]
        for format in manifest["matrix"]["formats"]
        for method in manifest["matrix"]["methods"]
    }
    assert set(count) == expected_cases and set(count.values()) == {6}
    metrics = [
        "seconds",
        "cpu_seconds",
        "user_cpu_seconds",
        "system_cpu_seconds",
        "peak_rss_bytes",
        "baseline_rss_bytes",
        "rows_per_second",
        "ipc_mib_per_second",
        "file_bytes",
    ]
    summary = []
    comparisons = []
    for key in sorted({(r["direction"], r["workload"], r["format"]) for r in results}):
        methods = {}
        for method in ["native", "pyarrow"]:
            rows = [
                r
                for r in results
                if (r["direction"], r["workload"], r["format"]) == key
                and r["method"] == method
            ]
            assert sum(r["warmup"] for r in rows) == 1
            measured = sorted(
                (r for r in rows if not r["warmup"]), key=lambda r: r["trial"]
            )
            assert [r["trial"] for r in measured] == [1, 2, 3, 4, 5]
            assert len({r["rows"] for r in rows}) == 1
            assert len({r["file_bytes"] for r in rows}) == 1
            assert len({r["schema_sha256"] for r in rows}) == 1
            assert len({r["validation_batches"] for r in rows}) == 1
            item = {
                "direction": key[0],
                "workload": key[1],
                "format": key[2],
                "method": method,
                "trials": 5,
                "rows": measured[0]["rows"],
                "validation_batches": measured[0]["validation_batches"],
                "schema_sha256": measured[0]["schema_sha256"],
            }
            for metric in metrics:
                values = [r[metric] for r in measured]
                item[metric] = {
                    "median": statistics.median(values),
                    "min": min(values),
                    "max": max(values),
                }
            summary.append(item)
            methods[method] = (item, measured)
        native, n_trials = methods["native"]
        pyarrow, p_trials = methods["pyarrow"]
        assert native["schema_sha256"] == pyarrow["schema_sha256"]
        assert native["validation_batches"] == pyarrow["validation_batches"]
        if key[0] == "import":
            assert native["file_bytes"] == pyarrow["file_bytes"]
        comparisons.append(
            {
                "direction": key[0],
                "workload": key[1],
                "format": key[2],
                "native_elapsed_change_percent": 100
                * (native["seconds"]["median"] / pyarrow["seconds"]["median"] - 1),
                "elapsed_ranges_overlap": max(
                    native["seconds"]["min"], pyarrow["seconds"]["min"]
                )
                <= min(native["seconds"]["max"], pyarrow["seconds"]["max"]),
                "paired_elapsed_ratios": [
                    n["seconds"] / p["seconds"]
                    for n, p in zip(n_trials, p_trials, strict=True)
                ],
                "encoded_byte_difference": native["file_bytes"]["median"]
                - pyarrow["file_bytes"]["median"],
            }
        )
    output = {
        "operations": len(results),
        "measured_operations": sum(not r["warmup"] for r in results),
        "warmups": sum(r["warmup"] for r in results),
        "validated_output_rows_including_warmups": sum(r["rows"] for r in results),
        "summaries": summary,
        "comparisons": comparisons,
    }
    (folder / "summary.json").write_text(json.dumps(output, indent=2) + "\n")
    lookup = {
        (x["direction"], x["workload"], x["format"], x["method"]): x for x in summary
    }
    lines = [
        "| Direction / workload | Format | Native seconds: median (range) | PyArrow seconds: median (range) | Native elapsed change |",
        "|---|---|---:|---:|---:|",
    ]
    for comparison in comparisons:
        key = tuple(comparison[k] for k in ["direction", "workload", "format"])
        values = []
        for method in ["native", "pyarrow"]:
            t = lookup[(*key, method)]["seconds"]
            values.append(f"{t['median']:.4f} ({t['min']:.4f}–{t['max']:.4f})")
        lines.append(
            f"| {key[0]} / {key[1]} | {key[2]} | {values[0]} | {values[1]} | {comparison['native_elapsed_change_percent']:+.1f}% |"
        )
    lines.extend(
        [
            "",
            f"Negative elapsed change means a lower native median; {sum(c['elapsed_ranges_overlap'] for c in comparisons)}/{len(comparisons)} elapsed ranges overlap.",
            "",
            "| Workload / format | Method | Output rows/s | IPC MiB/s | Client CPU seconds | Peak RSS MiB | Encoded MiB |",
            "|---|---|---:|---:|---:|---:|---:|",
        ]
    )
    for row in summary:
        lines.append(
            f"| {row['workload']} / {row['format']} | {row['method']} | {row['rows_per_second']['median']:,.0f} | {row['ipc_mib_per_second']['median']:.2f} | {row['cpu_seconds']['median']:.4f} | {row['peak_rss_bytes']['median'] / 1048576:.1f} | {row['file_bytes']['median'] / 1048576:.4f} |"
        )
    (folder / "tables.md").write_text("\n".join(lines) + "\n")
    print(
        f"{len(results)} validated operations, {output['measured_operations']} measured, {output['validated_output_rows_including_warmups']:,} output rows; schema/batch parity verified"
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("folder", type=Path)
    summarize(parser.parse_args().folder)
