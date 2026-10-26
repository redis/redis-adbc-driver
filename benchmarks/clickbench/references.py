"""Generate checked, typed DuckDB references without materializing the full dataset."""
import argparse
import hashlib
import json
from pathlib import Path

import duckdb
import pyarrow as pa
import pyarrow.parquet as pq

from run import FULL_ROWS, arrow_schema, commit, save


def file_sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as source:
        for chunk in iter(lambda: source.read(8 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def typed_source(con, parquet, schema):
    source = pq.ParquetFile(parquet).schema_arrow
    if set(source.names) != set(schema.names):
        raise ValueError("Parquet columns differ from the official ClickBench schema")
    con.from_parquet(str(parquet)).create_view("source_hits", replace=True)
    types = {pa.int64(): "BIGINT", pa.int32(): "INTEGER", pa.int16(): "SMALLINT",
             pa.string(): "VARCHAR", pa.date32(): "DATE", pa.timestamp("us"): "TIMESTAMP"}
    fields = []
    for field in schema:
        name = '"' + field.name.replace('"', '""') + '"'
        value = name
        original = source.field(field.name).type
        if pa.types.is_timestamp(field.type) and pa.types.is_integer(original):
            # Integer microseconds avoid timezone conversion and double rounding.
            value = f"make_timestamp(CAST({name} AS BIGINT) * 1000000)"
        elif pa.types.is_date(field.type) and pa.types.is_integer(original):
            value = f"DATE '1970-01-01' + CAST({name} AS INTEGER)"
        fields.append(f"CAST({value} AS {types[field.type]}) AS {name}")
    con.execute("CREATE OR REPLACE VIEW hits AS SELECT " + ",".join(fields) + " FROM source_hits")


def generate(con, queries, output, metadata):
    output.mkdir(parents=True, exist_ok=True)
    manifest = {"complete": False, "metadata": metadata, "query_count": len(queries)}
    save(output / "manifest.json", manifest)
    failures = []
    for number, sql in enumerate(queries, 1):
        reference = {"query": number, "sql": sql, "metadata": metadata}
        try:
            reference["rows"] = json.loads(json.dumps(con.execute(sql).fetchall(), default=str))
        except Exception as error:
            reference["error"] = type(error).__name__
            failures.append(number)
        save(output / f"q{number:02}.json", reference)
    manifest.update(complete=not failures, failed_queries=failures)
    save(output / "manifest.json", manifest)
    return not failures


def checked_reference(directory, number, sql, metadata):
    manifest = json.loads((directory / "manifest.json").read_text())
    if not manifest.get("complete") or manifest.get("query_count") != 43:
        raise ValueError("Reference generation incomplete")
    reference = json.loads((directory / f"q{number:02}.json").read_text())
    if reference.get("error") or not isinstance(reference.get("rows"), list) or not all(isinstance(r, list) for r in reference["rows"]):
        raise ValueError("Reference query failed or rows are malformed")
    if reference.get("query") != number or reference.get("sql") != sql:
        raise ValueError("Reference query identity differs")
    if manifest.get("metadata") != reference.get("metadata"):
        raise ValueError("Reference metadata differs from manifest")
    for key, value in metadata.items():
        if reference["metadata"].get(key) != value:
            raise ValueError(f"Reference {key} differs")
    return reference["rows"]


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--parquet", type=Path, required=True)
    ap.add_argument("--upstream", type=Path, required=True)
    ap.add_argument("--output", type=Path, required=True)
    ap.add_argument("--memory-limit", default="16GB")
    ap.add_argument("--temp-directory", type=Path, required=True)
    args = ap.parse_args()
    ddl = (args.upstream / "postgresql/create.sql").read_text()
    schema = arrow_schema(ddl)
    queries = [q.strip() for q in (args.upstream / "postgresql/queries.sql").read_text().splitlines() if q.strip()]
    if len(queries) != 43:
        ap.error("Expected 43 official queries")
    rows = pq.ParquetFile(args.parquet).metadata.num_rows
    if rows != FULL_ROWS:
        ap.error(f"Expected {FULL_ROWS} source rows, found {rows}")
    metadata = {"source_rows": rows, "loaded_rows": rows, "columns": len(schema),
                "source_sha256": file_sha256(args.parquet), "schema_sha256": hashlib.sha256(ddl.encode()).hexdigest(),
                "upstream_commit": commit(args.upstream), "engine": "DuckDB", "engine_version": duckdb.__version__,
                "normalization": "official epoch-day DATE and epoch-second TIMESTAMP"}
    args.temp_directory.mkdir(parents=True, exist_ok=True)
    with duckdb.connect() as con:
        con.execute("SET memory_limit = ?", [args.memory_limit])
        con.execute("SET temp_directory = ?", [str(args.temp_directory.resolve())])
        con.execute("SET TimeZone = 'UTC'")
        typed_source(con, args.parquet, schema)
        # Validate casts and required NULL constraints in one source scan.
        counts = con.execute("SELECT COUNT(*), " + ",".join(f'COUNT("{f.name}")' for f in schema) + " FROM hits").fetchone()
        if counts[0] != rows:
            raise ValueError("Typed source row count differs")
        for field, count in zip(schema, counts[1:]):
            if not field.nullable and count != rows:
                raise ValueError(f"NULL values in required column {field.name}")
        if not generate(con, queries, args.output, metadata):
            raise SystemExit("Oracle failures retained; references are incomplete")


if __name__ == "__main__":
    main()
