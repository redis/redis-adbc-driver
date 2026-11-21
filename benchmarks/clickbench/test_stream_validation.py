"""Bounded membership tests include large unordered IDs, gaps and duplicates."""
import datetime
from pathlib import Path
import tempfile
import unittest

import pyarrow as pa
import pyarrow.parquet as pq

from stream_validation import validate_managed_stream, IDSpool


class StreamValidation(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "source.parquet"
        self.rows = 70_003  # More than a validation batch; final partial batch.
        self.schema = pa.schema([("WatchID", pa.int64()), ("Day", pa.date32()),
                                 ("Time", pa.timestamp("us")), ("Text", pa.string())])
        source = pa.table({"WatchID": pa.array([2**60 + i for i in range(self.rows)], type=pa.int64()),
                           "Day": pa.array([18000] * self.rows, type=pa.int32()),
                           "Time": pa.array([1600000001] * self.rows, type=pa.int64()),
                           "Text": ["NUL\x00é"] * self.rows})
        pq.write_table(source, self.path, row_group_size=9000)
        with pq.ParquetFile(self.path, pre_buffer=False) as source:
            self.metadata = source.metadata

    def cursor(self, ids, corrupt=False):
        owner = self
        class Cursor:
            canceled = False
            calls = []
            def execute(cursor, sql):
                # No Go DISTINCT/ORDER BY state or broad 105-column predicate.
                owner.assertNotIn("DISTINCT", sql)
                owner.assertNotIn("ORDER BY", sql)
                owner.assertNotIn("watchid", sql.lower())
                cursor.calls.append(sql)
                if sql == "SELECT __rowid FROM hits":
                    cursor.rowid = None
                else:
                    owner.assertTrue(sql.startswith("SELECT * FROM hits WHERE __rowid = "))
                    cursor.rowid = int(sql.rsplit(" ", 1)[1])
            def adbc_cancel(cursor):
                cursor.canceled = True
            def fetch_record_batch(cursor):
                if cursor.rowid is None:
                    schema = pa.schema([("__rowid", pa.int64())])
                    batches = (pa.record_batch([pa.array(ids[start:start + 17001], type=pa.int64())], schema=schema)
                               for start in range(0, len(ids), 17001))
                    return pa.RecordBatchReader.from_batches(schema, batches)
                ordinal = (cursor.rowid - 11) // 3
                row = {"watchid": 2**60 + ordinal,
                       "day": datetime.date(2019, 4, 14),
                       "time": datetime.datetime(2020, 9, 13, 12, 26, 41),
                       "text": "wrong" if corrupt else "NUL\x00é"}
                schema = pa.schema([pa.field(f.name.lower(), f.type) for f in owner.schema])
                return pa.RecordBatchReader.from_batches(schema, pa.Table.from_pylist([row], schema=schema).to_batches())
        return Cursor()

    def validate(self, ids, **kwargs):
        return validate_managed_stream(self.cursor(ids), self.path, self.metadata,
            self.schema, len(ids), batch_size=8192, temp_parent=self.directory.name, **kwargs)

    def test_spool_rotates_to_bound_footer_metadata(self):
        schema = pa.schema([("__rowid", pa.int64())])
        directory = Path(self.directory.name)
        with IDSpool(directory, schema, file_rows=4) as spool:
            for start in range(0, 10, 2):
                spool.write_batch(pa.record_batch([pa.array([start + 1, start + 2])], schema=schema))
        files = sorted(directory.glob("rowids-*.parquet"))
        self.assertEqual(len(files), 3)
        self.assertEqual([row["__rowid"] for path in files for row in pq.read_table(path).to_pylist()], list(range(1, 11)))

    def test_large_ascending_sparse_ids_use_linear_fast_path(self):
        result = self.validate([11 + 3 * i for i in range(self.rows)])
        self.assertEqual(result["rows"], self.rows)
        self.assertEqual(result["membership"], "strict streamed IDs")
        self.assertEqual(result["samples"], 5)
        self.assertEqual(list(Path(self.directory.name).glob("adbc-validation-*")), [])

    def test_valid_unordered_ids_use_exact_external_sort(self):
        ids = [11 + 3 * i for i in range(self.rows)]
        ids = ids[::2] + ids[1::2]
        result = self.validate(ids)
        self.assertEqual(result["rows"], self.rows)
        self.assertIn("external ID-only sort", result["membership"])

    def test_duplicate_across_unsorted_batches_cannot_pass(self):
        ids = [11 + 3 * i for i in range(self.rows)]
        ids[-1] = ids[0]
        with self.assertRaisesRegex(ValueError, "Duplicate native row IDs"):
            self.validate(ids)
        self.assertEqual(list(Path(self.directory.name).glob("adbc-validation-*")), [])

    def test_typed_source_oracle_detects_value_corruption(self):
        cursor = self.cursor([11 + 3 * i for i in range(self.rows)], corrupt=True)
        with self.assertRaisesRegex(ValueError, "canonical source sample differs"):
            validate_managed_stream(cursor, self.path, self.metadata, self.schema,
                self.rows, temp_parent=self.directory.name)

    def test_count_mismatch_fails_without_value_samples(self):
        cursor = self.cursor([11 + 3 * i for i in range(10)])
        with self.assertRaisesRegex(ValueError, "row count differs"):
            validate_managed_stream(cursor, self.path, self.metadata, self.schema, self.rows,
                temp_parent=self.directory.name)
        self.assertEqual(cursor.calls, ["SELECT __rowid FROM hits"])

    def test_elapsed_budget_closes_reader_and_removes_spool(self):
        with self.assertRaisesRegex(RuntimeError, "deadline reached"):
            self.validate([11], deadline=0)
        self.assertEqual(list(Path(self.directory.name).glob("adbc-validation-*")), [])

    def test_temp_budget_fails_and_removes_spool(self):
        # Incompressible-enough IDs exceed a 1MiB spool as count grows.
        ids = [((i * 6364136223846793005) & ((1 << 62) - 1)) + 1 for i in range(200_003)]
        with self.assertRaisesRegex(RuntimeError, "temporary disk budget"):
            self.validate(ids, max_temp_mib=1)
        self.assertEqual(list(Path(self.directory.name).glob("adbc-validation-*")), [])


if __name__ == "__main__":
    unittest.main()
