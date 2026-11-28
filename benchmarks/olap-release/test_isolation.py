"""Harness regressions: synthetic engine adapters only, no Redis or SF1 queries."""

import argparse
import hashlib
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
DIAGNOSTICS = 'adbc.redis.execution_diagnostics_file'
class Statement:
    """The ADBC statement handle: records options, as the driver would accept them."""
    def __init__(self, conn):
        self.conn = conn
    def set_options(self, **options):
        if os.environ.get('REJECT_DIAGNOSTICS') == engine:
            raise RuntimeError("NOT_IMPLEMENTED: [redis] unknown statement option at rediss://test:private-secret@localhost")
        with (private / 'statement-options.jsonl').open('a') as f:
            f.write(json.dumps({'engine': engine, 'options': options}) + '\\n')
        self.conn.options.update(options)
class Connection:
    description = [('n',)]
    sql = ''
    def __init__(self):
        self.options = {}
    @property
    def adbc_statement(self):
        return Statement(self)
    def execute(self, sql):
        self.sql = sql
        with (private / 'calls.jsonl').open('a') as f:
            f.write(json.dumps({'engine': engine, 'sql': sql, 'pid': os.getpid()}) + '\\n')
        crash = sql == 'SELECT synthetic' and os.environ.get('FAIL_ENGINE') == engine
        # Like the driver, write the sidecar while executing; a crash leaves 'running'.
        sidecar = self.options.pop(DIAGNOSTICS, None)
        if sidecar and os.environ.get('FAKE_SIDECAR'):
            snapshot = json.loads(os.environ['FAKE_SIDECAR'])
            snapshot['status'] = 'running' if crash else 'execute_returned'
            Path(sidecar).write_text(json.dumps(snapshot))
        if crash:
            os._exit(42)
        return self
    def fetchall(self):
        if os.environ.get('HOG_ENGINE') == engine:
            # Allocate past the worker limit. 'tmpfs' charges the cgroup with
            # file pages outside the worker's RSS, like results on a tmpfs.
            hog = []
            if os.environ.get('HOG_MODE') == 'tmpfs':
                artifacts = Path(sys.argv[sys.argv.index('--artifacts') + 1])
                with (artifacts / f'{engine}-hog.pickle').open('wb') as f:
                    while True:
                        f.write(b'x' * (1 << 20))
                        f.flush()
            while True:
                hog.append(b'x' * (1 << 20))
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


def synthetic_profile(private, **profile):
    """Fake engine modules, a private profile and one exported query."""
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
        'db_kwargs': {'adbc.redis.flex': 'true', 'adbc.redis.default_schema': 'public'}, **profile}))
    output = private / 'results'
    queries = output / 'tpch' / 'queries'
    queries.mkdir(parents=True)
    (queries / '19.sql').write_text('SELECT synthetic')
    args = argparse.Namespace(private=private, output=output, suite='tpch', query='19',
                              duckdb_only=False, include_duckdb_adbc=True)
    return args, {'PYTHONPATH': str(fake), 'FAKE_LIBRARY': str(library)}


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
            header, *got = isolation.sorted_rows(root / 'ref', scratch, chunk_size=500, fan_in=3)
            self.assertEqual(header, (2, len(rows)))
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
                         worker_artifact=None, validator_artifact=False, oom_counts=None,
                         corrupt=None, inspect=None, engines_run=3, extra_env=None, on_command=None):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            args, env = synthetic_profile(private)
            env.update(extra_env or {})
            output, library = args.output, Path(env['FAKE_LIBRARY'])
            if failure:
                env['FAIL_ENGINE'] = failure
            if mismatch:
                env['MISMATCH_ENGINE'] = mismatch
            def runner(cmd, **kwargs):
                if on_command:
                    on_command(private, cmd)
                if validator_timeout and cmd[cmd.index('--phase') + 1] == 'validate' and cmd[-1] == 'redis':
                    raise subprocess.TimeoutExpired(cmd, kwargs['timeout'])
                child = subprocess.run(cmd, **kwargs)
                artifacts = Path(cmd[cmd.index('--artifacts') + 1])
                if corrupt and cmd[cmd.index('--phase') + 1] == 'engine':
                    corrupt(artifacts, cmd[-1])
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
            # The synthetic profile is Flex; never reach a real server for the split.
            tiers = patch('diagnostics.flex_tiers', return_value={'split_available': False})
            with patch.dict(os.environ, env), redirect_stdout(io.StringIO()), oom, tiers:
                record = isolation.run_query(args, runner=runner)
            calls = [json.loads(x) for x in (private / 'calls.jsonl').read_text().splitlines()]
            connections = [json.loads(x) for x in (private / 'connections.jsonl').read_text().splitlines()]
            pids = [item['pid'] for item in connections]
            self.assertEqual(len(set(pids)), engines_run)
            self.assertNotIn(os.getpid(), pids)
            self.assertEqual([x['engine'] for x in connections], list(isolation.ENGINES)[:engines_run])
            for connection in connections:
                engine = connection['engine']
                if engine != 'redis' or not worker_artifact:
                    self.assertEqual(record[f'{engine}_worker']['pid'], connection['pid'])
            self.assertEqual(connections[0]['kwargs'], {'read_only': True})
            if engines_run == 3:
                self.assertEqual(connections[2]['args'], [])  # no source DB in third engine
                attach = next(x['sql'] for x in calls if x['sql'].startswith('ATTACH'))
                self.assertIn('query_pushdown false', attach)
                self.assertIn('"adbc.redis.flex" \'true\'', attach)
                self.assertIn(str(library), attach)
            self.assertEqual(len(list(private.glob('query-*'))), 0)
            self.assertNotIn('private-secret', json.dumps(record))
            self.assertNotIn(str(library), json.dumps(record))
            self.assertNotIn(str(private), json.dumps(record))
            if inspect:
                inspect(private, record)
            with self.assertRaisesRegex(ValueError, 'already recorded'):
                isolation.run_query(args)
            historical = output / 'tpch' / 'queries.jsonl'
            historical.write_text(json.dumps({'query': '1'}) + '\n')
            with self.assertRaisesRegex(ValueError, 'historical'):
                isolation.run_query(args)
            return record

    def test_real_child_process_isolation_and_third_engine(self):
        def nothing_retained(private, record):
            self.assertFalse((private / 'failed-artifacts').exists())
            self.assertFalse(any(k.endswith('_retained_artifacts') for k in record))

        record = self.exercise_workers(inspect=nothing_retained)
        for engine in isolation.ENGINES:
            self.assertEqual(record[f'{engine}_status'], 'ok')
            self.assertEqual(len(record[f'{engine}_seconds']), 3)
            # Without worker_memory_limit_bytes the legacy shared cgroup is recorded.
            self.assertEqual(record[f'{engine}_worker']['memory'], {'isolation': 'shared'})
            self.assertNotIn(f'{engine}_validation_memory', record)
        self.assertIn('duckdb_adbc_over_duckdb', record)

    def test_crash_attributed_to_engine_and_third_still_runs(self):
        record = self.exercise_workers(failure='redis')
        self.assertEqual(record['redis_status'], 'worker_failed')
        self.assertEqual(record['duckdb_status'], 'ok')
        self.assertEqual(record['duckdb_adbc_status'], 'ok')
        self.assertNotIn('redis_over_duckdb', record)

    def test_mismatch_has_no_ratio(self):
        def retained(private, record):
            folder = private / record['redis_retained_artifacts']
            self.assertEqual(sorted(p.name for p in folder.iterdir()),
                             ['duckdb-0.pickle', 'duckdb.json', 'redis-0.pickle', 'redis-1.pickle',
                              'redis-2.pickle', 'redis-3.pickle', 'redis-validation.json',
                              'redis-warmup-diagnostics.json', 'redis.json'])
            # The raw sidecar keeps the plan's literals; it stays private.
            self.assertIn('BRAZIL', (folder / 'redis-warmup-diagnostics.json').read_text())

        record = self.exercise_workers(mismatch='redis', inspect=retained,
                                       extra_env={'FAKE_SIDECAR': json.dumps({'plan': {'residual': "'BRAZIL'"}})})
        self.assertEqual(record['redis_status'], 'mismatch')
        self.assertNotIn('redis_over_duckdb', record)
        # The shared reference is copied, so the third engine still validates.
        self.assertEqual(record['duckdb_adbc_status'], 'ok')
        self.assertNotIn('duckdb_adbc_retained_artifacts', record)

    def test_validator_timeout_is_not_engine_oom(self):
        record = self.exercise_workers(validator_timeout=True)
        self.assertEqual(record['redis_status'], 'validation_failed')
        self.assertNotIn('redis_over_duckdb', record)
        self.assertEqual(record['duckdb_adbc_status'], 'ok')
        self.assertTrue(record['redis_retained_artifacts'].startswith('failed-artifacts/tpch-q19-redis-'))

    def test_zero_filled_result_artifact_is_located_and_retained(self):
        # Issue #641: SF1 Flex Q11 reported only "invalid load key, '\x00'." and
        # its result files were deleted. Zero bytes after the last record are
        # what a reader sees when a file grows without its data being written.
        written = {}

        def corrupt(artifacts, engine):
            if engine == 'duckdb_adbc':
                path = artifacts / 'duckdb_adbc-2.pickle'
                written['bytes'] = path.stat().st_size
                with path.open('ab') as out:
                    out.write(bytes(4096))
                self.assertIn(b'\x00' * 4096, path.read_bytes())

        def retained(private, record):
            folder = private / record['duckdb_adbc_retained_artifacts']
            self.assertEqual(folder.stat().st_mode & 0o777, 0o700)
            self.assertEqual(sorted(p.name for p in folder.iterdir()),
                             ['duckdb-0.pickle', 'duckdb.json', 'duckdb_adbc-0.pickle', 'duckdb_adbc-1.pickle',
                              'duckdb_adbc-2.pickle', 'duckdb_adbc-3.pickle', 'duckdb_adbc-validation.json',
                              'duckdb_adbc.json'])
            # The retained file is the one that failed, zeros included.
            self.assertEqual((folder / 'duckdb_adbc-2.pickle').stat().st_size, written['bytes'] + 4096)

        record = self.exercise_workers(corrupt=corrupt, inspect=retained)
        self.assertEqual(record['duckdb_status'], 'ok')
        self.assertEqual(record['redis_status'], 'ok')
        self.assertEqual(record['duckdb_adbc_status'], 'validation_failed')
        self.assertNotIn('duckdb_adbc_over_duckdb', record)
        error = record['duckdb_adbc_error']
        self.assertIn('result duckdb_adbc-2.pickle did not read back as written', error)
        self.assertIn(f"decode failed at byte {written['bytes']} after 5 objects", error)
        self.assertIn("invalid load key, '\\x00'", error)
        self.assertIn(f"4096 zero bytes starting at byte {written['bytes']}", error)
        self.assertIn(f"recorded {written['bytes']} bytes", error)
        self.assertIn(f"a later re-read found {written['bytes'] + 4096} bytes", error)
        problem = record['duckdb_adbc_artifact_problem']
        self.assertEqual(problem['file'], 'duckdb_adbc-2.pickle')
        self.assertEqual(problem['role'], 'result')
        self.assertEqual(problem['decode_offset'], written['bytes'])
        self.assertEqual(problem['zero_bytes_at_offset'], 4096)
        self.assertEqual(problem['bytes_recorded'], written['bytes'])
        self.assertEqual(problem['bytes_read_back'], written['bytes'] + 4096)
        # The decoder reached the zeros itself: it consumed past the real data.
        self.assertGreater(problem['bytes_consumed'], written['bytes'])

    def test_changed_but_decodable_artifact_is_not_a_result_mismatch(self):
        # A rewritten file can decode cleanly; it is still not what the engine returned.
        def corrupt(artifacts, engine):
            if engine == 'duckdb_adbc':
                isolation.dump_rows(artifacts / 'duckdb_adbc-1.pickle', 1, [(99,), (1,), (1,), (None,)])

        record = self.exercise_workers(corrupt=corrupt)
        self.assertEqual(record['duckdb_adbc_status'], 'validation_failed')
        self.assertIn('result duckdb_adbc-1.pickle did not read back as written', record['duckdb_adbc_error'])
        self.assertIn('sha256', record['duckdb_adbc_error'])
        self.assertNotIn('decode_offset', record['duckdb_adbc_artifact_problem'])
        self.assertIn('duckdb_adbc_retained_artifacts', record)

    def test_zeroed_reference_fails_its_own_validation(self):
        def corrupt(artifacts, engine):
            if engine == 'duckdb':
                path = artifacts / 'duckdb-0.pickle'
                path.write_bytes(bytes(path.stat().st_size))

        record = self.exercise_workers(corrupt=corrupt, engines_run=1)
        self.assertEqual(record['duckdb_status'], 'validation_failed')
        self.assertIn('reference duckdb-0.pickle did not read back as written', record['duckdb_error'])
        self.assertEqual(record['duckdb_artifact_problem']['decode_offset'], 0)
        self.assertEqual(record['redis_status'], 'not_run_no_reference')

    def test_missing_artifact_digest_never_passes(self):
        def corrupt(artifacts, engine):
            if engine == 'duckdb_adbc':
                path = artifacts / 'duckdb_adbc.json'
                status = json.loads(path.read_text())
                del status['artifacts']['3']
                path.write_text(json.dumps(status))

        record = self.exercise_workers(corrupt=corrupt)
        self.assertEqual(record['duckdb_adbc_status'], 'validation_failed')
        self.assertIn('no recorded digest for repeat 3', record['duckdb_adbc_error'])

    def test_artifact_digest_matches_written_bytes_and_is_synced(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'artifact'
            rows = [(Decimal('1.5'), 'x' * 200000), (None, 'a' + chr(0) + 'b'), (2, b'\x00\x01')]
            with patch('isolation.os.fsync', wraps=os.fsync) as fsync:
                recorded = isolation.dump_rows(path, 2, rows)
            data = path.read_bytes()
            self.assertEqual(recorded, {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()})
            fsync.assert_called_once()
            self.assertEqual(list(isolation.sorted_rows(path, Path(tmp), expected=recorded))[0], (2, 3))

    def test_validator_scratch_runs_are_verified(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            rows = [(i % 7, str(i)) for i in range(2000)]
            recorded = isolation.dump_rows(root / 'ref', 2, rows)
            scratch = root / 'sort'
            scratch.mkdir()
            real = isolation.dump_objects

            def damaged(path, values, sync=True):
                result = real(path, values, sync)
                if path.name.startswith('merge-0-'):
                    with path.open('r+b') as out:
                        out.write(bytes(8))
                return result

            with patch('isolation.dump_objects', damaged):
                with self.assertRaisesRegex(isolation.ArtifactError,
                                            r'validator scratch merge-0-\d+ did not read back as written'):
                    list(isolation.sorted_rows(root / 'ref', scratch, chunk_size=100, fan_in=3,
                                               expected=recorded))

    def test_validator_scratch_never_uses_the_shared_artifact_directory(self):
        # TPC-DS SF1 Q72 and SF 0.1 Q78 on a macOS Docker Desktop bind mount:
        # the validator's own unsynced sort-0 reopened empty, so the DuckDB
        # reference failed validation and Redis never ran. Synthetic mount: an
        # unsynced write under the shared artifact directory is not visible
        # when reopened; the engines' fsynced result files are.
        with tempfile.TemporaryDirectory() as mount, tempfile.TemporaryDirectory() as local:
            artifacts, scratch = Path(mount), Path(local)
            rows = [(i % 7, str(i)) for i in range(300)]
            status = {'artifacts': {str(repeat): isolation.dump_rows(artifacts / f'duckdb-{repeat}.pickle', 2, rows)
                                    for repeat in range(4)}}
            (artifacts / 'duckdb.json').write_text(json.dumps(status))
            real = isolation.dump_objects
            unsynced = []

            def mount_writer(path, values, sync=True):
                result = real(path, values, sync)
                if not sync:
                    unsynced.append(path)
                    if path.is_relative_to(artifacts):
                        path.write_bytes(b'')  # the host has not caught up yet
                return result

            args = argparse.Namespace(artifacts=artifacts, scratch=scratch, engine='duckdb')
            with patch('isolation.dump_objects', mount_writer):
                code = isolation.validation_worker(args)
            failure = artifacts / 'duckdb-validation.json'
            self.assertEqual(code, 0, failure.read_text() if failure.exists() else '')
            # Every sort run (one per side and repeat) was written outside the mount.
            self.assertEqual(len(unsynced), 8)
            self.assertTrue(all(path.is_relative_to(scratch) for path in unsynced), unsynced)
            self.assertEqual(list(scratch.iterdir()), [])

    def test_coordinator_gives_the_validator_local_scratch_outside_the_profile(self):
        scratches = []

        def on_command(private, cmd):
            phase = cmd[cmd.index('--phase') + 1]
            self.assertEqual('--scratch' in cmd, phase == 'validate')
            if phase == 'validate':
                scratch = Path(cmd[cmd.index('--scratch') + 1])
                self.assertFalse(scratch.is_relative_to(private))
                self.assertFalse(scratch.is_relative_to(Path(cmd[cmd.index('--artifacts') + 1])))
                self.assertEqual(scratch.stat().st_mode & 0o777, 0o700)
                scratches.append(scratch)

        record = self.exercise_workers(on_command=on_command)
        self.assertEqual(len(scratches), 3)
        self.assertEqual(len(set(scratches)), 1)
        self.assertFalse(scratches[0].exists())
        # Same filesystem here; a bind-mounted profile in a container records "separate".
        self.assertEqual(record['validation_scratch_filesystem'], 'same')
        for engine in isolation.ENGINES:
            self.assertEqual(record[f'{engine}_status'], 'ok')

    def test_artifact_problem_separates_the_decoders_bytes_from_a_later_reread(self):
        # Both retained records had no decode offset: the decoder found an empty
        # sort-0. Their "read back" fields came from the error path's own later
        # read, which found zeros (Q72) or exactly the recorded bytes (Q78).
        for shape, later in (('zeros', lambda data: bytes(len(data))), ('recorded', lambda data: data)):
            with self.subTest(shape), tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp) / 'sort-0'
                recorded = isolation.dump_objects(path, [(1, 'a'), (2, 'b')] * 500, sync=False)
                data = path.read_bytes()
                path.write_bytes(b'')  # what the decoder's open finds
                real = isolation._inspect_artifact

                def visible_later(p, offset=None):
                    p.write_bytes(later(data))
                    if shape == 'zeros':
                        self.assertIn(b'\x00' * 4096, p.read_bytes())
                    return real(p, offset)

                with patch('isolation._inspect_artifact', visible_later):
                    with self.assertRaises(isolation.ArtifactError) as caught:
                        list(isolation.verified_objects(path, recorded, 'validator scratch'))
                problem = isolation.artifact_problem({'artifact': caught.exception.detail})
                message = str(caught.exception)
                self.assertEqual(problem['objects_decoded'], 0)
                self.assertNotIn('decode_offset', problem)
                self.assertEqual(problem['bytes_consumed'], 0)
                self.assertEqual(problem['sha256_consumed'], hashlib.sha256(b'').hexdigest()[:16])
                self.assertEqual(problem['bytes_read_back'], len(data))
                self.assertIn('the decoder consumed 0 bytes', message)
                self.assertIn(f'a later re-read found {len(data)} bytes', message)
                if shape == 'zeros':
                    self.assertEqual(problem['first_zero_page_offset'], 0)
                    self.assertEqual(problem['sha256_read_back'], hashlib.sha256(bytes(len(data))).hexdigest()[:16])
                    self.assertNotIn('matching the record', message)
                else:
                    self.assertNotIn('first_zero_page_offset', problem)
                    self.assertEqual(problem['sha256_read_back'], recorded['sha256'][:16])
                    self.assertIn('matching the record', message)

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


class WorkerMemoryProfileTests(unittest.TestCase):
    """Profile and cgroup preconditions; checked before any cgroup is written."""

    def test_worker_memory_limit_is_optional_and_validated(self):
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            self.assertIsNone(isolation.worker_memory_limit(private))
            self.assertIsNone(isolation.worker_memory(private))
            (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', 'worker_memory_limit_bytes': 8 << 30}))
            self.assertEqual(isolation.worker_memory_limit(private), 8 << 30)
            for invalid in (True, '8589934592', 8.0, 0, -1, (16 << 20) - 1, [], {}):
                with self.subTest(value=invalid):
                    (private / 'connection.json').write_text(json.dumps({'uri': 'synthetic', 'worker_memory_limit_bytes': invalid}))
                    with self.assertRaisesRegex(ValueError, 'at least 16 MiB'):
                        isolation.worker_memory_limit(private)

    def fake_cgroup(self, root, own, limits):
        (root / own.lstrip('/')).mkdir(parents=True, exist_ok=True)
        for path, value in limits.items():
            (root / path.lstrip('/') / 'memory.max').write_text(value + '\n')
        proc = root / 'self-cgroup'
        proc.write_text(f'0::{own}\n')
        return proc

    def test_refuses_without_cgroup_v2_namespace_or_limit(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            v1 = root / 'v1'
            v1.write_text('12:memory:/docker/abc\n1:name=systemd:/docker/abc\n')
            with self.assertRaisesRegex(RuntimeError, 'requires Linux cgroup v2'):
                isolation.WorkerCgroups(64 << 20, root=root, self_cgroup=v1)
            with self.assertRaisesRegex(RuntimeError, 'requires Linux cgroup v2'):
                isolation.WorkerCgroups(64 << 20, root=root, self_cgroup=root / 'missing')
            # A process moved outside its cgroup namespace sees an escaping path.
            outside = root / 'outside'
            outside.write_text('0::/../../system.slice\n')
            with self.assertRaisesRegex(RuntimeError, 'requires Linux cgroup v2'):
                isolation.WorkerCgroups(64 << 20, root=root, self_cgroup=outside)
            # The host's real root cgroup has no memory.max: never rearrange it.
            host = self.fake_cgroup(root, '/', {})
            with self.assertRaisesRegex(RuntimeError, "not the host's root cgroup"):
                isolation.WorkerCgroups(64 << 20, root=root, self_cgroup=host)

    def test_requires_coordinator_headroom_at_the_tightest_ancestor(self):
        limit, headroom = 64 << 20, isolation.COORDINATOR_HEADROOM_BYTES
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            # The client container sets the limit; a nested "max" cgroup does not lift it.
            proc = self.fake_cgroup(root, '/client', {'/': str(limit + headroom - 1), '/client': 'max'})
            with self.assertRaisesRegex(ValueError, 'coordinator headroom'):
                isolation.WorkerCgroups(limit, root=root, self_cgroup=proc)
            proc = self.fake_cgroup(root, '/', {'/': str(limit)})
            with self.assertRaisesRegex(ValueError, 'coordinator headroom'):
                isolation.WorkerCgroups(limit, root=root, self_cgroup=proc)
            # Enough headroom, but no writable cgroup interface files.
            proc = self.fake_cgroup(root, '/', {'/': str(limit + headroom)})
            with self.assertRaisesRegex(RuntimeError, 'must be writable'):
                isolation.WorkerCgroups(limit, root=root, self_cgroup=proc)


COORDINATOR = """
import argparse, json, sys
from pathlib import Path
import isolation
args = argparse.Namespace(private=Path(sys.argv[1]), output=Path(sys.argv[1]) / 'results', suite='tpch',
                          query='19', duckdb_only=False, include_duckdb_adbc=True)
print('RECORD ' + json.dumps(isolation.run_query(args)), flush=True)
"""


@unittest.skipUnless(os.environ.get('OLAP_HARNESS_CGROUP_TESTS') == '1',
                     'set OLAP_HARNESS_CGROUP_TESTS=1 only inside a disposable Linux container with a writable '
                     'cgroup v2 mount; the test moves every process in its cgroup into a harness leaf')
class WorkerMemoryCgroupTests(unittest.TestCase):
    """Issue #660: a worker OOM must not take the coordinator with it."""

    LIMIT = 64 << 20

    def test_worker_oom_is_contained_and_recorded(self):
        harness = Path(__file__).resolve().parent
        shm = os.statvfs('/dev/shm')
        self.assertGreater(shm.f_bavail * shm.f_frsize, 2 * self.LIMIT, 'run the container with --shm-size 256m')
        for mode in ('anonymous', 'tmpfs'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory(dir='/dev/shm' if mode == 'tmpfs' else None) as tmp:
                private = Path(tmp)
                _, env = synthetic_profile(private, worker_memory_limit_bytes=self.LIMIT)
                env['PYTHONPATH'] += os.pathsep + str(harness)
                coordinator = subprocess.run([sys.executable, '-c', COORDINATOR, str(private)],
                                             env={**os.environ, **env, 'HOG_ENGINE': 'redis', 'HOG_MODE': mode},
                                             capture_output=True, text=True, timeout=240)
                self.assertEqual(coordinator.returncode, 0, f'coordinator died: {coordinator.stderr[-2000:]}')
                line = next(x for x in coordinator.stdout.splitlines() if x.startswith('RECORD '))
                record = json.loads(line[len('RECORD '):])
                worker = record['redis_worker']
                self.assertEqual(record['redis_status'], 'worker_failed')
                self.assertEqual(record['redis_error'], 'redis worker exited with code -9')
                self.assertEqual(worker['returncode'], -9)
                self.assertGreaterEqual(worker['oom_kills_delta'], 1)
                self.assertEqual(worker['memory']['isolation'], 'cgroup')
                self.assertEqual(worker['memory']['limit_bytes'], self.LIMIT)
                self.assertGreaterEqual(worker['memory']['oom_kills'], 1)
                self.assertLessEqual(worker['memory'].get('peak_bytes', 0), self.LIMIT)
                self.assertNotIn('cleanup_error', worker['memory'])
                # Engines before and after the OOM still run and validate in their own leaves.
                for engine in ('duckdb', 'duckdb_adbc'):
                    self.assertEqual(record[f'{engine}_status'], 'ok')
                    self.assertEqual(record[f'{engine}_worker']['memory']['oom_kills'], 0)
                    self.assertEqual(record[f'{engine}_validation_memory']['oom_kills'], 0)
                self.assertNotIn(str(private), json.dumps(record))
        own = Path('/proc/self/cgroup').read_text().strip()
        self.assertTrue(own.endswith('/' + isolation.HARNESS_LEAF), own)
        client = isolation.CGROUP_ROOT / own[3:].lstrip('/')
        self.assertEqual([p.name for p in client.parent.iterdir()
                          if p.name.startswith(isolation.WORKER_PREFIX)], [])


if __name__ == '__main__':
    unittest.main()
