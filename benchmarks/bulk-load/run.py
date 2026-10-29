"""Compare Arrow bulk append with redis-py pipelined HSET on a dedicated server.

The server must initially be empty. Every trial FLUSHDBs this task-owned database.
Both paths start with the same typed Arrow file, empty managed table/index, and
end with identical HASH fields and an index containing every row. Native writes
are fixture-only: they omit generic SQL constraints and concurrent-DDL handling.
"""
import argparse
import concurrent.futures
from datetime import datetime, timezone
import hashlib
import importlib.metadata
import json
import multiprocessing
import os
from pathlib import Path
import platform
import resource
import statistics
import subprocess
import sys
import time
import uuid

import pyarrow as pa
import pyarrow.compute as pc
import redis

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'wide-transfer'))
import compare as wide

SCHEMA = 'bulkloadbench'
TABLE = 'hits'
MARKER = 'codex:bulk-load:owner'
BATCH_ROWS = 10000
PIPELINE_ROWS = 1000  # Matches the driver's writeRows pipelineChunk.


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def native_columns(batch):
    columns = []
    for field, arr in zip(batch.schema, batch.columns):
        if arr.null_count:
            raise ValueError('This fixed-schema fixture requires non-null fields')
        if pa.types.is_date32(field.type):
            arr = arr.cast(pa.int32())
        elif pa.types.is_timestamp(field.type):
            arr = pc.cast(arr, pa.timestamp('us')).cast(pa.int64())
        elif not (pa.types.is_integer(field.type) or pa.types.is_string(field.type)):
            raise TypeError(f'Unsupported fixture type: {field}')
        columns.append(arr.to_pylist())
    return columns


def writer(payload):
    method, driver, uri, source_path, lo, count, prefix, fields = payload
    started = time.perf_counter()
    cpu_start = time.process_time()
    source = pa.ipc.open_file(source_path).read_all().slice(lo, count)
    source_ready = time.perf_counter()
    batches = source.to_batches(max_chunksize=BATCH_ROWS)
    first_write = None
    written = 0
    conn = cursor = None
    try:
        if method == 'adbc':
            conn = wide.connect(driver, uri)
            cursor = conn.cursor()
        else:
            conn = redis.Redis.from_url(uri, socket_connect_timeout=10, socket_timeout=120)
            assert int(conn.client_info()['resp']) == 3
            connection = conn.connection_pool.get_connection()
            try:
                assert 'Hiredis' in type(connection._parser).__name__
            finally:
                conn.connection_pool.release(connection)
        connected = time.perf_counter()
        for batch in batches:
            if method == 'adbc':
                n = cursor.adbc_ingest(TABLE, batch, mode='append', db_schema_name=SCHEMA)
                assert n == batch.num_rows, n
            else:
                cols = native_columns(batch)
                last = conn.incrby(f'adbc:{{meta}}:seq:{SCHEMA}:{TABLE}', batch.num_rows)
                first = last - batch.num_rows + 1
                for offset in range(0, batch.num_rows, PIPELINE_ROWS):
                    with conn.pipeline(transaction=False) as pipe:
                        stop = min(offset + PIPELINE_ROWS, batch.num_rows)
                        for r in range(offset, stop):
                            mapping = dict(zip(fields, (col[r] for col in cols)))
                            mapping['__rowid'] = first + r
                            pipe.hset(prefix + str(first + r), mapping=mapping)
                        replies = pipe.execute()
                    assert all(n == len(fields) + 1 for n in replies), 'Unexpected HSET field count'
            if first_write is None:
                first_write = time.perf_counter()
            written += batch.num_rows
        write_complete = time.perf_counter()
    finally:
        if cursor is not None:
            cursor.close()
        if conn is not None:
            conn.close()
    return {'rows': written, 'started_at': started, 'source_ready_at': source_ready,
            'connected_at': connected, 'first_batch_written_at': first_write,
            'write_complete_at': write_complete, 'finished_at': time.perf_counter(),
            'client_cpu_seconds': time.process_time() - cpu_start,
            'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == 'darwin' else 1024)}


def index_info(control, index):
    raw = control.execute_command('FT.INFO', index)
    return raw if isinstance(raw, dict) else dict(zip(raw[::2], raw[1::2]))


def ready(control, index, rows):
    deadline = time.monotonic() + 120
    while True:
        info = index_info(control, index)
        if int(info[b'num_docs']) == rows and int(info[b'indexing']) == 0:
            if int(info.get(b'hash_indexing_failures', 0)):
                raise AssertionError('Search indexing failures')
            return info
        if time.monotonic() > deadline:
            raise TimeoutError('Index did not become ready')
        time.sleep(.01)


def validate(control, prefix, fields, expected):
    batches = []
    # Read every exact stored value through the same independent validator.
    for start in range(1, expected.num_rows + 1, PIPELINE_ROWS):
        stop = min(start + PIPELINE_ROWS, expected.num_rows + 1)
        with control.pipeline(transaction=False) as pipe:
            for row_id in range(start, stop):
                pipe.hmget(prefix + str(row_id), ['__rowid'] + fields)
            values = pipe.execute()
        assert [int(v[0]) for v in values] == list(range(start, stop))
        arrays = []
        for col, field in enumerate(expected.schema, 1):
            raw = [row[col] for row in values]
            assert all(v is not None for v in raw)
            if pa.types.is_string(field.type):
                arr = pa.array(raw, type=field.type)
            else:
                physical = pa.int32() if pa.types.is_date32(field.type) else pa.int64() if pa.types.is_timestamp(field.type) else field.type
                arr = pa.array([int(v) for v in raw], type=physical).cast(field.type, safe=True)
            arrays.append(arr)
        batches.append(pa.RecordBatch.from_arrays(arrays, schema=expected.schema))
    actual = pa.Table.from_batches(batches).sort_by([(n, 'ascending') for n in expected.column_names]).combine_chunks()
    assert actual.equals(expected, check_metadata=False), 'Full-value/multiplicity mismatch'
    return True


def trial(args, expected, control, method, writers, repeat):
    if control.get(MARKER) != args.owner.encode():
        raise RuntimeError('Dedicated database ownership marker changed')
    control.flushdb(asynchronous=False)
    control.set(MARKER, args.owner)
    assert control.execute_command('FT._LIST') == []
    before = control.info()
    commands_before = control.info('commandstats')
    started = time.perf_counter()
    with wide.connect(args.driver, args.uri) as conn, conn.cursor() as cursor:
        cursor.execute(f'CREATE SCHEMA IF NOT EXISTS {SCHEMA}')
        cursor.adbc_ingest(TABLE, expected.slice(0, 0), mode='create', db_schema_name=SCHEMA)
    metadata = json.loads(control.get(f'adbc:{{meta}}:table:{SCHEMA}:{TABLE}'))
    prefix = metadata.get('key_prefix') or f'{SCHEMA}:{TABLE}:'
    index = metadata.get('index_name') or f'idx:{SCHEMA}:{TABLE}'
    fields = [c.get('field') or c['name'] for c in metadata['columns']]
    assert [c['name'] for c in metadata['columns']] == expected.column_names
    empty_info = ready(control, index, 0)
    # Attributes exclude the per-trial prefix/index identity.
    attrs = empty_info[b'attributes']
    setup_end = time.perf_counter()
    count = expected.num_rows // writers
    payloads = [(method, args.driver, args.uri, str(args.work/'source.arrow'), i * count,
                 count, prefix, fields) for i in range(writers)]
    with concurrent.futures.ProcessPoolExecutor(max_workers=writers, mp_context=multiprocessing.get_context('spawn')) as pool:
        worker_results = list(pool.map(writer, payloads))
    workers_done = time.perf_counter()
    final_info = ready(control, index, expected.num_rows)
    ended = time.perf_counter()
    after, commands_after = control.info(), control.info('commandstats')
    assert sum(w['rows'] for w in worker_results) == expected.num_rows
    assert int(control.get(f'adbc:{{meta}}:seq:{SCHEMA}:{TABLE}')) == expected.num_rows
    assert final_info[b'attributes'] == attrs
    exact = validate(control, prefix, fields, expected)
    record = {'method': method, 'writers': writers, 'repeat': repeat, 'warmup': repeat < 0,
              'rows': expected.num_rows, 'columns': expected.num_columns,
              'total_index_ready_seconds': ended - started,
              'common_setup_seconds': setup_end - started,
              'append_and_worker_lifecycle_seconds': workers_done - setup_end,
              'write_complete_seconds': max(w['write_complete_at'] for w in worker_results) - started,
              'post_worker_index_wait_seconds': ended - workers_done,
              'rows_per_second': expected.num_rows / (ended - started),
              'worker_cpu_seconds': sum(w['client_cpu_seconds'] for w in worker_results),
              'server_cpu_seconds': sum(after[k] - before[k] for k in ['used_cpu_user', 'used_cpu_sys']),
              'server_input_bytes': after['total_net_input_bytes'] - before['total_net_input_bytes'],
              'server_output_bytes': after['total_net_output_bytes'] - before['total_net_output_bytes'],
              'server_used_memory_bytes': after['used_memory'],
              'max_worker_peak_rss_bytes': max(w['peak_rss_bytes'] for w in worker_results),
              'index_attributes': attrs, 'index_docs': int(final_info[b'num_docs']),
              'exact_full_values_validated': exact,
              'server_commands': {k.removeprefix('cmdstat_'): v['calls'] - commands_before.get(k, {}).get('calls', 0)
                                  for k, v in commands_after.items() if v['calls'] != commands_before.get(k, {}).get('calls', 0)},
              'workers': worker_results}
    print(json.dumps({k: record[k] for k in ['method','writers','repeat','total_index_ready_seconds','write_complete_seconds','exact_full_values_validated']}), flush=True)
    return record


def json_safe(obj):
    if isinstance(obj, bytes):
        return obj.decode()
    if isinstance(obj, dict):
        return {json_safe(k): json_safe(v) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [json_safe(v) for v in obj]
    return obj


def save_report(path, report):
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(json_safe(report), indent=2) + '\n')
    temporary.replace(path)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['driver', 'uri', 'parquet', 'clickbench', 'work']:
        parser.add_argument('--'+name, required=True, type=Path if name in ['parquet','clickbench','work'] else str)
    parser.add_argument('--repeats', type=int, default=6)
    parser.add_argument('--dedicated-empty-server', action='store_true', required=True)
    args = parser.parse_args()
    if args.repeats < 0 or '?protocol=3' not in args.uri:
        parser.error('Require nonnegative repeats and explicit ?protocol=3')
    args.work.mkdir(parents=True, exist_ok=True)
    if (args.work/'results.json').exists():
        parser.error('Use a fresh output directory')
    args.load = False
    expected = wide.fixture(args)
    args.owner = str(uuid.uuid4())
    with redis.Redis.from_url(args.uri, socket_connect_timeout=10, socket_timeout=120) as control:
        if control.dbsize() != 0 or control.execute_command('FT._LIST'):
            raise RuntimeError('Refusing a nonempty server; use a fresh dedicated container')
        control.set(MARKER, args.owner)
        report = {'status':'running','started_at':datetime.now(timezone.utc).isoformat(),
                  'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=wide.ROOT,text=True).strip(),
                  'driver_sha256':digest(args.driver),'runner_sha256':digest(__file__),
                  'fixture_helper_sha256':digest(wide.__file__),'parquet_sha256':digest(args.parquet),
                  'source_arrow_sha256':digest(args.work/'source.arrow'),
                  'clickbench_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=args.clickbench,text=True).strip(),
                  'platform':platform.platform(),'python':sys.version,'logical_cpus':os.cpu_count(),
                  'packages':{n:importlib.metadata.version(n) for n in ['redis','hiredis','pyarrow','adbc-driver-manager']},
                  'redis_version':control.info('server')['redis_version'],
                  'modules':control.module_list(),'protocol':3,'batch_rows':BATCH_ROWS,'pipeline_rows':PIPELINE_ROWS,
                  'rows':expected.num_rows,'columns':expected.num_columns,'logical_arrow_bytes':expected.nbytes,
                  'methodology':'Common typed Arrow IPC input. Timer includes identical ADBC empty-table/index setup for both clients, fresh process startup, source IPC read, connections, Arrow-to-native conversion, ID allocation, HSET writes, worker teardown, and index readiness. Fixture preparation, cleanup, and full independent verification excluded. Native HSET skips generic SQL constraints, metadata fencing and DDL concurrency semantics.',
                  'trials':[]}
        path=args.work/'results.json'
        cases=[('adbc',1),('redis_py',1),('adbc',4),('redis_py',4)]
        try:
            for repeat in range(-1,args.repeats):
                order=cases if repeat % 2 else list(reversed(cases))
                for method,writers in order:
                    report['trials'].append(trial(args,expected,control,method,writers,repeat))
                    save_report(path, report)
            report['summary']=[]
            for method,writers in cases:
                rows=[r for r in report['trials'] if not r['warmup'] and (r['method'],r['writers'])==(method,writers)]
                if not rows: continue
                summary={'method':method,'writers':writers,'measured_runs':len(rows)}
                for k in ['total_index_ready_seconds','write_complete_seconds','common_setup_seconds','append_and_worker_lifecycle_seconds','worker_cpu_seconds','server_cpu_seconds','post_worker_index_wait_seconds','max_worker_peak_rss_bytes']:
                    summary[k]={'median':statistics.median(r[k] for r in rows),'min':min(r[k] for r in rows),'max':max(r[k] for r in rows)}
                summary['rows_per_second']=expected.num_rows/summary['total_index_ready_seconds']['median']
                report['summary'].append(summary)
            report['status']='complete'
        except BaseException as exc:
            report.update(status='failed',failure=repr(exc))
            raise
        finally:
            report['finished_at']=datetime.now(timezone.utc).isoformat()
            save_report(path, report)

if __name__ == '__main__':
    main()
