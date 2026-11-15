import importlib.util
from pathlib import Path
import tempfile
import unittest


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


screen = load('point_screen_compare', 'screen_compare.py')
fixtures = load('full_comparison_test_fixtures', 'test_compare.py')


def windows():
    data = fixtures.windows()
    for window in data.values():
        meta = window['metadata']
        meta.update(suite='oltp', benchmark_kind='point_read_target_screen',
                    operation_selection=['point_read'],
                    wrapper_sha256=screen.full.digest(Path(__file__).with_name('point_read.py')))
        window['trials'] = [row for row in window['trials'] if row['operation'] == 'point_read']
    return data


class PointReadScreenAudit(unittest.TestCase):
    def test_exact_screen_counts_pairs_and_restored_full_checker(self):
        before = screen.full.chart_data.POINT
        data = windows()
        result = screen.compare(data)
        self.assertEqual(screen.full.chart_data.POINT, before)
        self.assertEqual(len(result['pooled']), 6)
        self.assertEqual([len(pair['rows']) for pair in result['pairs']], [6, 6])
        self.assertEqual(len([r for r in result['pooled'] if r['mode'] == 'redis_py']), 2)
        self.assertTrue(all(row['baseline']['latency_samples'] == 192 for row in result['pooled']))
        self.assertTrue(all(row['candidate']['measured_trials'] == 6 for row in result['pooled']))
        for window in data.values():
            with self.assertRaises(AssertionError): screen.full.validate(window)

    def test_gate_both_modes_and_raw_controls(self):
        data = windows()
        for name in ['B1', 'B2']:
            fixtures.change(data[name], lambda r: r['mode'] in ['managed', 'unmanaged'] and r['clients'] == 1, .9)
        self.assertTrue(screen.compare(data)['target']['met'])
        fixtures.change(data['B2'], lambda r: r['mode'] == 'unmanaged' and r['clients'] == 1, 1.2)
        self.assertFalse(screen.compare(data)['target']['met'])
        result = screen.compare(data)
        raw = next(row for row in result['pooled'] if row['mode'] == 'redis_py' and row['clients'] == 1)
        self.assertEqual(raw['p50_baseline_over_candidate'], 1.)
        self.assertEqual(result['pairs'][1]['execution_order'], ['candidate', 'baseline'])

    def test_reject_subset_fullsuite_duplicates_errors_bad_identity_and_selection(self):
        for kind in ['missing-window', 'missing-row', 'full-suite', 'duplicate', 'failed', 'p95', 'wrapper', 'selection', 'source', 'packages', 'qpf']:
            data = windows()
            if kind == 'missing-window': data.pop('B2')
            elif kind == 'missing-row': data['B2']['trials'].pop()
            elif kind == 'full-suite': data['B2'] = fixtures.fixture('candidate')
            elif kind == 'duplicate': data['B2']['trials'][-1] = data['B2']['trials'][0]
            elif kind == 'failed': data['A1']['trials'][0]['validated'] = False
            elif kind == 'p95': data['A1']['trials'][0]['p95_latency_ms'] = 1.
            elif kind == 'wrapper': data['B1']['metadata']['wrapper_sha256'] = 'e' * 64
            elif kind == 'selection': data['B1']['metadata']['operation_selection'].append('insert')
            elif kind == 'source': data['A2']['metadata']['driver_source_commit'] = 'e' * 40
            elif kind == 'packages': data['B2']['metadata']['packages']['redis'] = 'different'
            else: data['B2']['final_qpf']['factor'] = 1
            before = screen.full.chart_data.POINT
            with self.subTest(kind=kind), self.assertRaises(AssertionError): screen.compare(data)
            self.assertEqual(screen.full.chart_data.POINT, before)

    def test_valid_failed_gate_outputs_are_complete_and_never_overwritten(self):
        result = screen.compare(windows())
        self.assertFalse(result['target']['met'])
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'screen'
            screen.write_outputs(result, path)
            self.assertEqual(len((path / 'comparison.csv').read_text().splitlines()), 7)
            self.assertEqual(len((path / 'pairs.csv').read_text().splitlines()), 13)
            self.assertIn('Not a full CRUD/query matrix', (path / 'report.md').read_text())
            with self.assertRaises(FileExistsError): screen.write_outputs(result, path)


if __name__ == '__main__':
    unittest.main()
