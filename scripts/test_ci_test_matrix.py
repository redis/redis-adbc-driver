import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from ci_test_matrix import select_matrix


class MatrixTests(unittest.TestCase):
    def test_routine_pr_keeps_two_distinct_connection_paths(self):
        rows = select_matrix("pull_request", ["go/query.go"])["include"]
        self.assertEqual(
            [(r["arch"], r["environment"]) for r in rows],
            [("amd64", "software-single"), ("amd64", "oss-cluster")],
        )
        self.assertEqual([r["environment"] for r in rows if r["integrations"]],
                         ["software-single"])

    def test_sensitive_prs_keep_all_platforms(self):
        for path in (
            "go/database.go", "go/connection.go", "go/store.go",
            "go/partition_balance.go", "go/tls_test.go", "go/timeout.go",
            "go/acl_integration_test.go", "go/go.sum", "go/compose.yaml",
            "go/software-setup.sh", "go/software-qpf.sh", "go/flex-setup.sh",
            "scripts/test_software_qpf.py",
            "go/pkg/driver.go",
            "scripts/check_partition_tls.py", "scripts/test_cluster_ready.py",
            "go/validation/tests/test_partitions.py",
            "go/validation/tests/test_partitions_retained.py",
            ".github/workflows/test.yml", "scripts/ci_test_matrix.py",
        ):
            with self.subTest(path=path):
                self.assert_full(select_matrix("pull_request", ["go/query.go", path]))

    def test_non_pr_events_always_run_everything(self):
        for event in ("schedule", "release", "workflow_dispatch"):
            with self.subTest(event=event):
                self.assert_full(select_matrix(event, []))

    def test_docs_do_not_expand_coverage(self):
        self.assertEqual(len(select_matrix("pull_request", ["go/partitions.md"])["include"]), 2)

    def test_cli_uses_entire_diff_and_catches_renamed_sensitive_files(self):
        script = Path(__file__).with_name("ci_test_matrix.py").resolve()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)

            def git(*args):
                return subprocess.check_output(
                    ["git", "-c", "user.name=CI test", "-c", "user.email=ci@example.invalid",
                     *args], cwd=root, stderr=subprocess.PIPE,
                ).decode().strip()

            git("init")
            (root / "go").mkdir()
            sensitive = root / "go/database.go"
            sensitive.write_text("original\n")
            git("add", ".")
            git("commit", "-m", "base")
            base = git("rev-parse", "HEAD")
            sensitive.rename(root / "go/new_client.go")
            git("add", "-A")
            git("commit", "-m", "move connection handling")
            (root / "go/query.go").write_text("later ordinary change\n")
            git("add", ".")
            git("commit", "-m", "ordinary followup")
            args = [sys.executable, str(script), "--event", "pull_request",
                    "--base", base, "--head", git("rev-parse", "HEAD")]
            self.assert_full(json.loads(subprocess.check_output(args, cwd=root)))
            args[args.index("--base") + 1] = "missing-ref"
            failed = subprocess.run(args, cwd=root, capture_output=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(failed.stdout, b"")

    def assert_full(self, matrix):
        rows = matrix["include"]
        self.assertEqual(len(rows), 8)
        self.assertEqual(len({(r["arch"], r["environment"]) for r in rows}), 8)
        self.assertEqual({r["arch"] for r in rows}, {"amd64", "arm64"})
        self.assertEqual([r["environment"] for r in rows if r["integrations"]],
                         ["oss-single", "oss-single"])


if __name__ == "__main__":
    unittest.main()
