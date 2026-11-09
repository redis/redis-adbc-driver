"""Offline contracts for the shared inventory and its real runner entry points."""
import ast
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import tomllib
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / "python/scripts"
sys.path.insert(0, str(SCRIPTS))
try:
    import integration_registry as registry
finally:
    sys.path.pop(0)


def load_script(name):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / name)
    module = importlib.util.module_from_spec(spec)
    with patch.object(sys, "path", [str(SCRIPTS), *sys.path]):
        spec.loader.exec_module(module)
    return module


class InventoryTests(unittest.TestCase):
    def test_complete_unique_inventory_and_dependency_extras(self):
        guides = [g for profile in registry.PROFILES.values() for g in profile.guides]
        self.assertEqual(len(guides), len(set(guides)))
        documented = {p.stem for p in (ROOT / "docs/integrations").glob("*.md")}
        documented -= {"README", "acceptance", "example-setup"}
        # DuckDB keeps its linked workflow outside docs/integrations.
        self.assertTrue((ROOT / "docs/duckdb.md").is_file())
        documented.add("duckdb")
        self.assertEqual(set(guides), documented)
        candidates = [c for p in registry.PROFILES.values() for c in p.candidates]
        self.assertEqual(len(candidates), len(set(candidates)))
        self.assertEqual(set(candidates), set(registry.CANDIDATES))
        self.assertEqual(set(registry.GUIDE_PROFILES),
                         {name for name, p in registry.PROFILES.items() if p.guides})
        extras = tomllib.loads((ROOT / "python/pyproject.toml").read_text())["project"]["optional-dependencies"]
        for p in registry.PROFILES.values():
            self.assertLessEqual(set(p.extras.split(",")), set(extras))
        self.assertEqual(set(guides) - registry.READ_WRITE_GUIDES, {"fivetran", "superset"})

    def test_selectors_exist_and_tls_has_no_plaintext_selectors(self):
        for tool, selectors in registry.CANDIDATES.items():
            self.assertTrue(selectors or tool == "ray-tls", tool)
            for selector in selectors:
                file, *function = selector.split("::")
                tree = ast.parse((ROOT / "python/tests" / file).read_text())
                if function:
                    self.assertEqual(len(function), 1)
                    self.assertIn(function[0], {n.name for n in tree.body
                                               if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))})
        self.assertEqual(registry.PROFILES["ray-tls"].guides, ())
        self.assertEqual(registry.CANDIDATES["ray-tls"], ())

    def test_distinct_aliases_and_environments_remain_explicit(self):
        for profile, guide, alias in (("workflows", "huggingface-datasets", "datasets"),
                                      ("core", "pyarrow", "arrow"),
                                      ("core", "scikit-learn", "ml"),
                                      ("documents", "r-dbi", "r")):
            self.assertIn(guide, registry.PROFILES[profile].guides)
            self.assertIn(alias, registry.PROFILES[profile].candidates)
            self.assertNotIn(guide, registry.CANDIDATES)
        self.assertIn("rstudio", registry.PROFILES["documents"].guides)
        self.assertEqual(registry.PROFILES["airflow"].python, "3.14")
        self.assertEqual(registry.PROFILES["dbt"].python, "3.14")
        self.assertEqual(registry.PROFILES["superset"].extras, "superset,test-runner")

    def test_help_and_cli_selection_use_registry(self):
        result = subprocess.run([str(SCRIPTS / "run-candidate-tests.sh"), "--help"],
                                text=True, capture_output=True, check=True)
        self.assertEqual(set(result.stdout.split("Tools: ")[1].split()), set(registry.CANDIDATES))
        for profile, item in registry.PROFILES.items():
            result = subprocess.run([sys.executable, str(SCRIPTS / "integration_registry.py"),
                                     "profile", profile], text=True, capture_output=True, check=True)
            self.assertEqual(result.stdout.splitlines(), [item.python, item.extras, *item.candidates])
        checker = load_script("check_integration_guide.py")
        runner = load_script("run-integration-guides.py")
        self.assertEqual(checker.GUIDES, registry.GUIDES)
        self.assertEqual(runner.GUIDE_PROFILES, registry.GUIDE_PROFILES)
        self.assertEqual(runner.READ_WRITE_GUIDES, registry.READ_WRITE_GUIDES)

    def test_guide_checker_requires_success_and_read_write_marker(self):
        checker = load_script("check_integration_guide.py")
        for code, output, error in ((0, "read only", RuntimeError),
                                    (1, "REDIS_ADBC_READ_WRITE_OK", SystemExit),
                                    (0, "REDIS_ADBC_READ_WRITE_OK", None)):
            with self.subTest(code=code, output=output), patch.object(
                sys, "argv", ["checker", "numpy", "--require-read-write"]
            ), patch.object(checker.subprocess, "run", return_value=subprocess.CompletedProcess([], code, output)):
                if error:
                    with self.assertRaises(error):
                        checker.main()
                else:
                    checker.main()


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo with spaces"
        shutil.copytree(SCRIPTS, self.repo / "python/scripts", ignore=shutil.ignore_patterns("__pycache__"))
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.events = self.root / "events"
        self.results = self.root / "results"
        self.env = {**os.environ, "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
                    "EVENTS": str(self.events), "UV": str(self.bin / "uv"),
                    "CANDIDATE_ENV_ROOT": str(self.root / "envs"),
                    "CANDIDATE_RESULTS_DIR": str(self.results),
                    "CANDIDATE_PROFILES": "", "R_LIBS_USER": str(self.root / "r-library"),
                    "RSCRIPT": str(self.bin / "Rscript"), "QUARTO": str(self.bin / "quarto")}
        self.write_command(self.bin / "make", '#!/bin/bash\necho "make $*" >> "$EVENTS"\n')
        self.write_command(self.bin / "Rscript", '#!/bin/bash\necho Rscript >> "$EVENTS"\n')
        self.write_command(self.bin / "quarto", '#!/bin/bash\nexit 0\n')
        self.python = self.bin / "candidate-python"
        self.write_command(self.python, '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
with open(os.environ["EVENTS"], "a") as out:
    out.write(json.dumps({"pytest": sys.argv[1:], "python": sys.argv[0],
                          "ssl": os.environ.get("SSL_CERT_FILE")}) + "\\n")
xml = os.environ.get("CANDIDATE_JUNIT_XML")
if xml:
    Path(xml).write_text('<testsuite><testcase/></testsuite>')
raise SystemExit(int(os.environ.get("TEST_EXIT", "0")))
''')
        self.env["FAKE_PYTHON"] = str(self.python)
        self.write_command(self.bin / "uv", '''#!/usr/bin/env python3
import json, os, shutil, sys
from pathlib import Path
with open(os.environ["EVENTS"], "a") as out:
    out.write(json.dumps({"uv": sys.argv[1:]}) + "\\n")
if os.environ.get("INSTALL_FAIL") and sys.argv[1] == "pip":
    raise SystemExit(1)
if sys.argv[1] == "venv":
    target = Path(sys.argv[-1]) / "bin/python"
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy(os.environ["FAKE_PYTHON"], target)
''')
        # Keep exceptional hooks observable without downloading runtimes or
        # creating Redis. Actual TLS lifecycle has its own mocked test suite.
        self.write_command(self.repo / "python/scripts/prepare-kestra-candidate.sh",
                           '#!/bin/bash\necho kestra-prepare >> "$EVENTS"\n')
        self.write_command(self.repo / "python/scripts/run-ray-tls-candidates.sh", '''#!/bin/bash
echo ray-tls-owned >> "$EVENTS"
echo '<testsuite><testcase/></testsuite>' > "$CANDIDATE_JUNIT_XML"
''')

    def write_command(self, path, text):
        path.write_text(text)
        path.chmod(0o755)

    def run_script(self, script, *args, **env):
        return subprocess.run([str(self.repo / "python/scripts" / script), *args],
                              env={**self.env, **env}, capture_output=True, text=True)

    def read_events(self):
        return self.events.read_text().splitlines() if self.events.exists() else []

    def test_unknown_tool_and_profile_rejected_before_side_effects(self):
        for args in (("pandas", "unknown"), ("ray-tls", "pandas"),
                     ("ray-tls ",), ("unknown ray-tls",)):
            result = self.run_script("run-candidate-tests.sh", *args)
            self.assertEqual(result.returncode, 2, result.stderr)
        for profiles in ("core unknown", " "):
            result = self.run_script("run-all-native-candidates.sh", CANDIDATE_PROFILES=profiles)
            self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual(self.read_events(), [])
        self.assertFalse(self.results.exists())

    def test_every_profile_preserves_environment_selectors_and_special_hooks(self):
        result = self.run_script("run-all-native-candidates.sh", SSL_CERT_FILE="unchanged")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        events = self.read_events()
        invocations = [json.loads(e) for e in events if e.startswith("{")]
        venvs = [e["uv"] for e in invocations if "uv" in e and e["uv"][0] == "venv"]
        self.assertEqual([Path(v[-1]).name for v in venvs], list(registry.PROFILES))
        self.assertEqual([v[3] for v in venvs], [p.python for p in registry.PROFILES.values()])
        pytest = [e for e in invocations if "pytest" in e]
        self.assertEqual(len(pytest), len(registry.PROFILES) - 1)
        for i, line in enumerate(events):
            if line.startswith("{") and "pytest" in json.loads(line):
                self.assertTrue(events[i - 2].endswith("redis-up"))
                self.assertTrue(events[i - 1].endswith("build"))
        for event in pytest:
            profile = Path(event["python"]).parents[1].name
            expected = [str(self.repo / "python/tests" / t) for tool in registry.PROFILES[profile].candidates
                        for t in registry.CANDIDATES[tool]]
            self.assertEqual(event["pytest"][-len(expected):], expected)
            self.assertEqual(event["ssl"], "unchanged")
        installs = [e["uv"] for e in invocations if "uv" in e and e["uv"][0] == "pip"]
        for name, p in registry.PROFILES.items():
            self.assertTrue(any(f"{self.repo}/python[{p.extras}]" in args
                                and f"/envs/{name}/bin/python" in " ".join(args) for args in installs))
        self.assertTrue(any("redis-dbt-adapter.git@" in " ".join(args) for args in installs))
        self.assertIn("Rscript", events)
        self.assertIn("kestra-prepare", events)
        self.assertEqual(events.count("ray-tls-owned"), 1)
        self.assertEqual(sum(e.endswith("redis-up") for e in events), len(pytest))
        summary = json.loads((self.results / "summary.json").read_text())
        self.assertEqual(list(summary), list(registry.PROFILES))
        self.assertTrue(all(p["status"] == "completed" for p in summary.values()))

    def test_failed_install_discards_stale_report_and_continues(self):
        self.results.mkdir()
        (self.results / "core.xml").write_text('<testsuite><testcase/></testsuite>')
        (self.results / "core.exitcode").write_text("0")
        result = self.run_script("run-all-native-candidates.sh", CANDIDATE_PROFILES="core workflows", INSTALL_FAIL="1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        summary = json.loads((self.results / "summary.json").read_text())
        self.assertEqual(set(summary), {"core", "workflows"})
        self.assertTrue(all(p["status"] == "no_result" and p["exit_code"] == 1 for p in summary.values()))
        self.assertFalse((self.results / "core.xml").exists())
        self.assertFalse(any("pytest" in e for e in self.read_events()))

    def test_test_failure_is_not_hidden_by_successful_report(self):
        result = self.run_script("run-all-native-candidates.sh", CANDIDATE_PROFILES="workflows", TEST_EXIT="3")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        summary = json.loads((self.results / "summary.json").read_text())
        self.assertEqual(summary["workflows"]["status"], "runner_failed")
        self.assertEqual(summary["workflows"]["exit_code"], 3)

    def test_guide_runner_requires_evidence_and_refreshes_summary(self):
        runner = load_script("run-integration-guides.py")
        calls = []
        def run(command, **kwargs):
            calls.append(command)
            if command[2] == "numpy":
                raise subprocess.TimeoutExpired(command, 420)
            if command[2] == "scipy":
                raise OSError("missing interpreter")
            return subprocess.CompletedProcess(command, 0)
        self.results.mkdir()
        (self.results / "summary.json").write_text('{"stale": {"exit_code": 0}}')
        argv = ["runner", "--env-root", str(self.root / "envs"), "--results", str(self.results)]
        with patch.object(sys, "argv", argv), patch.dict(os.environ, {"REDIS_URI": "verified-fixture", "REDIS_ADBC_LIBRARY": "build"}), patch.object(runner.subprocess, "run", run):
            self.assertEqual(runner.main(), 1)
        self.assertEqual([c[2] for c in calls], list(registry.GUIDES))
        for call in calls:
            self.assertEqual("--require-read-write" in call, call[2] in registry.READ_WRITE_GUIDES)
        summary = json.loads((self.results / "summary.json").read_text())
        self.assertNotIn("stale", summary)
        self.assertEqual(summary["numpy"]["exit_code"], 124)
        self.assertEqual(summary["scipy"]["exit_code"], 127)
        self.assertEqual(summary["fivetran"]["read_write"], "not_verified")
        self.assertEqual(summary["pandas"]["read_write"], "passed")


if __name__ == "__main__":
    unittest.main()
