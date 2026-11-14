import copy
import itertools
import unittest

import compare


def fixture():
    m = {'automatic_retries': 0, 'tls': True, 'protocol': 3, 'suite': 'all',
         'columns': 4, 'repetitions': 3, 'operations': 32, 'queries': 8,
         'point_rows': 1000, 'olap_rows': 100000, 'driver_source_commit': 'a'*40,
         'driver_sha256': 'b'*64, 'harness_sha256': 'c'*64, 'packages': {},
         'generator': 'fixture', 'timing_scope': 'operation', 'addressing': 'known key'}
    rows = []
    for suite, operations in compare.OPERATIONS.items():
        count = 32 if suite == 'oltp' else 8
        for operation, mode, clients, repeat in itertools.product(operations, compare.MODES, (1, 4), (-1, 0, 1, 2)):
            # P50 2ms, nearest-rank p95 3ms; deliberately independent oracle.
            samples = [1000000]*(count//4) + [2000000]*(count//2) + [3000000]*(count//4)
            rows.append({'suite': suite, 'operation': operation, 'mode': mode, 'clients': clients,
                         'repeat': repeat, 'warmup': repeat == -1, 'validated': True, 'status': 'validated',
                         'columns': 4, 'fixture_rows': 1000 if suite == 'oltp' else 100000,
                         'operations': count, 'latency_ns': samples, 'seconds': 1.,
                         'median_latency_ms': 2., 'p95_latency_ms': 3., 'operations_per_second': float(count)})
    return {'metadata': m, 'status': 'complete', 'failures': [], 'final_qpf': dict(compare.SETTINGS), 'trials': rows}


class ComparisonTest(unittest.TestCase):
    def test_identical_results_and_distinct_driver_identity(self):
        before = fixture(); after = copy.deepcopy(before)
        after['metadata']['driver_source_commit'] = 'd'*40
        after['metadata']['driver_sha256'] = 'e'*64
        rows = compare.compare(before, after)
        self.assertEqual(len(rows), 42)
        self.assertTrue(all(r['p50_speedup'] == 1 and r['baseline_p50_ms'] == 2 and r['candidate_p95_ms'] == 3 for r in rows))
        self.assertTrue(all(r['candidate_elapsed_over_redis_py'] == 1 for r in rows))

    def test_missing_duplicate_failure_and_bad_statistics_rejected(self):
        for kind in ('missing', 'duplicate', 'failure', 'statistics', 'settings', 'sample'):
            with self.subTest(kind=kind):
                data = fixture()
                if kind == 'missing': data['trials'].pop()
                if kind == 'duplicate': data['trials'][-1] = data['trials'][0]
                if kind == 'failure': data['trials'][0]['validated'] = False
                if kind == 'statistics': data['trials'][0]['p95_latency_ms'] = 2.
                if kind == 'settings': data['final_qpf']['factor'] = 1
                if kind == 'sample': data['trials'][0]['latency_ns'][0] = 0
                with self.assertRaises(AssertionError): compare.audit(data)

    def test_incompatible_measurement_rejected(self):
        for key in ('harness_sha256', 'packages', 'generator', 'timing_scope', 'addressing'):
            with self.subTest(key=key):
                before = fixture(); after = fixture()
                after['metadata'][key] = 'd'*64 if key == 'harness_sha256' else 'changed'
                with self.assertRaises(AssertionError): compare.compare(before, after)


if __name__ == '__main__': unittest.main()
