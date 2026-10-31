"""Verify fixture ownership, failure cleanup, and candidate result accounting."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class CandidateTLSRunnerTests(unittest.TestCase):
    def run_fixture(self, **overrides):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        log = root / "events"
        fake_bin = root / "bin"
        fake_bin.mkdir()
        commands = {
            "docker": '''#!/bin/bash
if [[ "$*" == *"ps --all --quiet"* ]]; then
  echo "${EXISTING_PROJECT:-}"
elif [[ "$*" == *"ps --quiet redis-software"* ]]; then
  echo owned-container
elif [[ "$*" == *"down --volumes"* ]]; then
  echo cleanup >> "$EVENTS"
  exit "${CLEANUP_EXIT:-0}"
else
  exit 99
fi
''',
            "make": '''#!/bin/bash
echo make >> "$EVENTS"
exit "${SETUP_EXIT:-0}"
''',
            "python": '''#!/bin/bash
if [[ "$1" == *software_partition_tls.py ]]; then
  mkdir "$2"
  echo "rediss://default:private@localhost:27379/0" > "$2/admin.uri"
  echo ca > "$2/ca.crt"
  echo "$2" > "$FIXTURE_PATH"
else
  [[ "$REDIS_URI" == rediss://* ]] || exit 91
  [[ -f "$REDIS_TLS_CA_FILE" ]] || exit 92
  [[ "$REQUIRE_RAY_TLS" == 1 ]] || exit 93
  [[ "$SSL_CERT_FILE" == unchanged ]] || exit 94
  echo pytest >> "$EVENTS"
  exit "${PYTEST_EXIT:-0}"
fi
''',
        }
        for name, contents in commands.items():
            path = fake_bin / name
            path.write_text(contents)
            path.chmod(0o755)
        env = {**os.environ, "PATH": str(fake_bin) + os.pathsep + os.environ["PATH"],
               "PYTHON": str(fake_bin / "python"), "EVENTS": str(log),
               "FIXTURE_PATH": str(root / "fixture-path"), "SSL_CERT_FILE": "unchanged",
               "CANDIDATE_TLS_COMPOSE_PROJECT": "owned-unit-fixture", **overrides}
        result = subprocess.run([str(ROOT / "python/scripts/run-ray-tls-candidates.sh")],
                                env=env, capture_output=True, text=True)
        events = log.read_text().splitlines() if log.exists() else []
        fixture = root / "fixture-path"
        if fixture.exists():
            self.assertFalse(Path(fixture.read_text().strip()).exists())
        return result, events

    def test_success_cleans_private_material(self):
        result, events = self.run_fixture()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(events, ["make", "make", "pytest", "cleanup"])

    def test_pytest_failure_still_cleans_and_preserves_status(self):
        result, events = self.run_fixture(PYTEST_EXIT="3")
        self.assertEqual(result.returncode, 3, result.stderr)
        self.assertEqual(events[-2:], ["pytest", "cleanup"])

    def test_setup_failure_cleans_without_running_tests(self):
        result, events = self.run_fixture(SETUP_EXIT="9")
        self.assertEqual(result.returncode, 9, result.stderr)
        self.assertEqual(events, ["make", "cleanup"])

    def test_preexisting_project_is_never_mutated_or_removed(self):
        result, events = self.run_fixture(EXISTING_PROJECT="someone-elses-container")
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual(events, [])

    def test_cleanup_failure_cannot_report_success(self):
        result, events = self.run_fixture(CLEANUP_EXIT="1")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertEqual(events[-1], "cleanup")

    def test_summary_keeps_ray_tls_outcomes_separate(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            profiles = ("core workflows models orchestrators spark airflow superset "
                        "dbt documents ray-tls").split()
            for profile in profiles:
                (root / f"{profile}.xml").write_text('<testsuite><testcase /></testsuite>')
                (root / f"{profile}.exitcode").write_text("0")
            (root / "ray-tls.xml").write_text('''<testsuite>
              <testcase /><testcase><failure /></testcase><testcase><error /></testcase>
              <testcase><skipped /></testcase><testcase><skipped type="pytest.xfail" /></testcase>
            </testsuite>''')
            (root / "ray-tls.exitcode").write_text("1")
            result = subprocess.run([sys.executable,
                                    str(ROOT / "python/scripts/summarize-candidate-results.py"),
                                    str(root)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            summary = json.loads((root / "summary.json").read_text())
            self.assertEqual(summary["ray-tls"], {
                "status": "runner_failed", "exit_code": 1, "passed": 1,
                "failed": 1, "errors": 1, "skipped": 1, "xfailed": 1})
            self.assertEqual(summary["core"]["passed"], 1)


if __name__ == "__main__":
    unittest.main()
