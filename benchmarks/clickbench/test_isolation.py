"""Protect the supervisor from OOMs, descendants and credential exposure."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from isolation import MIB, memory_capacity, resolve_memory_limit, run_isolated


class Isolation(unittest.TestCase):
    def test_memory_budget_accounts_for_ancestor_headroom(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            proc, cg = root / 'proc', root / 'cgroup'
            (proc / 'self').mkdir(parents=True)
            (cg / 'system.slice/runner.service').mkdir(parents=True)
            (proc / 'meminfo').write_text('MemTotal: 131072 kB\nMemAvailable: 122880 kB\n')
            (proc / 'self/cgroup').write_text('0::/system.slice/runner.service\n')
            (cg / 'cgroup.controllers').write_text('memory cpu')
            for path, limit, used in [(cg, 'max', 0), (cg / 'system.slice', 96 * MIB, 32 * MIB),
                                       (cg / 'system.slice/runner.service', 'max', 8 * MIB)]:
                (path / 'memory.max').write_text(str(limit))
                (path / 'memory.current').write_text(str(used))
            self.assertEqual(memory_capacity(proc, cg), 64 * MIB)

    def test_default_budget_reserves_half_and_rejects_unsafe_override(self):
        with patch('isolation.memory_capacity', return_value=1024 * MIB):
            self.assertEqual(resolve_memory_limit(), 512)
            self.assertEqual(resolve_memory_limit(768), 768)
            for invalid in (0, 127, 769):
                with self.assertRaises(ValueError):
                    resolve_memory_limit(invalid)

    def test_oom_is_recorded_and_next_trial_can_succeed(self):
        launches, stopped = [], []
        results = iter(['oom-kill', 'success'])
        current = None

        def execute(args, **kwargs):
            nonlocal current
            if args[0] == 'systemd-run':
                launches.append((args, kwargs))
                current = next(results)
                return subprocess.CompletedProcess(args, 137 if current == 'oom-kill' else 0)
            if 'show' in args:
                return subprocess.CompletedProcess(args, 0, stdout=f'Result={current}\nMemoryPeak=67108864\n')
            if 'stop' in args:
                stopped.append(args[-1])
            return subprocess.CompletedProcess(args, 0, stdout='')

        with patch('isolation.subprocess.run', side_effect=execute):
            first = run_isolated(['/bin/true'], 10, 64, credentials={'uri': 'secret-connection'})
            second = run_isolated(['/bin/true'], 10, 64)
        self.assertEqual(first['error_type'], 'MemoryLimit')
        self.assertIsNone(first['seconds'])
        self.assertEqual(first['trial_memory_peak_bytes'], 64 * MIB)
        self.assertNotIn('error_type', second)
        self.assertEqual(len(stopped), 2)
        self.assertNotEqual(stopped[0], stopped[1])
        self.assertIn(f'--property=MemoryMax={64 * MIB}', launches[0][0])
        self.assertIn('--property=MemorySwapMax=0', launches[0][0])
        self.assertIn('--property=KillMode=control-group', launches[0][0])
        self.assertNotIn('secret-connection', ' '.join(launches[0][0]))
        self.assertEqual(json.loads(launches[0][1]['input']), {'uri': 'secret-connection'})
        self.assertNotIn('secret-connection', json.dumps(first))

    def test_timeout_stops_remote_unit_not_only_launcher(self):
        calls = []

        def execute(args, **kwargs):
            calls.append(args)
            if args[0] == 'systemd-run':
                raise subprocess.TimeoutExpired(args, 25)
            return subprocess.CompletedProcess(args, 0, stdout='Result=timeout\n')

        with patch('isolation.subprocess.run', side_effect=execute):
            record = run_isolated(['/bin/sleep', '1000'], 10, 256)
        self.assertEqual(record['error_type'], 'QueryDeadline')
        self.assertIn('--property=RuntimeMaxSec=10s', calls[0])
        self.assertTrue(any('stop' in command for command in calls))

    def test_systemd_failure_is_not_a_successful_query(self):
        def execute(args, **kwargs):
            return subprocess.CompletedProcess(args, 1 if args[0] == 'systemd-run' else 0, stdout='')
        with patch('isolation.subprocess.run', side_effect=execute):
            self.assertEqual(run_isolated(['/bin/true'], 10, 256)['error_type'], 'WorkerExit')


if __name__ == '__main__':
    unittest.main()
