"""Validate the opt-in issue-323 local profile against an independent DuckDB query.

This is a synthetic fixture validator, not the official full-scale validator.
"""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import re
import statistics

import duckdb
import pyarrow as pa

from validate import same_rows, same_value


def valid_boundary_ties(actual, expected, full, order_column):
    """Only these local fixtures: one descending numeric ORDER BY key.

    Check every complete output row, multiplicity, rank order, mandatory
    higher-ranked groups, and the boundary. Completion alone is insufficient.
    """
    if len(actual) != len(expected) or not expected:
        return actual == expected
    cutoff = expected[-1][order_column]
    remaining = list(full)
    ranks = []
    for row in actual:
        for i, candidate in enumerate(remaining):
            if len(row) == len(candidate) and all(same_value(a, b) for a, b in zip(row, candidate)):
                if candidate[order_column] < cutoff:
                    return False
                ranks.append(candidate[order_column])
                remaining.pop(i)
                break
        else:
            return False
    return ranks == sorted(ranks, reverse=True) and not any(row[order_column] > cutoff for row in remaining)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("directory", type=Path)
    args = ap.parse_args()
    source = args.directory / "fixture.jsonl"
    fixture = pa.Table.from_pylist([json.loads(line) for line in source.read_text().splitlines()])
    oracle = duckdb.connect()
    oracle.register("it_execution_profile", fixture)
    checks, groups = [], {}
    for path in sorted(args.directory.glob("q*-*-*.json")):
        if path.name.endswith(".execution.json"):
            continue
        record = json.loads(path.read_text())
        expected = oracle.execute(record["sql"]).fetchall()
        valid = same_rows(record["values"], expected)
        validation = "exact_rows"
        if record["query"] in {29, 32, 33, 36}:
            full = oracle.execute(re.sub(r"\s+LIMIT\s+\d+\s*;?\s*$", "", record["sql"], flags=re.I)).fetchall()
            order_column = {29: 1, 32: 2, 33: 2, 36: 4}[record["query"]]
            valid = valid_boundary_ties(record["values"], expected, full, order_column)
            validation = "complete_groups_and_rank_boundary"
        checks.append({"file": path.name, "valid": valid, "validation": validation})
        groups.setdefault((record["query"], record["strategy"]), []).append(record)
    summaries = []
    for (query, strategy), records in sorted(groups.items()):
        summaries.append({"query": query, "strategy": strategy, "trials": len(records),
                          **{field: statistics.median(r[field] for r in records) for field in
                             ["seconds", "process_cpu_seconds", "allocated_bytes", "connection_read_bytes",
                              "merge_seconds", "merge_cpu_seconds", "merge_allocated_bytes"]},
                          "spill_written_bytes": statistics.median(r["aggregate_metrics"].get("spill_written_bytes", 0) for r in records)})
    result = {"fixture_rows": fixture.num_rows, "fixture_sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
              "duckdb_version": duckdb.__version__, "platform": platform.platform(), "checks": checks,
              "all_valid": len(checks) == 36 and all(c["valid"] for c in checks), "medians": summaries}
    with (args.directory / "validation.json").open("x") as output:
        json.dump(result, output, indent=2)
        output.write("\n")
    print(json.dumps(result, indent=2))
    if not result["all_valid"]:
        raise SystemExit("Local profile validation failed or trials are missing")


if __name__ == "__main__":
    main()
