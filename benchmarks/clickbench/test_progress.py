import json
from pathlib import Path
import resource
import os
import subprocess
import sys
import tempfile
import time
import unittest

from progress import TrialProgress, progress_path, retained_progress, annotate_failure


class Progress(unittest.TestCase):
    def test_active_stage_survives_without_final_result(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "q19-1.json"
            progress = TrialProgress(output, time.perf_counter(), resource.getrusage(resource.RUSAGE_SELF), interval=0.01)
            progress.begin()
            try:
                progress.enter("execute", {"connect": 0.1, "session_setup": 0.02})
                snapshot = retained_progress(output)
                self.assertEqual(snapshot["active_stage"], "execute")
                self.assertEqual(snapshot["status"], "running")
                self.assertEqual(snapshot["stage_seconds"]["connect"], 0.1)
                self.assertNotIn("seconds", snapshot)
                self.assertFalse(output.exists())
                self.assertGreater(snapshot["process_peak_rss_bytes"], 0)
                self.assertGreaterEqual(snapshot["active_stage_elapsed_seconds"], 0)
            finally:
                progress.close("failed", {"connect": 0.1})
            self.assertEqual(retained_progress(output)["status"], "failed")
            self.assertFalse(progress.thread.is_alive())

    def test_killed_trial_retains_execute_checkpoint(self):
        with tempfile.TemporaryDirectory() as directory:
            output, job = Path(directory) / "q19-1.json", Path(directory) / "job.json"
            job.write_text(json.dumps({"driver": "fake", "namespace": "test", "sql": "SELECT 1", "output": str(output)}))
            script = """
import sys, time
from unittest.mock import MagicMock, patch
import trial
conn, cur = MagicMock(), MagicMock()
conn.__enter__.return_value = conn
conn.cursor.return_value = cur
cur.__enter__.return_value = cur
conn.adbc_connection.get_option.return_value = '8'
def execute(sql):
    if not sql.startswith('SET '):
        time.sleep(1000)
cur.execute.side_effect = execute
with patch.object(trial.dbapi, 'connect', return_value=conn), patch.object(trial.sys, 'argv', ['trial.py', sys.argv[1]]):
    trial.main()
"""
            env = dict(os.environ, REDIS_CLOUD_URI="secret-test-uri")
            with subprocess.Popen([sys.executable, "-c", script, str(job)],
                                  cwd=Path(__file__).parent, env=env,
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as process:
                try:
                    deadline = time.monotonic() + 10
                    while time.monotonic() < deadline:
                        snapshot = retained_progress(output)
                        if snapshot and snapshot["active_stage"] == "execute":
                            break
                        if process.poll() is not None:
                            self.fail("Mock trial exited before execute checkpoint")
                        time.sleep(0.02)
                    else:
                        self.fail("No execute checkpoint")
                finally:
                    process.kill()
                    process.wait(timeout=5)
            self.assertFalse(output.exists())
            self.assertEqual(retained_progress(output)["active_stage"], "execute")
            self.assertNotIn("seconds", retained_progress(output))
            self.assertNotIn("secret-test-uri", progress_path(output).read_text())
            record = {"error_type": "QueryDeadline", "seconds": None}
            annotate_failure(record, output)
            self.assertIsNone(record["seconds"])
            self.assertEqual(record["error_type"], "QueryDeadline")
            self.assertEqual(record["partial_diagnostics"]["active_stage"], "execute")
            success = {"seconds": 1.0}
            annotate_failure(success, output)
            self.assertNotIn("partial_diagnostics", success)

    def test_missing_or_invalid_checkpoint_is_ignored(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "q19-1.json"
            self.assertIsNone(retained_progress(output))
            path = progress_path(output)
            for value in ["broken", "null", '"not a record"', '{"active_stage":"unknown"}']:
                path.write_text(value)
                self.assertIsNone(retained_progress(output))
            path.write_text(json.dumps({"active_stage": "execute", "status": "running", "uri": "credential"}))
            self.assertNotIn("uri", retained_progress(output))

    def test_checkpoint_io_failure_does_not_mask_query(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "missing" / "out.json"
            progress = TrialProgress(output, time.perf_counter(), resource.getrusage(resource.RUSAGE_SELF))
            progress.begin()
            progress.enter("execute", {})
            progress.close("failed", {})
            self.assertIsNone(retained_progress(output))


if __name__ == "__main__":
    unittest.main()
