"""Bounded concurrent ADBC appends with a separate connection per worker."""
from collections import deque
from concurrent.futures import ThreadPoolExecutor
from contextlib import ExitStack
import threading


def ingest_batches(batches, writer_factory, workers=1, prefetch=True):
    """Yield successfully appended row counts, overlapping source preparation.

    writer_factory returns a context manager for a worker-owned write callable.
    At most two batches per worker are retained. On failure pending jobs are
    cancelled, running jobs finish, and every connection closes before return.
    Parallel writes allocate row IDs in completion order, not source order.
    """
    if workers < 1:
        raise ValueError("workers must be positive")
    if not prefetch:
        if workers != 1:
            raise ValueError("Parallel loading requires prefetch")
        with writer_factory() as write:
            for batch in batches:
                write(batch)
                yield batch.num_rows
        return
    local = threading.local()
    stacks = []
    lock = threading.Lock()

    def write(batch):
        if not hasattr(local, "writer"):
            stack = ExitStack()
            with lock:
                stacks.append(stack)
            local.writer = stack.enter_context(writer_factory())
        local.writer(batch)
        return batch.num_rows

    executor = ThreadPoolExecutor(max_workers=workers)
    pending = deque()
    try:
        for batch in batches:
            pending.append(executor.submit(write, batch))
            if len(pending) >= 2 * workers:
                yield pending.popleft().result()
        while pending:
            yield pending.popleft().result()
    finally:
        executor.shutdown(wait=True, cancel_futures=True)
        with ExitStack() as cleanup:
            for stack in stacks:
                cleanup.callback(stack.close)
