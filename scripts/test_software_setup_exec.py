"""The owned Software runner must verify API policy before executing tests."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "go/software-setup.sh"


class SoftwareSetupExec(unittest.TestCase):
    def run_fixture(self, policy):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = root / "bin"
            tools.mkdir()
            # Never contact Docker or an API; jq still evaluates the real gates.
            (tools / "docker").write_text("#!/bin/sh\nexit 0\n")
            (tools / "curl").write_text("""#!/usr/bin/env python3
import json, os, sys
url = next((a for a in sys.argv[1:] if a.startswith('https://')), '')
config = {'uid': 1, 'name': 'adbc-test', 'status': 'active', 'port': 12000,
          'bigstore': False, 'replication': False, 'eviction_policy': os.environ['POLICY'],
          'shards_count': 1, 'sharding': False, 'version': '8.6.2', 'module_list': [],
          'query_performance_factor': {'active': True, 'scaling_factor': 4},
          'search': {'search-workers': 6}, 'sched_policy': 'mnp', 'conns': 32}
if url.endswith('/bootstrap'):
    result = {'bootstrap_status': {'state': 'completed'}}
elif url.endswith('/bdbs'):
    result = [config]
elif url.endswith('/shards'):
    result = [{'role': 'master', 'status': 'active'}]
else:
    result = config
print(json.dumps(result))
""")
            for tool in tools.iterdir():
                tool.chmod(0o755)
            marker = root / "executed.json"
            command = ["python3", "-c", "import json,os,sys; "
                       "open(sys.argv[1],'w').write(json.dumps({k:os.environ[k] for k in "
                       "('REDIS_URI','REDIS_RETAINED_CONTROL_PERSISTENCE')}))", str(marker)]
            env = dict(os.environ, PATH=f"{tools}:{os.environ['PATH']}", POLICY=policy,
                       SOFTWARE_SHARDS="1", SOFTWARE_DB_PORT="26389")
            result = subprocess.run(["bash", str(SCRIPT), "--exec", *command],
                                    env=env, capture_output=True, text=True, timeout=15)
            return result, json.loads(marker.read_text()) if marker.exists() else None

    def test_verified_endpoint_and_policy_are_passed(self):
        result, executed = self.run_fixture("noeviction")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(executed, {"REDIS_URI": "redis://localhost:26389/0",
                                    "REDIS_RETAINED_CONTROL_PERSISTENCE": "guaranteed"})

    def test_unsafe_policy_never_executes_command(self):
        result, executed = self.run_fixture("allkeys-lru")
        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(executed)
        self.assertIn("does not match", result.stderr)

    def test_missing_command_rejected_before_setup(self):
        result = subprocess.run(["bash", str(SCRIPT), "--exec"], capture_output=True,
                                text=True, timeout=5)
        self.assertEqual(result.returncode, 2)
        self.assertIn("Usage:", result.stderr)


if __name__ == "__main__":
    unittest.main()
