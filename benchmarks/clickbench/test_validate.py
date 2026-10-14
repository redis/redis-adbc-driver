"""Validation failures and missing tie evidence must never become passes."""
import json
from pathlib import Path
import tempfile
import sys
import unittest
from unittest.mock import Mock, patch

import pyarrow as pa
import validate


class Validation(unittest.TestCase):
    def validate(self, result):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            upstream = root / 'upstream' / 'postgresql'
            upstream.mkdir(parents=True)
            (upstream / 'create.sql').write_text('fixture')
            run = root / 'run.json'
            run.write_text(json.dumps({'metadata':{'loaded_rows':1},
                'queries':['SELECT x FROM hits ORDER BY x LIMIT 1;'], 'result':[[1,2,3]]}))
            raw = root / 'run-raw'
            raw.mkdir()
            for trial in (1,2,3):
                (raw / f'q01-{trial}.json').write_text(json.dumps({'seconds':1,'values':[[2]]}))
            batch = pa.record_batch([pa.array([1])], names=['x'])
            oracle = Mock()
            oracle.execute.return_value.fetchall.return_value = [(1,)]
            kwargs = {'side_effect':result} if isinstance(result, Exception) else {'return_value':result}
            with patch.object(sys, 'argv',
                              ['validate.py',str(run),'--parquet',str(root/'hits.parquet'),'--upstream',str(root/'upstream')]), \
                 patch.object(validate, 'arrow_schema', return_value=batch.schema), \
                 patch.object(validate.pq, 'ParquetFile') as source, \
                 patch.object(validate, 'convert_batch', side_effect=lambda b,s:b), \
                 patch.object(validate.duckdb, 'connect', return_value=oracle), \
                 patch.object(validate, 'check_ties', **kwargs):
                source.return_value.iter_batches.return_value = iter([batch])
                validate.main()
            return json.loads(run.read_text()), json.loads((root/'run-correctness.json').read_text())

    def test_oracle_error_preserves_unresolved_status(self):
        run, evidence = self.validate(RuntimeError('oracle failed'))
        self.assertEqual(run['metadata']['correctness'], {'needs_tie_validation':3})
        self.assertTrue(all(c['oracle_error_type']=='RuntimeError' for c in evidence['checks']))
        self.assertEqual(run['result'], [[1,2,3]])

    def test_confirmed_mismatch_clears_timings(self):
        run, _ = self.validate('mismatch')
        self.assertEqual(run['metadata']['correctness'], {'mismatch':3})
        self.assertEqual(run['result'], [[None,None,None]])

    def test_independent_tie_check_can_certify_alternative_rows(self):
        run, _ = self.validate('valid')
        self.assertEqual(run['metadata']['correctness'], {'valid':3})
        self.assertEqual(run['result'], [[1,2,3]])


if __name__ == '__main__':
    unittest.main()
