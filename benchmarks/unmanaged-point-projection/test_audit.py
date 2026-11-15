import copy
import importlib.util
import itertools
from pathlib import Path
import statistics
import unittest

spec = importlib.util.spec_from_file_location('point_width_audit', Path(__file__).with_name('audit.py'))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)


def source(i, width):
    values = {'id': str(i), 'grp': str(i % 16), 'amount': str(9007199254740993 + i),
              'label': f'row-{i:08d}' + chr(0) + 'label', 'ordinal': str(i)}
    for name, enabled in [('registered_payload', width in ('registered', 'both')),
                          ('unregistered_payload', width in ('unregistered', 'both'))]:
        if enabled:
            values[name] = 'x' * 65536
    return {k: v.encode() for k, v in values.items()}


def fixture():
    meta = {'suite': 'unmanaged_known_key_projection', 'widths': list(audit.WIDTHS),
            'modes': list(audit.MODES), 'projection_columns': [1, 4], 'clients': [1, 4],
            'payload_bytes': 65536, 'tls': True, 'protocol': 3, 'automatic_retries': 0,
            'driver_source_commit': 'a' * 40, 'driver_sha256': 'b' * 64,
            'harness_sha256': 'c' * 64, 'cli_sha256': 'd' * 64, 'packages': {k: 'fixture' for k in ['pyarrow', 'adbc-driver-manager', 'redis', 'hiredis']},
            'repetitions': 1, 'operations': 4, 'point_rows': 64}
    data = {'metadata': meta, 'status': 'complete', 'failures': [],
            'final_qpf': {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296},
            'fixtures': [], 'trials': []}
    ids = [(i * 61) % 64 + 1 for i in range(4)]
    for width in audit.WIDTHS:
        data['fixtures'].append({'width': width, 'source_preserved': True, 'source_rows': 64,
                                 'complete_source_value_bytes': sum(sum(map(len, source(i, width).values())) for i in range(1, 65))})
        for mode, columns, clients, repeat in itertools.product(audit.MODES, (1, 4), (1, 4), (-1, 0)):
            names = ['id', 'grp', 'amount', 'label'][:columns]
            values = [10, 20, 30, 40]
            data['trials'].append({'width': width, 'mode': mode, 'projection_columns': columns, 'clients': clients,
                'repeat': repeat, 'warmup': repeat == -1, 'operations': 4, 'fixture_rows': 64,
                'seconds': .001, 'latency_ns': values, 'status': 'validated', 'validated': True,
                'median_latency_ms': statistics.median(values) / 1e6, 'p95_latency_ms': max(values) / 1e6,
                'operations_per_second': 4000., 'byte_oracle': {
                    'selected_value_bytes': sum(len(source(i, width)[name]) for i in ids for name in names),
                    'whole_hash_value_bytes': sum(sum(map(len, source(i, width).values())) for i in ids),
                    'scope': 'Synthetic source value bytes, not measured wire bytes.'}})
    return data


class PointProjectionAudit(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.valid = fixture()

    def test_complete_grid_and_independent_byte_arithmetic(self):
        summary = audit.summarize(self.valid)
        self.assertEqual(len(summary), 32)
        for row in summary:
            self.assertEqual(row['latency_samples'], 4)
            self.assertLessEqual(row['byte_oracle']['selected_value_bytes'], row['byte_oracle']['whole_hash_value_bytes'])

    def test_reject_missing_and_duplicate_records(self):
        for kind in ['missing', 'duplicate']:
            data = copy.deepcopy(self.valid)
            if kind == 'missing': data['trials'].pop()
            else: data['trials'][-1] = copy.deepcopy(data['trials'][0])
            with self.subTest(kind=kind), self.assertRaises(AssertionError): audit.summarize(data)

    def test_reject_failed_incomplete_and_wrong_qpf(self):
        for kind in ['failed', 'trial', 'qpf', 'fixture']:
            data = copy.deepcopy(self.valid)
            if kind == 'failed': data['status'] = 'failed'
            elif kind == 'trial': data['trials'][0]['validated'] = False
            elif kind == 'qpf': data['final_qpf']['factor'] = 2
            else: data['fixtures'][0]['source_preserved'] = False
            with self.subTest(kind=kind), self.assertRaises(AssertionError): audit.summarize(data)

    def test_reject_misreported_percentile_sample_and_bytes(self):
        for kind in ['percentile', 'count', 'negative', 'selected', 'whole', 'source']:
            data = copy.deepcopy(self.valid)
            row = data['trials'][0]
            if kind == 'percentile': row['p95_latency_ms'] *= 2
            elif kind == 'count': row['latency_ns'].pop()
            elif kind == 'negative': row['latency_ns'][0] = -1
            elif kind == 'selected': row['byte_oracle']['selected_value_bytes'] += 1
            elif kind == 'whole': row['byte_oracle']['whole_hash_value_bytes'] += 1
            else: data['fixtures'][0]['complete_source_value_bytes'] += 1
            with self.subTest(kind=kind), self.assertRaises(AssertionError): audit.summarize(data)


if __name__ == '__main__':
    unittest.main()
