"""Correctness gates for the GPU experiment; CPU tests require only PyArrow."""

import importlib.util
import os
import unittest
from pathlib import Path

import pyarrow as pa

spec = importlib.util.spec_from_file_location("gpu_query", Path(__file__).with_name("run.py"))
run = importlib.util.module_from_spec(spec)
spec.loader.exec_module(run)


class QueryTests(unittest.TestCase):
    def check(self, buckets, values, expected):
        table = pa.table({"bucket": pa.array(buckets, pa.int64()),
                          "value": pa.array(values, pa.int64())})
        self.assertEqual(run.reference(table), expected)
        run.validate(run.cpu_query(table), expected)
        return table

    def test_null_group_filter_and_exact_integer_above_float_precision(self):
        self.check([None, None, 1, 1, 2, 2], [3, 5, 2**53 + 1, 7, None, -1],
                   {None: [2, 3, 5, 8], 1: [2, 7, 2**53 + 1, 2**53 + 8]})

    def test_empty_and_all_filtered(self):
        self.check([], [], {})
        self.check([1, None], [None, -1], {})

    def test_overflow_is_refused(self):
        with self.assertRaisesRegex(ValueError, "overflow"):
            self.check([1, 1], [run.MAX_INT64, 1], {})

    def test_wrong_physical_type_is_refused(self):
        with self.assertRaisesRegex(ValueError, "int64"):
            run.reference(pa.table({"bucket": [1], "value": [1.0]}))

    def test_duplicate_groups_and_wrong_values_are_refused(self):
        result = run.cpu_query(run.fixture(100, 2))
        expected = run.reference(run.fixture(100, 2))
        with self.assertRaisesRegex(ValueError, "duplicate"):
            run.validate(pa.concat_tables([result, result]), expected)
        with self.assertRaisesRegex(ValueError, "differ"):
            run.validate(result, {})

    def test_identifier_escaping(self):
        scan, _ = run.queries('my"schema', 'table;DROP TABLE x')
        self.assertEqual(scan, 'SELECT "bucket", "value" FROM "my""schema"."table;DROP TABLE x"')
        with self.assertRaises(ValueError):
            run.identifier("bad\x00name")

    @unittest.skipUnless(os.environ.get("GPU_TEST") == "1", "set GPU_TEST=1 on CUDA host")
    def test_real_cuda_null_empty_and_exact_integer_cases(self):
        import cudf
        import cupy

        cases = [([], []), ([None, 1], [None, -1]),
                 ([None, None, 1, 1], [3, 5, 2**53 + 1, 7])]
        for buckets, values in cases:
            with self.subTest(values=values):
                table = pa.table({"bucket": pa.array(buckets, pa.int64()),
                                  "value": pa.array(values, pa.int64())})
                result, _ = run.gpu_query(table, cudf, cupy)
                run.validate(result, run.reference(table))


if __name__ == "__main__":
    unittest.main()
