"""Bounded row-group sources and non-replaying long managed appends.

The file metadata is immutable and shared; file readers and ADBC connections are
worker-owned. No producer queue, allocator purge or retry is used here.
"""
from dataclasses import dataclass
import queue
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


@dataclass(frozen=True)
class StreamWriter:
    """Worker-owned append and its public, thread-safe statement cancel hook."""
    append: object
    cancel: object


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
    handle_locks, active = [threading.Lock() for _ in assignments], {}
    completed = queue.Queue()
    cancel_threads = []
    cancel_started = None
    states = [{"worker": n, "row_groups": len(groups),
               "expected_rows": sum(s[2] for s in groups),
               "source_rows_yielded": 0, "acknowledged_rows": 0,
               "private_rows_staged": None, "phase": "pending",
               "index_seal": "not exposed by public ADBC API",
               "cancel_requested": False, "worker_finished": False}
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
                    "preserve_owned_data": any(
                        s.get("cancel_error", False) or s["phase"] not in
                        ("published", "published_after_cancel_request", "empty") for s in states),
                    "maintenance": "normal driver maintenance enabled; state not exposed by public ADBC API"}

    def request_cancel():
        nonlocal cancel_started
        stop.set()
        if cancel_started is not None:
            return
        cancel_started = time.monotonic()
        # The Redis driver's public cancellation contract must cooperate.
        # An external process supervisor is required for a hard limit against
        # a broken/noncooperative C driver; never abandon live C Arrow owners.
        for number in range(len(assignments)):
            def cancel(number=number):
                while True:
                    with handle_locks[number]:
                        handle = active.get(number)
                        if handle is None:
                            return
                        update(number, cancel_requested=True)
                        try:
                            handle.cancel()
                        except BaseException:
                            update(number, cancel_error=True)
                    # Repeat while active: cancellation could race the native
                    # execution's start before it has consumed its first batch.
                    time.sleep(0.05)
            thread = threading.Thread(target=cancel)
            cancel_threads.append(thread)
            thread.start()

    def worker(number, groups):
        count, error = 0, None
        try:
            if not groups:
                update(number, phase="empty", source_complete_seconds=0.0)
                return
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
                with writer_factory() as handle:
                    if not isinstance(handle, StreamWriter):
                        raise TypeError("writer_factory must yield StreamWriter(append, cancel)")
                    # Unregister before the context manager closes the cursor.
                    # The same lock serializes cancel with that close boundary.
                    with handle_locks[number]:
                        if stop.is_set() or time.time() >= deadline:
                            raise RuntimeError("Stream canceled before append; input will not be replayed")
                        active[number] = handle
                    try:
                        with pa.RecordBatchReader.from_batches(schema, source()) as reader:
                            update(number, phase="append_running")
                            acknowledged = handle.append(reader)
                        if acknowledged != count or count != states[number]["expected_rows"]:
                            raise RuntimeError("Managed append acknowledgment differs from assigned source rows")
                        update(number, acknowledged_rows=acknowledged,
                               phase="published_after_cancel_request" if stop.is_set() else "published",
                               publication_ack_seconds=time.monotonic() - started)
                    finally:
                        with handle_locks[number]:
                            active.pop(number, None)
        except BaseException as failure:
            stop.set()
            update(number, phase="failed_acknowledgment_unknown")
            error = failure
        finally:
            update(number, worker_finished=True)
            completed.put((number, error))

    # Every owner and cancellation thread joins before returning. No foreign
    # thread closes a cursor or Arrow reader, including caller interruption.
    threads = [threading.Thread(target=worker, args=(n, groups))
               for n, groups in enumerate(assignments)]
    for thread in threads:
        thread.start()
    pending, error = set(range(len(threads))), None
    callback_failed = False
    while pending:
        try:
            if time.time() >= deadline and error is None:
                error = RuntimeError("Load deadline reached; active statements canceled without replay")
                request_cancel()
            if stop.is_set():
                request_cancel()
            try:
                number, failure = completed.get(timeout=0.05)
            except queue.Empty:
                number, failure = None, None
            if number is not None:
                pending.remove(number)
                threads[number].join()
                if failure is not None:
                    error = error or failure
                    request_cancel()
            if progress and not callback_failed:
                try:
                    progress(snapshot())
                except BaseException:
                    callback_failed = True
                    raise
        except BaseException as failure:
            error = error or failure
            request_cancel()
    for thread in cancel_threads:
        thread.join()
    final = snapshot()
    if progress and not callback_failed:
        try:
            progress(final)
        except BaseException as failure:
            error = error or failure
    if error:
        error.stream_snapshot = final
        raise error
    return final
