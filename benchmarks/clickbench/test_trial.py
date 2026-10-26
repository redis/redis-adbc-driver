"""Trial diagnostics must retain stages and never hide query failures."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import MagicMock, patch

import pyarrow as pa
import trial


class TrialDiagnostics(unittest.TestCase):
    def run_trial(self, failure=None, diagnostic_failure=False, setup_failure=False):
        conn, cur = MagicMock(), MagicMock()
        conn.__enter__.return_value = conn
        conn.cursor.return_value = cur
        cur.__enter__.return_value = cur
        cur.fetch_arrow_table.return_value = pa.table({"count": [31]})
        if failure:
            cur.execute.side_effect = [None, failure]
        if setup_failure:
            cur.adbc_statement.set_options.side_effect = NotImplementedError("older driver")
        def option(key):
            if diagnostic_failure and key != "adbc.redis.query_workers":
                raise RuntimeError("diagnostics unavailable")
            return {"adbc.redis.query_workers": "8",
                    "adbc.redis.query_workers_last_grant": "2",
                    "adbc.redis.aggregate_metrics": '{"strategy":"partitioned_exact"}'}[key]
        conn.adbc_connection.get_option.side_effect = option
        with tempfile.TemporaryDirectory() as directory:
            job, output = Path(directory) / "job.json", Path(directory) / "out.json"
            job.write_text(json.dumps({"driver": "/test-driver", "namespace": "test",
                                       "sql": "SELECT COUNT(*) FROM hits", "output": str(output)}))
            with patch.object(trial.sys, "argv", ["trial.py", str(job)]), \
                 patch.dict(trial.os.environ, {"REDIS_CLOUD_URI": "redis://test"}), \
                 patch.object(trial.dbapi, "connect", return_value=conn):
                trial.main()
            return json.loads(output.read_text())

    def test_success_stages_and_actual_grant(self):
        result = self.run_trial()
        self.assertEqual(result["worker_grant"], 2)
        self.assertEqual(result["query_workers"], 8)
        self.assertEqual(result["values"], [[31]])
        self.assertEqual(set(result["stage_seconds"]),
                         {"connect", "session_setup", "execute", "arrow_fetch", "output_fingerprint"})
        self.assertGreaterEqual(result["trial_wall_seconds"], result["seconds"])
        self.assertGreater(result["process_peak_rss_bytes"], 0)

    def test_query_failure_keeps_diagnostics_and_null_timing(self):
        result = self.run_trial(ValueError("query failure"))
        self.assertIsNone(result["seconds"])
        self.assertEqual(result["error_type"], "ValueError")
        self.assertEqual(result["worker_grant"], 2)
        self.assertIn("execute", result["stage_seconds"])
        self.assertNotIn("arrow_fetch", result["stage_seconds"])

    def test_diagnostic_failure_does_not_mask_query_failure(self):
        result = self.run_trial(ValueError("query failure"), diagnostic_failure=True)
        self.assertEqual(result["error_type"], "ValueError")
        self.assertEqual(result["diagnostic_error_type"], "RuntimeError")
        self.assertIsNone(result["seconds"])

    def test_older_driver_still_executes(self):
        result = self.run_trial(setup_failure=True)
        self.assertEqual(result["diagnostics_setup_error_type"], "NotImplementedError")
        self.assertIsNotNone(result["seconds"])
        self.assertEqual(result["values"], [[31]])


if __name__ == "__main__":
    unittest.main()
