import copy
from pathlib import Path
import unittest

from summarize_routing import audit, load


class RoutingAuditTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profile = load(Path(__file__).with_name("routing-default.json.gz"))

    def test_complete_native_grid(self):
        self.assertEqual(len(audit(self.profile)), 320)

    def test_corrupted_measurements_are_rejected(self):
        for fault in ("missing", "duplicate", "wrong_route", "failed_request", "missing_response", "invalid_duration"):
            with self.subTest(fault=fault):
                value = copy.deepcopy(self.profile)
                if fault == "missing":
                    value["samples"].pop()
                elif fault == "duplicate":
                    value["samples"][-1] = copy.deepcopy(value["samples"][0])
                elif fault == "wrong_route":
                    value["samples"][0]["response_route"] = "unknown"
                elif fault == "failed_request":
                    value["samples"][0]["requests"][0]["failed"] = True
                elif fault == "missing_response":
                    value["samples"][0]["after_proof"].pop()
                else:
                    value["samples"][0]["after_proof"][0]["wall_milliseconds"] = float("nan")
                with self.assertRaises(AssertionError):
                    audit(value)


if __name__ == "__main__":
    unittest.main()
