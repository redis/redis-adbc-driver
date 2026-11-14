"""Compare managed/unmanaged SQL with redis-py on an owned Software fixture.

Endpoints and management credentials come only from the environment. The source
must be an independently typed Arrow IPC file; all complete outputs are checked.
"""
import argparse
import concurrent.futures
import datetime
import hashlib
import importlib.metadata
import json
import multiprocessing
import os
from pathlib import Path
import resource
import statistics
import subprocess
import sys
import tempfile
import time
import uuid
from urllib.parse import parse_qs, urlsplit

import adbc_driver_manager.dbapi as adbc
import pyarrow as pa
import pyarrow.compute as pc
import redis
import requests
import urllib3

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)
MARKER = 'codex:table-mode-benchmark:owner'
PREFIX = 'tablemode:source:'
INDEX = 'tablemode_source_index'
ORDINAL = 'bench_ordinal'
MODES = ('managed', 'unmanaged', 'redis_py')


def source(path):
    return pa.ipc.open_file(str(path)).read_all()


def connect(cfg):
    return adbc.connect(driver=cfg['driver'], autocommit=True,
                        db_kwargs={'uri': cfg['uri'], 'adbc.redis.stream_batch_rows': '4096'})


def client(cfg):
    return redis.Redis.from_url(cfg['uri'], ssl_ca_certs=cfg['ca'],
                                socket_connect_timeout=10, socket_timeout=300)


def canonical(table, schema):
    assert table.column_names == schema.names
    assert all(c.null_count == 0 for c in table.columns)
    assert all(a.type == b.type for a, b in zip(table.schema, schema)), (table.schema, schema)
    return pa.Table.from_arrays(table.columns, schema=schema).sort_by(
        [(n, 'ascending') for n in schema.names]).combine_chunks()


def native_columns(batch):
    out = []
    for f, a in zip(batch.schema, batch.columns):
        if pa.types.is_date32(f.type):
            a = a.cast(pa.int32())
        elif pa.types.is_timestamp(f.type):
            a = a.cast(pa.int64())
        out.append(a.to_pylist())
    return out


def sql_type(field):
    t = field.type
    if pa.types.is_string(t): return 'TEXT'
    if pa.types.is_date32(t): return 'DATE'
    if pa.types.is_timestamp(t): return 'TIMESTAMP'
    return {16: 'SMALLINT', 32: 'INTEGER', 64: 'BIGINT'}[t.bit_width]


def literal(value):
    if isinstance(value, datetime.datetime):
        return "TIMESTAMP '" + value.isoformat(sep=' ', timespec='microseconds') + "'"
    if isinstance(value, datetime.date):
        return "DATE '" + value.isoformat() + "'"
    if isinstance(value, str): return "'" + value.replace("'", "''") + "'"
    if isinstance(value, int): return str(value)
    raise TypeError(type(value))


def write_worker(payload):
    cfg, mode, lo, count, batch_rows = payload
    table = source(cfg['source']).slice(lo, count)
    rows_written = 0
    if mode == 'redis_py':
        with client(cfg) as r:
            for batch in table.to_batches(max_chunksize=batch_rows):
                cols = native_columns(batch)
                with r.pipeline(transaction=False) as pipe:
                    for i in range(batch.num_rows):
                        ordinal = lo + rows_written + i + 1
                        pipe.hset(PREFIX + str(ordinal), mapping={
                            **dict(zip(batch.schema.names, (c[i] for c in cols))), ORDINAL: ordinal})
                    replies = pipe.execute()
                assert all(n == table.num_columns + 1 for n in replies)
                rows_written += batch.num_rows
    else:
        with connect(cfg) as c, c.cursor() as q:
            fields = table.column_names + [ORDINAL]
            if mode == 'unmanaged': fields = ['__key'] + fields
            sql_prefix = 'INSERT INTO bench.' + mode + ' (' + ','.join('"'+n+'"' for n in fields) + ') VALUES '
            for batch in table.to_batches(max_chunksize=batch_rows):
                values = []
                for i, row in enumerate(batch.to_pylist()):
                    ordinal = lo + rows_written + i + 1
                    vals = list(row.values()) + [ordinal]
                    if mode == 'unmanaged': vals.insert(0, PREFIX + str(ordinal))
                    values.append('(' + ','.join(literal(v) for v in vals) + ')')
                q.execute(sql_prefix + ','.join(values))
                assert q.rowcount in (-1, batch.num_rows), q.rowcount
                rows_written += batch.num_rows
    return {'rows': rows_written, 'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss *
            (1 if sys.platform == 'darwin' else 1024)}


def safe_write_worker(payload):
    # ADBC exceptions cannot be unpickled by the current manager. Preserve the
    # original status without replaying SQL or masking it as a broken pool.
    try:
        return write_worker(payload)
    except Exception as error:
        return {'error_type': type(error).__name__,
                'error_message': str(error).replace(payload[0]['uri'], '[private endpoint]'),
                'sqlstate': getattr(error, 'sqlstate', None)}


def index_info(r):
    raw = r.execute_command('FT.INFO', INDEX)
    return raw if isinstance(raw, dict) else dict(zip(raw[::2], raw[1::2]))


def ready(cfg, rows):
    with client(cfg) as r:
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            info = index_info(r)
            if int(info[b'num_docs']) == rows and int(info[b'indexing']) == 0:
                assert int(info.get(b'hash_indexing_failures', 0)) == 0
                return
            time.sleep(.02)
    raise TimeoutError('source index readiness')


def raw_create(cfg, schema):
    args = ['FT.CREATE', INDEX, 'ON', 'HASH', 'PREFIX', 1, PREFIX, 'SCHEMA', ORDINAL, 'NUMERIC', 'SORTABLE']
    for f in schema:
        args += [f.name, 'TAG' if pa.types.is_string(f.type) else 'NUMERIC']
    with client(cfg) as r: r.execute_command(*args)


def seed(cfg, table):
    columns = native_columns(table.to_batches()[0])
    with client(cfg) as r:
        r.hset(PREFIX+'1', mapping={**dict(zip(table.column_names, (a[0] for a in columns))), ORDINAL: 1})
    ready(cfg, 1)


def register(cfg, schema, output):
    args = [cfg['cli'], 'adopt', '-prefix', PREFIX, '-schema', 'bench', '-table', 'unmanaged',
            '-index', INDEX, '-mode', 'unmanaged', '-apply', '-json']
    for field in list(schema) + [pa.field(ORDINAL, pa.int64())]:
        args += ['-type', field.name+'='+sql_type(field), '-encoding', field.name+'=native']
    result = subprocess.run(args, env={**os.environ, 'REDIS_URI': cfg['uri']}, capture_output=True,
                            text=True, timeout=300)
    output.write_text(result.stdout + result.stderr)
    if result.returncode: raise RuntimeError('Unmanaged registration failed; inspect private log')


def setup_managed(cfg, table):
    with connect(cfg) as c, c.cursor() as q:
        q.execute('CREATE SCHEMA IF NOT EXISTS bench')
        numbered = table.append_column(ORDINAL, pa.array(range(1, table.num_rows+1), type=pa.int64()))
        q.adbc_ingest('managed', numbered, mode='create', db_schema_name='bench')


def native_batches(cfg, schema, lo, hi, batch_rows):
    cursor = 0
    with client(cfg) as r:
        try:
            reply = r.execute_command('FT.AGGREGATE', INDEX, f'@{ORDINAL}:[{lo} {hi}]',
                                      'LOAD', 1, '@__key', 'TIMEOUT', 0,
                                      'WITHCURSOR', 'COUNT', 4096, 'DIALECT', 2)
            while True:
                page, cursor = reply
                keys = [row[b'extra_attributes'][b'__key'] for row in page[b'results']]
                for start in range(0, len(keys), 4096):
                    with r.pipeline(transaction=False) as pipe:
                        for key in keys[start:start+4096]: pipe.hmget(key, schema.names)
                        records = pipe.execute()
                    for off in range(0, len(records), batch_rows):
                        chunk = records[off:off+batch_rows]; arrays = []
                        for col, f in enumerate(schema):
                            raw = [row[col] for row in chunk]; assert all(v is not None for v in raw)
                            if pa.types.is_string(f.type): arr = pa.array(raw, type=f.type)
                            else:
                                typ = pa.int32() if pa.types.is_date32(f.type) else pa.int64() if pa.types.is_timestamp(f.type) else f.type
                                arr = pa.array([int(v) for v in raw], type=typ).cast(f.type)
                            arrays.append(arr)
                        yield pa.RecordBatch.from_arrays(arrays, schema=schema)
                if not cursor: break
                reply = r.execute_command('FT.CURSOR', 'READ', INDEX, cursor, 'COUNT', 4096)
        finally:
            if cursor: r.execute_command('FT.CURSOR', 'DEL', INDEX, cursor)


def read_worker(payload):
    cfg, mode, lo, hi, batch_rows, path = payload
    schema = source(cfg['source']).schema
    first = None; rows = 0
    c = q = None
    try:
        if mode == 'redis_py': batches = native_batches(cfg, schema, lo, hi, batch_rows)
        else:
            c = connect(cfg); q = c.cursor()
            q.adbc_statement.set_options(**{'adbc.redis.stream_batch_rows': str(batch_rows)})
            sql = 'SELECT '+','.join('"'+n+'"' for n in schema.names)+' FROM bench.'+mode
            if lo != 1 or hi != 100000: sql += f' WHERE {ORDINAL} BETWEEN {lo} AND {hi}'
            q.execute(sql); batches = q.fetch_record_batch()
        with pa.OSFile(path, 'wb') as sink, pa.ipc.new_stream(sink, schema) as w:
            for batch in batches:
                assert batch.schema.names == schema.names
                assert all(a.type == b.type for a, b in zip(batch.schema, schema))
                batch = pa.RecordBatch.from_arrays(batch.columns, schema=schema)
                assert 0 < batch.num_rows <= batch_rows
                if first is None: first = time.perf_counter()
                w.write_batch(batch); rows += batch.num_rows
    finally:
        if q is not None: q.close()
        if c is not None: c.close()
    assert first is not None
    return {'rows': rows, 'first_batch_at': first, 'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss *
            (1 if sys.platform == 'darwin' else 1024)}


class Benchmark:
    def __init__(self, args):
        self.args = args; self.work = args.work.resolve(); self.work.mkdir(parents=True, exist_ok=True)
        self.cfg = {'uri': os.environ['REDIS_BENCH_URI'], 'ca': os.environ['SSL_CERT_FILE'],
                    'source': str(args.source.resolve()), 'driver': str(args.driver.resolve()), 'cli': str(args.cli.resolve())}
        endpoint=urlsplit(self.cfg['uri'])
        if endpoint.scheme!='rediss' or parse_qs(endpoint.query).get('protocol')!=['3']:
            raise ValueError('Use an explicit TLS Redis URI with protocol=3')
        self.expected = source(args.source)
        assert self.expected.num_rows == 100000 and self.expected.num_columns == 105
        self.sorted = canonical(self.expected, self.expected.schema)
        self.owner = uuid.uuid4().hex
        self.results = {'reads': [], 'writes': [], 'failures': []}
        if (self.work/'results.json').exists(): raise RuntimeError('Use a fresh work directory')
        with client(self.cfg) as r:
            if r.dbsize() or r.execute_command('FT._LIST'): raise RuntimeError('Require an empty dedicated database')
        self.gate()
        with connect(self.cfg) as c:
            info = str(c.adbc_get_info()); assert args.version in info
        with client(self.cfg) as r: r.set(MARKER, self.owner)
        self.results['metadata'] = {'driver_version': args.version,
            'driver_source_commit': subprocess.check_output(
                ['git','rev-parse','HEAD'],cwd=args.driver.resolve().parents[2],text=True).strip(),
            'driver_sha256': hashlib.sha256(args.driver.read_bytes()).hexdigest(),
            'source_sha256': hashlib.sha256(args.source.read_bytes()).hexdigest(),
            'read_rows': 100000, 'write_rows': args.write_rows, 'columns': 105,
            'read_repetitions': args.read_repeats, 'write_repetitions': args.write_repeats,
            'sql_batch_rows': args.sql_batch_rows, 'protocol': 3, 'tls': True,
            'packages': {p: importlib.metadata.version(p) for p in ['pyarrow','adbc-driver-manager','redis','hiredis']}}
        with client(self.cfg) as r:
            assert int(r.client_info()['resp']) == 3
            connection = r.connection_pool.get_connection()
            try: assert 'Hiredis' in type(connection._parser).__name__
            finally: r.connection_pool.release(connection)
        self.save()

    def gate(self):
        endpoint = os.environ['REDIS_BENCH_API_URL'].rstrip('/')+'/bdbs'
        auth = (os.environ['REDIS_BENCH_API_USER'], os.environ['REDIS_BENCH_API_PASSWORD'])
        response = requests.get(endpoint, auth=auth, verify=False, timeout=30); response.raise_for_status()
        db = next(b for b in response.json() if b['name'] == os.environ.get('REDIS_BENCH_DATABASE','adbc-test'))
        response = requests.get(endpoint+'/'+str(db['uid'])+'?fields=status,query_performance_factor,search,sched_policy,conns,eviction_policy,shards_count,memory_size',auth=auth,verify=False,timeout=30);response.raise_for_status();d=response.json()
        assert d['status']=='active' and d['query_performance_factor']=={'active':True,'scaling_factor':4}
        assert d['search']['search-workers']==6 and d['sched_policy']=='mnp' and d['conns']==32
        assert d['eviction_policy']=='noeviction' and d['shards_count']==1
        return {'factor':4,'workers':6,'routing':'mnp','connections':32,'memory_bytes':d['memory_size']}

    def reset(self):
        self.gate()
        with client(self.cfg) as r:
            assert r.get(MARKER)==self.owner.encode()
            r.flushdb()
        # Initialize the managed catalog while empty, before the ownership key
        # or application index is present. Later opens reuse that catalog.
        with connect(self.cfg): pass
        with client(self.cfg) as r: r.set(MARKER,self.owner)

    def save(self):
        path=self.work/'results.json';tmp=path.with_suffix('.tmp');tmp.write_text(json.dumps(self.results,indent=2)+'\n');tmp.replace(path)

    def validate_sql(self, mode, expected):
        with connect(self.cfg) as c,c.cursor() as q:
            q.execute('SELECT '+','.join('"'+n+'"' for n in expected.schema.names)+' FROM bench.'+mode)
            actual=q.fetch_arrow_table()
        assert canonical(actual,expected.schema).equals(canonical(expected,expected.schema),check_metadata=False)

    def validate_raw(self, expected):
        batches=[]
        with client(self.cfg) as r:
            assert sum(1 for _ in r.scan_iter(match=PREFIX+'*'))==expected.num_rows
            for lo in range(1,expected.num_rows+1,1000):
                with r.pipeline(transaction=False) as pipe:
                    for n in range(lo,min(lo+1000,expected.num_rows+1)):pipe.hmget(PREFIX+str(n),expected.column_names+[ORDINAL])
                    rows=pipe.execute()
                assert [int(row[-1]) for row in rows]==list(range(lo,lo+len(rows)))
                arrays=[]
                for j,f in enumerate(expected.schema):
                    raw=[row[j] for row in rows];assert all(v is not None for v in raw)
                    if pa.types.is_string(f.type):arr=pa.array(raw,type=f.type)
                    else:
                        typ=pa.int32() if pa.types.is_date32(f.type) else pa.int64() if pa.types.is_timestamp(f.type) else f.type
                        arr=pa.array([int(v) for v in raw],type=typ).cast(f.type)
                    arrays.append(arr)
                batches.append(pa.RecordBatch.from_arrays(arrays,schema=expected.schema))
        assert canonical(pa.Table.from_batches(batches),expected.schema).equals(canonical(expected,expected.schema),check_metadata=False)

    def validate_partial_managed(self, expected):
        with connect(self.cfg) as c,c.cursor() as q:
            q.execute('SELECT '+','.join('"'+n+'"' for n in expected.column_names+[ORDINAL])+' FROM bench.managed')
            actual=q.fetch_arrow_table()
        ids=actual[ORDINAL].to_pylist()
        assert len(ids)==len(set(ids)) and all(1<=n<=expected.num_rows for n in ids)
        wanted=expected.take(pa.array([n-1 for n in ids],type=pa.int64()))
        assert canonical(actual.select(expected.column_names),expected.schema).equals(
            canonical(wanted,expected.schema),check_metadata=False)
        return actual.num_rows

    def run_reads(self):
        self.reset();setup_managed(self.cfg,self.expected)
        raw_create(self.cfg,self.expected.schema)
        write_worker((self.cfg,'redis_py',0,100000,1000));ready(self.cfg,100000)
        register(self.cfg,self.expected.schema,self.work/'read-registration-private.log')
        self.validate_sql('managed',self.expected);self.validate_sql('unmanaged',self.expected);self.validate_raw(self.expected)
        self.verify_unmanaged_arrow()
        cases=[(m,n,b) for b in [512,4096] for n in [1,4] for m in MODES]
        for rep in range(-1,self.args.read_repeats):
            for mode,readers,batch in (cases if rep%2 else reversed(cases)):
                self.gate()
                with tempfile.TemporaryDirectory(dir=self.work) as tmp:
                    tasks=1 if readers==1 else 8;paths=[str(Path(tmp)/f'{i}.arrow') for i in range(tasks)]
                    payloads=[(self.cfg,mode,i*100000//tasks+1,(i+1)*100000//tasks,batch,paths[i]) for i in range(tasks)]
                    start=time.perf_counter()
                    with concurrent.futures.ProcessPoolExecutor(readers,mp_context=multiprocessing.get_context('spawn')) as pool:
                        workers=list(pool.map(read_worker,payloads))
                    elapsed=time.perf_counter()-start
                    actual=pa.concat_tables([pa.ipc.open_stream(path).read_all() for path in paths])
                    assert canonical(actual,self.expected.schema).equals(self.sorted,check_metadata=False)
                row={'mode':mode,'readers':readers,'batch_rows':batch,'repeat':rep,'warmup':rep<0,'seconds':elapsed,
                     'first_batch_seconds':min(w['first_batch_at'] for w in workers)-start,'rows':100000,'columns':105,
                     'tasks':tasks,'validated':True,'peak_worker_rss_bytes':max(w['peak_rss_bytes'] for w in workers)}
                self.results['reads'].append(row);self.save();print('READ',json.dumps(row),flush=True)
        self.validate_raw(self.expected)

    def verify_unmanaged_arrow(self):
        with connect(self.cfg) as c,c.cursor() as q:
            try:q.adbc_ingest('unmanaged',self.expected.slice(0,1),mode='append',db_schema_name='bench')
            except Exception as e:
                assert 'Arrow ingestion is not supported for unmanaged' in str(e),str(e)
                self.results['unmanaged_arrow_ingestion']='NotImplemented (verified refusal)'
            else:raise AssertionError('Unmanaged Arrow ingestion unexpectedly succeeded')

    def run_writes(self):
        expected=self.expected.slice(0,self.args.write_rows)
        cases=[(m,n) for n in [1,4] for m in MODES]
        for rep in range(-1,self.args.write_repeats):
            for mode,writers in (cases if rep%2 else reversed(cases)):
                self.reset();setup_managed(self.cfg,expected.slice(0,0))
                if mode!='managed':
                    raw_create(self.cfg,expected.schema)
                    if mode=='unmanaged':
                        seed(self.cfg,expected.slice(0,1));register(self.cfg,expected.schema,self.work/'write-registration-private.log')
                        with client(self.cfg) as r:r.delete(PREFIX+'1')
                        ready(self.cfg,0)
                count=self.args.write_rows//writers
                payloads=[(self.cfg,mode,i*count,count,self.args.sql_batch_rows) for i in range(writers)]
                start=time.perf_counter()
                with concurrent.futures.ProcessPoolExecutor(writers,mp_context=multiprocessing.get_context('spawn')) as pool:
                    workers=list(pool.map(safe_write_worker,payloads))
                errors=[w for w in workers if 'error_type' in w]
                if errors:
                    if mode!='managed' or writers==1 or any(e.get('sqlstate')!='40001' for e in errors):
                        raise RuntimeError('Write worker failed: '+json.dumps(errors))
                    partial_rows=self.validate_partial_managed(expected)
                    row={'mode':mode,'writers':writers,'repeat':rep,'warmup':rep<0,
                         'rows':self.args.write_rows,'columns':105,'validated':False,
                         'status':'serialization_conflict','seconds':None,'errors':errors,
                         'partial_rows':partial_rows,'partial_values_validated':True}
                    self.results['writes'].append(row);self.save()
                    print('WRITE FAILURE',json.dumps(row),flush=True)
                    continue
                if mode!='managed':ready(self.cfg,self.args.write_rows)
                elapsed=time.perf_counter()-start
                assert sum(w['rows'] for w in workers)==self.args.write_rows
                if mode!='redis_py':self.validate_sql(mode,expected)
                if mode!='managed':self.validate_raw(expected)
                row={'mode':mode,'writers':writers,'repeat':rep,'warmup':rep<0,'seconds':elapsed,'rows':self.args.write_rows,
                     'columns':105,'sql_batch_rows':self.args.sql_batch_rows,'validated':True,
                     'peak_worker_rss_bytes':max(w['peak_rss_bytes'] for w in workers)}
                self.results['writes'].append(row);self.save();print('WRITE',json.dumps(row),flush=True)


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source',type=Path,required=True);p.add_argument('--driver',type=Path,required=True)
    p.add_argument('--cli',type=Path,required=True);p.add_argument('--work',type=Path,required=True)
    p.add_argument('--version',required=True);p.add_argument('--dedicated-empty-server',action='store_true',required=True)
    p.add_argument('--read-repeats',type=int,default=6);p.add_argument('--write-repeats',type=int,default=3)
    p.add_argument('--write-rows',type=int,default=10000);p.add_argument('--sql-batch-rows',type=int,default=1000)
    p.add_argument('--suite',choices=['reads','writes','all'],default='all')
    args=p.parse_args()
    if not 4<=args.write_rows<=100000 or args.write_rows%4 or min(args.sql_batch_rows,args.read_repeats,args.write_repeats)<=0:p.error('Invalid rows/repetitions/batch size')
    b=Benchmark(args)
    try:
        if args.suite in ['reads','all']:b.run_reads()
        if args.suite in ['writes','all']:b.run_writes()
        b.results['final_qpf']=b.gate()
        b.results['status']='complete_with_write_conflicts' if any(
            not row['validated'] for row in b.results['writes']) else 'complete'
        b.save()
    except Exception as e:
        b.results['status']='failed';b.results['failures'].append({'error_type':type(e).__name__});b.save();raise

if __name__=='__main__':main()
