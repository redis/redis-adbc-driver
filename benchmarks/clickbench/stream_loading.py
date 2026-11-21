"""Bounded row-group sources and non-replaying long managed appends.

The file metadata is immutable and shared; file readers and ADBC connections are
worker-owned. No producer queue, allocator purge or retry is used here.
"""
from concurrent.futures import ThreadPoolExecutor, wait, FIRST_COMPLETED
from contextlib import closing
import threading
import resource
import sys
import time

import pyarrow as pa
import pyarrow.parquet as pq


def row_group_assignments(metadata, rows, workers, retained=0):
    """Partition a source prefix into disjoint (group, start, length) slices."""
    if not 1 <= workers <= 4 or not 0 <= retained <= rows <= metadata.num_rows:
        raise ValueError("Expected 1..4 workers and a valid source prefix")
    assignments = [[] for _ in range(workers)]
    offset = 0
    for group in range(metadata.num_row_groups):
        size = metadata.row_group(group).num_rows
        start, end = max(retained, offset), min(rows, offset + size)
        if start < end:
            assignments[group % workers].append((group, start - offset, end - start))
        offset += size
        if offset >= rows:
            break
    return assignments


def bounded_batches(path, metadata, assignment, batch_size, convert, *, opener=pq.ParquetFile):
    """Keep one current row-group reader; exported Arrow buffers own their data.

    pre_buffer=False is explicit, including on Arrow versions that enable it by
    default. The metadata-only reader must have been closed by the coordinator.
    """
    if batch_size <= 0:
        raise ValueError("batch_size must be positive")
    for group, start, length in assignment:
        offset, remaining = 0, length
        with opener(path, metadata=metadata, pre_buffer=False,
                    memory_map=False, buffer_size=0) as source:
            for batch in source.iter_batches(batch_size=batch_size,
                                             row_groups=[group], use_threads=False):
                skip = max(0, start - offset)
                take = min(remaining, max(0, batch.num_rows - skip))
                offset += batch.num_rows
                if take:
                    yield convert(batch.slice(skip, take))
                    remaining -= take
                if not remaining:
                    break
        if remaining:
            raise ValueError("Parquet row group ended before its declared row count")


def ingest_streams(path, metadata, assignments, schema, batch_size, convert,
                   writer_factory, *, progress=None, deadline=float("inf")):
    """One append per nonempty worker. Return acknowledged counts, never replay.

    progress receives coordinator-thread snapshots; yielded input is not a
    private-row acknowledgment. A successful append return proves publication.
    Internal index sealing/maintenance are unavailable through the public API.
    """
    if not 1 <= len(assignments) <= 4:
        raise ValueError("Managed streams support 1..4 workers")
    started = time.monotonic()
    lock, stop = threading.Lock(), threading.Event()
    states = [{"worker": n, "row_groups": len(groups),
               "expected_rows": sum(s[2] for s in groups),
               "source_rows_yielded": 0, "acknowledged_rows": 0,
               "private_rows_staged": None, "phase": "pending",
               "index_seal": "not exposed by public ADBC API"}
              for n, groups in enumerate(assignments)]

    def update(number, **values):
        with lock:
            states[number].update(values)

    def snapshot():
        with lock:
            return {"workers": [dict(s) for s in states],
                    "elapsed_seconds": time.monotonic() - started,
                    "source_rows_yielded": sum(s["source_rows_yielded"] for s in states),
                    "acknowledged_rows": sum(s["acknowledged_rows"] for s in states),
                    "arrow_live_bytes": pa.total_allocated_bytes(),
                    "arrow_pool_peak_bytes": pa.default_memory_pool().max_memory(),
                    "process_peak_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == "darwin" else 1024),
                    "go_memory_bytes": None,
                    "container_file_cache_bytes": None,
                    "private_rows_staged": None,
                    "backup_ready": False,
                    "maintenance": "normal driver maintenance enabled; state not exposed by public ADBC API"}

    def worker(number, groups):
        if not groups:
            update(number, phase="empty", source_complete_seconds=0.0)
            return
        count = 0
        try:
            with closing(bounded_batches(path, metadata, groups, batch_size, convert)) as batches:
                def source():
                    nonlocal count
                    for batch in batches:
                        if stop.is_set() or time.time() >= deadline:
                            raise RuntimeError("Stream canceled or load deadline reached; input will not be replayed")
                        count += batch.num_rows
                        update(number, source_rows_yielded=count, phase="streaming")
                        yield batch
                    update(number, phase="source_complete_awaiting_append_ack",
                           source_complete_seconds=time.monotonic() - started)
                with writer_factory() as append:
                    with pa.RecordBatchReader.from_batches(schema, source()) as reader:
                        update(number, phase="append_running")
                        acknowledged = append(reader)
                if acknowledged != count or count != states[number]["expected_rows"]:
                    raise RuntimeError("Managed append acknowledgment differs from assigned source rows")
                update(number, acknowledged_rows=acknowledged, phase="published",
                       publication_ack_seconds=time.monotonic() - started)
        except BaseException:
            stop.set()
            update(number, phase="failed_acknowledgment_unknown")
            raise

    # Poll rather than writing result JSON from concurrent source callbacks.
    error = None
    with ThreadPoolExecutor(max_workers=len(assignments)) as executor:
        pending = {executor.submit(worker, n, groups) for n, groups in enumerate(assignments)}
        try:
            while pending:
                done, pending = wait(pending, timeout=1, return_when=FIRST_COMPLETED)
                for future in done:
                    try:
                        future.result()
                    except BaseException as failure:
                        error = error or failure
                        stop.set()
                if progress:
                    progress(snapshot())
        finally:
            stop.set()
    final = snapshot()
    if progress:
        progress(final)
    if error:
        raise error
    return final
