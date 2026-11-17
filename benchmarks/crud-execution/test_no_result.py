import copy
from pathlib import Path
import types
import unittest

from no_result import install, NoResultCursor
from test_compare import compare, fixture, change


class CallerRouting(unittest.TestCase):
    def test_explicit_mutation_selection_preserves_every_other_call(self):
        calls = []
        class Cursor:
            rowcount = 7
            def execute(self, sql):
                calls.append(('execute', sql))
            def executemany(self, sql, parameters):
                calls.append(('executemany', sql, parameters))
        class Benchmark:
            def __init__(self, args):
                self.results = {'metadata': {}}
            def save(self):
                pass
        def call(cfg, mode, operation, index, rows, rowids, q, r):
            q.execute('opaque SQL')
            return q.rowcount
        scenarios = types.SimpleNamespace(operation_call=call, ScenarioBenchmark=Benchmark)
        install(scenarios, 'executemany')
        for operation in ('insert', 'update', 'delete', 'point_read', 'filtered_projection', 'unknown'):
            for mode in ('managed', 'unmanaged', 'redis_py'):
                self.assertEqual(scenarios.operation_call(None, mode, operation, 0, 1, {}, Cursor(), None), 7)
                expected = 'executemany' if operation in ('insert', 'update', 'delete') and mode != 'redis_py' else 'execute'
                self.assertEqual(calls[-1][0], expected)
                if expected == 'executemany':
                    self.assertIsNone(calls[-1][2])
        self.assertEqual(len(calls), 18)
        benchmark = Benchmark(None)
        self.assertEqual(benchmark.results['metadata']['mutation_execution'], 'executemany')

    def test_no_replay_or_error_translation(self):
        failure = RuntimeError('original error')
        calls = []
        class Cursor:
            def executemany(self, sql, parameters):
                calls.append(parameters)
                raise failure
        with self.assertRaises(RuntimeError) as raised:
            NoResultCursor(Cursor()).execute('opaque SQL')
        self.assertIs(raised.exception, failure)
        self.assertEqual(calls, [None])


def windows():
    data = fixture('baseline')
    result = {name: copy.deepcopy(data) for name in compare.ORDER}
    for name, window in result.items():
        window['metadata'].update(
            mutation_execution='execute' if name.startswith('A') else 'executemany',
            no_result_wrapper_sha256=compare.digest(Path(__file__).with_name('no_result.py')))
    return result


class CallerComparison(unittest.TestCase):
    def test_same_binary_and_six_independent_gates_keep_complete_controls(self):
        data = windows()
        result = compare.compare(data, caller_paths=True)
        self.assertFalse(result['target']['met'])
        self.assertEqual(len(result['pooled']), 42)
        self.assertEqual(len(result['target']['cases']), 6)
        self.assertEqual(data, windows())  # Evidence is never modified.
        for name in ('B1', 'B2'):
            change(data[name], lambda r: r['operation'] in ('insert', 'update', 'delete') and r['mode'] != 'redis_py' and r['clients'] == 1, .8)
        self.assertTrue(compare.compare(data, caller_paths=True)['target']['met'])
        for name in ('B1', 'B2'):
            change(data[name], lambda r: r['operation'] == 'delete' and r['mode'] == 'unmanaged' and r['clients'] == 1, 1.5)
        result = compare.compare(data, caller_paths=True)
        self.assertFalse(result['target']['met'])
        self.assertIn('DELETE', compare.markdown(result))
        self.assertEqual(sum(case['met'] for case in result['target']['cases']), 5)

    def test_wrong_paths_builds_wrappers_packages_and_partial_evidence_rejected(self):
        for kind in ('path', 'binary', 'wrapper', 'package', 'partial', 'extra', 'diagnostic'):
            with self.subTest(kind=kind):
                data = windows()
                for name in ('B1', 'B2'):
                    meta = data[name]['metadata']
                    if kind == 'path': meta['mutation_execution'] = 'execute'
                    if kind == 'binary': meta['driver_sha256'] = 'f' * 64
                    if kind == 'wrapper': meta['no_result_wrapper_sha256'] = 'f' * 64
                    if kind == 'package': meta['packages']['adbc-driver-manager'] = 'unknown'
                    if kind == 'partial': data[name]['trials'].pop()
                    if kind == 'extra': meta['other'] = 1
                    if kind == 'diagnostic': meta['stage_diagnostic_wrapper_sha256'] = 'f' * 64
                with self.assertRaises(AssertionError):
                    compare.compare(data, caller_paths=True)
        with self.assertRaises(AssertionError):
            compare.compare(windows())  # Ordinary driver comparison stays strict.


if __name__ == '__main__':
    unittest.main()
