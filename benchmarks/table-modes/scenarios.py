"""Validated OLTP/OLAP chart scenarios on an exclusively owned Software fixture.

Persistent per-thread clients; no retries. Per-operation latency includes SQL
execute/full Arrow fetch or Redis commands plus equivalent typed conversion.
Connection setup, independent validation and fixture setup are outside timing.
"""
import argparse
import concurrent.futures
from contextlib import ExitStack
from decimal import Decimal
import hashlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import statistics
import subprocess
import threading
import time
import uuid
from urllib.parse import parse_qs, urlsplit

import pyarrow as pa
import run as base

SCHEMA = pa.schema([('id', pa.int64()), ('grp', pa.int64()),
                    ('amount', pa.int64()), ('label', pa.string())])
PROJECTION = pa.schema([SCHEMA.field(n) for n in ('id', 'amount', 'label')])
POINT = ('point_read', 'insert', 'update', 'delete')
QUERIES = ('filtered_projection', 'filtered_count_sum', 'grouped_count_sum')


def record(i):
    return {'id': i, 'grp': i % 16, 'amount': i * 3, 'label': f'row-{i:08d}'}


def dataset(rows):
    return pa.Table.from_pylist([record(i) for i in range(1, rows + 1)], schema=SCHEMA)


def percentile(values, fraction):
    assert values and all(math.isfinite(v) and v > 0 for v in values)
    return sorted(values)[max(0, math.ceil(len(values) * fraction) - 1)]


def point_id(operation, index, rows):
    return rows + index + 1 if operation == 'insert' else (index * 61) % rows + 1


def bounds(operation, index, rows):
    count = 100 if operation == 'filtered_projection' else 10000
    lo = 1 + (index * 997) % (rows - count + 1)
    return lo, lo + count - 1


def expected_query(operation, index, rows):
    if operation == 'grouped_count_sum':
        groups = []
        for grp in range(16):
            first = grp or 16
            count = (rows - first) // 16 + 1
            last = first + (count - 1) * 16
            groups.append({'grp': grp, 'n': count, 'total': 3 * (first + last) * count // 2})
        return pa.Table.from_pylist(groups, schema=pa.schema([('grp', pa.int64()), ('n', pa.int64()), ('total', pa.int64())]))
    lo, hi = bounds(operation, index, rows)
    if operation == 'filtered_projection':
        return pa.Table.from_pylist([record(i) for i in range(lo, hi + 1)], schema=SCHEMA).select(PROJECTION.names)
    n = hi - lo + 1
    return pa.table({'n': pa.array([n], type=pa.int64()),
                     'total': pa.array([3 * (lo + hi) * n // 2], type=pa.int64())})


def raw_integer(value):
    number = Decimal(value.decode() if isinstance(value, bytes) else str(value))
    assert number == number.to_integral_value()
    return int(number)


def raw_query(r, cfg, operation, index, rows):
    if operation == 'filtered_projection':
        lo, hi = bounds(operation, index, rows)
        cursor = 0; values = []
        try:
            reply = r.execute_command('FT.AGGREGATE', base.INDEX, f'@{base.ORDINAL}:[{lo} {hi}]',
                                      'LOAD', 1, '@__key', 'TIMEOUT', 0,
                                      'WITHCURSOR', 'COUNT', 4096, 'DIALECT', 2)
            while True:
                page, cursor = reply
                keys = [x[b'extra_attributes'][b'__key'] for x in page[b'results']]
                if keys:
                    with r.pipeline(transaction=False) as pipe:
                        for key in keys: pipe.hmget(key, PROJECTION.names)
                        for row in pipe.execute():
                            assert all(v is not None for v in row)
                            values.append({'id': int(row[0]), 'amount': int(row[1]), 'label': row[2].decode()})
                if not cursor: break
                reply = r.execute_command('FT.CURSOR','READ',base.INDEX,cursor,'COUNT',4096)
        finally:
            if cursor: r.execute_command('FT.CURSOR','DEL',base.INDEX,cursor)
        return pa.Table.from_pylist(values, schema=PROJECTION)
    query = '*' if operation == 'grouped_count_sum' else '@' + base.ORDINAL + ':[%d %d]' % bounds(operation, index, rows)
    group = ['GROUPBY', 1, '@grp'] if operation == 'grouped_count_sum' else ['GROUPBY', 0]
    reply = r.execute_command('FT.AGGREGATE', base.INDEX, query, *group,
                              'REDUCE', 'COUNT', 0, 'AS', 'n',
                              'REDUCE', 'SUM', 1, '@amount', 'AS', 'total', 'DIALECT', 2)
    schema = pa.schema(([('grp', pa.int64())] if operation == 'grouped_count_sum' else []) + [('n', pa.int64()), ('total', pa.int64())])
    values = [{k: raw_integer(row[b'extra_attributes'][k.encode()]) for k in schema.names}
              for row in reply[b'results']]
    return pa.Table.from_pylist(values, schema=schema)


def sql_query(mode, operation, index, rows):
    table = 'bench.' + mode
    if operation == 'grouped_count_sum':
        return f'SELECT grp,CAST(COUNT(*) AS BIGINT) AS n,CAST(SUM(amount) AS BIGINT) AS total FROM {table} GROUP BY grp'
    lo, hi = bounds(operation, index, rows)
    cols = 'id,amount,label' if operation == 'filtered_projection' else 'CAST(COUNT(*) AS BIGINT) AS n,CAST(SUM(amount) AS BIGINT) AS total'
    return f'SELECT {cols} FROM {table} WHERE {base.ORDINAL} BETWEEN {lo} AND {hi}'


def operation_call(cfg, mode, operation, index, rows, rowids, q, r):
    if operation in QUERIES:
        if mode == 'redis_py': return raw_query(r, cfg, operation, index, rows)
        q.execute(sql_query(mode, operation, index, rows))
        return q.fetch_arrow_table()
    i = point_id(operation, index, rows)
    key = base.PREFIX + str(i)
    values = record(i)
    if mode == 'redis_py':
        if operation == 'point_read':
            raw = r.hmget(key, SCHEMA.names)
            assert all(v is not None for v in raw)
            return pa.Table.from_pylist([{'id': int(raw[0]), 'grp': int(raw[1]),
                                         'amount': int(raw[2]), 'label': raw[3].decode()}], schema=SCHEMA)
        if operation == 'insert': return r.hset(key, mapping={**values, base.ORDINAL: i})
        if operation == 'update': return r.hset(key, 'amount', -i)
        return r.delete(key)
    predicate = f'__rowid={rowids[i]}' if mode == 'managed' and operation != 'insert' else '__key=' + base.literal(key)
    if operation == 'point_read':
        q.execute(f'SELECT id,grp,amount,label FROM bench.{mode} WHERE {predicate}')
        return q.fetch_arrow_table()
    if operation == 'insert':
        fields = SCHEMA.names + [base.ORDINAL]
        data = list(values.values()) + [i]
        if mode == 'unmanaged': fields = ['__key'] + fields; data = [key] + data
        sql = f'INSERT INTO bench.{mode} (' + ','.join(fields) + ') VALUES (' + ','.join(base.literal(v) for v in data) + ')'
    elif operation == 'update': sql = f'UPDATE bench.{mode} SET amount={-i} WHERE {predicate}'
    else: sql = f'DELETE FROM bench.{mode} WHERE {predicate}'
    q.execute(sql)
    return q.rowcount


def trial_worker(cfg, mode, operation, indices, rows, rowids, barriers, release):
    try:
        with ExitStack() as stack:
            r = stack.enter_context(base.client(cfg)) if mode == 'redis_py' else None
            c = stack.enter_context(base.connect(cfg)) if mode != 'redis_py' else None
            q = stack.enter_context(c.cursor()) if c is not None else None
            barriers[0].wait(timeout=180)
            barriers[1].wait(timeout=180)
            latencies = []; outputs = []
            for index in indices:
                start = time.perf_counter_ns()
                value = operation_call(cfg, mode, operation, index, rows, rowids, q, r)
                latencies.append(time.perf_counter_ns() - start)
                outputs.append((index, value))
            barriers[2].wait(timeout=180)
            assert release.wait(timeout=180)
        return {'latency_ns': latencies, 'outputs': outputs}
    except Exception as error:
        for barrier in barriers: barrier.abort()
        release.set()
        return {'error_type': type(error).__name__, 'sqlstate': getattr(error, 'sqlstate', None)}


class ScenarioBenchmark(base.Benchmark):
    def __init__(self, args):
        self.args = args; self.work = args.work.resolve(); self.work.mkdir(parents=True, exist_ok=False)
        self.cfg = {'uri': os.environ['REDIS_BENCH_URI'], 'ca': os.environ['SSL_CERT_FILE'],
                    'driver': str(args.driver.resolve()), 'cli': str(args.cli.resolve())}
        uri = urlsplit(self.cfg['uri'])
        if uri.scheme != 'rediss' or parse_qs(uri.query).get('protocol') != ['3']:
            raise ValueError('Require TLS and explicit protocol=3')
        self.owner = uuid.uuid4().hex
        self.results = {'trials': [], 'failures': [], 'status': 'running'}
        gate = self.gate(); assert gate['memory_bytes'] == 4294967296
        with base.client(self.cfg) as r:
            assert r.dbsize() == 0 and not r.execute_command('FT._LIST'), 'Require an empty owned database'
            assert int(r.client_info()['resp']) == 3
            conn = r.connection_pool.get_connection()
            try: assert 'Hiredis' in type(conn._parser).__name__
            finally: r.connection_pool.release(conn)
        with base.connect(self.cfg) as c: assert args.version in str(c.adbc_get_info())
        with base.client(self.cfg) as r: assert r.set(base.MARKER, self.owner, nx=True)
        (self.work/'owner-private.json').write_text(json.dumps({'owner': self.owner}))
        self.results['metadata'] = {'driver_version': args.version,
            'driver_source_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=args.driver.resolve().parents[2], text=True).strip(),
            'driver_sha256': hashlib.sha256(args.driver.read_bytes()).hexdigest(),
            'harness_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            'automatic_retries': 0, 'protocol': 3, 'tls': True, 'repetitions': args.repeats,
            'operations': args.operations, 'queries': args.queries, 'point_rows': args.point_rows,
            'olap_rows': args.olap_rows, 'columns': 4, 'suite': args.suite,
            'generator': 'id=1..N; grp=id%16; amount=id*3; label=row-{id:08d}',
            'timing_scope': 'Persistent per-thread clients; execute plus complete typed Arrow result or acknowledged write. Setup/teardown and independent validation excluded. Closed-loop fixed-operation trials, not saturated capacity.',
            'addressing': 'Managed known internal row IDs; unmanaged/raw known application keys. Mapping lookup excluded; no multi-statement transactions.',
            'packages': {p: importlib.metadata.version(p) for p in ['pyarrow','adbc-driver-manager','redis','hiredis']}}
        self.save()

    def setup(self, mode, rows):
        self.reset(); table = dataset(rows)
        if mode == 'managed':
            base.setup_managed(self.cfg, table)
            with base.connect(self.cfg) as c, c.cursor() as q:
                q.execute('SELECT id,__rowid FROM bench.managed')
                mapping = {x['id']: x['__rowid'] for x in q.fetch_arrow_table().to_pylist()}
            assert len(mapping) == rows
            return mapping
        base.raw_create(self.cfg, SCHEMA)
        with base.client(self.cfg) as r:
            for start in range(1, rows + 1, 1000):
                with r.pipeline(transaction=False) as pipe:
                    for i in range(start, min(start + 1000, rows + 1)):
                        pipe.hset(base.PREFIX + str(i), mapping={**record(i), base.ORDINAL: i})
                    assert all(n == 5 for n in pipe.execute())
        base.ready(self.cfg, rows)
        if mode == 'unmanaged': base.register(self.cfg, SCHEMA, self.work/'registration-private.log')
        return {}

    def validate_state(self, mode, operation, rows, operations):
        values = {i: record(i) for i in range(1, rows + 1)}
        if operation == 'insert': values.update({rows+i+1: record(rows+i+1) for i in range(operations)})
        elif operation == 'update':
            for j in range(operations):
                i = point_id(operation, j, rows); values[i]['amount'] = -i
        elif operation == 'delete':
            for j in range(operations): del values[point_id(operation, j, rows)]
        expected = pa.Table.from_pylist(list(values.values()), schema=SCHEMA)
        if mode != 'redis_py': self.validate_sql(mode, expected)
        if mode != 'managed':
            with base.client(self.cfg) as r:
                keys = list(r.scan_iter(match=base.PREFIX+'*'))
                assert set(keys) == {(base.PREFIX+str(i)).encode() for i in values}
                actual = []
                for start in range(0, len(keys), 1000):
                    part = keys[start:start+1000]
                    with r.pipeline(transaction=False) as pipe:
                        for key in part: pipe.hmget(key, SCHEMA.names+[base.ORDINAL])
                        outputs = pipe.execute()
                    for key, row in zip(part, outputs):
                        assert all(v is not None for v in row)
                        i = int(key.decode()[len(base.PREFIX):]); assert int(row[-1]) == i
                        actual.append({'id': int(row[0]), 'grp': int(row[1]), 'amount': int(row[2]), 'label': row[3].decode()})
            assert base.canonical(pa.Table.from_pylist(actual, schema=SCHEMA), SCHEMA).equals(base.canonical(expected, SCHEMA), check_metadata=False)

    def run_trial(self, suite, operation, mode, clients, repeat, mapping):
        rows = self.args.point_rows if suite == 'oltp' else self.args.olap_rows
        count = self.args.operations if suite == 'oltp' else self.args.queries
        self.gate()
        barriers = [threading.Barrier(clients+1) for _ in range(3)]; release = threading.Event()
        broken = False
        with concurrent.futures.ThreadPoolExecutor(max_workers=clients) as pool:
            futures = [pool.submit(trial_worker, self.cfg, mode, operation, list(range(i, count, clients)), rows, mapping, barriers, release) for i in range(clients)]
            try:
                barriers[0].wait(timeout=180)
                start = time.perf_counter_ns(); barriers[1].wait(timeout=180)
                barriers[2].wait(timeout=180); elapsed = (time.perf_counter_ns()-start)/1e9
            except threading.BrokenBarrierError:
                broken = True; elapsed = None
            finally: release.set()
            workers = [f.result() for f in futures]
        errors = [w for w in workers if 'error_type' in w]
        row = {'suite': suite, 'operation': operation, 'mode': mode, 'clients': clients,
               'repeat': repeat, 'warmup': repeat < 0, 'fixture_rows': rows, 'columns': 4,
               'operations': count, 'validated': False, 'status': 'failed'}
        if errors or broken:
            row['errors'] = errors; self.results['trials'].append(row); self.save()
            raise RuntimeError('Scenario failed; original error types/SQLSTATE retained; no retries')
        for w in workers:
            for index, value in w['outputs']:
                if operation in QUERIES: expected = expected_query(operation, index, rows)
                elif operation == 'point_read': expected = pa.Table.from_pylist([record(point_id(operation,index,rows))], schema=SCHEMA)
                else:
                    assert value in ((5,) if mode == 'redis_py' and operation == 'insert' else (0,) if mode == 'redis_py' and operation == 'update' else (1,) if mode == 'redis_py' else (-1,1)), value
                    continue
                assert base.canonical(value, expected.schema).equals(base.canonical(expected, expected.schema), check_metadata=False)
        if suite == 'oltp': self.validate_state(mode, operation, rows, count)
        latencies = [v for w in workers for v in w['latency_ns']]
        assert len(latencies) == count
        row.update(validated=True, status='validated', seconds=elapsed, latency_ns=latencies,
                   median_latency_ms=statistics.median(latencies)/1e6,
                   p95_latency_ms=percentile(latencies,.95)/1e6, operations_per_second=count/elapsed)
        self.results['trials'].append(row); self.save()
        print(suite, operation, mode, clients, repeat, 'validated', flush=True)

    def run(self):
        for suite, operations in [('oltp', POINT), ('olap', QUERIES)]:
            if self.args.suite not in ('all', suite): continue
            if suite == 'oltp':
                cases = [(op,m,c) for op in operations for c in (1,4) for m in base.MODES]
                for repeat in range(-1, self.args.repeats):
                    for op,mode,clients in (cases if repeat%2 else reversed(cases)):
                        mapping = self.setup(mode, self.args.point_rows)
                        self.run_trial(suite,op,mode,clients,repeat,mapping)
            else:
                # Read-only cases share one immutable fixture per mode/round;
                # validation/ordering/setup remain outside every timed trial.
                for repeat in range(-1, self.args.repeats):
                    modes = base.MODES if repeat%2 else reversed(base.MODES)
                    for mode in modes:
                        mapping = self.setup(mode,self.args.olap_rows)
                        for op,clients in [(op,c) for op in operations for c in (1,4)]:
                            self.run_trial(suite,op,mode,clients,repeat,mapping)
                        self.validate_state(mode,'point_read',self.args.olap_rows,0)
        self.results['final_qpf'] = self.gate(); self.results['status'] = 'complete'; self.save()


def main():
    p=argparse.ArgumentParser(description=__doc__)
    for name in ('driver','cli','work'): p.add_argument('--'+name,type=Path,required=True)
    p.add_argument('--version',required=True);p.add_argument('--dedicated-empty-server',action='store_true',required=True)
    p.add_argument('--suite',choices=('all','oltp','olap'),default='all')
    p.add_argument('--repeats',type=int,default=3);p.add_argument('--operations',type=int,default=32)
    p.add_argument('--queries',type=int,default=8);p.add_argument('--point-rows',type=int,default=1000)
    p.add_argument('--olap-rows',type=int,default=100000)
    a=p.parse_args()
    if a.repeats<1 or min(a.operations,a.queries)<4 or a.operations%4 or a.queries%4 or a.point_rows<a.operations or a.point_rows%61==0 or a.olap_rows<10000:p.error('Invalid fixture/repetition/operation counts')
    b=ScenarioBenchmark(a)
    try: b.run()
    except Exception as error:
        b.results['status']='failed';b.results['failures'].append({'error_type':type(error).__name__});b.save();raise


if __name__=='__main__':main()
