"""Regression checks for failed scan completion and actionable safe diagnostics."""
import unittest
from unittest.mock import Mock, patch
import contextlib
import io
import json
import tempfile
from pathlib import Path

from diagnostics import error_details
from scan_paths import finish_report
from backup_health import check_primary
from compare import main as compare_main


class CompletionTests(unittest.TestCase):
    def report(self):
        return {'cases': [{'name': name} for name in ('a', 'b', 'c')],
                'trials': [{'case': name, 'trial': trial, 'api': api, 'seconds': 1.0}
                           for name in ('a', 'b', 'c') for trial in range(1, 4)
                           for api in ('statement', 'partitions')],
                'comparisons': [{'case': name, 'trial': trial, 'timings_eligible': True}
                                for name in ('a', 'b', 'c') for trial in range(1, 4)]}

    def test_finished_attempts_with_failed_measurements_are_not_complete(self):
        report = self.report()
        report['trials'][0].update(seconds=None, error_type='OperationalError')
        report['comparisons'][0]['timings_eligible'] = False
        finish_report(report)
        self.assertTrue(report['attempts_complete'])
        self.assertFalse(report['complete'])
        self.assertEqual(report['status'], 'failed')
        self.assertEqual(report['valid_pairs'], 8)

    def test_missing_pair_cannot_pass(self):
        report = self.report()
        report['comparisons'].pop()
        finish_report(report)
        self.assertFalse(report['complete'])

    def test_all_required_valid_pairs_pass(self):
        report = self.report()
        finish_report(report)
        self.assertTrue(report['complete'])
        self.assertEqual(report['status'], 'complete')

    def test_duplicate_measurement_cannot_replace_missing_api(self):
        report = self.report()
        report['trials'][0] = dict(report['trials'][1])
        finish_report(report)
        self.assertFalse(report['attempts_complete'])
        self.assertFalse(report['complete'])

    def test_nonfinite_time_cannot_pass(self):
        report = self.report()
        report['trials'][0]['seconds'] = float('nan')
        finish_report(report)
        self.assertFalse(report['complete'])


class DiagnosticTests(unittest.TestCase):
    def test_error_keeps_cause_but_not_uri_or_secrets(self):
        uri = 'rediss://alice:p%40ss@example.test:6379/0?token=private'
        result = error_details(RuntimeError('connection rejected: ' + uri), uri)
        self.assertIn('connection rejected', result['message'])
        for secret in (uri, 'alice', 'p%40ss', 'private'):
            self.assertNotIn(secret, result['message'])

    def test_other_urls_are_redacted(self):
        result = error_details(RuntimeError('failed https://host/path?X-Amz-Signature=secret'))
        self.assertNotIn('secret', result['message'])


class BackupHealthTests(unittest.TestCase):
    def client(self):
        client = Mock()
        client.execute_command.return_value = [b'num_docs', 100, b'indexing', 0]
        client.cluster.return_value = {'cluster_state': 'ok'}
        client.info.return_value = {'loading': 0}
        return client

    def test_cluster_health_uses_cluster_info_not_info_cluster(self):
        client = self.client()
        check_primary(client, 'index', 100, 'primary-03')
        client.cluster.assert_called_once_with('info')
        client.info.assert_called_once_with('persistence')

    def test_count_failure_names_primary_and_expected_actual(self):
        client = self.client()
        with self.assertRaisesRegex(RuntimeError, r'primary-03.*expected=101.*actual=100'):
            check_primary(client, 'index', 101, 'primary-03')


class ComparisonTests(unittest.TestCase):
    def test_correct_but_semantically_excluded_query_has_no_ratio(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            run = {'metadata': {'placement': {}, 'loaded_rows': 99997497,
                                'official_scale': True,
                                'comparison_excluded_queries': {'29': 'Different regex semantics'}},
                   'result': [[2.0, 2.0, 2.0] for _ in range(43)]}
            (root / 'run.json').write_text(json.dumps(run))
            (root / 'run-correctness.json').write_text(json.dumps({'checks': [
                {'query': q, 'trial': t, 'status': 'valid'}
                for q in range(1, 44) for t in (2, 3)]}))
            (root / 'baseline.json').write_text(json.dumps({'system': 'baseline', 'machine': 'single',
                                                          'result': [[1.0] * 3 for _ in range(43)]}))
            output = io.StringIO()
            with patch('sys.argv', ['compare', str(root / 'run.json'), str(root / 'baseline.json')]), \
                    patch('run.validate_placement'), contextlib.redirect_stdout(output):
                compare_main()
            lines = output.getvalue().splitlines()
            self.assertIn('2.00×', next(line for line in lines if line.startswith('| 28 |')))
            self.assertNotIn('×', next(line for line in lines if line.startswith('| 29 |')))
            self.assertIn('comparison excluded', next(line for line in lines if line.startswith('| 29 |')))


if __name__ == '__main__':
    unittest.main()
