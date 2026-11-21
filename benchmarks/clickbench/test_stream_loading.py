"""Synthetic sources prove coverage/ownership, not full-scale RSS or throughput."""
from collections import Counter
from contextlib import contextmanager
import datetime
import os
import signal
import uuid

import adbc_driver_manager.dbapi as dbapi
from storage import driver_options
import tempfile
from pathlib import Path
import threading
import time
import unittest
from unittest.mock import patch

import pyarrow as pa
import pyarrow.parquet as pq

from run import convert_batch, ingest_batch, cleanup_allowed
from stream_validation import validate_managed_stream
from stream_loading import bounded_batches, ingest_streams, row_group_assignments, StreamWriter


class StreamLoading(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "fixture.parquet"
        # 105 columns, int64 values outside IEEE double exact range, dates,
        # seconds timestamps, nullable strings, and an uneven last row group.
        self.schema = pa.schema([("WatchID", pa.int64())] +
            [(f"n{i}", pa.int64()) for i in range(101)] +
            [("Day", pa.date32()), ("Time", pa.timestamp("us")), ("Text", pa.string())])
        self.rows = []
        for i in range(23):
            row = {"WatchID": 2**60 + i, **{f"n{n}": -(2**60) + i + n for n in range(101)},
                   "Day": 18000 + i, "Time": 1600000000 + i,
                   "Text": None if i % 3 == 0 else f"row {i}\x00é"}
            self.rows.append(row)
        self.source = pa.Table.from_pylist(self.rows)
        pq.write_table(self.source, self.path, row_group_size=5)
        with pq.ParquetFile(self.path, pre_buffer=False) as source:
            self.metadata = source.metadata
        self.convert = lambda b: ingest_batch(convert_batch(b, self.schema))
        self.target_schema = pa.schema([pa.field(f.name.lower(), f.type) for f in self.schema])
        self.expected = []
        for source in self.rows:
            expected = {key.lower(): value for key, value in source.items()}
            expected["day"] = datetime.date(1970, 1, 1) + datetime.timedelta(days=source["Day"])
            expected["time"] = datetime.datetime(1970, 1, 1) + datetime.timedelta(seconds=source["Time"])
            self.expected.append(expected)

    def test_disjoint_exhaustive_prefix_and_retained_boundary(self):
        for workers in (1, 4):
            assignments = row_group_assignments(self.metadata, 22, workers, retained=3)
            batches = [b for a in assignments for b in bounded_batches(
                self.path, self.metadata, a, 2, self.convert)]
            actual = Counter(r["watchid"] for b in batches for r in b.to_pylist())
            self.assertEqual(actual, Counter(r["WatchID"] for r in self.rows[3:22]))
        with self.assertRaises(ValueError):
            row_group_assignments(self.metadata, 23, 5)

    def test_reader_policy_group_boundaries_and_exported_lifetime(self):
        opened, closed, active = [], [], []
        def opener(path, **kwargs):
            self.assertFalse(kwargs["pre_buffer"])
            self.assertFalse(kwargs["memory_map"])
            self.assertIs(kwargs["metadata"], self.metadata)
            self.assertEqual(active, [])
            opened.append(kwargs)
            source = pq.ParquetFile(path, **kwargs)
            class Owner:
                def __enter__(owner):
                    active.append(True)
                    return source
                def __exit__(owner, *args):
                    source.close()
                    active.pop()
                    closed.append(True)
            return Owner()
        batches = list(bounded_batches(self.path, self.metadata,
            row_group_assignments(self.metadata, 23, 1)[0], 3, self.convert, opener=opener))
        self.assertEqual(len(opened), 5)
        self.assertEqual(len(closed), 5)
        self.assertEqual([b.num_rows for b in batches], [3, 2] * 4 + [3])
        # Read all 105 exact typed values after every Parquet reader has closed.
        self.assertEqual(pa.Table.from_batches(batches).to_pylist(), self.expected)
        self.assertEqual(batches[0].schema, self.target_schema)

    def test_early_source_close_closes_current_reader(self):
        closed = []
        @contextmanager
        def opener(*args, **kwargs):
            try:
                with pq.ParquetFile(*args, **kwargs) as source:
                    yield source
            finally:
                closed.append(True)
        batches = bounded_batches(self.path, self.metadata, [(0, 0, 5)], 2, self.convert, opener=opener)
        next(batches)
        batches.close()
        self.assertEqual(closed, [True])

    def test_four_long_streams_ack_only_after_publication(self):
        barrier = threading.Barrier(4)
        values, calls, closes, snapshots = [], [], [], []
        @contextmanager
        def writer():
            try:
                def append(reader):
                    calls.append(threading.get_ident())
                    barrier.wait(timeout=5)
                    table = reader.read_all()
                    values.extend(table.to_pylist())
                    return table.num_rows
                yield StreamWriter(append, lambda: None)
            finally:
                closes.append(True)
        result = ingest_streams(self.path, self.metadata,
            row_group_assignments(self.metadata, 23, 4), self.target_schema,
            2, self.convert, writer, progress=snapshots.append)
        self.assertEqual(len(set(calls)), 4)
        self.assertEqual(len(closes), 4)
        self.assertEqual(result["acknowledged_rows"], 23)
        self.assertEqual(Counter(r["watchid"] for r in values), Counter(r["WatchID"] for r in self.rows))
        self.assertEqual(len(values[0]), 105)
        self.assertEqual(sorted(values, key=lambda row: row["watchid"]), self.expected)
        self.assertFalse(result["backup_ready"])
        for state in result["workers"]:
            self.assertEqual(state["phase"], "published")
            self.assertLessEqual(state["source_complete_seconds"], state["publication_ack_seconds"])
            self.assertIsNone(state["private_rows_staged"])

    def test_failure_does_not_replay_or_acknowledge_consumed_input(self):
        calls, snapshots = [], []
        @contextmanager
        def writer():
            def append(reader):
                calls.append(True)
                reader.read_all()
                raise RuntimeError("uncertain append result")
            yield StreamWriter(append, lambda: None)
        with self.assertRaisesRegex(RuntimeError, "uncertain append"):
            ingest_streams(self.path, self.metadata,
                row_group_assignments(self.metadata, 23, 1), self.target_schema,
                2, self.convert, writer, progress=snapshots.append)
        self.assertEqual(calls, [True])
        self.assertEqual(snapshots[-1]["source_rows_yielded"], 23)
        self.assertEqual(snapshots[-1]["acknowledged_rows"], 0)
        self.assertTrue(snapshots[-1]["preserve_owned_data"])
        # This is the same helper run.main uses to gate its final DDL cleanup.
        metadata = {"preserve_owned_data": snapshots[-1]["preserve_owned_data"]}
        self.assertFalse(cleanup_allowed(True, metadata))
        self.assertTrue(cleanup_allowed(True, {"preserve_owned_data": False}))
        self.assertFalse(cleanup_allowed(False, {"preserve_owned_data": False}))

    def test_empty_assignments_do_not_open_empty_appends(self):
        calls = []
        @contextmanager
        def writer():
            def append(reader):
                calls.append(True)
                return reader.read_all().num_rows
            yield StreamWriter(append, lambda: None)
        result = ingest_streams(self.path, self.metadata,
            row_group_assignments(self.metadata, 2, 4), self.target_schema,
            2, self.convert, writer)
        self.assertEqual(calls, [True])
        self.assertEqual(result["acknowledged_rows"], 2)
        self.assertEqual([s["phase"] for s in result["workers"]], ["published", "empty", "empty", "empty"])

    def test_bad_acknowledgment_is_not_reported_as_committed(self):
        snapshots = []
        @contextmanager
        def writer():
            def append(reader):
                reader.read_all()
                return -1
            yield StreamWriter(append, lambda: None)
        with self.assertRaisesRegex(RuntimeError, "acknowledgment differs"):
            ingest_streams(self.path, self.metadata,
                row_group_assignments(self.metadata, 23, 1), self.target_schema,
                2, self.convert, writer, progress=snapshots.append)
        self.assertEqual(snapshots[-1]["acknowledged_rows"], 0)
        self.assertEqual(snapshots[-1]["source_rows_yielded"], 23)

    def test_deadline_stops_source_without_replay(self):
        calls, snapshots = [], []
        @contextmanager
        def writer():
            def append(reader):
                calls.append(True)
                return reader.read_all().num_rows
            yield StreamWriter(append, lambda: None)
        with self.assertRaisesRegex(RuntimeError, "deadline reached"):
            ingest_streams(self.path, self.metadata,
                row_group_assignments(self.metadata, 23, 1), self.target_schema,
                2, self.convert, writer, progress=snapshots.append, deadline=0)
        self.assertEqual(calls, [])
        self.assertEqual(snapshots[-1]["source_rows_yielded"], 0)
        self.assertEqual(snapshots[-1]["acknowledged_rows"], 0)

    def test_partial_thread_start_failure_joins_owners_before_return(self):
        original = threading.Thread.start
        started, calls, snapshots = [], [], []
        attempts = 0
        def start(thread):
            nonlocal attempts
            attempts += 1
            if attempts == 2:
                raise RuntimeError("injected second worker startup failure")
            original(thread)
            started.append(thread)
        @contextmanager
        def writer():
            def append(reader):
                calls.append(True)
                return reader.read_all().num_rows
            yield StreamWriter(append, lambda: None)
        with patch.object(threading.Thread, "start", start):
            with self.assertRaisesRegex(RuntimeError, "second worker startup failure") as caught:
                ingest_streams(self.path, self.metadata,
                    row_group_assignments(self.metadata, 23, 4), self.target_schema,
                    2, self.convert, writer, progress=snapshots.append)
        self.assertFalse(any(thread.is_alive() for thread in started))
        self.assertEqual(calls, [])  # Startup gate prevents any native publication.
        self.assertTrue(snapshots[-1]["preserve_owned_data"])
        self.assertFalse(cleanup_allowed(True, caught.exception.stream_snapshot))

    def test_second_interrupt_during_join_is_deferred_until_all_owners_finish(self):
        original = threading.Thread.join
        injected = False
        def join(thread, *args, **kwargs):
            nonlocal injected
            if not injected:
                injected = True
                raise KeyboardInterrupt()
            return original(thread, *args, **kwargs)
        # First interrupt comes from the progress callback after source EOF;
        # the injected second interrupt arrives while ownership is being joined.
        with patch.object(threading.Thread, "join", join):
            self.cancel_after_eof("interrupt")
        self.assertTrue(injected)

    def test_standard_sigint_during_startup_defers_and_joins_every_owner(self):
        original_start = threading.Thread.start
        original_handler = signal.getsignal(signal.SIGINT)
        started, calls = [], []
        @contextmanager
        def writer():
            def append(reader):
                calls.append(True)
                return reader.read_all().num_rows
            yield StreamWriter(append, lambda: None)
        def start(thread):
            original_start(thread)
            started.append(thread)
            if len(started) == 1:
                signal.raise_signal(signal.SIGINT)
                signal.raise_signal(signal.SIGINT)
        try:
            signal.signal(signal.SIGINT, signal.default_int_handler)
            with patch.object(threading.Thread, "start", start):
                with self.assertRaises(KeyboardInterrupt) as caught:
                    ingest_streams(self.path, self.metadata,
                        row_group_assignments(self.metadata, 23, 4), self.target_schema,
                        2, self.convert, writer)
            self.assertFalse(any(thread.is_alive() for thread in started))
            self.assertEqual(calls, [])
            self.assertTrue(caught.exception.stream_snapshot["preserve_owned_data"])
            self.assertIs(signal.getsignal(signal.SIGINT), signal.default_int_handler)
        finally:
            signal.signal(signal.SIGINT, original_handler)

    def test_custom_and_ignored_sigint_handlers_are_preserved(self):
        original = signal.getsignal(signal.SIGINT)
        calls = []
        @contextmanager
        def writer():
            yield StreamWriter(lambda reader: reader.read_all().num_rows, lambda: None)
        try:
            for handler in (lambda signum, frame: calls.append(signum), signal.SIG_IGN):
                signal.signal(signal.SIGINT, handler)
                sent = False
                def progress(snapshot):
                    nonlocal sent
                    self.assertIs(signal.getsignal(signal.SIGINT), handler)
                    if not sent:
                        sent = True
                        signal.raise_signal(signal.SIGINT)
                result = ingest_streams(self.path, self.metadata,
                    row_group_assignments(self.metadata, 23, 1), self.target_schema,
                    2, self.convert, writer, progress=progress)
                self.assertEqual(result["acknowledged_rows"], 23)
                self.assertIs(signal.getsignal(signal.SIGINT), handler)
            self.assertEqual(calls, [signal.SIGINT])
        finally:
            signal.signal(signal.SIGINT, original)

    def cancel_after_eof(self, cause):
        input_done, canceled = threading.Event(), threading.Event()
        closed, calls, snapshots = [], [], []
        @contextmanager
        def writer():
            try:
                def append(reader):
                    rows = reader.read_all()
                    calls.append(rows.num_rows)
                    if cause == "sibling" and rows.column(0)[0].as_py() != 2**60:
                        self.assertTrue(input_done.wait(2))
                        raise RuntimeError("sibling failed")
                    input_done.set()
                    self.assertTrue(canceled.wait(2), "statement was not actively canceled after EOF")
                    raise RuntimeError("statement canceled")
                yield StreamWriter(append, canceled.set)
            finally:
                closed.append(True)
        interrupted = False
        def progress(snapshot):
            nonlocal interrupted
            snapshots.append(snapshot)
            if cause == "interrupt" and input_done.is_set() and not interrupted:
                interrupted = True
                raise KeyboardInterrupt()
        start = time.monotonic()
        workers = 2 if cause == "sibling" else 1
        expected_error = KeyboardInterrupt if cause == "interrupt" else RuntimeError
        with self.assertRaises(expected_error):
            ingest_streams(self.path, self.metadata,
                row_group_assignments(self.metadata, 23, workers), self.target_schema,
                2, self.convert, writer, progress=progress,
                deadline=time.time() + 0.15 if cause == "deadline" else float("inf"))
        self.assertLess(time.monotonic() - start, 1)
        self.assertTrue(canceled.is_set())
        self.assertEqual(len(calls), workers)
        self.assertEqual(len(closed), workers)
        if cause != "interrupt":
            self.assertEqual(snapshots[-1]["acknowledged_rows"], 0)
            self.assertTrue(all(s["worker_finished"] for s in snapshots[-1]["workers"]))

    def test_sibling_failure_actively_cancels_eof_peer(self):
        self.cancel_after_eof("sibling")

    def test_deadline_actively_cancels_after_eof(self):
        self.cancel_after_eof("deadline")

    def test_caller_interrupt_actively_cancels_after_eof(self):
        self.cancel_after_eof("interrupt")

    def test_cancel_error_still_joins_owner_and_preserves_unknown_outcome(self):
        release, closed = threading.Event(), threading.Event()
        snapshots = []
        @contextmanager
        def writer():
            try:
                def append(reader):
                    reader.read_all()
                    self.assertTrue(release.wait(2))
                    raise RuntimeError("uncertain native result")
                def cancel():
                    release.set()
                    raise RuntimeError("cancel reply lost")
                yield StreamWriter(append, cancel)
            finally:
                closed.set()
        with self.assertRaises(RuntimeError):
            ingest_streams(self.path, self.metadata,
                row_group_assignments(self.metadata, 23, 1), self.target_schema,
                2, self.convert, writer, progress=snapshots.append,
                deadline=time.time() + 0.1)
        self.assertTrue(closed.is_set())
        self.assertTrue(snapshots[-1]["preserve_owned_data"])
        self.assertEqual(snapshots[-1]["acknowledged_rows"], 0)
        self.assertTrue(snapshots[-1]["workers"][0]["worker_finished"])



@unittest.skipUnless(os.environ.get("CLICKBENCH_STREAM_TEST_URI") and
                     os.environ.get("CLICKBENCH_STREAM_TEST_DRIVER"),
                     "requires an allocated, independently verified Software QPF4 fixture")
class NativeStreamLoading(unittest.TestCase):
    setUp = StreamLoading.setUp
    # Keep source unit checks in StreamLoading; native cases also exercise NOINDEX.
    def native_load(self, workers, noindex=False):
        uri = os.environ["CLICKBENCH_STREAM_TEST_URI"]
        driver = os.environ["CLICKBENCH_STREAM_TEST_DRIVER"]
        namespace = "clickbench_" + uuid.uuid4().hex[:12]
        options = {"uri": uri, **driver_options("flex-stopgap")}
        types = {pa.int64(): "BIGINT", pa.date32(): "DATE", pa.timestamp("us"): "TIMESTAMP", pa.string(): "TEXT"}
        with dbapi.connect(driver=driver, autocommit=True, db_kwargs=options) as coordinator:
            with coordinator.cursor() as cursor:
                cursor.execute(f"CREATE SCHEMA {namespace}")
                try:
                    cursor.execute(f"SET search_path TO {namespace}")
                    cursor.execute("CREATE TABLE hits (" + ",".join(f"{f.name.lower()} {types[f.type]}" + (" NOINDEX" if noindex else "") for f in self.schema) + ")")
                    @contextmanager
                    def writer():
                        with dbapi.connect(driver=driver, autocommit=True, db_kwargs=options) as connection:
                            with connection.cursor() as worker:
                                yield StreamWriter(lambda reader: worker.adbc_ingest("hits", reader, mode="append", db_schema_name=namespace), worker.adbc_cancel)
                    result = ingest_streams(self.path, self.metadata,
                        row_group_assignments(self.metadata, 23, workers), self.target_schema,
                        2, self.convert, writer)
                    self.assertEqual(result["acknowledged_rows"], 23)
                    cursor.execute("SELECT COUNT(*), COUNT(DISTINCT __rowid) FROM hits")
                    self.assertEqual(tuple(cursor.fetchone()), (23, 23))
                    cursor.execute("SELECT * FROM hits ORDER BY watchid")
                    self.assertEqual(cursor.fetch_arrow_table().to_pylist(), self.expected)
                    validate_managed_stream(cursor, self.path, self.metadata, self.schema, 23, batch_size=2)
                finally:
                    cursor.execute(f"SET search_path TO {namespace}")
                    cursor.execute("DROP TABLE IF EXISTS hits")
                    cursor.execute("SET search_path TO public")
                    cursor.execute(f"DROP SCHEMA {namespace}")

    def test_native_serial_stream(self):
        self.native_load(1)

    def test_native_four_streams(self):
        self.native_load(4)

    def test_native_four_noindex_streams(self):
        self.native_load(4, noindex=True)


if __name__ == "__main__":
    unittest.main()
