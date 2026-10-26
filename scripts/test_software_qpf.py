"""Exercise the fixture's QPF gate against accepted and drifting API replies."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from software_partition_tls import qpf_4x_ready


READY = {'status': 'active',
         'query_performance_factor': {'active': True, 'scaling_factor': 4},
         'search': {'search-workers': 6}, 'sched_policy': 'mnp', 'conns': 32}


class SoftwareQPF(unittest.TestCase):
    def run_gate(self, initial, updated):
        helper = Path(__file__).resolve().parents[1] / 'go/software-qpf.sh'
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'current').write_text(json.dumps(initial))
            (root / 'updated').write_text(json.dumps(updated))
            script = r'''
set -euo pipefail
api=https://localhost:9443/v1
uid=1
call() {
    if [[ " $* " == *" -X PUT "* ]]; then
        while [ "$1" != -d ]; do shift; done
        printf '%s' "$2" > "$ROOT/request"
        cp "$ROOT/updated" "$ROOT/current"
    else
        cat "$ROOT/current"
    fi
}
wait_for() {
    message=$1; shift
    for attempt in 1 2 3; do if "$@"; then return 0; fi; done
    echo "$message" >&2
    return 1
}
source "$HELPER"
software_qpf_4x
echo READY
'''
            result = subprocess.run(['bash', '-c', script], text=True, capture_output=True,
                env=dict(os.environ, ROOT=str(root), HELPER=str(helper)))
            request = root / 'request'
            return result, json.loads(request.read_text()) if request.exists() else None

    def test_standard_database_is_upgraded_before_ready(self):
        initial = dict(READY, query_performance_factor={'active': False, 'scaling_factor': 0},
                       search={'search-workers': 0}, sched_policy='cmp', conns=5)
        result, request = self.run_gate(initial, READY)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(request, {'query_performance_factor': {'active': True, 'scaling_factor': 4}})
        self.assertIn('READY', result.stdout)

    def test_already_ready_database_is_not_reconfigured(self):
        result, request = self.run_gate(READY, {})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(request)

    def test_effective_configuration_drift_fails_closed(self):
        for bad in [dict(READY, search={'search-workers': 0}),
                    dict(READY, search=None),
                    dict(READY, sched_policy='cmp'), dict(READY, conns=5),
                    dict(READY, status='pending'),
                    dict(READY, query_performance_factor={'active': True, 'scaling_factor': 2})]:
            with self.subTest(bad=bad):
                result, _ = self.run_gate(bad, bad)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('READY', result.stdout)
                self.assertIn('effective 4x QPF', result.stderr)
                self.assertFalse(qpf_4x_ready(bad))
        self.assertTrue(qpf_4x_ready(READY))


if __name__ == '__main__':
    unittest.main()
