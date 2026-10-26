"""Matched native-driver versus PyArrow IPC export/import benchmarks."""

import argparse
import gc
import hashlib
import json
import os
import resource
import subprocess
import sys
import tempfile
import time
import uuid
from contextlib import contextmanager
from datetime import UTC, datetime, timedelta
from decimal import Decimal
from pathlib import Path

import adbc_driver_manager as adbc
import pyarrow as pa
from adbc_driver_redis import connect

BATCH_ROWS = 4096


def fixture(rows, width):
    ids = list(range(rows))
    columns = {
        "id": pa.array(ids, type=pa.int64()),
        "bucket": pa.array([i % 97 for i in ids], type=pa.int32()),
        "label": pa.array(
            [
                None
                if i % 11 == 0
                else f"r{i}:é:" + "x" * (i % 65) + ("\x00z" if i % 17 == 0 else "")
                for i in ids
            ],
            type=pa.string(),
        ),
    }
    if width == "wide":
        columns.update(
            {
                "amount": pa.array(
                    [
                        None if i % 13 == 0 else Decimal(i - rows // 2).scaleb(-4)
                        for i in ids
                    ],
                    type=pa.decimal128(20, 4),
                ),
                "at": pa.array(
                    [
                        datetime(2026, 1, 1, tzinfo=UTC)
                        + timedelta(microseconds=i * 1001)
                        for i in ids
                    ],
                    type=pa.timestamp("us", tz="UTC"),
                ),
                "score": pa.array(
                    [None if i % 19 == 0 else i / 8 for i in ids], type=pa.float64()
                ),
                "flag": pa.array(
                    [None if i % 7 == 0 else i % 2 == 0 for i in ids], type=pa.bool_()
                ),
                "payload": pa.array(
                    [
                        None
                        if i % 23 == 0
                        else i.to_bytes(8, "little") + b"\x00\xff" * (i % 17)
                        for i in ids
                    ],
                    type=pa.binary(),
                ),
            }
        )
        for c in range(24):
            columns[f"text_{c:02}"] = pa.array(
                [
                    None
                    if (i + c) % 29 == 0
                    else f"c{c}:{i % 1000}:" + "y" * (8 + 8 * (c % 4))
                    for i in ids
                ],
                type=pa.string(),
            )
    return pa.table(columns)


def sql(conn, query):
    with adbc.AdbcStatement(conn) as st:
        st.set_sql_query(query)
        return st.execute_update()


@contextmanager
def ipc_reader(path, format):
    with pa.OSFile(str(path), "rb") as source:
        if format == "stream":
            with pa.ipc.open_stream(source) as reader:
                yield reader
        else:
            file_reader = pa.ipc.open_file(source)
            batches = (
                file_reader.get_batch(i) for i in range(file_reader.num_record_batches)
            )
            with pa.RecordBatchReader.from_batches(
                file_reader.schema, batches
            ) as reader:
                yield reader


def writer(sink, schema, format):
    options = pa.ipc.IpcWriteOptions(compression=None, use_legacy_format=False)
    factory = pa.ipc.new_file if format == "file" else pa.ipc.new_stream
    return factory(sink, schema, options=options)


def rss_bytes():
    rss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    return rss if sys.platform == "darwin" else rss * 1024


def usage():
    u = resource.getrusage(resource.RUSAGE_SELF)
    return time.perf_counter(), u.ru_utime, u.ru_stime


def expected_table(case):
    with ipc_reader(case["reference"], "file") as reader:
        reference = reader.read_all()
    if case["workload"] == "union-wide":
        return pa.concat_tables([reference, reference])
    if case["workload"] == "aggregate-narrow":
        n = reference.num_rows
        buckets = list(range(min(97, n)))
        counts = [(n - 1 - b) // 97 + 1 for b in buckets]
        totals = [
            count * b + 97 * count * (count - 1) // 2
            for b, count in zip(buckets, counts, strict=True)
        ]
        # The benchmark SQL explicitly casts SUM to DECIMAL(38,0). Validate
        # both its exact numeric values and the declared Arrow type.
        return pa.table(
            {
                "bucket": pa.array(buckets, type=pa.int32()),
                "n": pa.array(counts, type=pa.int64()),
                "total": pa.array(
                    [Decimal(x) for x in totals], type=pa.decimal128(38, 0)
                ),
            }
        )
    return reference


def validate(reader, expected, aggregate=False, batch_bound=False):
    assert reader.schema.names == expected.schema.names, reader.schema
    if aggregate:
        assert reader.schema.field("bucket").type == pa.int32()
        assert reader.schema.field("n").type == pa.int64()
        assert pa.types.is_decimal(reader.schema.field("total").type)
    else:
        assert reader.schema.equals(expected.schema, check_metadata=False), (
            reader.schema,
            expected.schema,
        )
    at = batches = 0
    for batch in reader:
        if batch_bound:
            assert 0 < batch.num_rows <= BATCH_ROWS
        actual = pa.Table.from_batches([batch])
        want = expected.slice(at, batch.num_rows)
        if aggregate:
            assert actual.to_pylist() == want.to_pylist()
        else:
            assert actual.equals(want, check_metadata=False), (at, batch.num_rows)
        at += batch.num_rows
        batches += 1
    assert at == expected.num_rows, (at, expected.num_rows)
    return batches, hashlib.sha256(reader.schema.serialize().to_pybytes()).hexdigest()


def child(case_path):
    case = json.loads(Path(case_path).read_text())
    uri = os.environ["REDIS_BENCH_URI"]
    target = case.get("target")
    output = Path(case["output"]) if case["direction"] == "export" else None
    try:
        with (
            connect(uri, {"adbc.redis.query_workers": "1"}) as db,
            adbc.AdbcConnection(db) as conn,
        ):
            with adbc.AdbcStatement(conn) as st:
                st.set_options(
                    **{
                        "adbc.redis.stream_batch_rows": str(BATCH_ROWS),
                        "adbc.redis.aggregate_pushdown": "none",
                    }
                )
                if case["direction"] == "export":
                    st.set_sql_query(case["query"])
                    if case["method"] == "native":
                        st.set_options(
                            **{
                                "adbc.redis.ipc.output": str(output),
                                "adbc.redis.ipc.format": case["format"],
                            }
                        )
                else:
                    st.set_options(
                        **{
                            "adbc.ingest.target_table": target,
                            "adbc.ingest.mode": "adbc.ingest.mode.create",
                        }
                    )
                    if case["method"] == "native":
                        st.set_options(
                            **{
                                "adbc.redis.ipc.input": case["input"],
                                "adbc.redis.ipc.format": case["format"],
                            }
                        )
                baseline = rss_bytes()
                started, user0, sys0 = usage()
                if case["direction"] == "export":
                    if case["method"] == "native":
                        rows = st.execute_update()
                    else:
                        stream, _ = st.execute_query()
                        rows = 0
                        with (
                            pa.RecordBatchReader.from_stream(stream) as reader,
                            pa.OSFile(str(output), "wb") as sink,
                            writer(sink, reader.schema, case["format"]) as out,
                        ):
                            for batch in reader:
                                out.write_batch(batch)
                                rows += batch.num_rows
                elif case["method"] == "native":
                    rows = st.execute_update()
                else:
                    with ipc_reader(case["input"], case["format"]) as reader:
                        st.bind_stream(reader.__arrow_c_stream__())
                        rows = st.execute_update()
                ended, user1, sys1 = usage()
                peak = (
                    rss_bytes()
                )  # Capture BEFORE verification or statement/connection teardown.
                elapsed = ended - started
                file_bytes = (output if output else Path(case["input"])).stat().st_size
                result = {
                    k: case[k]
                    for k in [
                        "direction",
                        "workload",
                        "width",
                        "method",
                        "format",
                        "trial",
                        "warmup",
                        "sequence",
                    ]
                }
                result.update(
                    rows=rows,
                    seconds=elapsed,
                    user_cpu_seconds=user1 - user0,
                    system_cpu_seconds=sys1 - sys0,
                    cpu_seconds=user1 - user0 + sys1 - sys0,
                    baseline_rss_bytes=baseline,
                    peak_rss_bytes=peak,
                    file_bytes=file_bytes,
                    rows_per_second=rows / elapsed,
                    ipc_mib_per_second=file_bytes / 1048576 / elapsed,
                )
            # Verification is deliberately outside every reported operation metric.
            expected = expected_table(case)
            assert rows == expected.num_rows, (rows, expected.num_rows)
            if output:
                with ipc_reader(output, case["format"]) as reader:
                    batches, schema_hash = validate(
                        reader,
                        expected,
                        case["workload"] == "aggregate-narrow",
                        batch_bound=True,
                    )
            else:
                with adbc.AdbcStatement(conn) as verify:
                    verify.set_sql_query(f'SELECT * FROM "{target}"')
                    stream, _ = verify.execute_query()
                    with pa.RecordBatchReader.from_stream(stream) as reader:
                        batches, schema_hash = validate(reader, expected)
            result.update(
                validated=True, validation_batches=batches, schema_sha256=schema_hash
            )
            if target:
                sql(conn, f'DROP TABLE "{target}"')
                target = None
        print(json.dumps(result), flush=True)
    finally:
        if output:
            output.unlink(missing_ok=True)
        if target:
            with connect(uri) as db, adbc.AdbcConnection(db) as conn:
                sql(conn, f'DROP TABLE IF EXISTS "{target}"')


def run(args):
    args.output.mkdir(parents=True, exist_ok=True)
    results = []
    fixtures = {}
    tables = {}
    owned_targets = []
    env = {**os.environ, "REDIS_BENCH_URI": args.uri}
    with tempfile.TemporaryDirectory(prefix="redis-native-ipc-") as work_dir:
        work = Path(work_dir)
        with (
            connect(args.uri, {"adbc.redis.query_workers": "1"}) as db,
            adbc.AdbcConnection(db) as conn,
        ):
            try:
                for width in ["narrow", "wide"]:
                    data = fixture(args.rows, width)
                    name = f"it_ipc_bench_{uuid.uuid4().hex}"
                    tables[width] = name
                    for format in ["stream", "file"]:
                        path = work / f"{width}.{format}.arrow"
                        with (
                            pa.OSFile(str(path), "wb") as sink,
                            writer(sink, data.schema, format) as out,
                        ):
                            for batch in data.to_batches(max_chunksize=BATCH_ROWS):
                                out.write_batch(batch)
                        fixtures[(width, format)] = path
                    with adbc.AdbcStatement(conn) as st:
                        st.set_options(**{"adbc.ingest.target_table": name})
                        st.bind_stream(
                            data.to_reader(
                                max_chunksize=BATCH_ROWS
                            ).__arrow_c_stream__()
                        )
                        assert st.execute_update() == args.rows
                    del data
                    gc.collect()
                inputs = {
                    f"{w}-{f}": {
                        "bytes": p.stat().st_size,
                        "sha256": hashlib.sha256(p.read_bytes()).hexdigest(),
                    }
                    for (w, f), p in fixtures.items()
                }
                (args.output / "inputs.json").write_text(
                    json.dumps(inputs, indent=2) + "\n"
                )
                jobs = [
                    ("export", "scan-narrow", "narrow"),
                    ("export", "scan-wide", "wide"),
                    ("export", "union-wide", "wide"),
                    ("export", "aggregate-narrow", "narrow"),
                    ("import", "import-narrow", "narrow"),
                    ("import", "import-wide", "wide"),
                ]
                sequence = 0
                for job_index, (direction, workload, width) in enumerate(jobs):
                    table = tables[width]
                    if workload == "union-wide":
                        query = (
                            f'SELECT * FROM "{table}" UNION ALL SELECT * FROM "{table}"'
                        )
                    elif workload == "aggregate-narrow":
                        query = f'SELECT bucket,COUNT(*) AS n,CAST(SUM(id) AS DECIMAL(38,0)) AS total FROM "{table}" GROUP BY bucket ORDER BY bucket'
                    else:
                        query = f'SELECT * FROM "{table}"'
                    for format in ["stream", "file"]:
                        for trial in range(args.repeats + 1):
                            methods = (
                                ["native", "pyarrow"]
                                if (trial + job_index) % 2 == 0
                                else ["pyarrow", "native"]
                            )
                            for method in methods:
                                sequence += 1
                                target = (
                                    f"it_ipc_import_{uuid.uuid4().hex}"
                                    if direction == "import"
                                    else None
                                )
                                if target:
                                    owned_targets.append(target)
                                case = {
                                    "direction": direction,
                                    "workload": workload,
                                    "width": width,
                                    "format": format,
                                    "method": method,
                                    "trial": trial,
                                    "warmup": trial == 0,
                                    "sequence": sequence,
                                    "query": query,
                                    "target": target,
                                    "reference": str(fixtures[(width, "file")]),
                                    "input": str(fixtures[(width, format)]),
                                    "output": str(work / f"out-{sequence}.arrow"),
                                }
                                config = work / "case.json"
                                config.write_text(json.dumps(case))
                                completed = subprocess.run(
                                    [sys.executable, __file__, "--child", str(config)],
                                    env=env,
                                    capture_output=True,
                                    text=True,
                                    check=False,
                                )
                                if completed.returncode:
                                    raise RuntimeError(
                                        f"case {sequence} failed: {completed.stderr}\n{completed.stdout}"
                                    )
                                result = json.loads(completed.stdout)
                                results.append(result)
                                if target:
                                    owned_targets.remove(target)
                                (args.output / "results.json").write_text(
                                    json.dumps(results, indent=2) + "\n"
                                )
                                print(json.dumps(result), flush=True)
            finally:
                for target in [*owned_targets, *tables.values()]:
                    sql(conn, f'DROP TABLE IF EXISTS "{target}"')


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--uri")
    p.add_argument("--rows", type=int, default=50000)
    p.add_argument("--repeats", type=int, default=5)
    p.add_argument(
        "--output", type=Path, default=Path("benchmarks/native-ipc/2026-10-22")
    )
    p.add_argument("--child")
    args = p.parse_args()
    if args.child:
        child(args.child)
    else:
        if not args.uri or args.rows < 97 or args.repeats < 1:
            p.error("--uri, --rows >= 97 and --repeats >= 1 are required")
        run(args)


if __name__ == "__main__":
    main()
