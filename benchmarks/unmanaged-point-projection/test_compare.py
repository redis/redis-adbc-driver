import copy
import importlib.util
import itertools
import json
from pathlib import Path
import statistics
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('native_width_compare', Path(__file__).with_name('compare.py'))
compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compare)


def fixture(build):
    meta = copy.deepcopy(compare.FIXED_METADATA)
    meta.update(driver_source_commit='a' * 40 if build == 'baseline' else 'c' * 40,
                driver_sha256='b' * 64 if build == 'baseline' else 'd' * 64,
                cli_sha256='e' * 64 if build == 'baseline' else 'f' * 64,
                driver_version='v0.0.0-' + build, harness_sha256='9' * 64,
                helper_sha256={'scenarios.py': '8' * 64, 'table-modes/run.py': '7' * 64},
                packages={'pyarrow': '26.0.1', 'adbc-driver-manager': '1.13.0', 'redis': '8.1.0', 'hiredis': '3.5.0'})
    data = {'metadata': meta, 'status': 'complete', 'failures': [],
            'final_qpf': copy.deepcopy(compare.full.SETTINGS), 'fixtures': [], 'trials': []}
    for width in compare.audit.WIDTHS:
        total = sum(compare.audit.value_bytes(i, width, 4, 65536)[1] for i in range(1, 1001))
        data['fixtures'].append({'width': width, 'source_preserved': True, 'source_rows': 1000,
                                 'complete_source_value_bytes': total})
        for mode, columns, clients, repeat in itertools.product(compare.audit.MODES, (1, 4), (1, 4), (-1, 0, 1, 2)):
            ids = [(i * 61) % 1000 + 1 for i in range(32)]
            selected, whole = zip(*(compare.audit.value_bytes(i, width, columns, 65536) for i in ids))
            samples = [1000000] * 8 + [2000000] * 16 + [3000000] * 8
            data['trials'].append({'width': width, 'mode': mode, 'projection_columns': columns, 'clients': clients,
                'repeat': repeat, 'warmup': repeat == -1, 'operations': 32, 'fixture_rows': 1000,
                'seconds': 1., 'latency_ns': samples, 'status': 'validated', 'validated': True,
                'median_latency_ms': 2., 'p95_latency_ms': 3., 'operations_per_second': 32.,
                'byte_oracle': {'selected_value_bytes': sum(selected), 'whole_hash_value_bytes': sum(whole), 'scope': compare.BYTE_SCOPE}})
    return data


def windows():
    a, b = fixture('baseline'), fixture('candidate')
    return {'A1': a, 'B1': b, 'B2': copy.deepcopy(b), 'A2': copy.deepcopy(a)}


def change(data, predicate, factor):
    for row in data['trials']:
        if predicate(row):
            row['latency_ns'] = [round(value * factor) for value in row['latency_ns']]
            row['median_latency_ms'] = statistics.median(row['latency_ns']) / 1e6
            row['p95_latency_ms'] = sorted(row['latency_ns'])[30] / 1e6
            row['seconds'] *= factor
            row['operations_per_second'] = 32 / row['seconds']


class WidthABBAAudit(unittest.TestCase):
    def test_all_pooled_pairs_controls_and_exact_samples_oracles(self):
        result = compare.compare(windows())
        self.assertEqual(len(result['pooled']), 32)
        self.assertEqual([len(pair['rows']) for pair in result['pairs']], [32, 32])
        self.assertEqual(result['pairs'][1]['execution_order'], ['candidate', 'baseline'])
        self.assertEqual(len([row for row in result['pooled'] if row['mode'] == 'redis_py']), 16)
        for row in result['pooled']:
            self.assertEqual(row['baseline']['latency_samples'], 192)
            self.assertEqual(row['candidate']['measured_trials'], 6)
            self.assertEqual(row['candidate']['p95_ms'], 3.)
            self.assertEqual(row['candidate']['mean_ms'], 2.)
            self.assertEqual(row['candidate']['summed_trial_operations_per_second'], 32.)
            self.assertEqual(row['p50_baseline_over_candidate'], 1.)
            self.assertEqual(row['candidate_elapsed_over_redis_py'], 1.)
            self.assertLess(row['value_byte_oracle_per_trial']['selected_value_bytes'], row['value_byte_oracle_per_trial']['whole_hash_value_bytes'])

    def test_slower_cases_control_drift_and_unrounded_ratios_remain(self):
        data = windows()
        for name in ['B1', 'B2']:
            change(data[name], lambda row: row['mode'] == 'unmanaged' and row['width'] == 'both', .876543)
            change(data[name], lambda row: row['mode'] == 'unmanaged' and row['width'] == 'none', 1.5)
        change(data['B1'], lambda row: row['mode'] == 'redis_py', .5)
        change(data['B2'], lambda row: row['mode'] == 'redis_py', 1.5)
        result = compare.compare(data)
        fast = next(row for row in result['pooled'] if row['mode'] == 'unmanaged' and row['width'] == 'both')
        slow = next(row for row in result['pooled'] if row['mode'] == 'unmanaged' and row['width'] == 'none')
        self.assertEqual(fast['p50_baseline_over_candidate'], 2000000 / 1753086)
        self.assertEqual(slow['p50_elapsed_reduction_percent'], -50.)
        self.assertEqual(next(row for row in result['pairs'][0]['rows'] if row['mode'] == 'redis_py')['candidate']['p50_ms'], 1.)
        self.assertEqual(next(row for row in result['pairs'][1]['rows'] if row['mode'] == 'redis_py')['candidate']['p50_ms'], 3.)

    def test_reject_incomplete_failed_duplicate_or_corrupt_timing_and_bytes(self):
        for kind in ['window', 'row', 'duplicate', 'failed', 'percentile', 'sample', 'source', 'selected-bytes', 'whole-bytes', 'scope', 'boolean']:
            data = windows()
            row = data['B2']['trials'][0]
            if kind == 'window': data.pop('A2')
            elif kind == 'row': data['B2']['trials'].pop()
            elif kind == 'duplicate': data['B2']['trials'][-1] = row
            elif kind == 'failed': row['validated'] = False
            elif kind == 'percentile': row['p95_latency_ms'] += .1
            elif kind == 'sample': row['latency_ns'][0] = 0
            elif kind == 'source': data['B2']['fixtures'][0]['complete_source_value_bytes'] += 1
            elif kind == 'selected-bytes': row['byte_oracle']['selected_value_bytes'] += 1
            elif kind == 'whole-bytes': row['byte_oracle']['whole_hash_value_bytes'] += 1
            elif kind == 'scope': row['byte_oracle']['scope'] = 'wire bytes'
            else: row['clients'] = True
            with self.subTest(kind=kind), self.assertRaises(AssertionError): compare.compare(data)

    def test_reject_identity_helper_package_policy_or_nondefault_fixture_changes(self):
        for kind in ['cli', 'source', 'version', 'harness', 'helper', 'packages', 'qpf', 'rows', 'repetitions', 'same-library']:
            data = windows()
            if kind == 'cli': data['A2']['metadata']['cli_sha256'] = '0' * 64
            elif kind == 'source': data['B2']['metadata']['driver_source_commit'] = '0' * 40
            elif kind == 'version': data['B2']['metadata']['driver_version'] = 'vchanged'
            elif kind == 'harness':
                for name in ['B1', 'B2']: data[name]['metadata']['harness_sha256'] = '0' * 64
            elif kind == 'helper':
                for name in ['B1', 'B2']: data[name]['metadata']['helper_sha256']['scenarios.py'] = '0' * 64
            elif kind == 'packages':
                for name in ['B1', 'B2']: data[name]['metadata']['packages']['redis'] = 'changed'
            elif kind == 'qpf': data['B1']['final_qpf']['factor'] = 1
            elif kind == 'rows':
                for window in data.values(): window['metadata']['point_rows'] = 64
            elif kind == 'repetitions':
                for window in data.values(): window['metadata']['repetitions'] = 1
            else:
                for name in ['B1', 'B2']: data[name]['metadata']['driver_sha256'] = data['A1']['metadata']['driver_sha256']
            with self.subTest(kind=kind), self.assertRaises(AssertionError): compare.compare(data)

    def test_expected_sources_frozen_harness_hash_and_output_files(self):
        data = windows()
        with self.assertRaises(AssertionError): compare.compare(data, baseline_source='0' * 40)
        with self.assertRaises(AssertionError): compare.compare(data, candidate_source='0' * 40)
        with self.assertRaises(AssertionError): compare.compare(data, harness_sha256='0' * 64)
        result = compare.compare(data, baseline_source='a' * 40, candidate_source='c' * 40, harness_sha256='9' * 64)
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'output'
            compare.write_outputs(result, path)
            self.assertEqual(json.loads((path / 'comparison.json').read_text()), result)
            self.assertEqual(len((path / 'comparison.csv').read_text().splitlines()), 33)
            self.assertEqual(len((path / 'pairs.csv').read_text().splitlines()), 65)
            self.assertIn('not measured wire bytes', (path / 'report.md').read_text())
            with self.assertRaises(FileExistsError): compare.write_outputs(result, path)


if __name__ == '__main__':
    unittest.main()
