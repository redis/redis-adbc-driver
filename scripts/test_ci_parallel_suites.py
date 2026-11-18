"""Protect fixture isolation, complete shard matrices and required gates."""
import itertools
import os
from pathlib import Path
import re
import subprocess
import textwrap
import unittest

WORKFLOW = (Path(__file__).resolve().parents[1] / '.github/workflows/test.yml').read_text()


def job(name):
    match = re.search(rf'^  {re.escape(name)}:\n(.*?)(?=^  [a-z][\w-]*:|\Z)',
                      WORKFLOW, re.MULTILINE | re.DOTALL)
    if match is None:
        raise AssertionError(f'Missing job {name}')
    return match[1]


class ParallelSuiteTests(unittest.TestCase):
    def test_mutating_suites_own_verified_fixtures_and_keep_architectures(self):
        for name, matrix in (('driver', 'matrix'), ('go_tests', 'go_matrix'),
                             ('snapshots', 'snapshot_matrix'), ('integrations', 'matrix')):
            with self.subTest(job=name):
                source = job(name)
                self.assertRegex(source, r'(?m)^    needs: coverage$')
                self.assertIn('matrix: ${{ fromJSON(needs.coverage.outputs.' + matrix + ') }}', source)
                self.assertIn('runs-on: ${{ matrix.runner }}', source)
                self.assertIn('fail-fast: false', source)
                self.assertIn('working-directory: go\n        run: make redis-up', source)
                self.assertRegex(source, r'if: always\(\)\n        working-directory: go\n        run: make redis-down')
                if name != 'driver':
                    self.assertNotIn('continue-on-error: true', source)
        for name in ('go_tests', 'snapshots'):
            self.assertIn('bash software-setup.sh --exec python3 ../scripts/ci_go_suites.py run', job(name))
            self.assertIn('if: always()\n        uses: actions/upload-artifact', job(name))
        self.assertIn('--shards 3', job('coverage'))
        self.assertIn('--shards 4', job('coverage'))

    def test_go_bridge_and_native_checks_run_independently(self):
        driver, snapshots, integrations, bridge, go_tests = (
            job(n) for n in ('driver', 'snapshots', 'integrations', 'bridge', 'go_tests'))
        self.assertNotIn('- name: Go tests', driver)
        self.assertNotIn('go test -v -count=1', driver)
        self.assertNotIn('GOEXPERIMENT=cgocheck2', driver)
        self.assertIn('GOEXPERIMENT=cgocheck2', bridge)
        self.assertIn('-race -gcflags=all=-d=checkptr=2 -tags driverlib ./pkg', bridge)
        self.assertNotIn('redis-up', bridge)
        self.assertIn('unset REDIS_SNAPSHOT_URI', go_tests)
        self.assertIn('--suite ordinary --shard "$SHARD" --shards "$SHARDS"', go_tests)
        self.assertIn('--suite snapshot --shard "$SHARD" --shards "$SHARDS"', snapshots)
        self.assertIn('export REDIS_SNAPSHOT_URI="${REDIS_URI:?verified Software fixture required}"', snapshots)
        self.assertNotIn('build-adbc-scanner', driver)
        self.assertIn('- name: Python analytics integrations', integrations)
        for name in ('Python ADBC validation', 'Native C Device CPU ownership and binding',
                     'Enforced SQL SELECT grants through Software RBAC',
                     'Native partitions over Software proxy TLS and independent RBAC'):
            self.assertIn(f'- name: {name}', driver)
        for moved in ('Ray worker datasource', 'Airflow integration', 'dlt integration on Redis Software'):
            self.assertNotIn(f'- name: {moved}', integrations)
        self.assertIn('uses: ./.github/actions/build-adbc-scanner', integrations)

    def test_original_check_names_wait_for_every_suite_and_verify_coverage(self):
        gate = job('linux')
        self.assertIn('name: linux-${{ matrix.arch }}-${{ matrix.environment }}', gate)
        self.assertIn("if: always() && needs.coverage.result == 'success'", gate)
        self.assertIn('needs: [coverage, bridge, go_tests, driver, snapshots, integrations]', gate)
        self.assertIn('matrix: ${{ fromJSON(needs.coverage.outputs.matrix) }}', gate)
        for name in ('bridge', 'go_tests', 'driver', 'snapshots', 'integrations'):
            self.assertIn('${{ needs.' + name + '.result }}', gate)
        self.assertIn('pattern: go-tests-${{ matrix.arch }}-${{ matrix.environment }}-*', gate)
        self.assertIn('scripts/ci_go_suites.py verify --reports go-results', gate)

    def test_gate_rejects_failure_cancellation_and_skips_in_every_upstream(self):
        # Execute the actual gate shell, including each upstream outcome position.
        match = re.search(r'        run: \|\n((?:          .*\n|\n)*)', job('linux'))
        shell = textwrap.dedent(match[1])
        keys = ('BRIDGE_RESULT', 'GO_RESULT', 'DRIVER_RESULT', 'SNAPSHOTS_RESULT', 'INTEGRATIONS_RESULT')
        cases = [('success',) * len(keys)]
        for index, outcome in itertools.product(range(len(keys)), ('failure', 'cancelled', 'skipped')):
            case = ['success'] * len(keys)
            case[index] = outcome
            cases.append(tuple(case))
        for results in cases:
            with self.subTest(results=results):
                result = subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', shell],
                                        env=dict(os.environ, **dict(zip(keys, results))),
                                        capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode == 0, all(r == 'success' for r in results))


if __name__ == '__main__':
    unittest.main()
