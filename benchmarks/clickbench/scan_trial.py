"""Time one fully drained scan through a statement or public ADBC partition API."""
import concurrent.futures
import hashlib
import json
import multiprocessing
from pathlib import Path
import resource
import sys
import time

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa

from diagnostics import error_details


def consume(reader, path):
    rows = 0
    schema = reader.schema
    with pa.OSFile(str(path), 'wb') as output, pa.ipc.new_stream(output, schema) as writer:
        for batch in reader:
            writer.write_batch(batch)
            rows += batch.num_rows
    return {'path': str(path), 'rows': rows,
            'schema': schema.serialize().to_pybytes().hex(),
            'process_peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * 1024}


def partition_worker(args):
    driver, uri, descriptor, path, planned_schema = args
    with dbapi.connect(driver=driver, autocommit=True,
                      db_kwargs={'uri': uri, 'adbc.redis.query_workers': '1',
                                 'adbc.redis.read_timeout': '20m',
                                 'adbc.redis.write_timeout': '20m'}) as conn:
        with conn.cursor() as cur:
            cur.adbc_read_partition(descriptor)
            with cur.fetch_record_batch() as reader:
                assert reader.schema.serialize().to_pybytes().hex() == planned_schema
                return consume(reader, path)


def execute(job, uri):
    base = Path(job['data_directory'])
    base.mkdir(parents=True, exist_ok=True)
    started = time.perf_counter()
    with dbapi.connect(driver=job['driver'], autocommit=True,
                      db_kwargs={'uri': uri, 'adbc.redis.query_workers': '16',
                                 'adbc.redis.read_timeout': '20m',
                                 'adbc.redis.write_timeout': '20m'}) as conn:
        with conn.cursor() as cur:
            cur.execute('SET search_path TO ' + job['namespace'])
            if job['api'] == 'statement':
                cur.execute(job['sql'])
                planning = None
                with cur.fetch_record_batch() as reader:
                    files = [consume(reader, base / 'statement.arrow')]
                partitions = None
            else:
                cur.adbc_statement.set_options(**{'adbc.redis.partition_count': 16})
                plan_started = time.perf_counter()
                descriptors, schema = cur.adbc_execute_partitions(job['sql'])
                planning = time.perf_counter() - plan_started
                partitions = len(descriptors)
                schema_hex = schema.serialize().to_pybytes().hex()
                if descriptors:
                    jobs = [(job['driver'], uri, d, str(base / f'part-{i:03d}.arrow'), schema_hex)
                            for i, d in enumerate(descriptors)]
                    with concurrent.futures.ProcessPoolExecutor(
                            max_workers=16, mp_context=multiprocessing.get_context('spawn')) as pool:
                        files = list(pool.map(partition_worker, jobs))
                else:
                    with pa.RecordBatchReader.from_batches(schema, []) as reader:
                        files = [consume(reader, base / 'empty.arrow')]
    seconds = time.perf_counter() - started
    # Hash retained outputs after timing; both paths include Arrow IPC writes.
    for record in files:
        path = Path(record['path'])
        with path.open('rb') as stream:
            record['sha256'] = hashlib.file_digest(stream, 'sha256').hexdigest()
        record['bytes'] = path.stat().st_size
    return {'seconds': seconds, 'planning_seconds': planning,
            'partitions': partitions, 'external_workers': min(16, partitions) if partitions else 0,
            'rows': sum(f['rows'] for f in files), 'files': files,
            'timer': 'connection/session setup, execution/planning, complete Arrow consumption, IPC writes, worker startup/teardown and connection close; excludes output hashes and correctness validation',
            'process_peak_rss_bytes': resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * 1024}


def main():
    job = json.loads(Path(sys.argv[1]).read_text())
    credentials = json.load(sys.stdin)
    try:
        result = execute(job, credentials['uri'])
    except Exception as error:
        result = {'seconds': None, **error_details(error, credentials['uri'])}
    Path(job['output']).write_text(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
