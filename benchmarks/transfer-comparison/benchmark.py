"""Matched local 100K x 105 full-result Arrow transfer comparison.

Uses only dedicated benchmark databases. Loads are outside timing. Every trial
includes fresh spawned processes/connections, query execution, complete Arrow
fetch, normalization and IPC writes. Full-value validation is outside timing.
"""
import argparse
import base64
import concurrent.futures
import hashlib
import importlib.metadata
import json
import multiprocessing
import os
from pathlib import Path
import platform
import pickle
import resource
import statistics
import sys
import tempfile
import time
import urllib.parse
import urllib.request

import adbc_driver_manager.dbapi as adbc
import adbc_driver_postgresql.dbapi as pg
import duckdb
import psutil
import pyarrow as pa
import pyarrow.compute as pc
import pyarrow.parquet as pq
import redis

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
sys.path.insert(0, str(ROOT / 'benchmarks/clickbench'))
from run import arrow_schema, convert_batch

PG_URI = 'postgresql://postgres:local-benchmark-only@127.0.0.1:17932/transferbench'
REDIS_URI = 'redis://127.0.0.1:17979/0'
CH_URL = 'http://127.0.0.1:17923/'
CH_AUTH = 'Basic ' + base64.b64encode(b'default:local-benchmark-only').decode()
ENGINES = ['redis', 'postgresql', 'clickhouse', 'duckdb']


def ch(sql, data=None):
    url = CH_URL + '?' + urllib.parse.urlencode({'query': sql, 'max_threads': 4})
    request = urllib.request.Request(url, data=data if data is not None else b'',
                                     headers={'Authorization': CH_AUTH})
    try:
        return urllib.request.urlopen(request, timeout=300)
    except urllib.error.HTTPError as exc:
        raise RuntimeError(exc.read().decode()[:3000]) from exc


def connect_redis(driver):
    return adbc.connect(driver=driver, db_kwargs={'uri': REDIS_URI}, autocommit=True)


def load(args):
    ddl = (args.upstream / 'postgresql/create.sql').read_text()
    schema = arrow_schema(ddl)
    batches = [convert_batch(b, schema) for b in pq.ParquetFile(args.parquet).iter_batches(batch_size=10000)]
    source = pa.Table.from_batches(batches).slice(0, 100000)
    assert source.num_rows == 100000 and source.num_columns == 105
    source = source.rename_columns([n.lower() for n in source.column_names])
    with pa.OSFile(str(args.work / 'source.arrow'), 'wb') as sink, pa.ipc.new_file(sink, source.schema) as writer:
        writer.write_table(source)
    state = {'rows': source.num_rows, 'columns': source.num_columns, 'arrow_payload_bytes': source.nbytes,
             'source_prefix_sha256': hashlib.sha256(args.parquet.read_bytes()).hexdigest(),
             'driver_sha256': hashlib.sha256(Path(args.driver).read_bytes()).hexdigest(),
             'platform': platform.platform(), 'host_logical_cpus': os.cpu_count(),
             'host_memory_bytes': psutil.virtual_memory().total,
             'packages': {n: importlib.metadata.version(n) for n in ['pyarrow','duckdb','redis','psutil','adbc-driver-manager','adbc-driver-postgresql']},
             'load_seconds': {}, 'versions': {}}
    for engine in args.engines:
        if engine in ['redis_py', 'redis_py_raw']:
            continue  # Shares the same Redis HASH rows loaded for the ADBC path.
        start = time.perf_counter()
        if engine == 'redis':
            with connect_redis(args.driver) as conn, conn.cursor() as cur:
                cur.execute('CREATE SCHEMA transferbench')
                cur.execute('SET search_path TO transferbench')
                cur.execute(ddl)
                for batch in source.to_batches(max_chunksize=10000):
                    cur.adbc_ingest('hits', batch, mode='append', db_schema_name='transferbench')
            control = redis.Redis.from_url(REDIS_URI, decode_responses=True)
            deadline = time.monotonic() + 300
            while True:
                raw = control.execute_command('FT.INFO', 'idx:transferbench:hits')
                info = dict(zip(raw[::2],raw[1::2])) if isinstance(raw,list) else raw
                if int(info['num_docs']) == 100000 and int(info['indexing']) == 0:
                    break
                if time.monotonic() > deadline:
                    raise TimeoutError('Redis index readiness')
                time.sleep(.05)
            state['versions'][engine] = {'redis': control.info('server')['redis_version'], 'modules': control.execute_command('MODULE','LIST')}
        elif engine == 'postgresql':
            with pg.connect(PG_URI, autocommit=True) as conn, conn.cursor() as cur:
                cur.execute(ddl)
                cur.execute('ALTER TABLE hits ADD COLUMN __bench_rowid BIGSERIAL')
                for batch in source.to_batches(max_chunksize=10000):
                    cur.adbc_ingest('hits', batch, mode='append')
                cur.execute('CREATE INDEX ON hits (__bench_rowid)')
                cur.execute('ANALYZE hits')
                cur.execute('SELECT version()')
                state['versions'][engine] = cur.fetchone()[0]
        elif engine == 'clickhouse':
            types = {str(pa.int64()):'Int64',str(pa.int32()):'Int32',str(pa.int16()):'Int16',str(pa.string()):'String',str(pa.timestamp('us')):"DateTime64(6, 'UTC')",str(pa.date32()):'Date32'}
            columns = ','.join('`'+f.name+'` '+types[str(f.type)] for f in source.schema)
            ch('CREATE TABLE hits ('+columns+', __bench_rowid UInt64) ENGINE=MergeTree ORDER BY __bench_rowid').close()
            data = source.append_column('__bench_rowid', pa.array(range(1,100001),type=pa.uint64()))
            sink = pa.BufferOutputStream()
            with pa.ipc.new_stream(sink,data.schema) as writer:
                writer.write_table(data)
            ch('INSERT INTO hits FORMAT ArrowStream',sink.getvalue().to_pybytes()).close()
            state['versions'][engine] = ch('SELECT version()').read().decode().strip()
        else:
            conn = duckdb.connect(str(args.work/'duckdb.db'))
            conn.execute('SET threads=4')
            conn.execute("SET memory_limit='2GB'")
            conn.register('source',source)
            conn.execute('CREATE TABLE hits AS SELECT *, row_number() OVER () AS __bench_rowid FROM source')
            conn.execute('CHECKPOINT')
            state['versions'][engine] = conn.execute('SELECT version()').fetchone()[0]
            conn.close()
        state['load_seconds'][engine] = time.perf_counter()-start
        (args.work/'manifest.json').write_text(json.dumps(state,indent=2,default=str)+'\n')
        print('loaded',engine,round(state['load_seconds'][engine],3),flush=True)
    return source


def native_redis_raw_batches(client, fields, start, stop):
    """Fetch dense known keys as native redis-py byte values, without Arrow."""
    for offset in range(start, stop + 1, 1000):
        with client.pipeline(transaction=False) as pipe:
            for rowid in range(offset, min(offset + 1000, stop + 1)):
                pipe.hmget('transferbench:hits:' + str(rowid), fields)
            yield pipe.execute()


def native_rows_to_batch(rows, schema):
    arrays = []
    for i, field in enumerate(schema):
        values = [row[i] for row in rows]
        if any(v is None for v in values):
            raise AssertionError('Missing value in this non-null dense known-key fixture')
        if pa.types.is_string(field.type):
            array = pa.array(values, type=field.type)
        elif pa.types.is_date32(field.type):
            array = pa.array([int(v) for v in values], type=pa.int32()).cast(field.type)
        elif pa.types.is_timestamp(field.type):
            array = pa.array([int(v) for v in values], type=pa.int64()).cast(field.type)
        else:
            array = pa.array([int(v) for v in values], type=field.type)
        arrays.append(array)
    return pa.RecordBatch.from_arrays(arrays, schema=schema)


def native_redis_batches(client, schema, start, stop):
    """Dense known-key fixture only. Pipeline HMGET and decode every Arrow value."""
    for rows in native_redis_raw_batches(client, schema.names, start, stop):
        yield native_rows_to_batch(rows, schema)


def task(payload):
    engine, driver, sql, descriptor, output, work = payload
    expected = pa.ipc.open_file(str(Path(work)/'source.arrow')).schema
    started = time.perf_counter()
    conn = cursor = response = None
    try:
        if engine in ['redis_py', 'redis_py_raw']:
            conn = redis.Redis.from_url(REDIS_URI, protocol=2, decode_responses=False,
                                        socket_connect_timeout=5, socket_timeout=300)
            start, stop = descriptor
            if engine == 'redis_py_raw':
                count, first_batch_at = 0, None
                with open(output, 'wb') as sink:
                    for rows in native_redis_raw_batches(conn, expected.names, start, stop):
                        if first_batch_at is None:
                            first_batch_at = time.perf_counter()
                        pickle.dump(rows, sink, protocol=5)
                        count += len(rows)
                return {'rows':count, 'arrow_payload_bytes':None, 'first_batch_at':first_batch_at,
                        'worker_seconds':time.perf_counter()-started,
                        'worker_lifetime_peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*(1 if sys.platform=='darwin' else 1024),
                        'ipc_bytes':Path(output).stat().st_size,
                        'output_format':'Python Redis byte values, pickle protocol 5; no Arrow conversion/output in timed path'}
            reader = native_redis_batches(conn, expected, start, stop)
        elif engine == 'redis':
            conn = connect_redis(driver)
            cursor = conn.cursor()
            cursor.adbc_read_partition(descriptor) if descriptor is not None else cursor.execute(sql)
            reader = cursor.fetch_record_batch()
        elif engine == 'postgresql':
            conn = pg.connect(PG_URI,autocommit=True)
            cursor = conn.cursor()
            cursor.execute('SET max_parallel_workers_per_gather=4')
            cursor.execute(sql)
            reader = cursor.fetch_record_batch()
        elif engine == 'clickhouse':
            response = ch(sql + ' FORMAT ArrowStream')
            reader = pa.ipc.open_stream(response)
        else:
            conn = duckdb.connect(str(Path(work)/'duckdb.db'),read_only=True)
            conn.execute('SET threads=4')
            conn.execute("SET memory_limit='2GB'")
            reader = conn.execute(sql).fetch_record_batch(65536)
        count = nbytes = 0
        first_batch_at = None
        with pa.OSFile(output,'wb') as sink, pa.ipc.new_stream(sink,expected) as writer:
            for batch in reader:
                if first_batch_at is None:
                    first_batch_at = time.perf_counter()
                # The same physical Arrow schema and IPC serialization for all paths.
                normalized = pa.RecordBatch.from_arrays([pc.cast(batch.column(i),f.type,safe=True) for i,f in enumerate(expected)],schema=expected)
                writer.write_batch(normalized)
                count += normalized.num_rows
                nbytes += normalized.nbytes
        return {'rows':count,'arrow_payload_bytes':nbytes,'first_batch_at':first_batch_at,
                'worker_seconds':time.perf_counter()-started,
                'worker_lifetime_peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*(1 if sys.platform=='darwin' else 1024),
                'ipc_bytes':Path(output).stat().st_size}
    finally:
        if cursor is not None: cursor.close()
        if response is not None: response.close()
        if conn is not None: conn.close()


def trial(args,engine,profile,repeat,expected):
    names = expected.column_names
    cols = ','.join('"'+n+'"' for n in names) if engine != 'clickhouse' else ','.join('`'+n+'`' for n in names)
    table = 'transferbench.hits' if engine=='redis' else 'hits'
    sql = 'SELECT '+cols+' FROM '+table
    with tempfile.TemporaryDirectory(prefix='transfer-',dir=args.work) as temp:
        started = time.perf_counter()
        if profile=='four_readers':
            if engine in ['redis_py', 'redis_py_raw']:
                descriptors = [(i*12500+1, (i+1)*12500) for i in range(8)]
                statements = ['known-key HMGET']*8
            elif engine=='redis':
                with connect_redis(args.driver) as conn, conn.cursor() as cur:
                    cur.adbc_statement.set_options(**{'adbc.redis.partition_count':8})
                    descriptors,_ = cur.adbc_execute_partitions(sql)
                statements = [sql]*len(descriptors)
            else:
                descriptors = [None]*8
                statements = [sql+f' WHERE __bench_rowid BETWEEN {i*12500+1} AND {(i+1)*12500}' for i in range(8)]
            workers = 4
        else:
            descriptors, statements, workers = ([(1,100000)] if engine in ['redis_py','redis_py_raw'] else [None]), [sql], 1
        paths = [str(Path(temp)/f'{i}.arrow') for i in range(len(descriptors))]
        with concurrent.futures.ProcessPoolExecutor(max_workers=workers,mp_context=multiprocessing.get_context('spawn')) as pool:
            metrics = list(pool.map(task,[(engine,args.driver,s,d,path,str(args.work)) for s,d,path in zip(statements,descriptors,paths)]))
        elapsed = time.perf_counter()-started
        if engine == 'redis_py_raw':
            # Decode only for exact validation, after the timer has stopped.
            tables = []
            for path in paths:
                batches = []
                with open(path, 'rb') as source:
                    while True:
                        try:
                            batches.append(native_rows_to_batch(pickle.load(source),expected.schema))
                        except EOFError:
                            break
                tables.append(pa.Table.from_batches(batches))
        else:
            tables = [pa.ipc.open_stream(path).read_all() for path in paths]
        actual = pa.concat_tables(tables)
        payload_bytes = actual.nbytes
        # Unordered scan: compare all 105 values and multiplicities after canonical sorting.
        actual = actual.sort_by([(n,'ascending') for n in names]).combine_chunks()
        valid = actual.equals(expected,check_metadata=False)
        if not valid:
            with pa.OSFile(str(args.work/f'failure-{engine}-{profile}.arrow'),'wb') as sink, pa.ipc.new_file(sink,actual.schema) as writer:
                writer.write_table(actual)
            raise AssertionError(f'Full value validation failed for {engine}/{profile}: {actual.num_rows} rows')
        record = {'engine':engine,'profile':profile,'repeat':repeat,'warmup':repeat<0,
                  'seconds':elapsed,'rows':actual.num_rows,'columns':actual.num_columns,
                  'arrow_payload_bytes':payload_bytes if engine!='redis_py_raw' else None,
                  'logical_dataset_arrow_bytes':payload_bytes,'rows_per_second':actual.num_rows/elapsed,
                  'arrow_mib_per_second':payload_bytes/1048576/elapsed if engine!='redis_py_raw' else None,
                  'output_format':'Python Redis byte values, pickle protocol 5' if engine=='redis_py_raw' else 'Arrow IPC stream',
                  'time_to_first_batch_seconds':min(m['first_batch_at'] for m in metrics if m['first_batch_at'])-started,
                  'workers':workers,'partitions':len(descriptors),'exact_full_values_validated':True,
                  'metrics':metrics}
        print('trial',engine,profile,repeat,round(elapsed,4),'VALID',flush=True)
        return record


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--driver',required=True)
    ap.add_argument('--parquet',type=Path,required=True)
    ap.add_argument('--upstream',type=Path,required=True)
    ap.add_argument('--work',type=Path,required=True)
    ap.add_argument('--phase',choices=['load','trials','all'],default='all')
    ap.add_argument('--engines',nargs='+',choices=ENGINES+['redis_py','redis_py_raw'],default=ENGINES)
    args = ap.parse_args()
    args.work.mkdir(parents=True,exist_ok=True)
    if args.phase in ['load','all']:
        source = load(args)
    else:
        source = pa.ipc.open_file(str(args.work/'source.arrow')).read_all()
    if args.phase=='load': return
    expected = source.sort_by([(n,'ascending') for n in source.column_names]).combine_chunks()
    report = {'methodology':__doc__,'trials':[],'status':'running'}
    results = args.work/'results.json'
    pairs = [(e,p) for p in ['single_reader','four_readers'] for e in args.engines]
    for repeat in [-1,0,1,2]:
        offset = 0 if repeat<0 else (repeat*3)%len(pairs)
        order = pairs[offset:]+pairs[:offset]
        for engine,profile in order:
            try:
                report['trials'].append(trial(args,engine,profile,repeat,expected))
                results.write_text(json.dumps(report,indent=2)+'\n')
            except Exception as exc:
                report['status']='failed'
                report['failure']={'engine':engine,'profile':profile,'repeat':repeat,'error':repr(exc)}
                results.write_text(json.dumps(report,indent=2)+'\n')
                raise
    report['summary']=[]
    for engine,profile in pairs:
        records=[r for r in report['trials'] if r['engine']==engine and r['profile']==profile and not r['warmup']]
        median=statistics.median(r['seconds'] for r in records)
        report['summary'].append({'engine':engine,'profile':profile,'median_seconds':median,
                                 'rows_per_second':100000/median,'arrow_mib_per_second':source.nbytes/1048576/median if engine!='redis_py_raw' else None,
                                 'min_seconds':min(r['seconds'] for r in records),'max_seconds':max(r['seconds'] for r in records),
                                 'trials':len(records)})
    report['status']='complete'
    results.write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report['summary'],indent=2),flush=True)


if __name__=='__main__': main()
