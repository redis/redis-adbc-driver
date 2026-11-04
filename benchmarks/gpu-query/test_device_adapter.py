"""Public Python device adapter checks; CUDA tests require real hardware."""
import ctypes as c
import gc
import os
from pathlib import Path
import sys
import threading
import unittest
from concurrent.futures import CancelledError

import pyarrow as pa

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "python"))
from adbc_driver_redis.device import DeviceAdapterV1, _exports


@unittest.skipUnless(os.environ.get("REDIS_ADBC_LIBRARY"), "set REDIS_ADBC_LIBRARY")
class AdapterTests(unittest.TestCase):
    def setUp(self):
        self.adapter = DeviceAdapterV1()

    def test_cpu_capsules_and_retained_metadata(self):
        batch = pa.record_batch([pa.array(["before", "", None, "雪"]).slice(1),
                                 pa.array([b"", None, b"\x00\xff"], pa.large_binary())],
                                names=["text", "binary"]).replace_schema_metadata({b"owner": b"external"})
        result = self.adapter.to_host(batch, device_type=1, device_id=-1)
        del batch
        gc.collect()
        result.validate(full=True)
        self.assertEqual(result.to_pylist(), [{"text": "", "binary": b""},
            {"text": None, "binary": None}, {"text": "雪", "binary": b"\x00\xff"}])
        self.assertEqual(result.schema.metadata, {b"owner": b"external"})

    def test_unused_and_consumed_capsule_cleanup(self):
        from adbc_driver_redis.device import (_Array, _Schema, _DeviceArray,
            _own_capsule, _schema_destructor, _device_destructor)
        before = len(_exports)
        for consume in [False, True]:
            array, schema = _Array(), _Schema()
            pa.array(["", None, "雪"])._export_to_c(c.addressof(array), c.addressof(schema))
            device = _DeviceArray(array=array, device_id=-1, device_type=1)
            schema_capsule = _own_capsule(schema, b"arrow_schema", _schema_destructor, self.adapter._lib)
            device_capsule = _own_capsule(device, b"arrow_device_array", _device_destructor, self.adapter._lib)
            del array
            if consume:
                result = pa.Array._import_from_c(c.addressof(device.array), c.addressof(schema))
            del schema_capsule, device_capsule, schema, device
            gc.collect()
            self.assertEqual(len(_exports), before)
            if consume:
                self.assertEqual(result.to_pylist(), ["", None, "雪"])
                del result

    def test_array_capsules(self):
        for typ in [pa.string(), pa.large_string(), pa.binary(), pa.large_binary()]:
            values = pa.array(["", None, "snow"], pa.string()).cast(typ).slice(1)
            result = self.adapter.to_host(values, device_type=1, device_id=-1)
            self.assertEqual(result, values)

    def test_wrong_device_and_unsupported_layout(self):
        with self.assertRaisesRegex(ValueError, "does not match"):
            self.adapter.to_host(pa.array([1]), device_type=2, device_id=0)
        with self.assertRaisesRegex(ValueError, "0..65535"):
            self.adapter.to_device(pa.array([1]), device_id=-1)
        with self.assertRaises(OSError):
            self.adapter.to_host(pa.array([[1]]), device_type=1, device_id=-1)

    def test_cancel_before_producer_export(self):
        event = threading.Event()
        event.set()
        class Producer:
            def __arrow_c_device_array__(self):
                raise AssertionError("cancelled request must not touch producer")
        with self.assertRaises(CancelledError):
            self.adapter.to_host(Producer(), device_type=1, device_id=-1, cancel=event)

    def test_missing_extension_is_actionable(self):
        import unittest.mock as mock
        with mock.patch("adbc_driver_redis.device.c.CDLL", return_value=object()):
            with self.assertRaisesRegex(NotImplementedError, "upgrade.*CPU"):
                DeviceAdapterV1()

    def test_unsupported_manager_version(self):
        import unittest.mock as mock
        with mock.patch("importlib.metadata.version", return_value="2.0.0"):
            with self.assertRaisesRegex(NotImplementedError, "CPU Arrow"):
                DeviceAdapterV1()

    @unittest.skipUnless(os.environ.get("GPU_TEST") == "1", "requires real CUDA hardware")
    def test_pending_external_event_cancellation(self):
        import cupy
        from test_native_device import Array, DeviceArray, Schema, exported, release
        from adbc_driver_redis.device import _capsule_new
        batch = pa.record_batch([pa.array([41, 42], pa.int64())], names=["x"])
        array, schema = exported(batch)
        stream = cupy.cuda.Stream(non_blocking=True)
        started, unblock, cancel = threading.Event(), threading.Event(), threading.Event()
        def hold(stream, status, argument):
            started.set()
            unblock.wait(10)
        with stream:
            values = cupy.asarray([41, 42], dtype=cupy.int64)
            stream.add_callback(hold, None)
            event = cupy.cuda.Event()
            event.record(stream)
        handle = c.c_void_p(event.ptr)
        child = array.children[0].contents
        original = child.buffers[1]
        child.buffers[1] = values.data.ptr
        device = DeviceArray(array=array, device_id=0, device_type=2, sync_event=c.addressof(handle))
        class Producer:
            def __arrow_c_device_array__(self):
                return (_capsule_new(c.addressof(schema), b"arrow_schema", None),
                        _capsule_new(c.addressof(device), b"arrow_device_array", None))
        results = []
        def stage():
            try:
                self.adapter.to_host(Producer(), device_type=2, device_id=0, cancel=cancel)
                results.append("unexpected success")
            except CancelledError:
                results.append("cancelled")
        worker = threading.Thread(target=stage)
        try:
            self.assertTrue(started.wait(5))
            self.assertFalse(event.done)
            worker.start()
            import time
            time.sleep(0.02)
            cancel.set()
            worker.join(5)
            self.assertFalse(worker.is_alive())
            self.assertEqual(results, ["cancelled"])
            self.assertTrue(schema.release)
            self.assertTrue(device.array.release)
        finally:
            unblock.set()
            event.synchronize()
            if worker.ident is not None:
                worker.join(5)
            child.buffers[1] = original
            release(array)
            release(schema)

    @unittest.skipUnless(sys.platform == "darwin", "unsupported CUDA platform check")
    def test_no_cuda_does_not_leak_exports(self):
        before = len(_exports)
        with self.assertRaisesRegex(OSError, "CPU Arrow"):
            self.adapter.to_device(pa.array([1]), device_id=0).__arrow_c_device_array__()
        gc.collect()
        self.assertEqual(len(_exports), before)

    @unittest.skipUnless(os.environ.get("GPU_TEST") == "1", "requires real CUDA hardware")
    def test_external_consumer_and_roundtrip(self):
        import pylibcudf
        # pylibcudf consumes genuine CUDA capsules and produces fresh capsules.
        batch = pa.record_batch([pa.array(["prefix", "", None, "雪"]).slice(1),
                                 pa.array([7, None, -9], pa.int64())], names=["text", "x"])
        producer = self.adapter.to_device(batch, device_id=0)
        consumer = pylibcudf.Table.from_arrow(producer)
        del producer, batch
        gc.collect()
        result = self.adapter.to_host(consumer, device_type=2, device_id=0)
        del consumer
        gc.collect()
        result.validate(full=True)
        self.assertEqual(result.to_pylist(), [{"text": "", "x": 7},
            {"text": None, "x": None}, {"text": "雪", "x": -9}])


@unittest.skipUnless(os.environ.get("REDIS_URI") and os.environ.get("REDIS_ADBC_LIBRARY"),
                     "requires verified Redis Software")
class ManagerTests(unittest.TestCase):
    def test_public_manager_bind_releases_producer_before_execute(self):
        self._manager_bind(cuda=False)

    @unittest.skipUnless(os.environ.get("GPU_TEST") == "1", "requires real CUDA hardware")
    def test_public_manager_cuda_bind_releases_producer_before_execute(self):
        self._manager_bind(cuda=True)

    def _manager_bind(self, cuda):
        import adbc_driver_manager as manager
        from adbc_driver_redis import connect
        from uuid import uuid4
        adapter = DeviceAdapterV1()
        table = "it_adapter_" + uuid4().hex
        with connect(os.environ["REDIS_URI"]) as db, manager.AdbcConnection(db) as conn:
            with manager.AdbcStatement(conn) as stmt:
                stmt.set_options(**{"adbc.ingest.target_table": table,
                                    "adbc.ingest.mode": "adbc.ingest.mode.create"})
                producer = pa.record_batch([pa.array(["", None, "雪"])], names=["text"])
                if cuda:
                    import pylibcudf
                    producer = pylibcudf.Table.from_arrow(adapter.to_device(producer, device_id=0))
                adapter.bind(stmt, producer, device_type=2 if cuda else 1, device_id=0 if cuda else -1)
                del producer
                gc.collect()
                self.assertEqual(stmt.execute_update(), 3)
            try:
                with manager.AdbcStatement(conn) as stmt:
                    stmt.set_sql_query(f'SELECT text FROM "{table}"')
                    stream, _ = stmt.execute_query()
                    with pa.RecordBatchReader._import_from_c(stream.address) as reader:
                        result = reader.read_all()
                    self.assertEqual(result.column(0).to_pylist(), ["", None, "雪"])
            finally:
                with manager.AdbcStatement(conn) as stmt:
                    stmt.set_sql_query(f'DROP TABLE "{table}"')
                    stmt.execute_update()


if __name__ == "__main__":
    unittest.main()
