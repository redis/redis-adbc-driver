"""Protect fixture isolation, coverage and failure propagation when splitting CI."""
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
    def test_suites_select_identical_architectures_and_own_verified_fixtures(self):
        for name in ('driver', 'snapshots', 'integrations'):
            with self.subTest(job=name):
                source = job(name)
                self.assertRegex(source, r'(?m)^    needs: coverage$')
                self.assertIn('matrix: ${{ fromJSON(needs.coverage.outputs.matrix) }}', source)
                self.assertIn('runs-on: ${{ matrix.runner }}', source)
                self.assertIn('fail-fast: false', source)
                self.assertIn('working-directory: go\n        run: make redis-up', source)
                self.assertRegex(source, r'if: always\(\)\n        working-directory: go\n        run: make redis-down')
                if name != 'driver':
                    self.assertNotIn('continue-on-error: true', source)

    def test_expensive_suites_are_independent_and_keep_execution_flags(self):
        driver, snapshots, integrations = (job(n) for n in ('driver', 'snapshots', 'integrations'))
        self.assertIn('go test -v -count=1 -p 1 -timeout=30m ./...', driver)
        self.assertNotIn('TEST_ARGS=', driver)
        self.assertNotIn('build-adbc-scanner', driver)
        self.assertIn('make test-software TEST_ARGS="-race -v -run Snapshot"', snapshots)
        self.assertIn('export REDIS_SNAPSHOT_URI="${REDIS_URI:?verified Software fixture required}"', snapshots)
        for name in ('Python analytics integrations', 'Ray worker datasource',
                     'Airflow integration', 'dlt integration on Redis Software'):
            self.assertIn(f'- name: {name}', integrations)
            self.assertNotIn(f'- name: {name}', driver)
        for name in ('Python ADBC validation', 'Native C Device CPU ownership and binding',
                     'Enforced SQL SELECT grants through Software RBAC',
                     'Native partitions over Software proxy TLS and independent RBAC'):
            self.assertIn(f'- name: {name}', driver)
        self.assertIn("if: matrix.arch == 'amd64' && matrix.environment == 'software-cluster'", integrations)
        self.assertIn('REQUIRE_RAY_TLS:', integrations)
        self.assertIn('uses: ./.github/actions/build-adbc-scanner', integrations)

    def test_original_check_names_wait_for_every_suite_even_on_failure(self):
        gate = job('linux')
        self.assertIn('name: linux-${{ matrix.arch }}-${{ matrix.environment }}', gate)
        self.assertIn("if: always() && needs.coverage.result == 'success'", gate)
        self.assertIn('needs: [coverage, driver, snapshots, integrations]', gate)
        self.assertIn('matrix: ${{ fromJSON(needs.coverage.outputs.matrix) }}', gate)
        for name in ('driver', 'snapshots', 'integrations'):
            self.assertIn('${{ needs.' + name + '.result }}', gate)

    def test_gate_rejects_failure_cancellation_and_skips(self):
        # Execute the real gate shell for every upstream outcome combination.
        shell = textwrap.dedent(job('linux').split('        run: |\n', 1)[1])
        keys = ('DRIVER_RESULT', 'SNAPSHOTS_RESULT', 'INTEGRATIONS_RESULT')
        for results in itertools.product(('success', 'failure', 'cancelled', 'skipped'), repeat=3):
            with self.subTest(results=results):
                result = subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', shell],
                                        env=dict(os.environ, **dict(zip(keys, results))),
                                        capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode == 0, all(r == 'success' for r in results))


if __name__ == '__main__':
    unittest.main()
