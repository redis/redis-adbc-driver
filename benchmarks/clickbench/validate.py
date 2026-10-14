"""Check recorded query outputs against independent DuckDB reference values.

Full-scale reference JSONs may be supplied; pilot references are computed from
the same Arrow-decoded source prefix. Different LIMIT ties require independent
membership and rank-band checks.
"""
import argparse
import json
import hashlib
import math
from pathlib import Path

import duckdb
import pyarrow as pa
import pyarrow.parquet as pq

from run import FULL_ROWS, arrow_schema, convert_batch, save, commit
from references import checked_reference, file_sha256, typed_source
from ties import check_ties


def same_value(actual, expected):
    if isinstance(expected, float):
        try:
            return math.isclose(float(actual), expected, rel_tol=1e-10, abs_tol=1e-8)
        except (TypeError, ValueError):
            return False
    if isinstance(expected, int):
        return str(actual) == str(expected)
    return actual == expected


def same_rows(actual, expected):
    remaining = list(expected)
    for row in actual:
        for number, candidate in enumerate(remaining):
            if len(row) == len(candidate) and all(same_value(a, b) for a, b in zip(row, candidate)):
                remaining.pop(number)
                break
        else:
            return False
    return not remaining


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("run", type=Path)
    ap.add_argument("--parquet", type=Path, required=True)
    ap.add_argument("--upstream", type=Path, required=True)
    ap.add_argument("--reference-dir", type=Path)
    ap.add_argument("--tie-memory-limit", default="4GB")
    ap.add_argument("--tie-temp-directory", type=Path)
    args = ap.parse_args()
    run = json.loads(args.run.read_text())
    rows = run["metadata"]["loaded_rows"]
    if not rows:
        ap.error("No loaded rows to validate")
    if rows == FULL_ROWS and not args.reference_dir:
        ap.error("Full-scale validation requires independent full-data references")
    oracle = None
    if rows != FULL_ROWS:
        schema = arrow_schema((args.upstream / "postgresql/create.sql").read_text())
        batches, remaining = [], rows
        for batch in pq.ParquetFile(args.parquet).iter_batches(batch_size=10000):
            batch = batch.slice(0, min(batch.num_rows, remaining))
            batches.append(convert_batch(batch, schema))
            remaining -= batch.num_rows
            if remaining == 0:
                break
        oracle = duckdb.connect()
        oracle.register("hits", pa.Table.from_batches(batches))
    reference_meta = None
    if oracle is None:
        ddl = (args.upstream / "postgresql/create.sql").read_text()
        reference_meta = {"loaded_rows": rows, "source_rows": pq.ParquetFile(args.parquet).metadata.num_rows,
                          "columns": len(arrow_schema(ddl)), "upstream_commit": commit(args.upstream),
                          "source_sha256": file_sha256(args.parquet),
                          "schema_sha256": hashlib.sha256(ddl.encode()).hexdigest()}
        if reference_meta["upstream_commit"] != run["metadata"]["upstream_commit"]:
            ap.error("Run and reference upstream commits differ")
    tie_oracle = oracle
    checks = []
    for number, sql in enumerate(run["queries"], 1):
        if oracle:
            try:
                expected = json.loads(json.dumps(oracle.execute(sql).fetchall(), default=str))
            except Exception as error:
                checks.append({"query": number, "status": "reference_error", "type": type(error).__name__})
                continue
        else:
            try:
                expected = checked_reference(args.reference_dir, number, sql, reference_meta)
            except (OSError, ValueError, KeyError, TypeError) as error:
                checks.append({"query": number, "status": "reference_error", "type": type(error).__name__})
                continue
        for trial in range(1, 4):
            raw = args.run.parent / (args.run.stem + "-raw") / f"q{number:02}-{trial}.json"
            measured = json.loads(raw.read_text()) if raw.exists() else {}
            status = "failed"
            oracle_error = None
            method = None
            if measured.get("seconds") is not None:
                actual = measured.get("values", [])
                ordered_equal = len(actual) == len(expected) and all(
                    len(a) == len(e) and all(same_value(x, y) for x, y in zip(a, e))
                    for a, e in zip(actual, expected))
                if ordered_equal or ("ORDER BY" not in sql.upper() and same_rows(actual, expected)):
                    status = "valid"
                    method = "reference_rows"
                elif "LIMIT" in sql.upper() and len(actual) == len(expected):
                    status = "needs_tie_validation"
                    try:
                        if tie_oracle is None:
                            tie_oracle = duckdb.connect()
                            tie_oracle.execute("SET memory_limit = ?", [args.tie_memory_limit])
                            if args.tie_temp_directory:
                                args.tie_temp_directory.mkdir(parents=True, exist_ok=True)
                                tie_oracle.execute("SET temp_directory = ?", [str(args.tie_temp_directory.resolve())])
                            tie_oracle.execute("SET TimeZone = 'UTC'")
                            typed_source(tie_oracle, args.parquet, arrow_schema(ddl))
                        status = check_ties(tie_oracle, sql, actual, same_value)
                        method = "independent_limit_membership_and_ranks"
                    except Exception as error:
                        # Oracle failures cannot certify or condemn the trial.
                        status = "needs_tie_validation"
                        oracle_error = type(error).__name__
                else:
                    status = "mismatch"
                if status == "mismatch":
                    run["result"][number - 1][trial - 1] = None
            check = {"query": number, "trial": trial, "status": status}
            if raw.exists():
                check["raw_sha256"] = file_sha256(raw)
            if method:
                check["validation_method"] = method
            if oracle_error:
                check["oracle_error_type"] = oracle_error
            checks.append(check)
    run["metadata"]["correctness"] = {s:sum(c["status"] == s for c in checks)
        for s in sorted({c["status"] for c in checks})}
    save(args.run, run)
    save(args.run.with_name(args.run.stem + "-correctness.json"), {"checks": checks,
        "reference_engine": "DuckDB", "reference_engine_version": duckdb.__version__,
        "reference_metadata": reference_meta or {"loaded_rows": rows, "source": "Arrow-decoded prefix"}, "float_relative_tolerance": 1e-10,
        "float_absolute_tolerance": 1e-8})
    print(json.dumps(run["metadata"]["correctness"]))


if __name__ == "__main__":
    main()
