"""Recompute the fixed historical SF1 report without modifying its evidence.

This deliberately does not discover result directories or include later retries.
Run with --check to check every published query row and summary against samples.
"""

import argparse
import json
import math
from pathlib import Path
import statistics


ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / "docs/performance/release-sf1-2026-11-23.json"
REPORT = ROOT / "PERFORMANCE.md"
SUITES = {"ssb": ("SSB", 13), "tpch": ("TPC-H (RAM)", 22), "tpcds": ("TPC-DS (Flex)", 99)}


def summarize(evidence):
    assert set(evidence["suites"]) == set(SUITES), "unexpected historical suite"
    output = {}
    for name, (_, expected) in SUITES.items():
        suite = evidence["suites"][name]
        assert suite["expected_queries"] == expected
        rows = suite["queries"]
        ids = [row["query"] for row in rows]
        assert len(set(ids)) == len(ids), "duplicate query"
        expected_ids = (
            [f"{a}.{b}" for a, n in [(1, 3), (2, 3), (3, 4), (4, 3)] for b in range(1, n + 1)]
            if name == "ssb" else [str(i) for i in range(1, expected + 1)]
        )
        assert ids == expected_ids[:len(ids)], "unexpected query coverage/order"
        counts = {kind: 0 for kind in ("validated", "timeout", "process_failure", "interrupted", "other_failure")}
        valid = []
        query_lines = []
        for row in rows:
            counts[row["outcome"]] += 1
            for engine in ("duckdb", "redis"):
                assert all(math.isfinite(x) and x > 0 for x in row[engine + "_seconds"])
            duck = statistics.median(row["duckdb_seconds"]) if len(row["duckdb_seconds"]) == 3 else None
            redis = None
            if row["outcome"] == "validated":
                assert duck is not None and len(row["redis_seconds"]) == 3
                redis = statistics.median(row["redis_seconds"])
                valid.append((duck, redis))
            status = {"validated": "validated", "timeout": "timeout", "process_failure": "process failure",
                      "interrupted": "interrupted by user", "other_failure": "other failure"}[row["outcome"]]
            if "issue_url" in row:
                url = row["issue_url"]
                assert url.startswith("https://github.com/redis/redis-adbc-driver/issues/")
                status += f" ([#{int(url.rsplit('/', 1)[1])}]({url}))"
            ms = lambda value: "—" if value is None else f"{value * 1000:.3f}"
            ratio = "—" if redis is None else f"{redis / duck:.2f}×"
            query_lines.append(f"| {row['query']} | {ms(duck)} | {ms(redis)} | {ratio} | {status} |")
        for query in expected_ids[len(ids):]:
            query_lines.append(f"| {query} | — | — | — | not run |")
        d = sum(a for a, _ in valid)
        r = sum(b for _, b in valid)
        output[name] = {"expected": expected, "attempted": len(rows), "counts": counts,
                        "not_started": expected - len(rows), "duckdb_total_seconds": d,
                        "redis_total_seconds": r,
                        "redis_over_duckdb_geomean": math.exp(statistics.mean(math.log(b / a) for a, b in valid)),
                        "query_lines": query_lines}
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="verify published Markdown; never write files")
    args = parser.parse_args()
    result = summarize(json.loads(EVIDENCE.read_text()))
    report = REPORT.read_text() if args.check else ""
    for name, (display, _) in SUITES.items():
        stats = result[name]
        counts = stats["counts"]
        mode = "Software Flex (experimental)" if name == "tpcds" else "Software RAM"
        summary_line = (f"| {display} | {mode} | {counts['validated']}/{stats['expected']} | "
                        f"{counts['timeout']} | {counts['process_failure']} | {counts['other_failure']} | "
                        f"{counts['interrupted']} / {stats['not_started']} | {stats['redis_over_duckdb_geomean']:.2f}× |")
        total_line = (f"| {display} | {stats['duckdb_total_seconds']:.6f} | {stats['redis_total_seconds']:.6f} | "
                      f"{stats['redis_total_seconds']/stats['duckdb_total_seconds']:.2f}× |")
        if args.check:
            for line in [summary_line, total_line, *stats["query_lines"]]:
                assert line in report, f"report differs from samples: {line}"
        del stats["query_lines"]
    print(json.dumps(result, indent=2))
    if args.check:
        print("Historical summary and all expected query rows match; evidence unchanged.")


if __name__ == "__main__":
    main()
