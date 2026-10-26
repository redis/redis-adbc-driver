"""Compare loading strategies on an isolated Redis server, not the leaderboard.

Loads every official column, verifies aggregates and complete sampled rows
against DuckDB, and cleans up each generated schema. Never run concurrently
with a timed cloud query sweep on the same database.
"""
import argparse
from contextlib import contextmanager
import json
import hashlib
import os
from pathlib import Path
import time
import uuid

import adbc_driver_manager.dbapi as dbapi
import duckdb
import pyarrow as pa
import pyarrow.parquet as pq
import redis

from loading import ingest_batches
from run import arrow_schema, convert_batch


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--baseline-driver', type=Path, required=True)
    ap.add_argument('--driver', type=Path, required=True)
    ap.add_argument('--upstream', type=Path, required=True)
    ap.add_argument('--parquet', type=Path, required=True)
    ap.add_argument('--rows', type=int, default=100000)
    ap.add_argument('--batch-size', type=int, default=10000)
    ap.add_argument('--workers', type=int, nargs='+', default=[1, 4, 8])
    ap.add_argument('--serial-only', action='store_true', help='Isolate driver changes without prefetch or parallelism')
    ap.add_argument('--repeats', type=int, default=3)
    ap.add_argument('--output', type=Path, required=True)
    args = ap.parse_args()
    if args.serial_only and args.workers != [1]:
        ap.error('--serial-only requires --workers 1')
    if args.rows < 1 or args.batch_size < 1 or args.repeats < 1 or min(args.workers) < 1:
        ap.error('Rows, batch size, repeats and workers must be positive')
    if args.output.exists():
        ap.error('Choose a new output path to retain existing measurements')
    uri = os.environ['REDIS_URI']
    ddl = (args.upstream/'postgresql/create.sql').read_text()
    schema = arrow_schema(ddl)
    parquet = pq.ParquetFile(args.parquet)
    if args.rows > parquet.metadata.num_rows:
        ap.error('Source has fewer rows than requested')

    def batches():
        remaining = args.rows
        for batch in parquet.iter_batches(batch_size=args.batch_size):
            batch = batch.slice(0, min(remaining, batch.num_rows))
            yield convert_batch(batch, schema)
            remaining -= batch.num_rows
            if remaining == 0:
                break

    oracle = duckdb.connect()
    oracle.register('hits', pa.Table.from_batches(list(batches())))
    checks = ['SELECT COUNT(*), SUM(AdvEngineID), SUM(ResolutionWidth), MIN(UserID), MAX(UserID) FROM hits',
              'SELECT * FROM hits ORDER BY WatchID LIMIT 100']
    expected = [oracle.execute(q).fetchall() for q in checks]
    control = redis.Redis.from_url(uri, protocol=2, socket_timeout=30)
    def digest(path):
        with path.open('rb') as f:
            return hashlib.file_digest(f, 'sha256').hexdigest()

    report = {'baseline_driver_sha256':digest(args.baseline_driver),
              'optimized_driver_sha256':digest(args.driver),
              'redis_version':control.info('server')['redis_version'],
              'kind': 'loading profile; not full-scale ClickBench', 'rows':args.rows,
              'columns':len(schema), 'batch_size':args.batch_size, 'results':[]}
    strategies = [('baseline', args.baseline_driver, 1, False)] + [
                  ('optimized', args.driver, n, not args.serial_only) for n in args.workers]
    # Alternate strategies between repeats to reduce order/thermal bias.
    for repeat in range(args.repeats):
        offset = repeat % len(strategies)
        ordered = strategies[offset:] + strategies[:offset]
        for label, driver, workers, prefetch in ordered:
            namespace = 'loadprofile_' + uuid.uuid4().hex[:12]
            with dbapi.connect(driver=str(driver.resolve()), autocommit=True, db_kwargs={'uri':uri}) as conn:
                with conn.cursor() as cur:
                    cur.execute('CREATE SCHEMA '+namespace)
                    cur.execute('SET search_path TO '+namespace)
                    try:
                        started = time.perf_counter()
                        cur.execute(ddl)
                        @contextmanager
                        def writer():
                            with dbapi.connect(driver=str(driver.resolve()), autocommit=True, db_kwargs={'uri':uri}) as c:
                                with c.cursor() as cursor:
                                    yield lambda batch: cursor.adbc_ingest('hits',batch,mode='append',db_schema_name=namespace)
                        if prefetch:
                            loaded = sum(ingest_batches(batches(), writer, workers))
                        else:
                            loaded = 0
                            for batch in batches():
                                cur.adbc_ingest('hits',batch,mode='append',db_schema_name=namespace)
                                loaded += batch.num_rows
                        assert loaded == args.rows
                        deadline = time.monotonic() + 300
                        while True:
                            info = control.execute_command('FT.INFO','idx:'+namespace+':hits')
                            info = dict(zip(info[::2],info[1::2])) if isinstance(info,list) else info
                            if float(info.get(b'num_docs',info.get('num_docs',0))) == args.rows and not int(info.get(b'indexing',info.get('indexing',0))):
                                break
                            if time.monotonic()>deadline:
                                raise TimeoutError('Index readiness')
                            time.sleep(.05)
                        cur.execute('SELECT COUNT(*) FROM hits')
                        assert cur.fetchone()[0] == args.rows
                        seconds = time.perf_counter()-started
                        for query, reference in zip(checks, expected):
                            cur.execute(query)
                            actual = list(cur.fetchall())
                            assert actual == reference, 'Independent source-data check failed'
                        row = {'strategy':label,'workers':workers,'prefetch':prefetch,'repeat':repeat+1,
                               'seconds':seconds,'rows_per_second':args.rows/seconds,'correctness':'valid'}
                        report['results'].append(row)
                        args.output.parent.mkdir(parents=True,exist_ok=True)
                        args.output.write_text(json.dumps(report,indent=2)+'\n')
                        print(json.dumps(row),flush=True)
                    finally:
                        cur.execute('DROP TABLE IF EXISTS hits')
                        cur.execute('SET search_path TO public')
                        cur.execute('DROP SCHEMA '+namespace)
    control.close()
    oracle.close()


if __name__ == '__main__':
    main()
