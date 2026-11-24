"""Harness regressions: synthetic engine adapters only, no Redis or SF1 queries."""

import argparse
from contextlib import redirect_stdout
from decimal import Decimal
import io
import json
import os
from pathlib import Path
import pickle
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import isolation
import report
import run


FAKE = '''
import json, os, sys
from pathlib import Path
engine = sys.argv[sys.argv.index('--engine') + 1]
private = Path(sys.argv[sys.argv.index('--private') + 1])
class Connection:
    description = [('n',)]
    sql = ''
    def execute(self, sql):
        self.sql = sql
        with (private / 'calls.jsonl').open('a') as f:
            f.write(json.dumps({'engine': engine, 'sql': sql, 'pid': os.getpid()}) + '\\n')
        if sql == 'SELECT synthetic' and os.environ.get('FAIL_ENGINE') == engine:
            os._exit(42)
        return self
    def fetchall(self):
        if os.environ.get('MISMATCH_ENGINE') == engine:
            return [(99,)]
        return [(2,), (1,), (1,), (None,)]
    def fetchone(self):
        return ('test-version',)
    def cursor(self): return self
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def interrupt(self): pass
    def close(self): pass
def connect(*args, **kwargs):
    with (private / 'connections.jsonl').open('a') as f:
        f.write(json.dumps({'engine': engine, 'args': args, 'kwargs': kwargs, 'pid': os.getpid()}) + '\\n')
    return Connection()
'''


class IsolationTests(unittest.TestCase):
    def test_external_multiset_validation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            rows = [(None, 'a'), (Decimal('1.0'), 'b'), (2, 'x'), (2, 'x')] * 2300
            isolation.dump_rows(root / 'ref', 2, rows)
            isolation.dump_rows(root / 'actual', 2, list(reversed(rows)))
            isolation.validate_files(root / 'ref', root / 'actual', root)
            isolation.dump_rows(root / 'actual', 2, rows[:-1] + [(3, 'x')])
            with self.assertRaisesRegex(ValueError, 'result mismatch'):
                isolation.validate_files(root / 'ref', root / 'actual', root)
            # Force multiple merge generations rather than only one pass.
            scratch = root / 'sort'
            scratch.mkdir()
            got = list(isolation.sorted_rows(root / 'ref', scratch, chunk_size=500, fan_in=3))
            self.assertEqual(got, sorted(tuple(run.cell(v) for v in row) for row in rows))

    def test_corrupt_shape_and_tolerance(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            isolation.dump_rows(root / 'ref', 1, [(Decimal('10.00000001'),)])
            isolation.dump_rows(root / 'actual', 1, [(10,)])
            isolation.validate_files(root / 'ref', root / 'actual', root)
            with (root / 'actual').open('wb') as out:
                pickle.dump((1, 1), out)
            with self.assertRaisesRegex(ValueError, 'truncated'):
                isolation.validate_files(root / 'ref', root / 'actual', root)

    def exercise_workers(self, failure=None, mismatch=None, validator_timeout=False):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            fake = private / 'modules'
            fake.mkdir()
            (fake / 'duckdb.py').write_text(FAKE + "\nassert engine != 'redis', 'Redis worker imported DuckDB'\n")
            package = fake / 'adbc_driver_redis'
            package.mkdir()
            (package / '__init__.py').write_text("import os\ndef _driver_path(): return os.environ['FAKE_LIBRARY']\n")
            (package / 'dbapi.py').write_text(FAKE + "\nassert 'duckdb' not in sys.modules, 'Redis worker retained DuckDB'\n")
            library = private / 'driver.so'
            library.write_bytes(b'synthetic library, never loaded')
            (private / 'connection.json').write_text(json.dumps({
                'uri': 'rediss://test:private-secret@localhost',
                'db_kwargs': {'adbc.redis.flex': 'true', 'adbc.redis.default_schema': 'public'}}))
            output = private / 'results'
            queries = output / 'tpch' / 'queries'
            queries.mkdir(parents=True)
            (queries / '19.sql').write_text('SELECT synthetic')
            args = argparse.Namespace(private=private, output=output, suite='tpch', query='19',
                                      duckdb_only=False, include_duckdb_adbc=True)
            env = {'PYTHONPATH': str(fake), 'FAKE_LIBRARY': str(library)}
            if failure:
                env['FAIL_ENGINE'] = failure
            if mismatch:
                env['MISMATCH_ENGINE'] = mismatch
            def runner(cmd, **kwargs):
                if validator_timeout and cmd[cmd.index('--phase') + 1] == 'validate' and cmd[-1] == 'redis':
                    raise subprocess.TimeoutExpired(cmd, kwargs['timeout'])
                return subprocess.run(cmd, **kwargs)

            with patch.dict(os.environ, env), redirect_stdout(io.StringIO()):
                record = isolation.run_query(args, runner=runner)
            calls = [json.loads(x) for x in (private / 'calls.jsonl').read_text().splitlines()]
            connections = [json.loads(x) for x in (private / 'connections.jsonl').read_text().splitlines()]
            pids = [record[f'{e}_worker']['pid'] for e in isolation.ENGINES]
            self.assertEqual(len(set(pids)), 3)
            self.assertNotIn(os.getpid(), pids)
            self.assertEqual([x['engine'] for x in connections], list(isolation.ENGINES))
            self.assertEqual(connections[2]['args'], [])  # no source DB in third engine
            self.assertEqual(connections[0]['kwargs'], {'read_only': True})
            attach = next(x['sql'] for x in calls if x['sql'].startswith('ATTACH'))
            self.assertIn('query_pushdown false', attach)
            self.assertIn('"adbc.redis.flex" \'true\'', attach)
            self.assertIn(str(library), attach)
            self.assertEqual(len(list(private.glob('query-*'))), 0)
            self.assertNotIn('private-secret', json.dumps(record))
            self.assertNotIn(str(library), json.dumps(record))
            with self.assertRaisesRegex(ValueError, 'already recorded'):
                isolation.run_query(args)
            historical = output / 'tpch' / 'queries.jsonl'
            historical.write_text(json.dumps({'query': '1'}) + '\n')
            with self.assertRaisesRegex(ValueError, 'historical'):
                isolation.run_query(args)
            return record

    def test_real_child_process_isolation_and_third_engine(self):
        record = self.exercise_workers()
        for engine in isolation.ENGINES:
            self.assertEqual(record[f'{engine}_status'], 'ok')
            self.assertEqual(len(record[f'{engine}_seconds']), 3)
        self.assertIn('duckdb_adbc_over_duckdb', record)

    def test_crash_attributed_to_engine_and_third_still_runs(self):
        record = self.exercise_workers(failure='redis')
        self.assertEqual(record['redis_status'], 'worker_failed')
        self.assertEqual(record['duckdb_status'], 'ok')
        self.assertEqual(record['duckdb_adbc_status'], 'ok')
        self.assertNotIn('redis_over_duckdb', record)

    def test_mismatch_has_no_ratio(self):
        record = self.exercise_workers(mismatch='redis')
        self.assertEqual(record['redis_status'], 'mismatch')
        self.assertNotIn('redis_over_duckdb', record)

    def test_validator_timeout_is_not_engine_oom(self):
        record = self.exercise_workers(validator_timeout=True)
        self.assertEqual(record['redis_status'], 'validation_failed')
        self.assertNotIn('redis_over_duckdb', record)
        self.assertEqual(record['duckdb_adbc_status'], 'ok')

    def test_failed_reference_skips_all_redis_engines(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            folder = root / 'tpch'
            folder.mkdir()
            args = argparse.Namespace(private=root, output=root, suite='tpch', query='19',
                                      duckdb_only=False, include_duckdb_adbc=True)
            calls = []

            def runner(cmd, **kwargs):
                calls.append(cmd)
                return subprocess.CompletedProcess(cmd, 42)

            with redirect_stdout(io.StringIO()):
                record = isolation.run_query(args, runner=runner)
            self.assertEqual(len(calls), 1)
            self.assertEqual(record['duckdb_status'], 'worker_failed')
            self.assertEqual(record['redis_status'], 'not_run_no_reference')
            self.assertEqual(record['duckdb_adbc_status'], 'not_run_no_reference')

    def test_connection_error_redaction(self):
        message = "error ATTACH 'rediss://test:private-secret@localhost' AS r (password 'more-secret')"
        self.assertNotIn('secret', run.safe_error(message))

    def test_report_keeps_third_engine_separate(self):
        with tempfile.TemporaryDirectory() as tmp:
            folder = Path(tmp)
            record = {'query': '19', 'duckdb_status': 'ok', 'redis_status': 'ok',
                      'duckdb_median_seconds': 1, 'redis_median_seconds': 2,
                      'redis_over_duckdb': 2, 'duckdb_adbc_status': 'ok',
                      'duckdb_adbc_median_seconds': 3, 'duckdb_adbc_over_duckdb': 3}
            (folder / 'queries.jsonl').write_text(json.dumps(record) + '\n')
            (folder / 'metadata.json').write_text(json.dumps({'scope': 'Q19 only'}))
            rendered = report.render(folder)
            self.assertIn('Q19 only', rendered)
            self.assertIn('Direct Redis ADBC | 2000.000 | 2.00× | ok', rendered)
            self.assertIn('DuckDB over Redis ADBC | 3000.000 | 3.00× | ok', rendered)
            record['duckdb_adbc_status'] = 'mismatch'
            (folder / 'queries.jsonl').write_text(json.dumps(record) + '\n')
            self.assertIn('DuckDB over Redis ADBC | — | — | mismatch', report.render(folder))


if __name__ == '__main__':
    unittest.main()
