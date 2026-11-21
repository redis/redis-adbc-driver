"""Exact bounded membership and independent canonical values for managed loads."""
from contextlib import contextmanager
from pathlib import Path
import tempfile
import threading
import time

import pyarrow as pa
import pyarrow.compute as pc
import pyarrow.parquet as pq


@contextmanager
def cancellation_deadline(cancel, deadline):
    """Cancel an executing/fetching statement even when a page has not returned.

    This relies on the driver's cooperative public cancellation contract. A
    hard process wall-clock bound for a broken C driver needs a supervisor.
    """
    if time.time() >= deadline:
        raise RuntimeError("Managed validation deadline reached")
    done = threading.Event()
    def watch():
        remaining = deadline - time.time()
        if not done.wait(max(0, remaining)):
            try:
                cancel()
            except Exception:
                pass  # The owner reports its execution/fetch outcome.
    watcher = threading.Thread(target=watch)
    watcher.start()
    try:
        yield
    finally:
        done.set()
        watcher.join()


def ascending_ids(batch, previous=None):
    """Check strict int64 ordering vectorially, retaining only the last ID."""
    if batch.num_columns != 1 or batch.column(0).type != pa.int64():
        raise ValueError("Expected one exact int64 native row-ID column")
    ids = batch.column(0)
    if ids.null_count:
        raise ValueError("NULL native row ID")
    if not len(ids):
        return True, previous
    first, last = ids[0].as_py(), ids[-1].as_py()
    if pc.any(pc.less_equal(ids, 0)).as_py():
        raise ValueError("Native row IDs must be positive")
    ordered = previous is None or first > previous
    if len(ids) > 1:
        ordered = ordered and pc.all(pc.greater(ids.slice(1), ids.slice(0, len(ids) - 1))).as_py()
    return bool(ordered), last


def validate_point_schema(actual, canonical):
    """Names/order and physical Arrow types are exact; query nullability may widen.

    Python numeric equality alone accepts Decimal/int, float/int and bool/int.
    Field metadata and nullability are not canonical query-value type identity.
    """
    expected_names = [field.name.lower() for field in canonical]
    if actual.names != expected_names or actual.types != canonical.types:
        raise ValueError("Native point result physical Arrow schema differs from canonical columns")


class IDSpool:
    """Rotate files so Parquet footer metadata is bounded as well as row data."""
    def __init__(self, directory, schema, file_rows=1 << 20):
        self.directory, self.schema, self.file_rows = directory, schema, file_rows
        self.writer, self.rows, self.number = None, 0, 0

    def __enter__(self):
        return self

    def write_batch(self, batch):
        if self.writer is None:
            self.writer = pq.ParquetWriter(self.directory / f"rowids-{self.number:06}.parquet", self.schema)
        self.writer.write_batch(batch)
        self.rows += batch.num_rows
        if self.rows >= self.file_rows:
            self.writer.close()
            self.writer, self.rows = None, 0
            self.number += 1

    def __exit__(self, *args):
        if self.writer is not None:
            self.writer.close()


def validate_managed_stream(cursor, path, metadata, schema, expected_rows, *,
                            batch_size=65536, memory_mib=256, max_temp_mib=4096,
                            timeout=1200, deadline=float("inf"), temp_parent=None):
    """Spool only IDs, prove unique membership, then point-fetch typed samples.

    Unordered SQL is valid. Sorted native directories use a linear fast path;
    otherwise DuckDB externally sorts the ID-only spool under explicit memory
    and spill limits. There is no table-sized DISTINCT hash in Go or Python.
    """
    import duckdb
    from references import typed_source

    if min(expected_rows, batch_size, memory_mib, max_temp_mib, timeout) <= 0:
        raise ValueError("Validation limits must be positive")
    deadline = min(deadline, time.time() + timeout)
    max_temp_bytes = max_temp_mib << 20
    ranks = sorted({0, expected_rows // 4, expected_rows // 2,
                    3 * expected_rows // 4, expected_rows - 1})
    sample_ids = []
    def budget(directory):
        if time.time() >= deadline:
            raise RuntimeError("Managed validation deadline reached")
        if sum(f.stat().st_size for f in directory.rglob("*") if f.is_file()) > max_temp_bytes:
            raise RuntimeError("Managed validation temporary disk budget exceeded")

    with tempfile.TemporaryDirectory(prefix="adbc-validation-", dir=temp_parent) as directory:
        directory = Path(directory)
        spool = directory / "rowids-*.parquet"
        count, previous, ordered = 0, None, True
        id_schema = pa.schema([("__rowid", pa.int64())])
        with cancellation_deadline(cursor.adbc_cancel, deadline):
            cursor.execute("SELECT __rowid FROM hits")
            with cursor.fetch_record_batch() as reader, IDSpool(directory, id_schema) as writer:
                for batch in reader:
                    # The driver streams bounded pages; further slice exported
                    # batches to cap spool row groups and vector working arrays.
                    for start in range(0, batch.num_rows, batch_size):
                        chunk = batch.slice(start, batch_size)
                        this_ordered, previous = ascending_ids(chunk, previous)
                        ordered = ordered and this_ordered
                        for rank in ranks:
                            if count <= rank < count + chunk.num_rows:
                                sample_ids.append(chunk.column(0)[rank - count].as_py())
                        writer.write_batch(pa.RecordBatch.from_arrays([chunk.column(0)], schema=id_schema))
                        count += chunk.num_rows
                        budget(directory)
        budget(directory)
        if count != expected_rows:
            raise ValueError(f"Native row count differs: expected {expected_rows}, got {count}")
        with duckdb.connect() as con:
            spill = directory / "spill"
            spill.mkdir()
            con.execute("SET memory_limit = ?", [f"{memory_mib}MiB"])
            con.execute("SET threads = 1")
            con.execute("SET temp_directory = ?", [str(spill)])
            remaining = max_temp_bytes - sum(f.stat().st_size for f in directory.glob("rowids-*.parquet"))
            if remaining <= 0:
                raise RuntimeError("Managed validation temporary disk budget exhausted")
            con.execute("SET max_temp_directory_size = ?", [f"{remaining}B"])
            con.execute("SET TimeZone = 'UTC'")
            with cancellation_deadline(con.interrupt, deadline):
                if not ordered:
                    relation = con.sql("SELECT __rowid FROM read_parquet(?) ORDER BY __rowid", params=[str(spool)])
                    previous, sorted_count = None, 0
                    with relation.to_arrow_reader(batch_size) as reader:
                        for batch in reader:
                            strict, previous = ascending_ids(batch, previous)
                            if not strict:
                                raise ValueError("Duplicate native row IDs")
                            sorted_count += batch.num_rows
                            budget(directory)
                    if sorted_count != count:
                        raise ValueError("External membership sort lost native IDs")
                typed_source(con, path, schema)
                # LIMIT stays below the oracle predicate for pilot prefixes;
                # full-scale uses the whole immutable source without that barrier.
                if expected_rows != metadata.num_rows:
                    con.execute(f"CREATE VIEW expected_hits AS SELECT * FROM hits LIMIT {expected_rows}")
                    oracle_view = "expected_hits"
                else:
                    oracle_view = "hits"
                for row_id in sample_ids:
                    budget(directory)
                    with cancellation_deadline(cursor.adbc_cancel, deadline):
                        cursor.execute(f"SELECT * FROM hits WHERE __rowid = {row_id}")
                        target = None
                        with cursor.fetch_record_batch() as reader:
                            validate_point_schema(reader.schema, schema)
                            for batch in reader:
                                if batch.num_rows:
                                    if target is not None or batch.num_rows != 1:
                                        raise ValueError("Native point lookup is not unique")
                                    row = batch.to_pylist()[0]
                                    target = tuple(row[f.name.lower()] for f in schema)
                    if target is None:
                        raise ValueError("Native sampled logical row ID disappeared")
                    watchid = target[schema.get_field_index("WatchID")]
                    relation = con.sql(f'SELECT * FROM {oracle_view} WHERE "WatchID" = ?', params=[watchid])
                    matched = False
                    with relation.to_arrow_reader(min(batch_size, 4096)) as reader:
                        for batch in reader:
                            # DuckDB's SQL casts, not ingestion's convert_batch,
                            # independently establish canonical date/time/int types.
                            for row in batch.to_pylist():
                                if tuple(row[f.name] for f in schema) == target:
                                    matched = True
                                    break
                            budget(directory)
                            if matched:
                                break
                    if not matched:
                        raise ValueError("Full typed canonical source sample differs after publication")
        budget(directory)
    return {"rows": count, "samples": len(sample_ids),
            "membership": "strict streamed IDs" if ordered else "external ID-only sort + strict streamed IDs",
            "memory_limit_mib": memory_mib, "temp_limit_mib": max_temp_mib,
            "oracle": "independent DuckDB typed_source SQL casts; destination logical-ID point samples"}
