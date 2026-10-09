# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


"""Tests for comparison decisions that could conceal a compatibility gap."""
import json
from pathlib import Path
import unittest
from decimal import Decimal
import run

class CompareTests(unittest.TestCase):
    def test_order_scope(self):
        for sql in ("SELECT 'ORDER BY'", "SELECT row_number() OVER (ORDER BY x) FROM t", "SELECT * FROM (SELECT * FROM t ORDER BY x) s", "SELECT 1 -- ORDER BY x"):
            self.assertFalse(run.ordered(sql), sql)
        self.assertTrue(run.ordered("WITH c AS (SELECT * FROM t ORDER BY x) SELECT * FROM c ORDER BY y"))

    def test_multiset_keeps_duplicates(self):
        case = {"sql": "SELECT x FROM t"}
        a = {"types": ["int32"], "rows": [[1], [2], [1]]}
        b = {"types": ["int32"], "rows": [[2], [1], [1]]}
        self.assertEqual(run.classify(a, b, case), "pass")
        b["rows"] = [[1], [2]]
        self.assertEqual(run.classify(a, b, case), "fail")

    def test_ordered_rows(self):
        a = {"types": ["int32"], "rows": [[1], [2]]}
        b = {"types": ["int32"], "rows": [[2], [1]]}
        self.assertEqual(run.classify(a, b, {"sql": "SELECT x FROM t ORDER BY x"}), "fail")

    def test_type_width_and_empty_results(self):
        a = {"types": ["int32"], "rows": []}
        b = {"types": ["int64"], "rows": []}
        self.assertEqual(run.classify(a, b, {"sql": "SELECT x FROM t"}), "fail")

    def test_errors_compare_states(self):
        case = {"sql": "SELECT 1 / 0"}
        a = {"sqlstate": "22012", "message": "first"}
        b = {"sqlstate": "22012", "message": "second"}
        self.assertEqual(run.classify(a, b, case), "pass")
        b["sqlstate"] = "22003"
        self.assertEqual(run.classify(a, b, case), "fail")
        a["sqlstate"] = b["sqlstate"] = "MISSING"
        self.assertEqual(run.classify(a, b, case), "fail")

    def test_allowlist_requires_exact_outcomes(self):
        a = {"types": ["int32"], "rows": [[1]]}
        b = {"types": ["int64"], "rows": [[1]]}
        rule = {"postgres": a, "redis": b}
        case = {"sql": "SELECT 1"}
        self.assertEqual(run.classify(a, b, case, rule), "allowed")
        self.assertEqual(run.classify(a, {**b, "rows": [[2]]}, case, rule), "fail")

    def test_unsupported_is_explicit(self):
        a = {"types": ["int32"], "rows": [[1]]}
        b = {"sqlstate": "0A000"}
        case = {"sql": "SELECT 1"}
        self.assertEqual(run.classify(a, b, case), "fail")
        case["unsupported"] = {"sqlstate": "0A000", "reason": "Documented feature exclusion"}
        self.assertEqual(run.classify(a, b, case), "unsupported")
        self.assertEqual(run.classify(a, {"sqlstate": "42601"}, case), "fail")

    def test_values_preserve_precision_and_signed_zero(self):
        self.assertNotEqual(run.value(-0.0, "float64"), run.value(0.0, "float64"))
        n = Decimal("12345678901234567890123456789012345678")
        self.assertEqual(run.value(n, "decimal"), str(n))
        self.assertEqual(run.value(Decimal("1.2500"), "decimal"), "1.25")
        self.assertEqual(run.value("P1Y2M3DT4H5M6.123456S", "interval"), [14, 3, 14706123456, 0])

    def test_corpus_single_statements_and_ids(self):
        for path in (Path(__file__).parent / "corpus").glob("*.json"):
            corpus = json.loads(path.read_text())
            ids = [c["id"] for c in corpus["cases"]]
            self.assertEqual(len(ids), len(set(ids)), path)
            for sql in corpus.get("setup", []) + [c["sql"] for c in corpus["cases"]]:
                run.validate_sql(sql)
        with self.assertRaises(ValueError):
            run.validate_sql("SELECT 1; SELECT 2")

if __name__ == "__main__":
    unittest.main()
