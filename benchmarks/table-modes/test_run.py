"""Focused guards for the observed unpicklable worker exception failure."""
import importlib.util
from pathlib import Path
import pickle
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('table_modes_run', Path(__file__).with_name('run.py'))
bench = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bench)


class WorkerFailureTests(unittest.TestCase):
    def test_conflict_is_serializable_and_not_retried(self):
        class Conflict(Exception):
            sqlstate = '40001'
        endpoint = 'rediss://private.example'
        with patch.object(bench, 'write_worker', side_effect=Conflict('failed '+endpoint)) as worker:
            result = bench.safe_write_worker(({'uri': endpoint},))
        worker.assert_called_once()
        self.assertEqual(result['sqlstate'], '40001')
        self.assertNotIn(endpoint, result['error_message'])
        self.assertEqual(pickle.loads(pickle.dumps(result)), result)

    def test_unknown_failure_keeps_original_type_and_no_success(self):
        with patch.object(bench, 'write_worker', side_effect=ValueError('bad input')) as worker:
            result = bench.safe_write_worker(({'uri': 'private'},))
        worker.assert_called_once()
        self.assertEqual(result['error_type'], 'ValueError')
        self.assertIsNone(result['sqlstate'])
        self.assertNotIn('rows', result)

if __name__ == '__main__':
    unittest.main()
