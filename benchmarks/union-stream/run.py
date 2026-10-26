"""Fresh-process UNION ALL memory comparison with complete independent checks."""

import argparse
import json
import os
import resource
import subprocess
import sys
import time
import uuid
from pathlib import Path

import adbc_driver_manager as adbc
import pyarrow as pa
from adbc_driver_redis import connect


def rss_bytes():
    rss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    return rss if sys.platform == "darwin" else rss * 1024


def measure(args):
    with (
        connect(args.uri) as db,
        adbc.AdbcConnection(db) as conn,
        adbc.AdbcStatement(conn) as st,
    ):
        c = args.cardinality
        st.set_sql_query(
            f'SELECT id % {c} AS k,label FROM "{args.table}" UNION ALL SELECT id % {c} AS k,label FROM "{args.table}"'
        )
        st.set_options(
            **{
                "adbc.redis.stream_results": args.mode,
                "adbc.redis.stream_batch_rows": "1024",
            }
        )
        before = rss_bytes()
        started = time.monotonic()
        stream, reported = st.execute_query()
        returned = time.monotonic()
        count = batches = 0
        with pa.RecordBatchReader.from_stream(stream) as reader:
            assert reader.schema.names == ["k", "label"]
            assert reader.schema.field(0).type == pa.int64()
            assert reader.schema.field(1).type == pa.string()
            for batch in reader:
                if args.mode == "true":
                    assert 0 < batch.num_rows <= 1024
                keys = batch.column(0).to_pylist()
                labels = batch.column(1).to_pylist()
                for i, (key, label) in enumerate(zip(keys, labels, strict=True)):
                    assert key == ((count + i) % args.rows) % c
                    assert label == "x" * 128
                count += batch.num_rows
                batches += 1
        assert count == 2 * args.rows
        assert reported == (-1 if args.mode == "true" else count)
        print(
            json.dumps(
                {
                    "input_rows": args.rows,
                    "output_rows": count,
                    "cardinality": c,
                    "stream": args.mode == "true",
                    "validated": True,
                    "batches": batches,
                    "baseline_rss_bytes": before,
                    "peak_rss_bytes": rss_bytes(),
                    "execute_seconds": returned - started,
                    "complete_seconds": time.monotonic() - started,
                }
            )
        )


def run(args):
    results = []
    for rows in args.sizes:
        table = f"it_union_memory_{uuid.uuid4().hex}"
        with (
            connect(args.uri) as db,
            adbc.AdbcConnection(db) as conn,
            adbc.AdbcStatement(conn) as st,
        ):
            try:
                st.set_sql_query(
                    f"CREATE TABLE \"{table}\" AS SELECT CAST(g AS BIGINT) AS id, repeat('x',128) AS label FROM generate_series(0,{rows - 1}) AS s(g)"
                )
                st.execute_update()
                for cardinality in [1, rows]:
                    for trial in range(args.repeats):
                        modes = (
                            ["false", "true"] if trial % 2 == 0 else ["true", "false"]
                        )
                        for mode in modes:
                            command = [
                                sys.executable,
                                __file__,
                                "--child",
                                "--uri",
                                args.uri,
                                "--table",
                                table,
                                "--rows",
                                str(rows),
                                "--cardinality",
                                str(cardinality),
                                "--mode",
                                mode,
                            ]
                            completed = subprocess.run(
                                command,
                                check=True,
                                capture_output=True,
                                text=True,
                                env=os.environ,
                            )
                            result = json.loads(completed.stdout)
                            result["trial"] = trial + 1
                            results.append(result)
                            args.output.write_text(json.dumps(results, indent=2) + "\n")
                            print(json.dumps(result), flush=True)
            finally:
                st.set_sql_query(f'DROP TABLE IF EXISTS "{table}"')
                st.execute_update()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--uri", required=True)
    p.add_argument("--output", type=Path, default=Path("results.json"))
    p.add_argument("--sizes", type=int, nargs="+", default=[10000, 50000, 100000])
    p.add_argument("--repeats", type=int, default=3)
    p.add_argument("--child", action="store_true")
    p.add_argument("--table")
    p.add_argument("--rows", type=int)
    p.add_argument("--cardinality", type=int)
    p.add_argument("--mode", choices=["true", "false"])
    args = p.parse_args()
    measure(args) if args.child else run(args)


if __name__ == "__main__":
    main()
