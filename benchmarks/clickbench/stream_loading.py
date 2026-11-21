"""Bounded row-group sources and non-replaying long managed appends.

The file metadata is immutable and shared; file readers and ADBC connections are
worker-owned. No producer queue, allocator purge or retry is used here.
"""
from dataclasses import dataclass
import queue
import signal
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
    startup_gate = threading.Event()
    interrupted = False
    inline_cancels = set()
    error = None
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

    def remember_failure(failure):
        nonlocal error
        error = error or failure
        stop.set()

    def cancel_once(number):
        with handle_locks[number]:
            handle = active.get(number)
            if handle is None:
                return False
            update(number, cancel_requested=True)
            try:
                handle.cancel()
            except BaseException:
                update(number, cancel_error=True)
            return True

    def join_owned(thread):
        # Defer even a directly raised/injected KeyboardInterrupt until every
        # C Arrow owner and cancellation helper has completed its ownership.
        while True:
            try:
                thread.join()
                return
            except BaseException as failure:
                remember_failure(failure)

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
                while cancel_once(number):
                    # Repeat across the native execution-start race.
                    time.sleep(0.05)
            thread = None
            try:
                thread = threading.Thread(target=cancel)
                thread.start()
            except BaseException as failure:
                remember_failure(failure)
                if thread is not None and thread.ident is not None:
                    cancel_threads.append(thread)
                else:
                    # Thread creation failure must not disable cancellation.
                    inline_cancels.add(number)
                    update(number, cancel_error=True)
            else:
                cancel_threads.append(thread)

    def worker(number, groups):
        count, error = 0, None
        try:
            startup_gate.wait()
            if stop.is_set():
                update(number, phase="canceled_before_append")
                return
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

    # Install signal deferral before the first thread starts. A real SIGINT
    # cannot tear Thread.start/join between ownership registration and cleanup.
    # Non-main callers still get the same handling for raised exceptions.
    original_sigint = None
    if (threading.current_thread() is threading.main_thread() and
            signal.getsignal(signal.SIGINT) is signal.default_int_handler):
        original_sigint = signal.default_int_handler
        def defer_interrupt(signum, frame):
            nonlocal interrupted
            # Signal handlers cannot take Event/registry locks or call ADBC.
            interrupted = True
        signal.signal(signal.SIGINT, defer_interrupt)
    # Preserve SIG_IGN, SIG_DFL and custom handlers unchanged. Only Python's
    # standard KeyboardInterrupt handler participates in this deferral.
    threads, pending = [], set()
    callback_failed = False
    try:
        # No native append starts until startup succeeds as a whole. A partial
        # startup failure releases started owners into a canceled state instead.
        try:
            for number, groups in enumerate(assignments):
                thread = threading.Thread(target=worker, args=(number, groups))
                try:
                    thread.start()
                except BaseException as failure:
                    remember_failure(failure)
                    if thread.ident is not None:
                        threads.append(thread)
                        pending.add(number)
                    break
                else:
                    threads.append(thread)
                    pending.add(number)
        except BaseException as failure:
            remember_failure(failure)
        finally:
            if interrupted:
                remember_failure(KeyboardInterrupt())
            startup_gate.set()
        while pending:
            try:
                if interrupted:
                    remember_failure(KeyboardInterrupt())
                if time.time() >= deadline and error is None:
                    remember_failure(RuntimeError("Load deadline reached; active statements canceled without replay"))
                if stop.is_set():
                    request_cancel()
                for number in inline_cancels:
                    cancel_once(number)
                try:
                    number, failure = completed.get(timeout=0.05)
                except queue.Empty:
                    number, failure = None, None
                if number is not None:
                    pending.remove(number)
                    if failure is not None:
                        remember_failure(failure)
                        request_cancel()
                if progress and not callback_failed:
                    try:
                        progress(snapshot())
                    except BaseException:
                        callback_failed = True
                        raise
            except BaseException as failure:
                remember_failure(failure)
                request_cancel()
        for thread in threads:
            join_owned(thread)
        for thread in cancel_threads:
            join_owned(thread)
        if interrupted:
            remember_failure(KeyboardInterrupt())
    finally:
        if original_sigint is not None:
            signal.signal(signal.SIGINT, original_sigint)
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
