import socket
import hashlib
import json
import sys
import tempfile
import threading
import unittest
from pathlib import Path

import pyarrow as pa

from meter import Meter
from screen import query, run_harness, validate
from reference import build_reference
from summarize import summarize


class ScreenTests(unittest.TestCase):
    def test_unavailable_counters_are_not_reported_as_zero(self):
        report = json.loads((Path(__file__).parent/'2026-11-03/direct-results.json').read_text())
        report['consumer_boundary'] = 'C shared library'
        for trial in report['trials']:
            for name in ['allocated_bytes', 'allocations', 'sampled_peak_go_heap_bytes']:
                trial[name] = None
        result = summarize(report)
        self.assertIn('unavailable, not zero', result['interpretation'])
        self.assertTrue(all(case[build]['allocated_bytes_median'] is None
                            for case in result['cases'] for build in ['baseline', 'candidate']))
        measured = next(t for t in report['trials'] if not t['warmup'])
        measured['allocated_bytes'] = 1
        with self.assertRaisesRegex(ValueError, 'Incomplete metric instrumentation'):
            summarize(report)

    def test_child_rss_sampling_observes_live_allocation(self):
        with tempfile.TemporaryDirectory() as d:
            child = Path(d)/'child'
            child.write_text('#!' + sys.executable + '\n'
                             'import json,sys,time\n'
                             'json.load(sys.stdin)\n'
                             'data=bytearray(64*1024*1024)\n'
                             'data[::4096]=b"x"*(len(data)//4096)\n'
                             'time.sleep(0.2)\n'
                             'print(len(data))\n')
            child.chmod(0o700)
            process, peak, samples = run_harness(str(child), {})
            self.assertEqual(process.returncode, 0)
            self.assertEqual(int(process.stdout), 64*1024*1024)
            self.assertGreaterEqual(peak, 64*1024*1024)
            self.assertGreaterEqual(samples, 2)

    def test_select_schema_nullability_preserves_exact_null_validation(self):
        expected = pa.Table.from_arrays([pa.array([1, 2], type=pa.int64())],
                                       schema=pa.schema([pa.field('n', pa.int64(), nullable=False)]))
        with tempfile.TemporaryDirectory() as d:
            path = Path(d)/'result.arrow'
            def write(values):
                table = pa.table({'n': pa.array(values, type=pa.int64())})
                with pa.OSFile(str(path), 'wb') as sink, pa.ipc.new_stream(sink, table.schema) as writer:
                    writer.write_table(table)
            write([1, 2])
            validate([path], expected)
            write([1, None])
            with self.assertRaises(AssertionError):
                validate([path], expected)

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
        self.assertEqual(query('fixture', ['watchid'], None, None),
                         'SELECT "watchid" FROM "fixture".hits')

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
