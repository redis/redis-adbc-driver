"""Focused regression checks for fixture conversion and result serialization."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

import pyarrow as pa

spec = importlib.util.spec_from_file_location('bulk_load_runner', Path(__file__).with_name('run.py'))
bulk = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bulk)


class FixtureConversionTests(unittest.TestCase):
    def test_preserves_exact_storage_values(self):
        batch = pa.record_batch([
            pa.array([2**53 + 1, -2**53 - 1], type=pa.int64()),
            pa.array(['a\x00b', 'café']),
            pa.array([0, -1], type=pa.int32()).cast(pa.date32()),
            pa.array([1000001, -1], type=pa.int64()).cast(pa.timestamp('us')),
        ], names=['big', 'text', 'date', 'ts'])
        self.assertEqual(bulk.native_columns(batch), [
            [9007199254740993, -9007199254740993], ['a\x00b', 'café'],
            [0, -1], [1000001, -1],
        ])

    def test_rejects_nullable_fixture(self):
        batch = pa.record_batch([pa.array([None], type=pa.int64())], names=['n'])
        with self.assertRaises(ValueError):
            bulk.native_columns(batch)

    def test_rejects_unsupported_storage_type(self):
        batch = pa.record_batch([pa.array([1.25], type=pa.float64())], names=['f'])
        with self.assertRaises(TypeError):
            bulk.native_columns(batch)

    def test_resp3_byte_key_metadata_is_saved_atomically(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'results.json'
            bulk.save_report(path, {'attributes': [{b'identifier': b'__rowid'}]})
            self.assertEqual(json.loads(path.read_text()), {'attributes': [{'identifier': '__rowid'}]})
            self.assertFalse(path.with_suffix('.tmp').exists())


if __name__ == '__main__':
    unittest.main()
