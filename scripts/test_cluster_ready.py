"""Exercise the readiness gate with one primary lagging behind the seed."""

import os
import subprocess
import tempfile
import unittest
from pathlib import Path


class ClusterReadinessTest(unittest.TestCase):
    def test_every_primary_and_search_must_be_ready(self):
        script = Path(__file__).resolve().parents[1] / "go/cluster-ready.sh"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            cli = root / "redis-cli"
            cli.write_text("""#!/bin/sh
while [ "$1" != "-p" ]; do shift; done
port=$2
shift 2
printf '%s %s\\n' "$port" "$*" >> "$CALLS"
if [ "$port" = "${UNREACHABLE_PORT:-}" ]; then exit 1; fi
if [ "$1" = CLUSTER ]; then
  if [ "$port" = "${DOWN_PORT:-}" ]; then
    printf 'cluster_state:fail\\r\\n'
  else
    printf 'cluster_state:ok\\r\\n'
  fi
elif [ "$port" = "${NO_SEARCH_PORT:-}" ]; then
  exit 1
fi
""")
            cli.chmod(0o755)
            calls = root / "calls"
            environment = {
                **os.environ,
                "PATH": f"{root}:{os.environ['PATH']}",
                "CALLS": str(calls),
            }
            cases = [
                {},
                {"DOWN_PORT": "7002"},
                {"DOWN_PORT": "7003"},
                {"UNREACHABLE_PORT": "7002"},
                {"NO_SEARCH_PORT": "7003"},
            ]
            for settings in cases:
                with self.subTest(settings=settings):
                    calls.write_text("")
                    result = subprocess.run(
                        ["sh", str(script)],
                        env={**environment, **settings},
                        capture_output=True,
                        check=False,
                    )
                    self.assertEqual(result.returncode == 0, not settings)
                    if not settings:
                        self.assertEqual(
                            calls.read_text().splitlines(),
                            [
                                f"{port} {command}"
                                for port in (7001, 7002, 7003)
                                for command in ("CLUSTER INFO", "FT._LIST")
                            ],
                        )


if __name__ == "__main__":
    unittest.main()
