"""Post-Arrow comparison: matched SQL/Arrow/native writes plus creation paths."""
import argparse
import concurrent.futures
import json
import multiprocessing
from pathlib import Path
import resource
import sys
import time

import pyarrow as pa

# A normal import keeps spawned workers importable and shares the independently
# checked source, fixture gates, readers, validation and ownership safeguards.
import run as base


def write_worker(payload):
    cfg, mode, lo, count, batch_rows = payload
    if cfg['api'] != 'arrow':
        return base.write_worker(payload)
    table = base.source(cfg['source']).slice(lo, count)
    numbered = table.append_column(base.ORDINAL, pa.array(range(lo + 1, lo + count + 1), type=pa.int64()))
    if mode == 'unmanaged':
        numbered = numbered.append_column('__key', pa.array([base.PREFIX + str(n) for n in range(lo + 1, lo + count + 1)], type=pa.string()))
    with base.connect(cfg) as c, c.cursor() as q:
        operation = cfg['operation']
        if mode == 'unmanaged' and operation != 'append':
            q.adbc_statement.set_options(**{'adbc.redis.ingest.unmanaged_source': json.dumps(
                {'prefix': base.PREFIX, 'index': base.INDEX})})
        written = 0
        for batch in numbered.to_batches(max_chunksize=batch_rows):
            current = 'append' if operation == 'create' and written else operation
            n = q.adbc_ingest(mode, batch, mode=current, db_schema_name='bench')
            assert n == batch.num_rows, n
            written += n
    return {'rows': written, 'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss *
            (1 if sys.platform == 'darwin' else 1024)}


def safe_write_worker(payload):
    try:
        return write_worker(payload)
    except Exception as error:
        return {'error_type': type(error).__name__,
                'error_message': str(error).replace(payload[0]['uri'], '[private endpoint]'),
                'sqlstate': getattr(error, 'sqlstate', None)}


class Benchmark(base.Benchmark):
    def __init__(self, args):
        super().__init__(args)
        self.results['creation'] = []
        self.results['metadata'].update(
            scope='Unreleased PR #401 source; SQL/Arrow/native comparison',
            arrow_batch_rows=args.sql_batch_rows,
            write_paths=['managed_sql', 'unmanaged_sql', 'managed_arrow', 'unmanaged_arrow', 'redis_py'],
            automatic_retries=0)
        self.save()

    def verify_unmanaged_arrow(self):
        empty = self.expected.slice(0, 0).append_column(base.ORDINAL, pa.array([], type=pa.int64()))
        empty = empty.append_column('__key', pa.array([], type=pa.string()))
        with base.connect(self.cfg) as c, c.cursor() as q:
            assert q.adbc_ingest('unmanaged', empty, mode='append', db_schema_name='bench') == 0
        self.results['unmanaged_arrow_ingestion'] = 'Supported (empty typed append verified)'

    def run_writes(self):
        cases = [(mode, api, n, 'append') for n in (1, 4)
                 for mode, api in [('managed', 'sql'), ('unmanaged', 'sql'),
                                   ('managed', 'arrow'), ('unmanaged', 'arrow'), ('redis_py', 'hset')]]
        self.write_cases('writes', cases)

    def run_creation(self):
        cases = [(m, 'arrow', 1, 'create') for m in ('managed', 'unmanaged')]
        cases += [(m, 'arrow', n, 'create_append') for n in (1, 4) for m in ('managed', 'unmanaged')]
        cases += [('redis_py', 'hset', n, 'create') for n in (1, 4)]
        self.write_cases('creation', cases)

    def write_cases(self, suite, cases):
        expected = self.expected.slice(0, self.args.write_rows)
        for rep in range(-1, self.args.write_repeats):
            for mode, api, writers, operation in (cases if rep % 2 else reversed(cases)):
                self.reset()
                if suite == 'writes':
                    base.setup_managed(self.cfg, expected.slice(0, 0))
                    if mode != 'managed':
                        base.raw_create(self.cfg, expected.schema)
                        if mode == 'unmanaged':
                            base.seed(self.cfg, expected.slice(0, 1))
                            base.register(self.cfg, expected.schema, self.work / 'registration-private.log')
                            with base.client(self.cfg) as r:
                                r.delete(base.PREFIX + '1')
                            base.ready(self.cfg, 0)
                else:
                    with base.connect(self.cfg) as c, c.cursor() as q:
                        q.execute('CREATE SCHEMA IF NOT EXISTS bench')
                cfg = {**self.cfg, 'api': api, 'operation': operation}
                count = self.args.write_rows // writers
                payloads = [(cfg, mode, i * count, count, self.args.sql_batch_rows) for i in range(writers)]
                start = time.perf_counter()
                if suite == 'creation' and mode == 'redis_py':
                    with base.client(cfg) as r:
                        r.execute_command('FT.CREATE', base.INDEX, 'ON', 'HASH', 'PREFIX', 1, base.PREFIX,
                                          'SKIPINITIALSCAN', 'SCHEMA', expected.column_names[0], 'TAG', 'NOINDEX')
                with concurrent.futures.ProcessPoolExecutor(writers, mp_context=multiprocessing.get_context('spawn')) as pool:
                    workers = list(pool.map(safe_write_worker, payloads))
                errors = [w for w in workers if 'error_type' in w]
                row = {'mode': mode, 'api': api, 'operation': operation, 'writers': writers,
                       'repeat': rep, 'warmup': rep < 0, 'rows': self.args.write_rows, 'columns': 105,
                       'batch_rows': self.args.sql_batch_rows,
                       'setup_in_clock': suite == 'creation',
                       'index_strategy': 'membership_only' if suite == 'creation' and mode != 'managed' else 'full_field_fixture'}
                if errors:
                    if mode != 'managed' or api != 'sql' or writers != 4 or any(e.get('sqlstate') != '40001' for e in errors):
                        raise RuntimeError('Unexpected write failure: ' + json.dumps(errors))
                    row.update(validated=False, status='serialization_conflict', seconds=None, errors=errors,
                               partial_rows=self.validate_partial_managed(expected), partial_values_validated=True)
                else:
                    if mode != 'managed':
                        base.ready(cfg, self.args.write_rows)
                    elapsed = time.perf_counter() - start
                    assert sum(w['rows'] for w in workers) == self.args.write_rows
                    if mode != 'redis_py':
                        self.validate_sql(mode, expected)
                    if mode != 'managed':
                        self.validate_raw(expected)
                    row.update(validated=True, status='validated', seconds=elapsed,
                               peak_worker_rss_bytes=max(w['peak_rss_bytes'] for w in workers))
                self.results[suite].append(row)
                self.save()
                print(suite.upper(), json.dumps(row), flush=True)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('source', 'driver', 'cli', 'work'):
        p.add_argument('--' + name, type=Path, required=True)
    p.add_argument('--version', required=True)
    p.add_argument('--dedicated-empty-server', action='store_true', required=True)
    p.add_argument('--read-repeats', type=int, default=6)
    p.add_argument('--write-repeats', type=int, default=3)
    p.add_argument('--write-rows', type=int, default=10000)
    p.add_argument('--sql-batch-rows', type=int, default=1000)
    p.add_argument('--suite', choices=['all', 'reads', 'writes', 'creation', 'write-all'], default='all')
    args = p.parse_args()
    if not 4 <= args.write_rows <= 100000 or args.write_rows % 4 or min(args.read_repeats, args.write_repeats, args.sql_batch_rows) <= 0:
        p.error('Invalid rows/repetitions/batch size')
    b = Benchmark(args)
    try:
        if args.suite in ('all', 'reads'):
            b.run_reads()
        if args.suite in ('all', 'writes', 'write-all'):
            b.run_writes()
        if args.suite in ('all', 'creation', 'write-all'):
            b.run_creation()
        b.results['final_qpf'] = b.gate()
        b.results['status'] = 'complete_with_write_conflicts' if any(not r['validated'] for r in b.results['writes']) else 'complete'
        b.save()
    except Exception as error:
        b.results['status'] = 'failed'
        b.results['failures'].append({'error_type': type(error).__name__})
        b.save()
        raise


if __name__ == '__main__':
    main()
