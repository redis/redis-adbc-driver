"""Native C Device ownership tests; set REDIS_ADBC_LIBRARY and optionally GPU_TEST=1."""
import ctypes as c
import os
import unittest
import json
import uuid
import threading
import time

import pyarrow as pa


class Schema(c.Structure):
    pass


class Array(c.Structure):
    pass


Schema._fields_ = [("format", c.c_char_p), ("name", c.c_char_p), ("metadata", c.c_void_p),
                   ("flags", c.c_int64), ("n_children", c.c_int64),
                   ("children", c.POINTER(c.POINTER(Schema))), ("dictionary", c.POINTER(Schema)),
                   ("release", c.c_void_p), ("private_data", c.c_void_p)]
Array._fields_ = [("length", c.c_int64), ("null_count", c.c_int64), ("offset", c.c_int64),
                  ("n_buffers", c.c_int64), ("n_children", c.c_int64), ("buffers", c.POINTER(c.c_void_p)),
                  ("children", c.POINTER(c.POINTER(Array))), ("dictionary", c.POINTER(Array)),
                  ("release", c.c_void_p), ("private_data", c.c_void_p)]


class DeviceArray(c.Structure):
    _fields_ = [("array", Array), ("device_id", c.c_int64), ("device_type", c.c_int32),
                ("sync_event", c.c_void_p), ("reserved", c.c_int64 * 3)]


class DeviceStream(c.Structure):
    _fields_ = [("device_type", c.c_int32), ("get_schema", c.c_void_p), ("get_next", c.c_void_p),
                ("get_last_error", c.c_void_p), ("release", c.c_void_p), ("private_data", c.c_void_p)]


class HostStream(c.Structure):
    _fields_ = [(name, c.c_void_p) for name in ("get_schema", "get_next", "get_last_error", "release", "private_data")]


class Handle(c.Structure):
    _fields_ = [("private_data", c.c_void_p), ("private_driver", c.c_void_p)]


class Error(c.Structure):
    _fields_ = [("message", c.c_void_p), ("vendor_code", c.c_int32), ("sqlstate", c.c_char * 5),
                ("release", c.c_void_p), ("private_data", c.c_void_p), ("private_driver", c.c_void_p)]


class Native:
    def __init__(self, library, uri):
        self.lib = library
        self.db, self.conn, self.stmt = Handle(), Handle(), Handle()
        hp, ep = c.POINTER(Handle), c.POINTER(Error)
        signatures = {
            "RedisDatabaseNew": [hp, ep], "RedisDatabaseInit": [hp, ep],
            "RedisDatabaseSetOption": [hp, c.c_char_p, c.c_char_p, ep],
            "RedisConnectionNew": [hp, ep], "RedisConnectionInit": [hp, hp, ep],
            "RedisStatementNew": [hp, hp, ep], "RedisStatementSetSqlQuery": [hp, c.c_char_p, ep],
            "RedisStatementCancel": [hp, ep],
            "RedisStatementSetOption": [hp, c.c_char_p, c.c_char_p, ep],
            "RedisStatementGetOption": [hp, c.c_char_p, c.c_void_p, c.POINTER(c.c_size_t), ep],
            "RedisStatementExecuteQuery": [hp, c.c_void_p, c.POINTER(c.c_int64), ep],
            "RedisStatementExecuteDevice": [hp, c.c_int64, c.POINTER(DeviceStream), c.POINTER(c.c_int64), ep],
            "RedisStatementBindDevice": [hp, c.POINTER(DeviceArray), c.POINTER(Schema), ep],
            "RedisStatementRelease": [hp, ep], "RedisConnectionRelease": [hp, ep], "RedisDatabaseRelease": [hp, ep],
        }
        for name, args in signatures.items():
            function = getattr(library, name)
            function.argtypes, function.restype = args, c.c_uint8
        self.call("RedisDatabaseNew", c.byref(self.db))
        self.call("RedisDatabaseSetOption", c.byref(self.db), b"uri", uri.encode())
        self.call("RedisDatabaseInit", c.byref(self.db))
        self.call("RedisConnectionNew", c.byref(self.conn))
        self.call("RedisConnectionInit", c.byref(self.conn), c.byref(self.db))
        self.call("RedisStatementNew", c.byref(self.conn), c.byref(self.stmt))

    def call(self, name, *args):
        error = Error()
        status = getattr(self.lib, name)(*args, c.byref(error))
        message = c.string_at(error.message).decode() if error.message else ""
        release(error)
        if status:
            raise RuntimeError(f"ADBC status {status}: {message}")

    def option(self, name, value, statement=None):
        self.call("RedisStatementSetOption", c.byref(statement or self.stmt), name.encode(), value.encode())

    def query(self, sql, backend="cpu"):
        self.option("adbc.redis.execution_backend", backend)
        self.call("RedisStatementSetSqlQuery", c.byref(self.stmt), sql.encode())
        stream, affected = HostStream(), c.c_int64()
        self.call("RedisStatementExecuteQuery", c.byref(self.stmt), c.byref(stream), c.byref(affected))
        with pa.RecordBatchReader._import_from_c(c.addressof(stream)) as reader:
            return reader.read_all()

    def metrics(self):
        output, size = c.create_string_buffer(4096), c.c_size_t(4096)
        self.call("RedisStatementGetOption", c.byref(self.stmt), b"adbc.redis.gpu_metrics", output, c.byref(size))
        return json.loads(output.value)

    def close(self):
        self.call("RedisStatementRelease", c.byref(self.stmt))
        self.call("RedisConnectionRelease", c.byref(self.conn))
        self.call("RedisDatabaseRelease", c.byref(self.db))

    def bind(self, target, device, schema, after_bind=None):
        statement = Handle()
        self.call("RedisStatementNew", c.byref(self.conn), c.byref(statement))
        try:
            self.option("adbc.ingest.target_table", target, statement)
            self.option("adbc.ingest.mode", "adbc.ingest.mode.create", statement)
            self.call("RedisStatementBindDevice", c.byref(statement), c.byref(device), c.byref(schema))
            if after_bind is not None:
                after_bind()
            affected = c.c_int64()
            self.call("RedisStatementExecuteQuery", c.byref(statement), None, c.byref(affected))
            return affected.value
        finally:
            self.call("RedisStatementRelease", c.byref(statement))


def release(value):
    if value.release:
        c.CFUNCTYPE(None, c.POINTER(type(value)))(value.release)(c.byref(value))


def exported(batch):
    array, schema = Array(), Schema()
    batch._export_to_c(c.addressof(array), c.addressof(schema))
    return array, schema


def import_host(array, batch_schema):
    schema = Schema()
    batch_schema._export_to_c(c.addressof(schema))
    return pa.RecordBatch._import_from_c(c.addressof(array), c.addressof(schema))


@unittest.skipUnless(os.environ.get("REDIS_ADBC_LIBRARY"), "set REDIS_ADBC_LIBRARY")
class DeviceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.lib = c.CDLL(os.environ["REDIS_ADBC_LIBRARY"])
        cls.lib.RedisDeviceArrayToHost.argtypes = [c.POINTER(DeviceArray), c.POINTER(Schema), c.POINTER(Array), c.c_char_p, c.c_size_t]
        cls.lib.RedisDeviceArrayToHost.restype = c.c_int
        cls.lib.RedisDeviceArrayFromHost.argtypes = [c.POINTER(Array), c.POINTER(Schema), c.c_int64, c.POINTER(DeviceArray), c.c_char_p, c.c_size_t]
        cls.lib.RedisDeviceArrayFromHost.restype = c.c_int

    def test_cpu_device_copy_preserves_nulls_slices_and_independent_ownership(self):
        batch = pa.record_batch([pa.array([9, None, 2**53 + 1, -7], pa.int64()),
                                 pa.array([True, None, False, True])], names=["x", "flag"]).slice(1)
        array, schema = exported(batch)
        device = DeviceArray(array=array, device_id=-1, device_type=1)
        host, error = Array(), c.create_string_buffer(256)
        try:
            rc = self.lib.RedisDeviceArrayToHost(c.byref(device), c.byref(schema), c.byref(host), error, len(error))
            self.assertEqual(rc, 0, error.value)
            self.assertTrue(array.release)
            release(array)  # Output must no longer reference source buffers.
            result = import_host(host, batch.schema)
            self.assertEqual(result.to_pylist(), batch.to_pylist())
        finally:
            release(host)
            release(array)
            release(schema)


    def test_unsupported_device_never_dereferences_device_buffer_as_cpu(self):
        batch = pa.record_batch([pa.array([1], pa.int64())], names=["x"])
        array, schema = exported(batch)
        device = DeviceArray(array=array, device_id=0, device_type=8)
        host, error = Array(), c.create_string_buffer(256)
        child = array.children[0].contents
        previous = child.buffers[1]
        child.buffers[1] = 1
        try:
            self.assertNotEqual(self.lib.RedisDeviceArrayToHost(c.byref(device), c.byref(schema), c.byref(host), error, len(error)), 0)
            self.assertIn(b"unsupported Arrow device", error.value)
            self.assertFalse(host.release)
        finally:
            child.buffers[1] = previous
            release(array)
            release(schema)

    def test_unsupported_type_and_invalid_lengths_leave_output_released(self):
        for batch in [pa.record_batch([["unsupported"]], names=["x"]),
                      pa.record_batch([pa.array([1], pa.int64())], names=["x"])]:
            array, schema = exported(batch)
            old_length = array.length
            if batch.schema.field(0).type == pa.int64():
                array.length = -1
            device = DeviceArray(array=array, device_id=-1, device_type=1)
            host, error = Array(), c.create_string_buffer(256)
            try:
                self.assertNotEqual(self.lib.RedisDeviceArrayToHost(c.byref(device), c.byref(schema), c.byref(host), error, len(error)), 0)
                self.assertFalse(host.release)
            finally:
                array.length = old_length
                release(array)
                release(schema)

    @unittest.skipUnless(os.environ.get("GPU_TEST") == "1", "set GPU_TEST=1 on CUDA host")
    def test_cuda_export_consumer_and_source_release(self):
        import cupy

        batch = pa.record_batch([pa.array([None, 2**53 + 1, -7], pa.int64())], names=["x"])
        array, schema = exported(batch)
        device, error = DeviceArray(), c.create_string_buffer(256)
        try:
            self.assertEqual(self.lib.RedisDeviceArrayFromHost(c.byref(array), c.byref(schema), 0, c.byref(device), error, len(error)), 0, error.value)
            release(array)
            self.assertEqual(device.device_type, 2)
            self.assertFalse(device.sync_event)
            pointer = device.array.children[0].contents.buffers[1]
            memory = cupy.cuda.UnownedMemory(pointer, 24, device)
            view = cupy.ndarray((3,), dtype=cupy.int64, memptr=cupy.cuda.MemoryPointer(memory, 0))
            self.assertEqual(view[1:].get().tolist(), [2**53 + 1, -7])
            del view, memory
            host = Array()
            self.assertEqual(self.lib.RedisDeviceArrayToHost(c.byref(device), c.byref(schema), c.byref(host), error, len(error)), 0, error.value)
            release(device.array)
            result = import_host(host, batch.schema)
            self.assertEqual(result.to_pylist(), batch.to_pylist())
        finally:
            release(device.array)
            release(array)
            release(schema)

    @unittest.skipUnless(os.environ.get("GPU_TEST") == "1", "set GPU_TEST=1 on CUDA host")
    def test_real_arrow_c_device_consumer(self):
        import pylibcudf

        batch = pa.record_batch([pa.array([None, 2**53 + 1, -7], pa.int64())], names=["x"])
        array, schema = exported(batch)
        device, error = DeviceArray(), c.create_string_buffer(256)
        capsule = c.pythonapi.PyCapsule_New
        capsule.argtypes = [c.c_void_p, c.c_char_p, c.c_void_p]
        capsule.restype = c.py_object

        class Producer:
            def __arrow_c_device_array__(self, requested_schema=None):
                if requested_schema is not None:
                    raise NotImplementedError("schema negotiation is not supported by this test producer")
                return (capsule(c.addressof(schema), b"arrow_schema", None),
                        capsule(c.addressof(device), b"arrow_device_array", None))

        try:
            self.assertEqual(self.lib.RedisDeviceArrayFromHost(c.byref(array), c.byref(schema), 0, c.byref(device), error, len(error)), 0, error.value)
            release(array)
            consumer = pylibcudf.Table.from_arrow(Producer())
            self.assertEqual(consumer.to_arrow(["x"]).to_pylist(), batch.to_pylist())
            del consumer  # Consumer owns the moved release callback.
        finally:
            release(device.array)
            release(array)
            release(schema)


@unittest.skipUnless(os.environ.get("REDIS_URI") and os.environ.get("REDIS_ADBC_LIBRARY"), "requires verified Redis Software and library")
class CPUDeviceBindTests(unittest.TestCase):
    def test_device_bind_cpu_descriptor_borrows_input_and_preserves_values(self):
        lib = c.CDLL(os.environ["REDIS_ADBC_LIBRARY"])
        native = Native(lib, os.environ["REDIS_URI"])
        table = "it_device_cpu_" + uuid.uuid4().hex[:12]
        batch = pa.record_batch([pa.array([None, -7, 2**53 + 1], pa.int64())], names=["x"])
        array, schema = exported(batch)
        device = DeviceArray(array=array, device_id=-1, device_type=1)
        try:
            def release_source_before_execute():
                self.assertTrue(array.release)
                self.assertTrue(schema.release)
                release(array)
                release(schema)

            self.assertEqual(native.bind(table, device, schema, release_source_before_execute), 3)
            self.assertEqual(native.query(f'SELECT x FROM {table}').to_pylist(), batch.to_pylist())
        finally:
            release(array)
            release(schema)
            native.query(f'DROP TABLE IF EXISTS {table}')
            native.close()


@unittest.skipUnless(os.environ.get("GPU_TEST") == "1" and os.environ.get("REDIS_URI"), "requires CUDA and verified Redis Software")
class NativeQueryTests(unittest.TestCase):
    def setUp(self):
        self.lib = c.CDLL(os.environ["REDIS_ADBC_LIBRARY"])
        self.native = Native(self.lib, os.environ["REDIS_URI"])
        self.table = "it_cuda_" + uuid.uuid4().hex[:12]
        self.native.query(f'CREATE TABLE {self.table} (x BIGINT, bucket BIGINT)')
        self.native.query(f'INSERT INTO {self.table} VALUES (NULL,NULL),(-5,1),(0,1),(1,2),(2,NULL),(9007199254740993,2),(9223372036854775807,3),(-9223372036854775808,3)')

    def tearDown(self):
        try:
            self.native.query(f'DROP TABLE {self.table}')
        finally:
            self.native.close()

    def test_native_cuda_sql_matches_cpu_and_reports_actual_launches(self):
        for operator in ["=", "!=", "<>", "<", "<=", ">", ">="]:
            sql = f'SELECT x, bucket FROM {self.table} WHERE x {operator} 9007199254740993'
            with self.subTest(operator=operator):
                cpu = self.native.query(sql)
                gpu = self.native.query(sql, "cuda")
                self.assertEqual(cpu.schema, gpu.schema)
                self.assertEqual(cpu.to_pylist(), gpu.to_pylist())
                metrics = self.native.metrics()
                self.assertGreater(metrics["kernel_launches"], 0)
                self.assertEqual(metrics["input_rows"], 8)
        sql = f'SELECT bucket,COUNT(x),MIN(x),MAX(x) FROM {self.table} WHERE x >= 0 GROUP BY bucket'
        cpu, gpu = self.native.query(sql), self.native.query(sql, "cuda")
        self.assertEqual(sorted(map(repr, cpu.to_pylist())), sorted(map(repr, gpu.to_pylist())))
        with self.assertRaisesRegex(RuntimeError, "overflow"):
            self.native.query(f'SELECT SUM(x) FROM {self.table} WHERE x >= 0', "cuda")
        with self.assertRaisesRegex(RuntimeError, "CUDA supports"):
            self.native.query(f'SELECT x FROM {self.table}', "cuda")

    def test_device_stream_early_release_retains_independent_array(self):
        n = self.native
        n.option("adbc.redis.execution_backend", "cuda")
        n.call("RedisStatementSetSqlQuery", c.byref(n.stmt), f'SELECT x FROM {self.table} WHERE x >= 0'.encode())
        stream, affected = DeviceStream(), c.c_int64()
        n.call("RedisStatementExecuteDevice", c.byref(n.stmt), 0, c.byref(stream), c.byref(affected))
        schema, device = Schema(), DeviceArray()
        try:
            self.assertEqual(c.CFUNCTYPE(c.c_int, c.POINTER(DeviceStream), c.POINTER(Schema))(stream.get_schema)(c.byref(stream), c.byref(schema)), 0)
            self.assertEqual(c.CFUNCTYPE(c.c_int, c.POINTER(DeviceStream), c.POINTER(DeviceArray))(stream.get_next)(c.byref(stream), c.byref(device)), 0)
            self.assertTrue(device.array.release)
            release(stream)
            host, error = Array(), c.create_string_buffer(256)
            self.lib.RedisDeviceArrayToHost.argtypes = [c.POINTER(DeviceArray), c.POINTER(Schema), c.POINTER(Array), c.c_char_p, c.c_size_t]
            self.assertEqual(self.lib.RedisDeviceArrayToHost(c.byref(device), c.byref(schema), c.byref(host), error, len(error)), 0, error.value)
            release(device.array)
            result = pa.RecordBatch._import_from_c(c.addressof(host), c.addressof(schema))
            self.assertEqual(result.column(0).to_pylist(), [0, 1, 2, 9007199254740993, 9223372036854775807])
        finally:
            release(device.array)
            release(stream)
            release(schema)

    def test_external_cuda_producer_event_and_device_bind(self):
        import cupy

        n = self.native
        target = self.table + "_input"
        statement = Handle()
        n.call("RedisStatementNew", c.byref(n.conn), c.byref(statement))
        schema = Schema()
        pa.schema([("x", pa.int64())])._export_to_c(c.addressof(schema))
        stream = cupy.cuda.Stream(non_blocking=True)
        with stream:
            values = cupy.arange(2, dtype=cupy.int64) + 41
            event = cupy.cuda.Event()
            event.record(stream)
        event_handle = c.c_void_p(event.ptr)
        child_buffers = (c.c_void_p * 2)(None, values.data.ptr)
        root_buffers = (c.c_void_p * 1)(None)
        child = Array(length=2, null_count=0, n_buffers=2, buffers=child_buffers)
        children = (c.POINTER(Array) * 1)(c.pointer(child))
        noop = c.CFUNCTYPE(None, c.POINTER(Array))(lambda pointer: None)
        root = Array(length=2, null_count=0, n_buffers=1, n_children=1,
                     buffers=root_buffers, children=children, release=c.cast(noop, c.c_void_p))
        device = DeviceArray(array=root, device_id=0, device_type=2, sync_event=c.addressof(event_handle))
        try:
            n.option("adbc.ingest.target_table", target, statement)
            n.option("adbc.ingest.mode", "adbc.ingest.mode.create", statement)
            n.call("RedisStatementBindDevice", c.byref(statement), c.byref(device), c.byref(schema))
            self.assertTrue(device.array.release)  # Inputs are borrowed, never consumed.
            del values
            affected = c.c_int64()
            n.call("RedisStatementExecuteQuery", c.byref(statement), None, c.byref(affected))
            self.assertEqual(affected.value, 2)
            self.assertEqual(n.query(f'SELECT x FROM {target}').column(0).to_pylist(), [41, 42])
        finally:
            n.call("RedisStatementRelease", c.byref(statement))
            release(schema)
            n.query(f'DROP TABLE IF EXISTS {target}')

    def test_cancel_bind_while_waiting_for_external_device_event(self):
        import cupy

        n = self.native
        statement = Handle()
        n.call("RedisStatementNew", c.byref(n.conn), c.byref(statement))
        schema = Schema()
        pa.schema([("x", pa.int64())])._export_to_c(c.addressof(schema))
        kernel = cupy.RawKernel('''extern "C" __global__ void delayed(long long* out, unsigned long long ticks) {
            unsigned long long start = clock64();
            while (clock64() - start < ticks) {}
            out[0] = 41; out[1] = 42;
        }''', "delayed")
        kernel.compile()
        stream = cupy.cuda.Stream(non_blocking=True)
        with stream:
            values = cupy.empty(2, dtype=cupy.int64)
            kernel((1,), (1,), (values, cupy.uint64(1_000_000_000)))
            event = cupy.cuda.Event()
            event.record(stream)
        event_handle = c.c_void_p(event.ptr)
        buffers = (c.c_void_p * 2)(None, values.data.ptr)
        root_buffers = (c.c_void_p * 1)(None)
        child = Array(length=2, null_count=0, n_buffers=2, buffers=buffers)
        children = (c.POINTER(Array) * 1)(c.pointer(child))
        noop = c.CFUNCTYPE(None, c.POINTER(Array))(lambda pointer: None)
        root = Array(length=2, null_count=0, n_buffers=1, n_children=1,
                     buffers=root_buffers, children=children, release=c.cast(noop, c.c_void_p))
        device = DeviceArray(array=root, device_id=0, device_type=2, sync_event=c.addressof(event_handle))
        outcomes = []

        def bind():
            try:
                n.call("RedisStatementBindDevice", c.byref(statement), c.byref(device), c.byref(schema))
                outcomes.append("unexpected success")
            except RuntimeError as error:
                outcomes.append(str(error))

        worker = threading.Thread(target=bind)
        try:
            worker.start()
            time.sleep(0.02)
            n.call("RedisStatementCancel", c.byref(statement))
            worker.join(5)
            self.assertFalse(worker.is_alive(), "device event wait ignored cancellation")
            self.assertEqual(len(outcomes), 1)
            self.assertIn("status 11", outcomes[0])
            self.assertTrue(schema.release)
            self.assertTrue(device.array.release)
        finally:
            event.synchronize()  # Cancellation does not terminate producer-owned work.
            worker.join(5)
            n.call("RedisStatementRelease", c.byref(statement))
            release(schema)



if __name__ == "__main__":
    unittest.main()
