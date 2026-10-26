"""Matched incremental Arrow reads: ADBC, public partitions, explicit ranges, redis-py.

Uses the existing independent 100K x 105 ClickBench fixture and native references.
Run only against a dedicated server: --load replaces transferbench.hits.
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
import sys
import tempfile
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'wide-transfer'))
import compare as wide
import pyarrow as pa
import pyarrow.compute as pc
import redis


def native_batches(client, schema, descriptor, method, batch_rows):
    lo, hi, prefix, index = descriptor
    for rows in wide.native_rows(client, schema.names, lo, hi, prefix, index,
                                 method == 'redis_py_discovery', 4096, 4096):
        # Match output batches without enlarging the common retrieval bound.
        for start in range(0, len(rows), batch_rows):
            chunk = rows[start:start + batch_rows]
            arrays = []
            for j, field in enumerate(schema):
                values = [r[j] for r in chunk]
                if any(v is None for v in values):
                    raise AssertionError('Native fixture requires non-null fields')
                if pa.types.is_string(field.type):
                    arr = pa.array(values, type=field.type)
                else:
                    physical = pa.int32() if pa.types.is_date32(field.type) else pa.int64() if pa.types.is_timestamp(field.type) else field.type
                    arr = pa.array([int(v) for v in values], type=physical).cast(field.type, safe=True)
                arrays.append(arr)
            yield pa.RecordBatch.from_arrays(arrays, schema=schema)


def worker(payload):
    method, driver, uri, sql, descriptor, output, source, batch_rows = payload
    schema = pa.ipc.open_file(source).schema
    started = time.perf_counter()
    sizes, logical_bytes, first_ready, first_written = [], 0, None, None
    options = {'adbc.redis.stream_batch_rows': str(batch_rows)}
    conn = cursor = None
    native = method.startswith('redis_py_')
    try:
        if native:
            conn = redis.Redis.from_url(uri, socket_connect_timeout=10, socket_timeout=120)
            conn.ping()
            batches = native_batches(conn, schema, descriptor, method, batch_rows)
        else:
            conn = wide.connect(driver, uri, options)
            cursor = conn.cursor()
            if method == 'adbc_partitions':
                cursor.adbc_read_partition(descriptor)
            else:
                cursor.execute(sql)
            batches = cursor.fetch_record_batch()
        with pa.OSFile(output, 'wb') as sink, pa.ipc.new_stream(sink, schema) as writer:
            for batch in batches:
                batch = pa.RecordBatch.from_arrays([
                    pc.cast(batch.column(i), field.type, safe=True)
                    for i, field in enumerate(schema)], schema=schema)
                if first_ready is None:
                    first_ready = time.perf_counter()
                writer.write_batch(batch)
                if first_written is None:
                    sink.flush()
                    first_written = time.perf_counter()
                sizes.append(batch.num_rows)
                logical_bytes += batch.nbytes
        if not sizes or sizes[0] != batch_rows or any(n > batch_rows for n in sizes):
            raise AssertionError(f'Unexpected output batch sizes: {method}: {sizes}')
        return {'rows': sum(sizes), 'batch_sizes': sizes,
                'logical_arrow_bytes': logical_bytes,
                'first_ready_at': first_ready, 'first_written_at': first_written,
                'task_start_at': started,
                'first_ready_from_task_seconds': first_ready - started,
                'first_batch_rows': sizes[0],
                'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == 'darwin' else 1024)}
    finally:
        if cursor is not None:
            cursor.close()
        if conn is not None:
            conn.close()


def trial(args, expected, method, readers, batch_rows, repeat):
    sql = 'SELECT ' + ','.join('"' + n + '"' for n in expected.column_names) + ' FROM transferbench.hits'
    count = 8 if readers == 4 else 1
    ranges = [(i * (100000 // count) + 1, (i + 1) * (100000 // count), args.prefix, args.index) for i in range(count)]
    with redis.Redis.from_url(args.uri) as control, tempfile.TemporaryDirectory(dir=args.work, prefix='trial-') as tmp:
        before, cmds_before = control.info(), control.info('commandstats')
        cpu_before = wide.cpu_usage()
        started = time.perf_counter()
        plan_started = time.perf_counter()
        if method == 'adbc_partitions':
            with wide.connect(args.driver, args.uri, {'adbc.redis.stream_batch_rows': str(batch_rows)}) as conn, conn.cursor() as cur:
                cur.adbc_statement.set_options(**{'adbc.redis.partition_count': 8})
                descriptors, _ = cur.adbc_execute_partitions(sql)
            if len(descriptors) != 8:
                raise AssertionError(f'Expected eight descriptors, got {len(descriptors)}')
        elif method.startswith('redis_py_'):
            descriptors = ranges
        else:
            descriptors = [None] * count
        planning_seconds = time.perf_counter() - plan_started
        paths = [str(Path(tmp) / f'{i}.arrow') for i in range(count)]
        queries = [sql] * count
        if method == 'adbc_ranges':
            queries = [sql + f' WHERE __rowid >= {lo} AND __rowid <= {hi}' for lo, hi, _, _ in ranges]
        payloads = [(method, args.driver, args.uri, query, descriptor, path, str(args.work / 'source.arrow'), batch_rows)
                    for query, descriptor, path in zip(queries, descriptors, paths)]
        with concurrent.futures.ProcessPoolExecutor(max_workers=readers, mp_context=multiprocessing.get_context('spawn')) as pool:
            workers = list(pool.map(worker, payloads))
        seconds = time.perf_counter() - started
        client_cpu = wide.cpu_usage() - cpu_before
        after, cmds_after = control.info(), control.info('commandstats')
        # Full independent validation is outside all timing and INFO counters.
        actual = pa.concat_tables([pa.ipc.open_stream(path).read_all() for path in paths])
        actual = actual.sort_by([(n, 'ascending') for n in expected.column_names]).combine_chunks()
        if not actual.equals(expected, check_metadata=False):
            raise AssertionError(f'Full values or multiplicities differ: {method}')
        first_worker = min(workers, key=lambda w: w['first_ready_at'])
        record = {'method': method, 'readers': readers, 'tasks': count,
                  'batch_rows': batch_rows, 'repeat': repeat, 'warmup': repeat < 0,
                  'seconds': seconds,
                  'first_batch_seconds': first_worker['first_ready_at'] - started,
                  'first_ipc_flushed_seconds': min(w['first_written_at'] for w in workers) - started,
                  'first_worker_setup_and_read_seconds': first_worker['first_ready_from_task_seconds'],
                  'planning_seconds': planning_seconds,
                  'client_cpu_seconds': client_cpu,
                  'server_cpu_seconds': sum(after[k] - before[k] for k in ['used_cpu_user', 'used_cpu_sys']),
                  'server_output_bytes': after['total_net_output_bytes'] - before['total_net_output_bytes'],
                  'server_input_bytes': after['total_net_input_bytes'] - before['total_net_input_bytes'],
                  'max_worker_peak_rss_bytes': max(w['peak_rss_bytes'] for w in workers),
                  'rows': actual.num_rows, 'columns': actual.num_columns,
                  'first_batch_rows': first_worker['first_batch_rows'],
                  'batch_sizes_by_task': [w['batch_sizes'] for w in workers],
                  'exact_full_values_validated': True,
                  'server_commands': {k.removeprefix('cmdstat_'): v['calls'] - cmds_before.get(k, {}).get('calls', 0)
                                      for k, v in cmds_after.items() if v['calls'] != cmds_before.get(k, {}).get('calls', 0)}}
        print(json.dumps({k: record[k] for k in ['method', 'readers', 'batch_rows', 'repeat', 'first_batch_seconds', 'seconds', 'planning_seconds', 'exact_full_values_validated']}), flush=True)
        return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['driver', 'uri', 'parquet', 'clickbench', 'work']:
        parser.add_argument('--' + name, required=True, type=Path if name in ['parquet', 'clickbench', 'work'] else str)
    parser.add_argument('--load', action='store_true')
    parser.add_argument('--repeats', type=int, default=6)
    parser.add_argument('--batch-rows', type=int, nargs='+', default=[512, 4096], choices=[512, 4096])
    args = parser.parse_args()
    if args.repeats < 0:
        parser.error('repeats must be nonnegative')
    if '?protocol=3' not in args.uri:
        parser.error('Use an explicit localhost benchmark URI ending ?protocol=3')
    args.work.mkdir(parents=True, exist_ok=True)
    if (args.work / 'results.json').exists():
        parser.error('Use a fresh output directory')
    args.baseline = args.driver
    expected = wide.fixture(args)
    with redis.Redis.from_url(args.uri, decode_responses=True) as control:
        metadata = json.loads(control.get('adbc:{meta}:table:transferbench:hits'))
        args.prefix = metadata.get('key_prefix') or 'transferbench:hits:'
        args.index = metadata.get('index_name') or 'idx:transferbench:hits'
        if int(control.client_info()['resp']) != 3:
            raise AssertionError('RESP3 was not negotiated')
        server = {'redis_version': control.info('server')['redis_version'], 'modules': control.module_list()}
    cases = []
    for rows in args.batch_rows:
        cases.extend([(m, 1, rows) for m in ['adbc', 'redis_py_known', 'redis_py_discovery']])
        cases.extend([(m, 4, rows) for m in ['adbc_partitions', 'adbc_ranges', 'redis_py_known', 'redis_py_discovery']])
    report = {'status': 'running', 'trials': [], 'server': server,
              'started_at': datetime.now(timezone.utc).isoformat(),
              'driver_sha256': hashlib.sha256(Path(args.driver).read_bytes()).hexdigest(),
              'fixture_sha256': hashlib.sha256(args.parquet.read_bytes()).hexdigest(),
              'runner_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              'reused_runner_sha256': hashlib.sha256(Path(wide.__file__).read_bytes()).hexdigest(),
              'platform': platform.platform(), 'python': sys.version, 'logical_cpus': os.cpu_count(),
              'packages': {n: importlib.metadata.version(n) for n in ['redis', 'hiredis', 'pyarrow', 'adbc-driver-manager']},
              'protocol': 3, 'native_retrieval_rows': 4096, 'batch_rows': args.batch_rows,
              'first_batch_definition': 'Same typed Arrow RecordBatch after normalization, before IPC write; also records first IPC flush. Fresh spawned workers, connections and partition planning included.',
              'full_definition': 'All data consumed and written as Arrow IPC, workers/connections closed. Fixture preparation and exact validation excluded.',
              'limitations': ['Immutable non-null dense-ID fixture; native references omit generic SQL/catalog/readiness.', 'Same first-batch row count, but traversal order and byte content may differ.', 'Localhost shared workstation, not WAN/TLS or a production ranking.']}
    output = args.work / 'results.json'
    for repeat in range(-1, args.repeats):
        for method, readers, rows in (cases if repeat % 2 else list(reversed(cases))):
            try:
                report['trials'].append(trial(args, expected, method, readers, rows, repeat))
            except Exception as exc:
                report.update(status='failed', failure=repr(exc))
                output.write_text(json.dumps(report, indent=2) + '\n')
                raise
            output.write_text(json.dumps(report, indent=2) + '\n')
    report['summary'] = []
    for method, readers, rows in cases:
        records = [r for r in report['trials'] if not r['warmup'] and (r['method'], r['readers'], r['batch_rows']) == (method, readers, rows)]
        if not records:
            continue
        summary = {'method': method, 'readers': readers, 'batch_rows': rows, 'trials': len(records)}
        for key in ['seconds', 'first_batch_seconds', 'first_ipc_flushed_seconds', 'first_worker_setup_and_read_seconds', 'planning_seconds', 'client_cpu_seconds', 'server_cpu_seconds', 'server_output_bytes', 'server_input_bytes', 'max_worker_peak_rss_bytes']:
            summary[key] = statistics.median(r[key] for r in records)
        for key in ['seconds', 'first_batch_seconds']:
            summary['min_' + key], summary['max_' + key] = min(r[key] for r in records), max(r[key] for r in records)
        report['summary'].append(summary)
    report['status'] = 'complete'
    report['completed_at'] = datetime.now(timezone.utc).isoformat()
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report['summary'], indent=2), flush=True)


if __name__ == '__main__':
    main()
