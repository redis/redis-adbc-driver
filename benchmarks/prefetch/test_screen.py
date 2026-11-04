import socket
import hashlib
import json
import tempfile
import threading
import unittest
from pathlib import Path

import pyarrow as pa

from meter import Meter
from screen import query, validate
from reference import build_reference


class ScreenTests(unittest.TestCase):
    def test_reference_rejects_wrong_official_source_identity(self):
        with tempfile.TemporaryDirectory() as d:
            path = Path(d)/'source'
            path.write_bytes(b'changed source')
            with self.assertRaisesRegex(ValueError, 'identity differs'):
                build_reference(None, 'fixture', path, None, None, d, '0'*64)

    def test_reference_rejects_missing_or_duplicate_native_membership(self):
        class FakeClient:
            def __init__(self, missing):
                self.missing = missing
                self.ids = []
            def get(self, key):
                return json.dumps({'key_prefix': 'fixture:', 'columns': [{'name': 'watchid'}]})
            def pipeline(self, transaction):
                self.ids = []
                return self
            def hmget(self, key, *fields):
                self.ids.append(int(key.rsplit(':', 1)[-1]))
            def execute(self):
                return [[None if self.missing else str(i), '7'] for i in self.ids]
        with tempfile.TemporaryDirectory() as d:
            path = Path(d)/'source'
            path.write_bytes(b'checksum-verified fixture placeholder')
            digest = hashlib.sha256(path.read_bytes()).hexdigest()
            for missing in [True, False]:
                with self.subTest(missing=missing), self.assertRaises(ValueError):
                    build_reference(FakeClient(missing), 'fixture', path, None, None, d, digest)

    def test_independent_validation_preserves_duplicate_multiplicity_and_types(self):
        expected = pa.table({'n': pa.array([1, 1, 2], type=pa.int64()), 's': ['a\0b', 'a\0b', None]})
        with tempfile.TemporaryDirectory() as d:
            p = Path(d)/'output.arrow'
            def write(table):
                with pa.OSFile(str(p), 'wb') as sink, pa.ipc.new_stream(sink, table.schema) as writer:
                    writer.write_table(table)
            write(expected.take(pa.array([2, 0, 1])))
            validate([p], expected)
            write(expected.take(pa.array([2, 2, 0])))
            with self.assertRaises(AssertionError):
                validate([p], expected)
            write(expected.set_column(0, 'n', pa.array([1, 1, 2], type=pa.int32())))
            with self.assertRaises(AssertionError):
                validate([p], expected)

    def test_projection_identifiers_are_quoted(self):
        self.assertEqual(query('a"b', ['c"d'], 1, 3),
                         'SELECT "c""d" FROM "a""b".hits WHERE __rowid>=1 AND __rowid<=3')

    def test_meter_preserves_binary_payload_and_both_direction_counts(self):
        payload = bytes(range(256))*1024
        listener = socket.socket()
        listener.bind(('127.0.0.1', 0))
        listener.listen()
        def serve():
            with listener, listener.accept()[0] as conn:
                chunks = []
                while data := conn.recv(8192):
                    chunks.append(data)
                conn.sendall(b''.join(chunks)[::-1])
        worker = threading.Thread(target=serve)
        worker.start()
        meter = Meter(listener.getsockname())
        with socket.create_connection(('127.0.0.1', meter.port), timeout=5) as conn:
            conn.sendall(payload)
            conn.shutdown(socket.SHUT_WR)
            chunks = []
            while data := conn.recv(8192):
                chunks.append(data)
        self.assertEqual(b''.join(chunks), payload[::-1])
        self.assertEqual(meter.close(), [len(payload), len(payload)])
        worker.join(5)
        self.assertFalse(worker.is_alive())


if __name__ == '__main__':
    unittest.main()
