"""Matched, fully validated Arrow transfer experiments for issue #442.

Use dedicated disposable databases. --load replaces only transferbench.hits.
Reads an independently typed source.arrow produced by compare.py. PostgreSQL
and ClickHouse receive the same values plus an unselected ordinal for disjoint
reader ranges. No source conversion, loading or validation is timed.
"""
import argparse
import concurrent.futures
import contextlib
import hashlib
import http.client
import importlib.metadata
import json
import multiprocessing
import os
from pathlib import Path
import platform
import resource
import statistics
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request

import adbc_driver_manager.dbapi as adbc
import adbc_driver_postgresql.dbapi as postgres
import pyarrow as pa
import pyarrow.compute as pc
import redis

from traffic_meter import TrafficMeter


def quoted(name):
    return '"' + name.replace('"', '""') + '"'


def canonical(table):
    return table.sort_by([(n, 'ascending') for n in table.column_names]).combine_chunks()


def validate(paths, expected):
    actual = pa.concat_tables([pa.ipc.open_stream(str(p)).read_all() for p in paths])
    if not canonical(actual).equals(expected, check_metadata=False):
        raise AssertionError('Full value / row multiplicity mismatch')


def ch_request(url, sql, data=None):
    # Body carries either SQL or Arrow IPC. No HTTP content compression.
    params = {'query': sql} if data is not None else {}
    endpoint = url.rstrip('/') + '/?' + urllib.parse.urlencode(params)
    return urllib.request.urlopen(urllib.request.Request(
        endpoint, data=sql.encode() if data is None else data), timeout=300)


def connection(engine, cfg):
    if engine.startswith('redis'):
        return adbc.connect(driver=cfg['drivers'][engine],
                            db_kwargs={'uri': cfg['redis_uri'],
                                       'adbc.redis.stream_batch_rows': str(cfg['batch_rows'])},
                            autocommit=True)
    if engine == 'postgres':
        return postgres.connect(cfg['postgres_uri'], autocommit=True)
    url = urllib.parse.urlsplit(cfg['clickhouse_url'])
    if url.scheme != 'http':
        raise ValueError('This local plaintext benchmark requires an http ClickHouse endpoint')
    return contextlib.closing(http.client.HTTPConnection(url.hostname, url.port, timeout=300))


def load_references(source, cfg):
    types = {pa.int16(): ('SMALLINT', 'Int16'), pa.int32(): ('INTEGER', 'Int32'),
             pa.int64(): ('BIGINT', 'Int64'), pa.string(): ('TEXT', 'String'),
             pa.date32(): ('DATE', 'Date32'),
             pa.timestamp('us'): ('TIMESTAMP', "DateTime64(6, 'UTC')")}
    # Fail before changing either table for unsupported fixtures.
    columns = [(quoted(f.name), types[f.type]) for f in source.schema]
    numbered = source.append_column('__transfer_rowid', pa.array(range(1, source.num_rows + 1), type=pa.int64()))
    with connection('postgres', cfg) as conn, conn.cursor() as cur:
        cur.adbc_statement.set_options(**{'adbc.postgresql.use_copy': False})
        cur.execute('CREATE SCHEMA IF NOT EXISTS transferbench')
        cur.execute('DROP TABLE IF EXISTS transferbench.hits')
        cur.execute('CREATE TABLE transferbench.hits (' + ','.join(n + ' ' + t[0] + ' NOT NULL' for n, t in columns)
                    + ', __transfer_rowid BIGINT NOT NULL)')
        for batch in numbered.to_batches(max_chunksize=10000):
            cur.adbc_ingest('hits', batch, mode='append', db_schema_name='transferbench')
        cur.execute('ANALYZE transferbench.hits')
    for sql in ['CREATE DATABASE IF NOT EXISTS transferbench', 'DROP TABLE IF EXISTS transferbench.hits',
                'CREATE TABLE transferbench.hits (' + ','.join(n + ' ' + t[1] for n, t in columns)
                + ', __transfer_rowid Int64) ENGINE=MergeTree ORDER BY tuple()']:
        with ch_request(cfg['clickhouse_url'], sql) as response:
            response.read()
    for batch in numbered.to_batches(max_chunksize=10000):
        sink = pa.BufferOutputStream()
        with pa.ipc.new_stream(sink, numbered.schema) as writer:
            writer.write_batch(batch)
        with ch_request(cfg['clickhouse_url'], 'INSERT INTO transferbench.hits FORMAT ArrowStream',
                        sink.getvalue().to_pybytes()) as response:
            response.read()


def query(engine, names, bounds, cfg):
    sql = 'SELECT ' + ','.join(map(quoted, names)) + ' FROM transferbench.hits'
    if bounds is not None:
        field = '__rowid' if engine.startswith('redis') else '__transfer_rowid'
        sql += f' WHERE {field} >= {bounds[0]} AND {field} <= {bounds[1]}'
    if engine == 'clickhouse':
        sql += (f" SETTINGS max_threads=4, max_block_size={cfg['batch_rows']}, "
                "output_format_arrow_compression_method='none', "
                "output_format_arrow_string_as_string=1 FORMAT ArrowStream")
    return sql


def fetch(engine, cfg, schema, bounds, output, conn=None):
    """All engines normalize and write the same IPC stream inside timing."""
    first = None
    first_rows = None
    rows = batches = 0
    reconnects = 0
    sizes = []
    with contextlib.ExitStack() as stack:
        if engine == 'clickhouse':
            if conn is None:
                conn = stack.enter_context(connection(engine, cfg))
            sql = query(engine, schema.names, bounds, cfg).encode()
            # Other engines' trials can exceed the HTTP keep-alive timeout.
            # Retry this read-only fixture SELECT only before any response is
            # delivered. Reconnection stays inside timing and is reported.
            for attempt in range(2):
                try:
                    conn.request('POST', '/', body=sql)
                    response = conn.getresponse()
                    break
                except (http.client.RemoteDisconnected, BrokenPipeError, ConnectionResetError):
                    conn.close()
                    if attempt:
                        raise
                    reconnects += 1
            response = stack.enter_context(response)
            if response.status != 200:
                raise RuntimeError(response.read().decode())
            reader = stack.enter_context(pa.ipc.open_stream(response))
        else:
            if conn is None:
                conn = stack.enter_context(connection(engine, cfg))
            cur = stack.enter_context(conn.cursor())
            if engine == 'postgres':
                cur.adbc_statement.set_options(**{'adbc.postgresql.use_copy': True,
                                                  'adbc.postgresql.batch_size_hint_bytes': cfg['pg_batch_bytes']})
            if engine.startswith('redis') and isinstance(bounds, bytes):
                cur.adbc_read_partition(bounds)
            else:
                cur.execute(query(engine, schema.names, bounds, cfg))
            reader = stack.enter_context(cur.fetch_record_batch())
        sink = stack.enter_context(pa.OSFile(str(output), 'wb'))
        writer = stack.enter_context(pa.ipc.new_stream(sink, schema))
        for batch in reader:
            if first is None:
                first, first_rows = time.perf_counter(), batch.num_rows
            if batch.schema.names != schema.names:
                raise AssertionError(f'Unexpected columns: {batch.schema.names}')
            batch = pa.RecordBatch.from_arrays([pc.cast(batch.column(i), f.type, safe=True)
                                                for i, f in enumerate(schema)], schema=schema)
            writer.write_batch(batch)
            rows += batch.num_rows
            batches += 1
            sizes.append(batch.num_rows)
        if engine == 'clickhouse':
            response.read()  # Finish HTTP chunk framing for keep-alive reuse.
    return {'rows': rows, 'batches': batches, 'first_batch_at': first,
            'http_reconnects': reconnects,
            'first_batch_rows': first_rows, 'min_batch_rows': min(sizes, default=0),
            'max_batch_rows': max(sizes, default=0),
            'peak_worker_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
            * (1 if platform.system() == 'Darwin' else 1024)}


def spawned_fetch(payload):
    engine, cfg, schema, bounds, output = payload
    return fetch(engine, cfg, schema, bounds, output)


def cpu():
    return sum(r.ru_utime + r.ru_stime for r in
               [resource.getrusage(resource.RUSAGE_SELF), resource.getrusage(resource.RUSAGE_CHILDREN)])


def server_cpu(container):
    if container is None:
        return None
    raw = subprocess.check_output(['docker', 'exec', container, 'cat', '/sys/fs/cgroup/cpu.stat'], text=True)
    return int(dict(line.split() for line in raw.splitlines())['usage_usec']) / 1e6


def redis_counters(cfg):
    with redis.Redis.from_url(cfg['redis_uri']) as client:
        info = client.info()
        commands = client.info('commandstats')
    return {**{k: info[k] for k in ['total_net_input_bytes', 'total_net_output_bytes']},
            **{'command_' + k.removeprefix('cmdstat_'): v['calls'] for k, v in commands.items()}}


def trial(engine, mode, workers, repeat, cfg, expected, work, connections, pool, meter=None):
    bounds = [None] if workers == 1 and not cfg.get("single_reader_layout") else [(i * cfg['rows'] // workers + 1, (i + 1) * cfg['rows'] // workers)
                                         for i in range(workers)]
    container = cfg['containers'].get('redis' if engine.startswith('redis') else engine)
    with tempfile.TemporaryDirectory(dir=work) as tmp:
        paths = [Path(tmp) / f'{i}.arrow' for i in range(workers)]
        before_server = server_cpu(container)
        before_redis = redis_counters(cfg) if engine.startswith('redis') else None
        before_cpu = cpu()
        before_traffic = meter.snapshot() if meter else None
        started = time.perf_counter()
        if engine.startswith('redis') and (workers > 1 or cfg.get('single_reader_layout')) and cfg.get('redis_layout') == 'partitions':
            with contextlib.ExitStack() as planning:
                conn = connections[0] if mode == 'persistent' else planning.enter_context(connection(engine, cfg))
                cur = planning.enter_context(conn.cursor())
                cur.adbc_statement.set_options(**{'adbc.redis.partition_count': workers})
                bounds, _ = cur.adbc_execute_partitions(query(engine, expected.column_names, None, cfg))
                if len(bounds) != workers:
                    raise AssertionError('Fixture must produce one descriptor per reader')
        if mode == 'spawned':
            with concurrent.futures.ProcessPoolExecutor(workers, mp_context=multiprocessing.get_context('spawn')) as process_pool:
                results = list(process_pool.map(spawned_fetch,
                               [(engine, cfg, expected.schema, b, str(p)) for b, p in zip(bounds, paths)]))
        else:
            futures = [pool.submit(fetch, engine, cfg, expected.schema, b, p, c)
                       for b, p, c in zip(bounds, paths, connections)]
            concurrent.futures.wait(futures)  # Join readers before connections unwind on failure.
            results = [f.result() for f in futures]
        elapsed = time.perf_counter() - started
        after_traffic = meter.snapshot() if meter else None
        client_cpu = cpu() - before_cpu
        after_server = server_cpu(container)
        after_redis = redis_counters(cfg) if before_redis else None
        validate(paths, expected)
    return {'engine': engine, 'mode': mode, 'workers': workers, 'repeat': repeat, 'warmup': repeat < 0,
            'seconds': elapsed, 'client_cpu_seconds': client_cpu,
            'server_cgroup_cpu_seconds': after_server - before_server if container else None,
            'first_batch_seconds': min(r['first_batch_at'] for r in results if r['first_batch_at']) - started,
            'reader_metrics': [{k: v for k, v in r.items() if k != 'first_batch_at'} for r in results],
            'redis_counters': {k: after_redis.get(k, 0) - before_redis.get(k, 0)
                               for k in after_redis.keys() | before_redis.keys()} if before_redis else None,
            'exact_full_values_validated': True, 'rows': expected.num_rows, 'columns': expected.num_columns,
            'logical_arrow_bytes': expected.nbytes,
            'tcp_payload_bytes': {'request': after_traffic[0] - before_traffic[0],
                                  'reply': after_traffic[1] - before_traffic[1]} if meter else None}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source', type=Path, required=True)
    p.add_argument('--redis-driver', required=True)
    p.add_argument('--redis-candidate')
    p.add_argument('--redis-uri', required=True)
    p.add_argument('--postgres-uri', required=True)
    p.add_argument('--clickhouse-url', required=True)
    p.add_argument('--work', type=Path, required=True)
    p.add_argument('--load', action='store_true')
    p.add_argument('--repeats', type=int, default=5)
    p.add_argument('--batch-rows', type=int, default=4096)
    p.add_argument('--pg-batch-bytes', type=int, default=4 * 1024 * 1024)
    p.add_argument('--workers', nargs='+', type=int, choices=[1, 4], default=[1, 4])
    p.add_argument('--modes', nargs='+', choices=['persistent', 'spawned'], default=['persistent', 'spawned'])
    p.add_argument('--engines', nargs='+', choices=['redis', 'redis_candidate', 'postgres', 'clickhouse'],
                   default=['redis', 'postgres', 'clickhouse'])
    p.add_argument('--columns', nargs='+')
    p.add_argument('--meter', action='store_true', help='Separate proxied byte diagnostic; do not mix with direct timings')
    p.add_argument('--single-reader-layout', action='store_true', help='Apply the selected Redis layout to one reader too (one descriptor or an explicit full row-ID range)')
    p.add_argument('--redis-layout', choices=['ranges', 'partitions'], default='partitions',
                   help='Four-reader Redis API; explicit SQL ranges can select a different execution path')
    for engine in ['redis', 'postgres', 'clickhouse']:
        p.add_argument('--' + engine + '-container')
    args = p.parse_args()
    if args.repeats < 1 or args.batch_rows < 1 or args.pg_batch_bytes < 1:
        p.error('repeats and batch settings must be positive')
    if 'redis_candidate' in args.engines and not args.redis_candidate:
        p.error('--redis-candidate required for redis_candidate')
    args.work.mkdir(parents=True, exist_ok=False)
    source = pa.ipc.open_file(str(args.source)).read_all()
    if source.num_rows != 100000 or source.num_columns != 105:
        p.error('source must be the independently typed 100K × 105 fixture')
    cfg = {'redis_uri': args.redis_uri, 'postgres_uri': args.postgres_uri,
           'clickhouse_url': args.clickhouse_url, 'batch_rows': args.batch_rows,
           'pg_batch_bytes': args.pg_batch_bytes, 'rows': source.num_rows,
           'redis_layout': args.redis_layout, 'single_reader_layout': args.single_reader_layout,
           'drivers': {'redis': args.redis_driver, 'redis_candidate': args.redis_candidate},
           'containers': {e: getattr(args, e + '_container') for e in ['redis', 'postgres', 'clickhouse']}}
    if args.load:
        load_references(source, cfg)
    expected = canonical(source.select(args.columns) if args.columns else source)
    report = {'status': 'running', 'trials': [], 'settings': {
        'batch_rows': args.batch_rows, 'pg_batch_hint_bytes': args.pg_batch_bytes,
        'clickhouse_arrow_compression': 'none', 'clickhouse_http_compression': False,
        'traffic_meter': args.meter,
        'ordering': 'unordered multiset equivalence; Redis may impose internal row-ID order',
        'single_reader_layout': args.single_reader_layout,
        'four_readers': {'redis': args.redis_layout,
                         'references': 'four explicit disjoint ordinal SQL ranges'},
        'runner_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'source_sha256': hashlib.sha256(args.source.read_bytes()).hexdigest(),
        'driver_sha256': {k: hashlib.sha256(Path(v).read_bytes()).hexdigest() for k, v in cfg['drivers'].items() if v}},
        'environment': {'platform': platform.platform(), 'python': platform.python_version(),
                        'logical_cpus': os.cpu_count(),
                        'packages': {n: importlib.metadata.version(n) for n in
                                     ['pyarrow', 'adbc-driver-manager', 'adbc-driver-postgresql', 'redis']}}}
    output = args.work / 'results.json'
    pairs = [(e, m, w) for e in args.engines for m in args.modes for w in args.workers]
    with contextlib.ExitStack() as stack:
        meters = {}
        if args.meter:
            for engine, key in [('redis', 'redis_uri'), ('postgres', 'postgres_uri'), ('clickhouse', 'clickhouse_url')]:
                url = urllib.parse.urlsplit(cfg[key])
                if url.hostname != '127.0.0.1' or url.port is None:
                    p.error('--meter requires explicit 127.0.0.1 endpoint ports')
                meter = stack.enter_context(TrafficMeter(url.port))
                meters[engine] = meter
                # Preserve credentials and parameters without logging them.
                auth = url.netloc.rsplit('@', 1)[0] + '@' if '@' in url.netloc else ''
                cfg[key] = urllib.parse.urlunsplit(url._replace(netloc=auth + f'127.0.0.1:{meter.port}'))
        pool = stack.enter_context(concurrent.futures.ThreadPoolExecutor(max(args.workers)))
        # Each persistent reader owns a distinct connection, opened outside timing.
        conns = {(e, w): [stack.enter_context(connection(e, cfg)) for _ in range(w)]
                 for e in args.engines for w in args.workers} if 'persistent' in args.modes else {}
        try:
            for repeat in range(-1, args.repeats):
                for engine, mode, workers in (pairs if repeat % 2 else reversed(pairs)):
                    result = trial(engine, mode, workers, repeat, cfg, expected, args.work,
                                   conns.get((engine, workers)), pool,
                                   meters.get('redis' if engine.startswith('redis') else engine))
                    report['trials'].append(result)
                    output.write_text(json.dumps(report, indent=2) + '\n')
                    print(json.dumps(result), flush=True)
        except Exception as exc:
            report.update(status='failed', failure=repr(exc))
            output.write_text(json.dumps(report, indent=2) + '\n')
            raise
    report['summary'] = []
    for engine, mode, workers in pairs:
        trials = [t for t in report['trials'] if (t['engine'], t['mode'], t['workers']) == (engine, mode, workers)
                  and not t['warmup']]
        summary = {'engine': engine, 'mode': mode, 'workers': workers, 'trials': len(trials)}
        for key in ['seconds', 'first_batch_seconds', 'client_cpu_seconds', 'server_cgroup_cpu_seconds']:
            values = [t[key] for t in trials if t[key] is not None]
            summary[key] = statistics.median(values) if values else None
        summary.update(min_seconds=min(t['seconds'] for t in trials), max_seconds=max(t['seconds'] for t in trials))
        report['summary'].append(summary)
    report['status'] = 'complete'
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report['summary'], indent=2), flush=True)


if __name__ == '__main__':
    main()
