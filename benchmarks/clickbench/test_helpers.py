"""Check source-data decoding and incomplete timing handling without a server."""
import unittest

import pyarrow as pa

from compare import hot
from run import convert_batch, source_batches, validate_placement


class Helpers(unittest.TestCase):
    def test_dirty_parquet_date_and_timestamp(self):
        schema = pa.schema([pa.field("EventDate", pa.date32(), nullable=False),
                            pa.field("EventTime", pa.timestamp("us"), nullable=False),
                            pa.field("Title", pa.string())])
        batch = pa.record_batch([pa.array([15900], type=pa.uint16()), pa.array([1373760000]),
                                 pa.array([b"Google"], type=pa.binary())],
                                names=schema.names)
        row = convert_batch(batch, schema).to_pylist()[0]
        self.assertEqual(str(row["EventDate"]), "2013-07-14")
        self.assertEqual(str(row["EventTime"]), "2013-07-14 00:00:00")
        self.assertEqual(row["Title"], "Google")

    def test_overflow_not_silently_truncated(self):
        batch = pa.record_batch([pa.array([32768])], names=["ResolutionWidth"])
        with self.assertRaises(pa.ArrowInvalid):
            convert_batch(batch, pa.schema([("ResolutionWidth", pa.int16())]))

    def test_null_in_required_column(self):
        batch = pa.record_batch([pa.array([None], type=pa.int64())], names=["UserID"])
        with self.assertRaisesRegex(ValueError, "NULL values"):
            convert_batch(batch, pa.schema([pa.field("UserID", pa.int64(), nullable=False)]))

    def test_source_prefix_and_resume_across_batches(self):
        schema = pa.schema([("id", pa.int64())])
        class Source:
            def iter_batches(self, batch_size):
                for start in [0, 4, 8]:
                    yield pa.record_batch([pa.array(range(start, start + 4))], schema=schema)
        def ids(rows, retained=0):
            return [r["id"] for b in source_batches(Source(), schema, 4, rows, retained)
                    for r in b.to_pylist()]
        self.assertEqual(ids(10), list(range(10)))
        self.assertEqual(ids(10, 6), [6, 7, 8, 9])
        self.assertEqual(ids(8, 4), [4, 5, 6, 7])
        self.assertEqual(ids(8, 8), [])

    def test_hot_requires_both_runs(self):
        self.assertEqual(hot([10, 3, 2]), 2)
        self.assertEqual(hot([None, 3, 2]), 2)
        self.assertIsNone(hot([10, None, 2]))

    def test_cross_az_runner_rejected(self):
        placement = {
            "database": {"provider": "aws", "region": "us-east-1", "az": "use1-az1", "evidence": "Cloud API"},
            "runner": {"provider": "aws", "region": "us-east-1", "az": "use1-az2", "evidence": "EC2 API"},
        }
        with self.assertRaisesRegex(ValueError, "verified az"):
            validate_placement(placement)
        placement["runner"]["az"] = "use1-az1"
        self.assertEqual(validate_placement(placement), placement)


if __name__ == "__main__":
    unittest.main()
