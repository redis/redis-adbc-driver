"""The owned Software runner must verify API policy before executing tests."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "go/software-setup.sh"


class SoftwareSetupExec(unittest.TestCase):
    def run_fixture(self, policy="noeviction", **overrides):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture = root / "go"
            fixture.mkdir()
            for name in ("software-setup.sh", "software-qpf.sh"):
                shutil.copyfile(SCRIPT.parent / name, fixture / name)
            tools = root / "bin"
            tools.mkdir()
            # Never contact Docker or an API; jq still evaluates the real gates.
            (tools / "docker").write_text("#!/bin/sh\nexit 0\n")
            # Fail immediately at a retry: a rejected preflight must not execute.
            (tools / "sleep").write_text("#!/bin/sh\nexit 1\n")
            (tools / "curl").write_text("""#!/usr/bin/env python3
import json, os, sys
url = next((a for a in sys.argv[1:] if a.startswith('https://')), '')
config = {'uid': 1, 'name': 'adbc-test', 'status': 'active', 'port': 12000,
          'bigstore': False, 'replication': False, 'eviction_policy': os.environ['POLICY'],
          'tls_mode': os.environ.get('TLS_MODE', 'enabled'),
          'shards_count': int(os.environ.get('SHARDS', '1')), 'sharding': False, 'version': '8.6.2', 'module_list': [],
          'query_performance_factor': {'active': os.environ.get('QPF_ACTIVE', 'true') == 'true',
                                       'scaling_factor': int(os.environ.get('QPF_FACTOR', '4'))},
          'search': {'search-workers': int(os.environ.get('WORKERS', '6'))}, 'sched_policy': 'mnp', 'conns': 32}
if url.endswith('/bootstrap'):
    result = {'bootstrap_status': {'state': 'completed'}}
elif url.endswith('/bdbs'):
    result = [config]
elif url.endswith('/shards'):
    result = [{'role': 'master', 'status': os.environ.get('SHARD_STATUS', 'active')}]
else:
    result = config
print(json.dumps(result))
""")
            for tool in tools.iterdir():
                tool.chmod(0o755)
            marker = root / "executed.json"
            command = ["python3", "-c", "import json,os,sys; "
                       "open(sys.argv[1],'w').write(json.dumps({k:os.environ[k] for k in "
                       "('REDIS_URI','SSL_CERT_FILE','REDIS_RETAINED_CONTROL_PERSISTENCE')}))", str(marker)]
            env = dict(os.environ, PATH=f"{tools}:{os.environ['PATH']}", POLICY=policy,
                       SOFTWARE_SHARDS="1", SOFTWARE_DB_PORT="26389",
                       SOFTWARE_DB_PASSWORD="fixture p@ss:/",
                       REDIS_URI="redis://unverified.invalid:6379/0",
                       SSL_CERT_FILE="/unverified/ca.pem",
                       REDIS_RETAINED_CONTROL_PERSISTENCE="inherited-unverified")
            env.pop("GITHUB_ENV", None)
            env.update(overrides)
            result = subprocess.run(["bash", str(fixture / SCRIPT.name), "--exec", *command],
                                    env=env, capture_output=True, text=True, timeout=15)
            executed = json.loads(marker.read_text()) if marker.exists() else None
            if executed is not None:
                self.assertEqual(executed.pop("SSL_CERT_FILE"), str(fixture / "build/test-tls/ca.pem"))
                self.assertTrue((fixture / "build/test-tls/ca.pem").is_file())
                self.assertNotIn("REDIS_RETAINED_CONTROL_PERSISTENCE",
                                 (fixture / "build/test.env").read_text())
            return result, executed

    def test_verified_endpoint_and_policy_are_passed(self):
        result, executed = self.run_fixture("noeviction")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(executed, {"REDIS_URI": "rediss://default:fixture%20p%40ss%3A%2F@localhost:26389/0",
                                    "REDIS_RETAINED_CONTROL_PERSISTENCE": "guaranteed"})

    def test_unsafe_policy_never_executes_command(self):
        result, executed = self.run_fixture("allkeys-lru")
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(executed)
        self.assertIn("does not match", result.stderr)

    def test_unknown_policy_never_executes_command(self):
        result, executed = self.run_fixture("")
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(executed)

    def test_topology_tls_and_effective_qpf_are_required(self):
        for overrides in ({"SHARDS": "3"}, {"TLS_MODE": "disabled"},
                          {"QPF_ACTIVE": "false"}, {"QPF_FACTOR": "2"},
                          {"WORKERS": "2"}, {"SHARD_STATUS": "pending"}):
            with self.subTest(overrides=overrides):
                result, executed = self.run_fixture(**overrides)
                self.assertNotEqual(result.returncode, 0)
                self.assertIsNone(executed)

    def test_missing_command_rejected_before_setup(self):
        result = subprocess.run(["bash", str(SCRIPT), "--exec"], capture_output=True,
                                text=True, timeout=5)
        self.assertEqual(result.returncode, 2)
        self.assertIn("Usage:", result.stderr)


if __name__ == "__main__":
    unittest.main()
