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
import unittest
from decimal import Decimal
from pathlib import Path

import run


class CompareTests(unittest.TestCase):
    def test_order_scope(self):
        for sql in (
            "SELECT 'ORDER BY'",
            "SELECT row_number() OVER (ORDER BY x) FROM t",
            "SELECT * FROM (SELECT * FROM t ORDER BY x) s",
            "SELECT 1 -- ORDER BY x",
        ):
            self.assertFalse(run.ordered(sql), sql)
        self.assertTrue(
            run.ordered(
                "WITH c AS (SELECT * FROM t ORDER BY x) SELECT * FROM c ORDER BY y"
            )
        )

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
        self.assertEqual(
            run.classify(a, b, {"sql": "SELECT x FROM t ORDER BY x"}), "fail"
        )

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
        case["unsupported"] = {
            "sqlstate": "0A000",
            "reason": "Documented feature exclusion",
        }
        self.assertEqual(run.classify(a, b, case), "unsupported")
        self.assertEqual(run.classify(a, {"sqlstate": "42601"}, case), "fail")

    def test_values_preserve_precision_and_signed_zero(self):
        self.assertNotEqual(run.value(-0.0, "float64"), run.value(0.0, "float64"))
        n = Decimal(12345678901234567890123456789012345678)
        self.assertEqual(run.value(n, "decimal"), str(n))
        self.assertEqual(run.value(Decimal("1.2500"), "decimal"), "1.25")
        self.assertEqual(
            run.value("P1Y2M3DT4H5M6.123456S", "interval"), [14, 3, 14706123456, 0]
        )

    def test_corpus_single_statements_and_ids(self):
        for path in (Path(__file__).parent / "corpus").rglob("*.json"):
            corpus = json.loads(path.read_text())
            ids = [c["id"] for c in corpus["cases"]]
            self.assertEqual(len(ids), len(set(ids)), path)
            for sql in corpus.get("setup", []) + [
                step["sql"] for c in corpus["cases"] for step in c.get("steps", [c])
            ]:
                run.validate_sql(sql)
        with self.assertRaises(ValueError):
            run.validate_sql("SELECT 1; SELECT 2")

    def test_parameters_ignore_strings_comments_and_repeat_bindings(self):
        sql = "SELECT $2, '$1 %', $1, $2 -- $9\n"
        q, values = run.parameter_sql(sql, [10, 20], True)
        self.assertEqual(values, [20, 10, 20])
        self.assertEqual(q, "SELECT %s, '$1 %%', %s, %s -- $9\n")
        q, values = run.parameter_sql(sql, [10, 20], False)
        self.assertEqual(q, "SELECT ?, '$1 %', ?, ? -- $9\n")
        with self.assertRaises(ValueError):
            run.parameter_sql("SELECT $3", [1], True)

    def test_type_precision_changes_do_not_disappear(self):
        a = {
            "types": ["decimal"],
            "rows": [["1.25"]],
            "type_details": [{"precision": 12, "scale": 3}],
        }
        b = {**a, "type_details": [{"precision": 38, "scale": 3}]}
        case = {"sql": "SELECT n FROM t", "type_checks": {"0": ["precision", "scale"]}}
        self.assertEqual(run.classify(a, b, case), "fail")
        self.assertEqual(run.classify(a, b, {"sql": "SELECT n FROM t"}), "pass")

    def test_ns_arrow_values_preserve_fraction_without_pandas(self):
        import pyarrow as pa

        col = pa.array([1704067200123456789, None, -1], type=pa.timestamp("ns"))
        self.assertEqual(
            run.arrow_values(col, col.type),
            ["2024-01-01T00:00:00.123456789", None, "1969-12-31T23:59:59.999999999"],
        )

    def test_decode_errors_are_never_success(self):
        outcome = {"decode_error": "out of range", "types": ["timestamp"]}
        self.assertEqual(run.classify(outcome, outcome, {"sql": "SELECT x"}), "fail")

    def test_fixture_failures_excluded_from_denominator(self):
        results = [
            {"status": s}
            for s in [
                "pass",
                "fail",
                "allowed",
                "unsupported",
                "blocked",
                "fixture-error",
            ]
        ]
        summary = run.summarize(results)
        self.assertEqual(summary["scored"], 2)
        self.assertEqual(summary["pass_rate"], 0.5)

    def test_source_inventory_references_exist(self):
        root = Path(__file__).parent
        inventory = json.loads((root / "issue-coverage.json").read_text())
        for entry in inventory["entries"]:
            for repro in entry.get("repros", []):
                if repro["status"] == "covered":
                    file, case_id = repro["case"].split(":")
                    corpus = json.loads((root / "corpus" / file).read_text())
                    self.assertIn(case_id, [c["id"] for c in corpus["cases"]])

    def test_unsupported_missing_state_requires_exact_error(self):
        pg = {"types": [], "rows": []}
        redis = {"sqlstate": "MISSING", "message": "unsupported DO"}
        case = {
            "sql": "DO $$ BEGIN NULL; END $$",
            "unsupported": {
                "reason": "Documented scope exclusion",
                "expected_redis": redis,
            },
        }
        self.assertEqual(run.classify(pg, redis, case), "unsupported")
        self.assertEqual(
            run.classify(pg, {**redis, "message": "different failure"}, case), "fail"
        )

    def test_context_normalization_requires_correct_backend_value(self):
        pg = {"types": ["string"], "rows": [["postgres"]]}
        redis = {"types": ["string"], "rows": [["redis"]]}
        case = {
            "sql": "SELECT current_database()",
            "context_values": {"0": {"postgres": "postgres", "redis": "redis"}},
        }
        self.assertEqual(run.classify(pg, redis, case), "pass")
        self.assertEqual(run.classify(pg, {**redis, "rows": [["wrong"]]}, case), "fail")

    def test_report_distinguishes_compatibility_from_infrastructure(self):
        import contextlib
        import io
        from tempfile import TemporaryDirectory
        from types import SimpleNamespace

        with TemporaryDirectory() as tmp, contextlib.redirect_stdout(io.StringIO()):
            args = SimpleNamespace(report=Path(tmp) / "report.json")
            result = {
                "file": "x.json",
                "area": "test",
                "tier": "test",
                "id": "x",
                "sql": "SELECT 1",
                "status": "fail",
            }
            self.assertEqual(run.write_report(args, {}, [result], []), 1)
            self.assertEqual(
                run.write_report(args, {}, [result], ["connection unavailable"]), 2
            )
            report = json.loads(args.report.read_text())
            self.assertEqual(
                report["infrastructure_errors"], ["connection unavailable"]
            )

    def test_transport_error_is_not_a_sql_difference(self):
        from unittest.mock import MagicMock

        conn = MagicMock()
        cur = conn.cursor.return_value.__enter__.return_value
        cur.execute.side_effect = run.psycopg.OperationalError("connection lost")
        with self.assertRaisesRegex(RuntimeError, "transport failure"):
            run.execute(conn, "SELECT 1", True)

    def test_importers_rebuild_identical_corpora(self):
        import import_dbt
        import import_postgres

        root = Path(__file__).parent
        files = list((root / "corpus/dbt").glob("*.json")) + list(
            (root / "corpus/postgres").glob("*.json")
        )
        before = {p: p.read_bytes() for p in files}
        import_dbt.rebuild()
        import_postgres.rebuild()
        self.assertEqual(before, {p: p.read_bytes() for p in files})


if __name__ == "__main__":
    unittest.main()
