"""Full-scale storage intent reaches each isolated driver connection."""
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

import scan_trial
from storage import FULL_ROWS, driver_options, storage_mode


class StorageIntent(unittest.TestCase):
    def test_scale_default_and_explicit_override(self):
        self.assertEqual(storage_mode(FULL_ROWS), "flex-stopgap")
        self.assertEqual(storage_mode(100_000), "ram")
        self.assertEqual(storage_mode(FULL_ROWS, "ram"), "ram")
        self.assertEqual(storage_mode(100_000, "flex-stopgap"), "flex-stopgap")

    def test_reject_unknown_storage(self):
        with self.assertRaises(ValueError):
            driver_options("unknown")

    def test_partition_reader_keeps_stopgap_selection(self):
        conn = MagicMock()
        conn.__enter__.return_value = conn
        cur = conn.cursor.return_value.__enter__.return_value
        reader = cur.fetch_record_batch.return_value.__enter__.return_value
        reader.schema.serialize.return_value.to_pybytes.return_value = b"schema"
        with patch.object(scan_trial.dbapi, "connect", return_value=conn) as connect, \
                patch.object(scan_trial, "consume", return_value={"rows": 1}):
            result = scan_trial.partition_worker(
                ("driver", "redis://private", b"descriptor", "output", b"schema".hex(), "flex-stopgap"))
        self.assertEqual(result["rows"], 1)
        self.assertEqual(connect.call_args.kwargs["db_kwargs"]["adbc.redis.flex"], "true")

    def test_statement_refusal_does_not_retry_in_ram(self):
        with tempfile.TemporaryDirectory() as directory:
            job = {"driver": "driver", "storage": "flex-stopgap",
                   "data_directory": str(Path(directory) / "data"), "api": "statement"}
            with patch.object(scan_trial.dbapi, "connect", side_effect=RuntimeError("unsupported Flex")) as connect:
                with self.assertRaisesRegex(RuntimeError, "unsupported Flex"):
                    scan_trial.execute(job, "redis://private")
            self.assertEqual(connect.call_count, 1)
            self.assertEqual(connect.call_args.kwargs["db_kwargs"]["adbc.redis.flex"], "true")
