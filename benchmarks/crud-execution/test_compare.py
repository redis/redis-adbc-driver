import copy
import importlib.util
import itertools
import json
import math
from pathlib import Path
import statistics
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('crud_abba_compare', Path(__file__).with_name('compare.py'))
compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compare)


def fixture(build):
    metadata = copy.deepcopy(compare.FIXED_METADATA)
    metadata.update(driver_source_commit='a' * 40 if build == 'baseline' else 'c' * 40,
                    driver_sha256='b' * 64 if build == 'baseline' else 'd' * 64,
                    driver_version='v0.0.0-' + build,
                    harness_sha256=compare.digest(compare.TABLE_MODES / 'scenarios.py'),
                    packages={'pyarrow': '26.0.1', 'adbc-driver-manager': '1.13.0', 'redis': '8.1.0', 'hiredis': '3.5.0'})
    data = {'metadata': metadata, 'status': 'complete', 'failures': [],
            'final_qpf': copy.deepcopy(compare.SETTINGS), 'trials': []}
    for suite, operations in [('oltp', compare.chart_data.POINT), ('olap', compare.chart_data.QUERIES)]:
        count = 32 if suite == 'oltp' else 8
        for op, mode, clients, repeat in itertools.product(operations, ('managed', 'unmanaged', 'redis_py'), (1, 4), (-1, 0, 1, 2)):
            values = [1000000] * (count // 4) + [2000000] * (count // 2) + [3000000] * (count // 4)
            data['trials'].append({'suite': suite, 'operation': op, 'mode': mode, 'clients': clients,
                'repeat': repeat, 'warmup': repeat == -1, 'columns': 4,
                'fixture_rows': 1000 if suite == 'oltp' else 100000, 'operations': count,
                'validated': True, 'status': 'validated', 'latency_ns': values,
                'seconds': 1., 'median_latency_ms': 2., 'p95_latency_ms': 3.,
                'operations_per_second': float(count)})
    return data


def windows():
    a, b = fixture('baseline'), fixture('candidate')
    return {'A1': a, 'B1': b, 'B2': copy.deepcopy(b), 'A2': copy.deepcopy(a)}


def change(data, predicate, factor, seconds_factor=None):
    for row in data['trials']:
        if predicate(row):
            row['latency_ns'] = [round(v * factor) for v in row['latency_ns']]
            row['median_latency_ms'] = statistics.median(row['latency_ns']) / 1e6
            row['p95_latency_ms'] = sorted(row['latency_ns'])[math.ceil(len(row['latency_ns']) * .95) - 1] / 1e6
            row['seconds'] *= factor if seconds_factor is None else seconds_factor
            row['operations_per_second'] = row['operations'] / row['seconds']


class ABBAComparison(unittest.TestCase):
    def test_complete_grid_pairs_samples_means_and_finite_throughput(self):
        result = compare.compare(windows())
        self.assertEqual(result['execution_order'], ['A1', 'B1', 'B2', 'A2'])
        self.assertEqual(len(result['pooled']), 42)
        self.assertEqual([len(pair['rows']) for pair in result['pairs']], [42, 42])
        self.assertEqual(result['pairs'][1]['execution_order'], ['candidate', 'baseline'])
        self.assertFalse(result['target']['met'])
        for row in result['pooled']:
            point = row['suite'] == 'oltp'
            self.assertEqual(row['baseline']['latency_samples'], 192 if point else 48)
            self.assertEqual(row['candidate']['measured_trials'], 6)
            self.assertEqual(row['baseline']['p50_ms'], 2.)
            self.assertEqual(row['baseline']['p95_ms'], 3.)
            self.assertEqual(row['baseline']['mean_ms'], 2.)
            self.assertEqual(row['baseline']['summed_trial_operations_per_second'], 32. if point else 8.)
            self.assertEqual(row['p50_baseline_over_candidate'], 1.)
            self.assertEqual(row['candidate_elapsed_over_redis_py'], 1.)

    def test_gate_requires_both_modes_and_keeps_other_regressions(self):
        data = windows()
        for name in ['B1', 'B2']:
            change(data[name], lambda r: r['operation'] == 'point_read' and r['mode'] in ['managed', 'unmanaged'] and r['clients'] == 1, .8)
            change(data[name], lambda r: r['operation'] == 'update' and r['mode'] == 'unmanaged' and r['clients'] == 4, 1.5)
        result = compare.compare(data)
        self.assertTrue(result['target']['met'])
        slow = next(r for r in result['pooled'] if r['operation'] == 'update' and r['mode'] == 'unmanaged' and r['clients'] == 4)
        self.assertEqual(slow['candidate']['p50_ms'], 3.)
        self.assertEqual(slow['p50_elapsed_reduction_percent'], -50.)
        for name in ['B1', 'B2']:
            change(data[name], lambda r: r['operation'] == 'point_read' and r['mode'] == 'unmanaged' and r['clients'] == 1, 1.25)
        result = compare.compare(data)
        self.assertFalse(result['target']['met'])
        self.assertEqual([r['met'] for r in result['target']['cases']], [True, False])

    def test_target_boundary_and_unrounded_ratios(self):
        data = windows()
        for name in ['B1', 'B2']:
            change(data[name], lambda r: r['operation'] == 'point_read' and r['mode'] in ['managed', 'unmanaged'] and r['clients'] == 1, .9)
            change(data[name], lambda r: r['operation'] == 'insert' and r['mode'] == 'managed', .876543)
        result = compare.compare(data)
        self.assertTrue(result['target']['met'])
        row = next(r for r in result['pooled'] if r['operation'] == 'insert' and r['mode'] == 'managed')
        self.assertEqual(row['p50_baseline_over_candidate'], 2000000 / 1753086)
        self.assertNotEqual(row['p50_baseline_over_candidate'], round(row['p50_baseline_over_candidate'], 3))

    def test_raw_control_drift_and_forward_reverse_results_are_retained(self):
        data = windows()
        change(data['B1'], lambda r: r['mode'] == 'redis_py', .5)
        change(data['B2'], lambda r: r['mode'] == 'redis_py', 1.5)
        result = compare.compare(data)
        raw = [row for row in result['pooled'] if row['mode'] == 'redis_py']
        self.assertEqual(len(raw), 14)
        for row in raw:
            self.assertEqual(row['candidate_elapsed_over_redis_py'], 1.)
        forward = next(r for r in result['pairs'][0]['rows'] if r['mode'] == 'redis_py')
        reverse = next(r for r in result['pairs'][1]['rows'] if r['mode'] == 'redis_py')
        self.assertEqual(forward['candidate']['p50_ms'], 1.)
        self.assertEqual(reverse['candidate']['p50_ms'], 3.)

    def test_reject_missing_windows_rows_duplicates_failures_and_bad_statistics(self):
        for kind in ['window', 'row', 'duplicate', 'failure', 'status', 'percentile', 'sample', 'duration', 'boolean']:
            data = windows()
            row = data['B2']['trials'][0]
            if kind == 'window': data.pop('A2')
            elif kind == 'row': data['B2']['trials'].pop()
            elif kind == 'duplicate': data['B2']['trials'][-1] = row
            elif kind == 'failure': row['validated'] = False
            elif kind == 'status': data['B2']['status'] = 'running'
            elif kind == 'percentile': row['p95_latency_ms'] += .01
            elif kind == 'sample': row['latency_ns'][0] = 0
            elif kind == 'boolean': row['clients'] = True
            else: row['seconds'] = float('inf')
            with self.subTest(kind=kind), self.assertRaises(AssertionError): compare.compare(data)

    def test_reject_changed_per_build_identity_and_cross_build_metadata(self):
        for kind in ['source', 'binary', 'version', 'harness', 'packages', 'fixture', 'settings', 'extra', 'same-build']:
            data = windows()
            if kind == 'source': data['A2']['metadata']['driver_source_commit'] = 'e' * 40
            elif kind == 'binary': data['B2']['metadata']['driver_sha256'] = 'e' * 64
            elif kind == 'version': data['B2']['metadata']['driver_version'] = 'vchanged'
            elif kind == 'harness': data['B1']['metadata']['harness_sha256'] = 'e' * 64
            elif kind == 'packages':
                for name in ['B1', 'B2']: data[name]['metadata']['packages']['redis'] = 'different'
            elif kind == 'fixture':
                for name in compare.ORDER: data[name]['metadata']['point_rows'] = 999
            elif kind == 'settings': data['B1']['final_qpf']['factor'] = 1
            elif kind == 'extra': data['B1']['metadata']['unexpected'] = True
            else:
                for name in ['B1', 'B2']:
                    for key in compare.BUILD_KEYS: data[name]['metadata'][key] = data['A1']['metadata'][key]
            with self.subTest(kind=kind), self.assertRaises(AssertionError): compare.compare(data)

    def test_source_expectations_and_fresh_output(self):
        data = windows()
        with self.assertRaises(AssertionError): compare.compare(data, baseline_source='e' * 40)
        with self.assertRaises(AssertionError): compare.compare(data, candidate_source='e' * 40)
        result = compare.compare(data, baseline_source='a' * 40, candidate_source='c' * 40)
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'output'
            compare.write_outputs(result, path)
            self.assertEqual(json.loads((path / 'comparison.json').read_text()), result)
            self.assertEqual(len((path / 'comparison.csv').read_text().splitlines()), 43)
            self.assertEqual(len((path / 'pairs.csv').read_text().splitlines()), 85)
            with self.assertRaises(FileExistsError): compare.write_outputs(result, path)


if __name__ == '__main__':
    unittest.main()
