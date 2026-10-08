"""Exercise bounded parallel writes, resource cleanup and failure propagation."""
from contextlib import contextmanager
from types import SimpleNamespace
import threading
import unittest

from loading import ingest_batches


class Loading(unittest.TestCase):
    def test_parallel_workers_own_connections_and_close(self):
        barrier = threading.Barrier(4)
        seen, opened, closed = [], [], []
        lock = threading.Lock()

        @contextmanager
        def writer():
            owner = threading.get_ident()
            with lock:
                opened.append(owner)
            try:
                def append(batch):
                    self.assertEqual(owner, threading.get_ident())
                    barrier.wait(timeout=5)
                    with lock:
                        seen.append(batch.number)
                yield append
            finally:
                closed.append(owner)

        batches = [SimpleNamespace(num_rows=10, number=n) for n in range(12)]
        self.assertEqual(sum(ingest_batches(batches, writer, 4)), 120)
        self.assertEqual(sorted(seen), list(range(12)))
        self.assertEqual(len(set(opened)), 4)
        self.assertEqual(sorted(opened), sorted(closed))

    def test_prefetch_is_bounded_and_serial_order_preserved(self):
        prepared, written, seen = 0, 0, []

        def batches():
            nonlocal prepared
            for n in range(20):
                prepared += 1
                self.assertLessEqual(prepared - written, 2)
                yield SimpleNamespace(num_rows=1, number=n)

        @contextmanager
        def writer():
            def append(batch):
                nonlocal written
                seen.append(batch.number)
                written += 1
            yield append

        self.assertEqual(sum(ingest_batches(batches(), writer, 1)), 20)
        self.assertEqual(seen, list(range(20)))

    def test_write_failure_closes_connections(self):
        closed = []

        @contextmanager
        def writer():
            try:
                def append(batch):
                    raise RuntimeError("write failed")
                yield append
            finally:
                closed.append(True)

        with self.assertRaisesRegex(RuntimeError, "write failed"):
            list(ingest_batches([SimpleNamespace(num_rows=1)] * 10, writer, 2))
        self.assertTrue(closed)

    def test_source_failure_closes_worker(self):
        closed = []
        @contextmanager
        def writer():
            try:
                yield lambda batch: None
            finally:
                closed.append(True)
        def batches():
            yield SimpleNamespace(num_rows=1)
            yield SimpleNamespace(num_rows=1)
            raise ValueError("bad source")
        with self.assertRaisesRegex(ValueError, "bad source"):
            list(ingest_batches(batches(), writer))
        self.assertEqual(closed, [True])

    def test_consumer_stop_closes_workers(self):
        closed = []
        @contextmanager
        def writer():
            try:
                yield lambda batch: None
            finally:
                closed.append(True)
        pending = ingest_batches([SimpleNamespace(num_rows=1)] * 10, writer, 2)
        self.assertEqual(next(pending), 1)
        pending.close()
        self.assertTrue(closed)

    def test_synchronous_default_has_no_prefetch(self):
        events = []
        def batches():
            for n in range(3):
                events.append(("prepare", n))
                yield SimpleNamespace(num_rows=1, number=n)
        @contextmanager
        def writer():
            yield lambda batch: events.append(("write", batch.number))
        self.assertEqual(sum(ingest_batches(batches(), writer, prefetch=False)), 3)
        self.assertEqual(events, [(name,n) for n in range(3) for name in ["prepare","write"]])
