import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from ci_test_matrix import select_matrix, select_release_matrix


class MatrixTests(unittest.TestCase):
    def test_routine_pr_uses_authenticated_enterprise_cluster(self):
        rows = select_matrix("pull_request", ["go/query.go"])["include"]
        self.assertEqual(
            [(r["arch"], r["environment"]) for r in rows],
            [("amd64", "software-cluster")],
        )
        self.assertEqual([r["environment"] for r in rows if r["integrations"]],
                         ["software-cluster"])

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
        self.assertEqual(len(select_matrix("pull_request", ["go/partitions.md"])["include"]), 1)

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
            release_args = [*args, "--suite", "release"]
            release = json.loads(subprocess.check_output(release_args, cwd=root))
            self.assertEqual(len(release["arch"]) * len(release["image"]), 10)
            args[args.index("--base") + 1] = "missing-ref"
            failed = subprocess.run(args, cwd=root, capture_output=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(failed.stdout, b"")
            failed_release = subprocess.run([*args, "--suite", "release"], cwd=root,
                                            capture_output=True)
            self.assertNotEqual(failed_release.returncode, 0)
            self.assertEqual(failed_release.stdout, b"")

    def assert_full(self, matrix):
        rows = matrix["include"]
        self.assertEqual(len(rows), 2)
        self.assertEqual(len({(r["arch"], r["environment"]) for r in rows}), 2)
        self.assertEqual({r["arch"] for r in rows}, {"amd64", "arm64"})
        self.assertEqual([r["environment"] for r in rows if r["integrations"]],
                         ["software-cluster", "software-cluster"])


class ReleaseMatrixTests(unittest.TestCase):
    def test_routine_packaging_pr_checks_oldest_glibc_on_both_architectures(self):
        matrix = select_release_matrix("pull_request", ["python/adbc_driver_redis/dbapi.py"])
        self.assertEqual(matrix, {"arch": ["amd64", "arm64"], "image": ["almalinux:8"]})

    def test_compatibility_changes_keep_all_distributions(self):
        for path in (
            "go/build-linux.sh", "go/Dockerfile.linux-builder", "go/go.mod",
            "go/pkg/driver.go", "go/validation/tests/test_connection.py",
            "go/partition.go", "go/software-setup.sh", "go/compose.yaml",
            "go/foreign.go", "go/transport.go", "go/aggregate.go", "go/rekey.go",
            "python/pyproject.toml", "scripts/build_python_wheel.py",
            "scripts/install_driver.py", "scripts/check_wheel_libraries.py",
            ".github/workflows/release.yml", ".github/workflows/wheel-client.yml",
            "scripts/ci_test_matrix.py", "scripts/test_ci_test_matrix.py",
        ):
            with self.subTest(path=path):
                self.assertEqual(len(select_release_matrix("pull_request", [path])["image"]), 5)

    def test_releases_and_manual_runs_keep_all_distributions(self):
        for event in ("release", "workflow_dispatch", "schedule"):
            with self.subTest(event=event):
                matrix = select_release_matrix(event, [])
                self.assertEqual(len(matrix["arch"]) * len(matrix["image"]), 10)

    def test_docs_do_not_expand_release_coverage(self):
        matrix = select_release_matrix("pull_request", ["go/validation/README.md"])
        self.assertEqual(matrix["image"], ["almalinux:8"])


if __name__ == "__main__":
    unittest.main()
