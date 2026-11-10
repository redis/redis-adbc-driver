"""Do not turn setup failure, skips or stale acceptance evidence into passes."""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("acceptance_runner", ROOT / "python/scripts/run-acceptance.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class AcceptanceRunnerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        library = self.root / "driver.so"
        library.touch()
        self.env = {"REDIS_URI": "rediss://test:secret%40value@localhost:6380/0",
                    "REDIS_ADBC_LIBRARY": str(library), "SSL_CERT_FILE": "/tmp/test-ca.pem"}

    def test_tls_auth_and_current_library_required(self):
        for uri in ("", "redis://test:pass@localhost", "rediss://localhost", "rediss://u:p@localhost?skip_verify=true"):
            with self.subTest(uri=uri), self.assertRaises(ValueError):
                runner.connection_env({**self.env, "REDIS_URI": uri})
        with self.assertRaises(ValueError):
            runner.connection_env({**self.env, "REDIS_ADBC_LIBRARY": "/missing"})

    def test_profile_keeps_private_ca_out_of_uv(self):
        env = runner.connection_env(self.env)
        self.assertEqual(env["REDIS_PASSWORD"], "secret@value")
        self.assertEqual(env["REDIS_USERNAME"], "test")
        self.assertNotIn("SSL_CERT_FILE", env)
        self.assertEqual(env["REDIS_ACCEPTANCE_CA_FILE"], "/tmp/test-ca.pem")

    def invoke(self, contents, code=0, suites=("sqlalchemy",)):
        results = self.root / "reports"
        results.mkdir(exist_ok=True)
        for suite in suites:
            (results / f"{suite}.xml").write_text('<testsuite><testcase/></testsuite>')

        def command(suite, report, env):
            if contents is not None:
                report.write_text(contents)
            return ["fake-suite"], ROOT

        with patch.dict(os.environ, self.env, clear=True), \
             patch.object(runner, "suite_command", side_effect=command), \
             patch.object(runner, "execute", return_value=subprocess.CompletedProcess([], code, self.env["REDIS_URI"])):
            result = runner.main([*suites, "--results", str(results)])
        return result, json.loads((results / "summary.json").read_text()), results

    def test_removes_stale_report_on_setup_failure(self):
        result, summary, _ = self.invoke(None, code=2)
        self.assertEqual(result, 1)
        self.assertEqual(summary["sqlalchemy"]["status"], "no_result")

    def test_all_skipped_or_empty_or_all_xfailed_is_not_success(self):
        for content in ('<testsuite/>', '<testsuite><testcase><skipped/></testcase></testsuite>',
                        '<testsuite><testcase><skipped type="pytest.xfail"/></testcase></testsuite>'):
            with self.subTest(content=content):
                result, _, _ = self.invoke(content)
                self.assertEqual(result, 1)

    def test_upstream_xfails_are_recorded_separately_from_passes(self):
        result, summary, _ = self.invoke('<testsuite><testcase/><testcase><skipped type="pytest.xfail"/></testcase></testsuite>')
        self.assertEqual(result, 0)
        self.assertEqual(summary["sqlalchemy"]["passed"], 1)
        self.assertEqual(summary["sqlalchemy"]["xfailed"], 1)

    def test_failure_and_invalid_xml_propagate(self):
        for content in ('<testsuite><testcase><failure/></testcase></testsuite>', 'invalid'):
            with self.subTest(content=content):
                result, _, _ = self.invoke(content)
                self.assertEqual(result, 1)

    def test_setup_exit_cannot_be_masked_by_report(self):
        result, _, _ = self.invoke('<testsuite><testcase/></testsuite>', code=3)
        self.assertEqual(result, 1)

    def test_continues_other_suites_and_keeps_skips_separate(self):
        result, summary, reports = self.invoke(
            '<testsuite><testcase/><testcase><skipped/></testcase></testsuite>', suites=("sqlalchemy", "fivetran"))
        self.assertEqual(result, 0)
        self.assertEqual(set(summary), {"sqlalchemy", "fivetran"})
        self.assertEqual(summary["fivetran"]["passed"], 1)
        self.assertEqual(summary["fivetran"]["skipped"], 1)
        self.assertNotIn(self.env["REDIS_URI"], (reports / "fivetran.log").read_text())

    def test_dedicated_projects_are_not_repeated(self):
        self.assertNotIn("dbt", runner.SUITES)
        self.assertNotIn("airbyte", runner.SUITES)

    def test_bootstrap_restores_ca_in_fresh_interpreter(self):
        env = runner.connection_env({**os.environ, **self.env})
        self.assertNotIn("SSL_CERT_FILE", env)
        result = subprocess.run(
            [sys.executable, str(ROOT / "python/acceptance/run-python.py"), "-c",
             'import os; print(os.environ["SSL_CERT_FILE"])'],
            env=env, text=True, capture_output=True, check=True,
        )
        self.assertEqual(result.stdout.strip(), "/tmp/test-ca.pem")

    def test_audit_covers_every_comparison_row(self):
        def rows(path):
            return {parts[1].strip(): parts[2].strip()
                    for line in path.read_text().splitlines()
                    if len(parts := line.split("|")) > 2 and parts[1].strip().isdigit()}
        comparison = rows(ROOT / "docs/data-engineering-adbc-support.md")
        self.assertEqual(len(comparison), 51)
        self.assertEqual(rows(ROOT / "docs/integrations/acceptance.md"), comparison)

    def test_timeout_still_runs_other_suites(self):
        with patch.dict(os.environ, self.env, clear=True), \
             patch.object(runner, "suite_command", side_effect=subprocess.TimeoutExpired("fake", 1)):
            result = runner.main(["sqlalchemy", "fivetran", "--results", str(self.root)])
        self.assertEqual(result, 1)
        summary = json.loads((self.root / "summary.json").read_text())
        self.assertEqual(set(summary), {"sqlalchemy", "fivetran"})

    @unittest.skipUnless(os.name == "posix", "Unix process groups")
    def test_timeout_terminates_suite_workers(self):
        process = Mock(pid=12345)
        process.communicate.side_effect = [subprocess.TimeoutExpired("fake", 1), ("", None)]
        with patch.object(runner.subprocess, "Popen", return_value=process), \
             patch.object(runner.os, "killpg") as kill, \
             self.assertRaises(subprocess.TimeoutExpired):
            runner.execute(["fake"], ROOT, {}, 1)
        kill.assert_called_once_with(12345, runner.signal.SIGKILL)
        self.assertEqual(process.communicate.call_count, 2)


if __name__ == "__main__":
    unittest.main()
