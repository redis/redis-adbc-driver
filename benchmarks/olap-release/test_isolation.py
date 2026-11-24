"""Harness regressions: synthetic engine adapters only, no Redis or SF1 queries."""

import argparse
from contextlib import nullcontext, redirect_stdout
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
import types
from unittest.mock import Mock, patch

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

    def exercise_workers(self, failure=None, mismatch=None, validator_timeout=False,
                         worker_artifact=None, validator_artifact=False, oom_counts=None):
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
                child = subprocess.run(cmd, **kwargs)
                artifacts = Path(cmd[cmd.index('--artifacts') + 1])
                if cmd[-1] == 'redis':
                    if cmd[cmd.index('--phase') + 1] == 'engine' and worker_artifact:
                        path = artifacts / 'redis.json'
                        if worker_artifact == 'missing':
                            path.unlink()
                        else:
                            path.write_text(worker_artifact)
                    elif cmd[cmd.index('--phase') + 1] == 'validate' and validator_artifact:
                        (artifacts / 'redis-validation.json').write_text('{')
                return child

            oom = patch('isolation.oom_kills', side_effect=oom_counts) if oom_counts else nullcontext()
            with patch.dict(os.environ, env), redirect_stdout(io.StringIO()), oom:
                record = isolation.run_query(args, runner=runner)
            calls = [json.loads(x) for x in (private / 'calls.jsonl').read_text().splitlines()]
            connections = [json.loads(x) for x in (private / 'connections.jsonl').read_text().splitlines()]
            pids = [item['pid'] for item in connections]
            self.assertEqual(len(set(pids)), 3)
            self.assertNotIn(os.getpid(), pids)
            self.assertEqual([x['engine'] for x in connections], list(isolation.ENGINES))
            for connection in connections:
                engine = connection['engine']
                if engine != 'redis' or not worker_artifact:
                    self.assertEqual(record[f'{engine}_worker']['pid'], connection['pid'])
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

    def test_malformed_crash_status_keeps_engine_code_and_oom(self):
        record = self.exercise_workers(failure='redis', worker_artifact='{',
                                       oom_counts=[10, 10, 10, 11, 11, 11])
        self.assertEqual(record['redis_status'], 'worker_failed')
        self.assertIn('code 42', record['redis_error'])
        self.assertEqual(record['redis_worker']['engine'], 'redis')
        self.assertEqual(record['redis_worker']['phase'], 'engine')
        self.assertEqual(record['redis_worker']['status_artifact'], 'malformed')
        self.assertEqual(record['redis_worker']['returncode'], 42)
        self.assertEqual(record['redis_worker']['oom_kills_delta'], 1)
        self.assertNotIn('redis_over_duckdb', record)
        self.assertEqual(record['duckdb_adbc_status'], 'ok')

    def test_missing_or_non_object_success_status_never_passes(self):
        for value, problem in [('missing', 'missing'), ('[]', 'invalid_json_object')]:
            with self.subTest(artifact=value):
                record = self.exercise_workers(worker_artifact=value)
                self.assertEqual(record['redis_status'], 'worker_failed')
                self.assertEqual(record['redis_worker']['status_artifact'], problem)
                self.assertEqual(record['redis_worker']['returncode'], 0)
                self.assertNotIn('redis_over_duckdb', record)
                self.assertEqual(record['duckdb_adbc_status'], 'ok')

    def test_malformed_validator_status_never_becomes_a_pass(self):
        record = self.exercise_workers(mismatch='redis', validator_artifact=True)
        self.assertEqual(record['redis_status'], 'validation_failed')
        self.assertIn('malformed status artifact', record['redis_error'])
        self.assertNotIn('redis_over_duckdb', record)
        self.assertEqual(record['duckdb_adbc_status'], 'ok')

    def test_atomic_json_failure_preserves_previous_checkpoint(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'checkpoint.json'
            isolation.atomic_json(path, {'active_engine': 'redis', 'active_phase': 'engine'})
            with patch('isolation.os.replace', side_effect=OSError('synthetic publish failure')):
                with self.assertRaisesRegex(OSError, 'synthetic publish failure'):
                    isolation.atomic_json(path, {'active_engine': 'duckdb_adbc'})
            self.assertEqual(json.loads(path.read_text()), {'active_engine': 'redis', 'active_phase': 'engine'})
            self.assertEqual(list(path.parent.iterdir()), [path])

    def test_duckdb_timeout_requires_actual_timer_fire(self):
        class Connection:
            def interrupt(self):
                pass
            def close(self):
                pass
            def execute(self, sql):
                raise RuntimeError('INTERRUPT Error: Interrupted!')

        class Timer:
            fire = False
            def __init__(self, seconds, callback):
                self.callback = callback
            def start(self):
                if self.fire:
                    self.callback()
            def cancel(self):
                pass
            def join(self):
                pass

        for fired in [False, True]:
            with self.subTest(timer_fired=fired), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                queries = root / 'tpch' / 'queries'
                queries.mkdir(parents=True)
                (queries / '19.sql').write_text('SELECT synthetic')
                args = argparse.Namespace(engine='duckdb_adbc', suite='tpch', query='19',
                                          output=root, artifacts=root)
                Timer.fire = fired
                with patch('isolation.connect_engine', return_value=(Connection(), {})), patch('isolation.threading.Timer', Timer):
                    isolation.engine_worker(args)
                status = json.loads((root / 'duckdb_adbc.json').read_text())
                self.assertEqual(status['phase'], 'execute')
                self.assertEqual(status['duckdb_adbc_status'], 'timeout' if fired else 'error')
                if fired:
                    self.assertEqual(status['duckdb_adbc_timeout_cause'], 'duckdb_interrupt_timer')
                    self.assertEqual(status['duckdb_adbc_timeout_seconds'], 60)
                    self.assertIn('deadline exceeded', status['duckdb_adbc_error'])
                else:
                    self.assertNotIn('duckdb_adbc_timeout_cause', status)
                self.assertEqual(status['duckdb_adbc_seconds'], [])

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


class ScannerThreadOptionTests(unittest.TestCase):
    def mock_third(self, private):
        library = private / 'synthetic.so'
        library.write_bytes(b'synthetic driver, never loaded')
        conn = Mock()
        conn.execute.return_value = conn
        conn.fetchone.return_value = ('synthetic-version',)
        connect = Mock(return_value=conn)
        modules = {'duckdb': types.SimpleNamespace(connect=connect),
                   'adbc_driver_redis': types.SimpleNamespace(_driver_path=lambda: str(library))}
        args = argparse.Namespace(private=private, suite='tpcds', engine='duckdb_adbc')
        return args, conn, connect, modules

    def test_default_and_one_omit_scanner_option_for_release_compatibility(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            self.assertEqual(isolation.duckdb_adbc_scan_threads(private), 1)
            for setting in ({}, {'duckdb_adbc_scan_threads': 1}):
                with self.subTest(setting=setting):
                    (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', **setting}))
                    args, conn, _, modules = self.mock_third(private)
                    with patch.dict(sys.modules, modules), patch.dict(os.environ, {'ADBC_SCANNER_EXTENSION': '/synthetic-scanner'}):
                        actual, provenance = isolation.connect_engine(args)
                    attach = next(c.args[0] for c in conn.execute.call_args_list if c.args[0].startswith('ATTACH'))
                    self.assertNotIn('scan_threads', attach)
                    self.assertEqual(provenance['requested_scan_threads'], 1)
                    self.assertIs(actual, conn)

    def test_explicit_scan_threads_route_without_changing_driver_options_or_leaking_profile(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            for count in (16, 1024):
                with self.subTest(count=count):
                    config = {'uri': 'rediss://test:private-secret@localhost', 'client_threads': 16,
                              'duckdb_adbc_scan_threads': count,
                              'db_kwargs': {'adbc.redis.partition_count': '16', 'adbc.redis.default_schema': 'public'}}
                    (private / 'connection.json').write_text(json.dumps(config))
                    args, conn, _, modules = self.mock_third(private)
                    with patch.dict(sys.modules, modules), patch.dict(os.environ, {'ADBC_SCANNER_EXTENSION': '/private-scanner'}):
                        _, provenance = isolation.connect_engine(args)
                    attach = next(c.args[0] for c in conn.execute.call_args_list if c.args[0].startswith('ATTACH'))
                    self.assertIn(f'READ_ONLY, query_pushdown false, scan_threads {count},', attach)
                    self.assertIn('"adbc.redis.partition_count" \'16\'', attach)
                    self.assertEqual(config, json.loads((private / 'connection.json').read_text()))
                    self.assertEqual(provenance['client_threads'], 16)
                    self.assertEqual(provenance['requested_scan_threads'], count)
                    self.assertIs(type(provenance['requested_scan_threads']), int)
                    for secret in ('private-secret', str(private), '/private-scanner', config['uri'], 'db_kwargs'):
                        self.assertNotIn(secret, json.dumps(provenance))
                    self.assertNotIn('actual_scan_threads', provenance)
                    self.assertNotIn('partition_count', provenance)

    def test_invalid_scan_threads_rejected_before_duckdb_open(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            for invalid in (True, False, 0, -1, 1025, 16.0, '16', None, [], {}):
                with self.subTest(value=invalid):
                    (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', 'duckdb_adbc_scan_threads': invalid}))
                    args, _, connect, modules = self.mock_third(private)
                    with patch.dict(sys.modules, modules), self.assertRaisesRegex(ValueError, 'integer from 1 to 1024'):
                        isolation.connect_engine(args)
                    connect.assert_not_called()

    def test_unsupported_scanner_option_fails_without_serial_retry(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', 'duckdb_adbc_scan_threads': 16}))
            args, conn, connect, modules = self.mock_third(private)
            def execute(sql):
                if sql.startswith('ATTACH'):
                    raise RuntimeError('unsupported scan_threads')
                return conn
            conn.execute.side_effect = execute
            with patch.dict(sys.modules, modules), patch.dict(os.environ, {'ADBC_SCANNER_EXTENSION': '/synthetic-scanner'}):
                with self.assertRaisesRegex(RuntimeError, 'unsupported scan_threads'):
                    isolation.connect_engine(args)
            connect.assert_called_once()
            conn.close.assert_called_once()
            self.assertEqual(sum(c.args[0].startswith('ATTACH') for c in conn.execute.call_args_list), 1)


    def test_custom_global_symbols_promotes_only_loaded_duckdb_before_load(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', 'duckdb_adbc_global_symbols': True}))
            args, conn, _, modules = self.mock_third(private)
            events = []
            promote = Mock(side_effect=lambda *a, **k: events.append('promote'))
            modules.update({'_duckdb': types.SimpleNamespace(__file__='/private-loaded-duckdb.so'),
                            'ctypes': types.SimpleNamespace(CDLL=promote, RTLD_GLOBAL=123)})
            def execute(sql):
                events.append('load' if sql.startswith('LOAD ') else 'other')
                return conn
            conn.execute.side_effect = execute
            with patch.dict(sys.modules, modules), patch.dict(os.environ, {'ADBC_SCANNER_EXTENSION': '/custom-scanner'}):
                _, provenance = isolation.connect_engine(args)
            promote.assert_called_once_with('/private-loaded-duckdb.so', mode=123)
            self.assertLess(events.index('promote'), events.index('load'))
            self.assertIs(provenance['custom_scanner_global_symbols'], True)
            self.assertNotIn('/private-loaded-duckdb.so', json.dumps(provenance))
            self.assertNotIn('/custom-scanner', json.dumps(provenance))

    def test_global_symbols_default_and_native_duckdb_never_promote(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            self.assertIs(isolation.duckdb_adbc_global_symbols(private), False)
            for engine, setting in (('duckdb_adbc', {}), ('duckdb_adbc', {'duckdb_adbc_global_symbols': False}),
                                    ('duckdb', {'duckdb_adbc_global_symbols': True})):
                with self.subTest(engine=engine, setting=setting):
                    (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', **setting}))
                    args, _, _, modules = self.mock_third(private)
                    args.engine = engine
                    promote = Mock()
                    modules.update({'ctypes': types.SimpleNamespace(CDLL=promote, RTLD_GLOBAL=123)})
                    with patch.dict(sys.modules, modules), patch.dict(os.environ, {'ADBC_SCANNER_EXTENSION': '/custom-scanner'}):
                        _, provenance = isolation.connect_engine(args)
                    promote.assert_not_called()
                    if engine == 'duckdb_adbc':
                        self.assertIs(provenance['custom_scanner_global_symbols'], False)

    def test_global_symbols_rejects_non_bool_or_missing_custom_scanner_before_open(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            for invalid in (0, 1, 'true', 1.0, None, [], {}):
                with self.subTest(value=invalid):
                    (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', 'duckdb_adbc_global_symbols': invalid}))
                    args, _, connect, modules = self.mock_third(private)
                    with patch.dict(sys.modules, modules), self.assertRaisesRegex(ValueError, 'must be a boolean'):
                        isolation.connect_engine(args)
                    connect.assert_not_called()
            (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', 'duckdb_adbc_global_symbols': True}))
            args, _, connect, modules = self.mock_third(private)
            with patch.dict(sys.modules, modules), patch.dict(os.environ, {}, clear=True):
                with self.assertRaisesRegex(ValueError, 'requires ADBC_SCANNER_EXTENSION'):
                    isolation.connect_engine(args)
            connect.assert_not_called()


if __name__ == '__main__':
    unittest.main()
