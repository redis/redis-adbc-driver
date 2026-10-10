"""Opt-in real cgroup OOM/timeout tests on an isolated Linux systemd host."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

from isolation import check_isolation, run_isolated


@unittest.skipUnless(os.environ.get('CLICKBENCH_SYSTEMD_TEST') == '1',
                     'requires opt-in on a disposable Linux systemd host')
class LinuxIsolation(unittest.TestCase):
    def test_real_oom_does_not_kill_supervisor_or_next_trial(self):
        check_isolation(64)
        record = run_isolated([sys.executable, '-c',
                              'data = [bytearray(1024*1024) for _ in range(256)]'], 15, 64)
        self.assertEqual(record.get('error_type'), 'MemoryLimit', record)
        followup = run_isolated(['/bin/true'], 10, 64)
        self.assertEqual(followup.get('exit_code'), 0, followup)
        self.assertNotIn('error_type', followup)

    def test_real_timeout_kills_descendants(self):
        with tempfile.TemporaryDirectory() as tmp:
            pidfile = Path(tmp) / 'child.pid'
            script = (
                'import subprocess,time,pathlib; '
                'p=subprocess.Popen(["/bin/sleep","120"]); '
                f'pathlib.Path({str(pidfile)!r}).write_text(str(p.pid)); '
                'time.sleep(120)'
            )
            record = run_isolated([sys.executable, '-c', script], 3, 64)
            self.assertEqual(record.get('error_type'), 'QueryDeadline', record)
            pid = int(pidfile.read_text())
            for _ in range(30):
                try:
                    os.kill(pid, 0)
                except ProcessLookupError:
                    break
                time.sleep(0.1)
            else:
                self.fail('Timed-out trial left a running descendant')

    def test_credentials_reach_trial_only_through_stdin(self):
        record = run_isolated([sys.executable, '-c',
                              'import sys,json; assert json.load(sys.stdin)["uri"] == "test-secret"'],
                              10, 64, credentials={'uri': 'test-secret'})
        self.assertEqual(record.get('exit_code'), 0, record)


if __name__ == '__main__':
    unittest.main()
