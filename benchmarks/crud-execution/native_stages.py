"""Diagnostic cursor-stage timings around unchanged normal native CRUD scenarios.

This does not replace the uninstrumented benchmark. All original typed-output and
complete post-state oracles remain. SQL, values, keys and endpoints are omitted.
"""
from pathlib import Path
import hashlib
import json
import os
import sys
import threading
import time


_active_record = threading.local()


def install_native_substages(dbapi):
    """Optional nested spans on the pinned DBAPI's unchanged real operations."""
    original_close = dbapi._RowIterator.close
    original_init = dbapi._RowIterator.__init__
    original_blocking = dbapi._blocking_call

    def timed(name, function, *args, **kwargs):
        record = getattr(_active_record, 'record', None)
        if record is None:
            return function(*args, **kwargs)
        started = time.perf_counter_ns()
        try:
            return function(*args, **kwargs)
        finally:
            spans = record.setdefault('native_stage_ns', {})
            spans[name] = spans.get(name, 0) + time.perf_counter_ns() - started

    def close(self):
        return timed('previous_result_close', original_close, self)

    def initialize(self, *args, **kwargs):
        return timed('result_construction', original_init, self, *args, **kwargs)

    def blocking(function, args, kwargs, cancel):
        name = getattr(function, '__name__', '')
        category = {'prepare': 'adbc_prepare', 'execute_query': 'adbc_execute_query',
                    'execute_update': 'adbc_execute_update',
                    'read_all': 'arrow_read_all'}.get(name)
        if category is None:
            return original_blocking(function, args, kwargs, cancel)
        return timed(category, original_blocking, function, args, kwargs, cancel)

    dbapi._RowIterator.close = close
    dbapi._RowIterator.__init__ = initialize
    dbapi._blocking_call = blocking


class CursorStages:
    """Transparent methods used by scenarios.operation_call; same real cursor."""
    def __init__(self, cursor, record):
        self._cursor = cursor
        self._record = record

    def _call(self, name, function, *args, **kwargs):
        started = time.perf_counter_ns()
        previous = getattr(_active_record, 'record', None)
        _active_record.record = self._record
        try:
            return function(*args, **kwargs)
        finally:
            self._record['stage_ns'][name] = time.perf_counter_ns() - started
            _active_record.record = previous

    def execute(self, *args, **kwargs):
        return self._call('execute', self._cursor.execute, *args, **kwargs)

    def executemany(self, *args, **kwargs):
        return self._call('executemany', self._cursor.executemany, *args, **kwargs)

    def fetch_arrow_table(self):
        return self._call('fetch_arrow_table', self._cursor.fetch_arrow_table)

    @property
    def rowcount(self):
        return self._call('rowcount', lambda: self._cursor.rowcount)

    def __getattr__(self, name):
        return getattr(self._cursor, name)


def install(scenarios):
    records = []
    lock = threading.Lock()
    current_trial = None
    original_call = scenarios.operation_call
    original_trial = scenarios.ScenarioBenchmark.run_trial
    original_init = scenarios.ScenarioBenchmark.__init__
    original_save = scenarios.ScenarioBenchmark.save
    nested = os.environ.get('REDIS_CRUD_NATIVE_SUBSTAGES') == '1'
    if nested:
        install_native_substages(scenarios.base.adbc)

    def call(cfg, mode, operation, index, rows, rowids, q, r):
        trial = current_trial
        if trial is None:
            raise RuntimeError('Native stage diagnostic requires one active trial')
        record = {**trial, 'index': index, 'stage_ns': {}, 'operation_completed': False}
        cursor = CursorStages(q, record) if q is not None else None
        started = time.perf_counter_ns()
        try:
            value = original_call(cfg, mode, operation, index, rows, rowids, cursor, r)
            record['operation_completed'] = True
            return value
        except Exception as error:
            record['error_type'] = type(error).__name__
            raise
        finally:
            record['elapsed_ns'] = time.perf_counter_ns() - started
            with lock:
                records.append(record)

    def trial(self, suite, operation, mode, clients, repeat, mapping):
        nonlocal current_trial
        if current_trial is not None:
            raise RuntimeError('Stage diagnostic must schedule trials sequentially')
        current_trial = dict(suite=suite, operation=operation, mode=mode,
                             clients=clients, repeat=repeat, warmup=repeat < 0)
        try:
            return original_trial(self, suite, operation, mode, clients, repeat, mapping)
        finally:
            current_trial = None

    def save(self):
        original_save(self)
        with lock:
            data = {
                'schema_version': 1,
                'wrapper_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                'metadata': self.results.get('metadata'),
                'status': self.results.get('status'),
                'nested_native_substages': nested,
                'records': list(records),
                'scope': ('Instrumented ordinary Python/ADBC scenarios.operation_call. '
                          'Transparent cursor proxy measures execute, complete fetch_arrow_table '
                          'and rowcount on the actual native cursor. Original full typed-result '
                          'and mutation final-state validation retained in results.json. '
                          'Proxy/observer overhead included; use uninstrumented native results '
                          'for headline comparisons. Execute may include prior-result cleanup; '
                          'fetch may include stream release. These spans do not isolate C ABI, '
                          'forced GC, Redis/server CPU or wire retries. Raw operations retain '
                          'their entire command/typed-conversion elapsed span. Optional '
                          'native_stage_ns are NESTED inside cursor spans, never additive: '
                          'actual DBAPI previous-result close, blocking prepare/execute_query '
                          'and Arrow read_all, with original cancellation and lifetime unchanged.'),
            }
        path = self.work / 'native-stages.json'
        temporary = path.with_suffix('.tmp')
        temporary.write_text(json.dumps(data, indent=2) + '\n')
        temporary.replace(path)

    def initialize(self, args):
        if args.suite != 'oltp':
            raise ValueError('Select --suite oltp for the current CRUD stage diagnostic')
        original_init(self, args)
        self.results['metadata']['stage_diagnostic_wrapper_sha256'] = hashlib.sha256(
            Path(__file__).read_bytes()).hexdigest()
        self.save()

    scenarios.operation_call = call
    scenarios.ScenarioBenchmark.run_trial = trial
    scenarios.ScenarioBenchmark.save = save
    scenarios.ScenarioBenchmark.__init__ = initialize


def main():
    root = Path(os.environ.get('REDIS_CRUD_STAGE_PROFILE_ROOT',
                               Path(__file__).resolve().parents[2]))
    sys.path.insert(0, str(root / 'benchmarks/table-modes'))
    import scenarios
    install(scenarios)
    scenarios.main()


if __name__ == '__main__':
    main()
