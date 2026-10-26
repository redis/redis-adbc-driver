"""Paired regular/partition ADBC transfer benchmark for issues #396 and #408.

Run against a dedicated Redis server. --load replaces transferbench.hits.
Requires adbc-driver-manager, pyarrow and redis; --native-references also needs
hiredis. Input is the independently
converted official 100K x 105 ClickBench prefix, never driver-produced values.
"""
import argparse
import concurrent.futures
import hashlib
import importlib.metadata
import json
import multiprocessing
import os
import platform
from pathlib import Path
import resource
import statistics
import sys
import tempfile
import time
import urllib.parse

import adbc_driver_manager.dbapi as adbc
import pyarrow as pa
import pyarrow.compute as pc
import pyarrow.parquet as pq
import redis

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'benchmarks/clickbench'))
from run import arrow_schema, convert_batch


def connect(driver, uri, options=None):
    return adbc.connect(driver=driver, db_kwargs={'uri': uri, **(options or {})}, autocommit=True)


def fixture(args):
    ddl = (args.clickbench / 'postgresql/create.sql').read_text()
    schema = arrow_schema(ddl)
    batches, remaining = [], 100000
    for batch in pq.ParquetFile(args.parquet).iter_batches(batch_size=10000):
        batch = batch.slice(0, remaining)
        batches.append(convert_batch(batch, schema))
        remaining -= batch.num_rows
        if remaining == 0:
            break
    source = pa.Table.from_batches(batches)
    source = source.rename_columns([n.lower() for n in source.column_names])
    assert source.num_rows == 100000 and source.num_columns == 105
    with pa.OSFile(str(args.work / 'source.arrow'), 'wb') as sink, pa.ipc.new_file(sink, source.schema) as writer:
        writer.write_table(source)
    if args.load:
        with connect(args.baseline, args.uri) as conn, conn.cursor() as cur:
            cur.execute('CREATE SCHEMA IF NOT EXISTS transferbench')
            cur.execute('SET search_path TO transferbench')
            cur.execute('DROP TABLE IF EXISTS hits')
            cur.execute(ddl)
            for batch in source.to_batches(max_chunksize=10000):
                cur.adbc_ingest('hits', batch, mode='append', db_schema_name='transferbench')
        control = redis.Redis.from_url(args.uri)
        try:
            metadata = json.loads(control.get('adbc:{meta}:table:transferbench:hits'))
            index = metadata.get('index_name') or 'idx:transferbench:hits'
            deadline = time.monotonic() + 300
            while True:
                raw = control.execute_command('FT.INFO', index)
                info = raw if isinstance(raw, dict) else dict(zip(raw[::2], raw[1::2]))
                if int(info[b'num_docs']) == 100000 and int(info[b'indexing']) == 0:
                    break
                if time.monotonic() > deadline:
                    raise TimeoutError('index readiness')
                time.sleep(.05)
        finally:
            control.close()
    return source.sort_by([(n, 'ascending') for n in source.column_names]).combine_chunks()


def worker(payload):
    driver, uri, sql, descriptor, output, source_path, native_pipeline, native_page, options = payload
    schema = pa.ipc.open_file(source_path).schema
    first = None
    count = 0
    if driver in ['redis_py_known', 'redis_py_discovery']:
        with redis.Redis.from_url(uri, socket_timeout=300) as client:
            lo, hi, prefix, index = descriptor
            with pa.OSFile(output, 'wb') as sink, pa.ipc.new_stream(sink, schema) as writer:
                for rows in native_rows(client, schema.names, lo, hi, prefix, index, driver == 'redis_py_discovery', native_pipeline, native_page):
                    arrays = []
                    for j, field in enumerate(schema):
                        values = [row[j] for row in rows]
                        if any(v is None for v in values):
                            raise AssertionError('Native reference requires the immutable non-null fixture')
                        if pa.types.is_string(field.type):
                            arr = pa.array(values, type=field.type)
                        else:
                            physical = pa.int32() if pa.types.is_date32(field.type) else pa.int64() if pa.types.is_timestamp(field.type) else field.type
                            arr = pa.array([int(v) for v in values], type=physical).cast(field.type, safe=True)
                        arrays.append(arr)
                    if first is None:
                        first = time.perf_counter()
                    writer.write_batch(pa.RecordBatch.from_arrays(arrays, schema=schema))
                    count += len(rows)
        return {'rows': count, 'first_batch_at': first,
                'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == 'darwin' else 1024)}
    with connect(driver, uri, options) as conn, conn.cursor() as cur:
        if descriptor is None:
            cur.execute(sql)
        else:
            cur.adbc_read_partition(descriptor)
        with pa.OSFile(output, 'wb') as sink, pa.ipc.new_stream(sink, schema) as writer:
            for batch in cur.fetch_record_batch():
                if first is None:
                    first = time.perf_counter()
                batch = pa.RecordBatch.from_arrays(
                    [pc.cast(batch.column(i), field.type, safe=True) for i, field in enumerate(schema)], schema=schema)
                writer.write_batch(batch)
                count += batch.num_rows
    return {'rows': count, 'first_batch_at': first,
            'peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * (1 if sys.platform == 'darwin' else 1024)}


def native_rows(client, fields, lo, hi, prefix, index, discover, pipeline_size=1000, page_size=1869):
    """Fixture-only references: exact dense known keys or Search discovery.

    Both use bounded HMGET, an existence field, and identical Arrow IPC output.
    They deliberately omit the driver's SQL/catalog/readiness semantics.
    """
    def fetch(keys):
        for start in range(0, len(keys), pipeline_size):
            with client.pipeline(transaction=False) as pipe:
                for key in keys[start:start + pipeline_size]:
                    pipe.hmget(key, ['__rowid'] + fields)
                rows = pipe.execute()
            if any(row[0] is None for row in rows):
                raise AssertionError('Fixture row disappeared')
            yield [row[1:] for row in rows]
    if not discover:
        for start in range(lo, hi + 1, pipeline_size):
            yield from fetch([prefix + str(i) for i in range(start, min(start + pipeline_size, hi + 1))])
        return
    cursor = 0
    try:
        reply = client.execute_command('FT.AGGREGATE', index, f'@__rowid:[{lo} {hi}]',
                                       'LOAD', 2, '@__key', '@__rowid', 'TIMEOUT', 0,
                                       'WITHCURSOR', 'COUNT', page_size, 'DIALECT', 2)
        while True:
            page, cursor = reply
            if isinstance(page, dict):
                keys = [row[b'extra_attributes'][b'__key'] for row in page[b'results']]
            else:
                keys = [dict(zip(row[::2], row[1::2]))[b'__key'] for row in page[1:]]
            yield from fetch(keys)
            if not cursor:
                break
            reply = client.execute_command('FT.CURSOR', 'READ', index, cursor, 'COUNT', page_size)
    finally:
        if cursor:
            client.execute_command('FT.CURSOR', 'DEL', index, cursor)


def cpu_usage():
    return sum(r.ru_utime + r.ru_stime for r in [resource.getrusage(resource.RUSAGE_SELF), resource.getrusage(resource.RUSAGE_CHILDREN)])


def trial(args, expected, name, profile, repeat):
    native = name.startswith('redis_py_')
    driver = name if native else getattr(args, name)
    options = {}
    if not native:
        for suffix in ['rows', 'bytes']:
            value = getattr(args, name + '_batch_' + suffix, None)
            if value is not None:
                options['adbc.redis.stream_batch_' + suffix] = str(value)
    sql = 'SELECT ' + ','.join('"' + n + '"' for n in expected.column_names) + ' FROM transferbench.hits'
    control = redis.Redis.from_url(args.uri)
    before = control.info()
    commands_before = control.info('commandstats')
    cpu_before = cpu_usage()
    with tempfile.TemporaryDirectory(prefix='trial-', dir=args.work) as tmp:
        started = time.perf_counter()
        if native:
            descriptors = [(i * 12500 + 1, (i + 1) * 12500, args.native_prefix, args.native_index) for i in range(8)] if profile == 'partition' else [(1, 100000, args.native_prefix, args.native_index)]
            workers = 4 if profile == 'partition' else 1
        elif profile == 'partition':
            with connect(driver, args.uri, options) as conn, conn.cursor() as cur:
                cur.adbc_statement.set_options(**{'adbc.redis.partition_count': 8})
                descriptors, _ = cur.adbc_execute_partitions(sql)
            workers = 4
        else:
            descriptors, workers = [None], 1
        paths = [str(Path(tmp) / f'{i}.arrow') for i in range(len(descriptors))]
        with concurrent.futures.ProcessPoolExecutor(max_workers=workers, mp_context=multiprocessing.get_context('spawn')) as pool:
            metrics = list(pool.map(worker, [(driver, args.uri, sql, d, p, str(args.work / 'source.arrow'), args.native_pipeline, args.native_page, options) for d, p in zip(descriptors, paths)]))
        seconds = time.perf_counter() - started
        client_cpu = cpu_usage() - cpu_before
        after = control.info()
        commands_after = control.info('commandstats')
        # Validation, including every field and row multiplicity, is outside timing.
        actual = pa.concat_tables([pa.ipc.open_stream(path).read_all() for path in paths])
        actual = actual.sort_by([(n, 'ascending') for n in expected.column_names]).combine_chunks()
        if not actual.equals(expected, check_metadata=False):
            raise AssertionError(f'Full value mismatch: {name}/{profile}/{repeat}')
        record = {'build': name, 'api': profile, 'repeat': repeat, 'warmup': repeat < 0,
                  'seconds': seconds, 'client_cpu_seconds': client_cpu,
                  'server_cpu_seconds': sum(after[k] - before[k] for k in ['used_cpu_user', 'used_cpu_sys']),
                  'server_net_output_bytes': after['total_net_output_bytes'] - before['total_net_output_bytes'],
                  'server_net_input_bytes': after['total_net_input_bytes'] - before['total_net_input_bytes'],
                  'first_batch_seconds': min(m['first_batch_at'] for m in metrics) - started,
                  'max_worker_peak_rss_bytes': max(m['peak_rss_bytes'] for m in metrics),
                  'workers': workers, 'partitions': len(descriptors), 'rows': actual.num_rows,
                  'columns': actual.num_columns, 'logical_arrow_bytes': actual.nbytes,
                  'server_commands': {k.removeprefix('cmdstat_'): v['calls'] - commands_before.get(k, {}).get('calls', 0)
                                      for k, v in commands_after.items()
                                      if v['calls'] != commands_before.get(k, {}).get('calls', 0)},
                  'exact_full_values_validated': True, 'stream_options': options}
    control.close()
    print(json.dumps(record), flush=True)
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['baseline', 'candidate', 'uri', 'parquet', 'clickbench', 'work']:
        parser.add_argument('--' + name, required=True, type=Path if name in ['parquet', 'clickbench', 'work'] else str)
    for build in ['baseline', 'candidate']:
        parser.add_argument('--' + build + '-batch-rows', type=int)
        parser.add_argument('--' + build + '-batch-bytes', type=int)
    parser.add_argument('--columns', nargs='+', help='Optional narrow output projection from the same 105-column fixture')
    parser.add_argument('--load', action='store_true')
    parser.add_argument('--repeats', type=int, default=5)
    parser.add_argument('--native-references', action='store_true', help='Include official redis-py known-key and Search-discovery Arrow references')
    parser.add_argument('--native-pipeline', type=int, default=1000, help='Native HMGET pipeline rows (default 1000)')
    parser.add_argument('--native-page', type=int, default=1869, help='Native Search cursor page rows (default 1869)')
    parser.add_argument('--protocol', type=int, choices=[2, 3], default=3, help='Explicit protocol for both ADBC and native clients (default RESP3)')
    args = parser.parse_args()
    for build in ['baseline', 'candidate']:
        rows, size = getattr(args, build + '_batch_rows'), getattr(args, build + '_batch_bytes')
        if rows is not None and rows < 1 or size is not None and size < 0:
            parser.error('batch rows must be positive and bytes nonnegative')
    if args.repeats < 1:
        parser.error('--repeats must be positive')
    if not 1 <= args.native_pipeline <= 10000 or not 1 <= args.native_page <= 10000:
        parser.error('native pipeline and page sizes must be within 1..10000')
    uri = urllib.parse.urlsplit(args.uri)
    query = urllib.parse.parse_qs(uri.query)
    if 'protocol' in query and query['protocol'] != [str(args.protocol)]:
        parser.error('URI protocol must match --protocol')
    query['protocol'] = [str(args.protocol)]
    args.uri = urllib.parse.urlunsplit(uri._replace(query=urllib.parse.urlencode(query, doseq=True)))
    args.work.mkdir(parents=True, exist_ok=True)
    expected = fixture(args)
    with redis.Redis.from_url(args.uri) as client:
        actual_protocol = int(client.client_info()['resp'])
        if actual_protocol != args.protocol:
            raise AssertionError('Control connection did not negotiate the requested protocol')
    if args.native_references:
        with redis.Redis.from_url(args.uri, decode_responses=True) as client:
            meta = json.loads(client.get('adbc:{meta}:table:transferbench:hits'))
            args.native_prefix = meta.get('key_prefix') or 'transferbench:hits:'
            args.native_index = meta.get('index_name') or 'idx:transferbench:hits'
    if args.columns:
        expected = expected.select(args.columns).sort_by([(n, 'ascending') for n in args.columns]).combine_chunks()
        with pa.OSFile(str(args.work / 'source.arrow'), 'wb') as sink, pa.ipc.new_file(sink, expected.schema) as writer:
            writer.write_table(expected)
    report = {'status': 'running', 'trials': [], 'binaries': {
        name: {'sha256': hashlib.sha256(Path(getattr(args, name)).read_bytes()).hexdigest()} for name in ['baseline', 'candidate']},
        'parquet_sha256': hashlib.sha256(args.parquet.read_bytes()).hexdigest(),
        'environment': {'platform': platform.platform(), 'logical_cpus': os.cpu_count(),
                        'protocol': args.protocol,
                        'observed_control_protocol': actual_protocol,
                        'packages': {n: importlib.metadata.version(n) for n in ['pyarrow', 'redis', 'adbc-driver-manager'] + (['hiredis'] if args.native_references else [])},
                        'native_references': args.native_references,
                        'native_pipeline': args.native_pipeline, 'native_page': args.native_page,
                        'profile_note': 'Native regular/partition labels mean 1 reader / 4 readers with 8 ranges; native paths do not use ADBC APIs.'}}
    output = args.work / 'results.json'
    pairs = [('baseline', 'regular'), ('candidate', 'regular'), ('baseline', 'partition'), ('candidate', 'partition')]
    if args.native_references:
        pairs += [(name, profile) for name in ['redis_py_known', 'redis_py_discovery'] for profile in ['regular', 'partition']]
    for repeat in range(-1, args.repeats):
        order = pairs if repeat % 2 else list(reversed(pairs))
        for name, profile in order:
            try:
                report['trials'].append(trial(args, expected, name, profile, repeat))
            except Exception as exc:
                report['status'] = 'failed'
                report['failure'] = repr(exc)
                output.write_text(json.dumps(report, indent=2) + '\n')
                raise
            output.write_text(json.dumps(report, indent=2) + '\n')
    report['summary'] = []
    for name, profile in pairs:
        records = [r for r in report['trials'] if not r['warmup'] and (r['build'], r['api']) == (name, profile)]
        summary = {'build': name, 'api': profile, 'trials': len(records)}
        for key in ['seconds', 'client_cpu_seconds', 'server_cpu_seconds', 'server_net_output_bytes', 'server_net_input_bytes', 'first_batch_seconds', 'max_worker_peak_rss_bytes']:
            summary[key] = statistics.median(r[key] for r in records)
        summary['min_seconds'] = min(r['seconds'] for r in records)
        summary['max_seconds'] = max(r['seconds'] for r in records)
        report['summary'].append(summary)
    report['status'] = 'complete'
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report['summary'], indent=2), flush=True)


if __name__ == '__main__':
    main()
