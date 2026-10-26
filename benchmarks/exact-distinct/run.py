"""Compare Q5/Q6/Q9/Q14 with original and modified drivers on an official fixture.

Requires pyarrow and adbc-driver-manager. Use an isolated Redis server.
Supply the official PostgreSQL create.sql and queries.sql from ClickBench.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--phase', choices=['load', 'query', 'cleanup', 'trial'], required=True)
    p.add_argument('--parquet', type=Path, required=True)
    p.add_argument('--ddl', type=Path, required=True)
    p.add_argument('--queries', type=Path, required=True)
    p.add_argument('--driver', type=Path, required=True)
    p.add_argument('--baseline', type=Path)
    p.add_argument('--table', default='it_213_hits')
    p.add_argument('--output', type=Path)
    p.add_argument('--query', type=int)
    p.add_argument('--rows', type=int, default=100000)
    p.add_argument('--synthetic', choices=['low','high'])
    a = p.parse_args()
    if not re.fullmatch(r'it_213_[a-z0-9_]+', a.table):
        p.error('use a dedicated table name beginning it_213_')
    if a.phase == 'query':
        if a.baseline is None or a.output is None:
            p.error('--baseline and --output are required for query')
        results = []
        for trial in range(3):
            drivers = [('baseline', a.baseline), ('deduplicated', a.driver)]
            if trial % 2: drivers.reverse()
            for label, driver in drivers:
                for query in (5, 6, 9, 14):
                    cmd = [sys.executable, __file__, '--phase', 'trial', '--driver', str(driver.resolve()),
                           '--parquet', str(a.parquet), '--ddl', str(a.ddl), '--queries', str(a.queries),
                           '--table', a.table, '--query', str(query), '--rows', str(a.rows)]
                    if a.synthetic: cmd += ['--synthetic', a.synthetic]
                    result = json.loads(subprocess.check_output(cmd, text=True))
                    result.update(strategy=label, trial=trial+1)
                    results.append(result)
                    print(json.dumps(result), flush=True)
                    a.output.write_text(json.dumps(results, indent=2)+'\n')
        return
    import adbc_driver_manager.dbapi as dbapi
    import pyarrow as pa
    import pyarrow.compute as pc
    import pyarrow.parquet as pq
    uri = os.environ['REDIS_URI']
    import redis
    from urllib.parse import urlsplit, urlunsplit
    from proxy import Proxy
    monitor = redis.Redis.from_url(uri)
    proxy = Proxy(uri)
    proxy.start()
    parsed = urlsplit(uri)
    uri = urlunsplit(parsed._replace(netloc='127.0.0.1:'+str(proxy.port)))
    with dbapi.connect(driver=str(a.driver.resolve()), autocommit=True,
                      db_kwargs={'uri': uri, 'adbc.redis.aggregate_pushdown': 'exact'}) as conn:
        with conn.cursor() as cur:
            if a.phase == 'cleanup':
                cur.execute('DROP TABLE '+a.table)
                return
            if a.phase == 'load' and a.synthetic:
                cur.execute('CREATE TABLE '+a.table+' (RegionID INTEGER, UserID BIGINT, SearchPhrase VARCHAR)')
                data = synthetic(a.synthetic, a.rows)
                cur.adbc_ingest(a.table, data, mode='append')
                print(json.dumps({'loaded_rows':a.rows,'synthetic':a.synthetic,'table':a.table}))
                return
            if a.phase == 'load':
                ddl = a.ddl.read_text()
                types = {'BIGINT': pa.int64(), 'INTEGER': pa.int32(), 'SMALLINT': pa.int16(),
                         'TEXT': pa.string(), 'VARCHAR': pa.string(), 'CHAR': pa.string(),
                         'TIMESTAMP': pa.timestamp('us'), 'DATE': pa.date32()}
                fields = []
                for line in ddl.splitlines():
                    m = re.fullmatch(r'\s*(\w+)\s+(\w+)(?:\(\d+\))?(\s+NOT NULL)?[,]?\s*', line)
                    if m and m[2].upper() in types:
                        fields.append(pa.field(m[1], types[m[2].upper()], nullable=not bool(m[3])))
                assert len(fields) == 105
                schema = pa.schema(fields)
                cur.execute(re.sub(r'\bhits\b', a.table, ddl))
                n = 0
                for batch in pq.ParquetFile(a.parquet).iter_batches(batch_size=10000):
                    batch = batch.slice(0, min(batch.num_rows, a.rows-n))
                    values = []
                    for f in schema:
                        v = batch.column(batch.schema.get_field_index(f.name))
                        if pa.types.is_date(f.type) and pa.types.is_integer(v.type):
                            v = pc.cast(pc.cast(v, pa.int32()), pa.date32())
                        elif pa.types.is_timestamp(f.type) and pa.types.is_integer(v.type):
                            v = pc.multiply_checked(pc.cast(v, pa.int64()), 1000000)
                        values.append(pc.cast(v, f.type, safe=True))
                    cur.adbc_ingest(a.table, pa.RecordBatch.from_arrays(values, schema=schema), mode='append')
                    n += batch.num_rows
                    if n == a.rows: break
                assert n == a.rows
                cur.execute('SELECT COUNT(*) FROM '+a.table)
                assert cur.fetchone()[0] == a.rows
                print(json.dumps({'loaded_rows': n, 'columns': 105, 'table': a.table}))
                return
            import resource
            sql = re.sub(r'\bhits\b', a.table, a.queries.read_text().splitlines()[a.query-1])
            import threading
            before_memory = monitor.info('memory')['used_memory']
            memory = [before_memory]
            stop = threading.Event()
            def sample():
                while not stop.wait(.01):
                    memory.append(monitor.info('memory')['used_memory'])
            sampler = threading.Thread(target=sample, daemon=True)
            sampler.start()
            before_network = proxy.snapshot()
            begin = time.perf_counter()
            cur.execute(sql)
            table = cur.fetch_arrow_table()
            seconds = time.perf_counter()-begin
            network = {k:v-before_network[k] for k,v in proxy.snapshot().items()}
            stop.set(); sampler.join()
            peak = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
            if sys.platform != 'darwin': peak *= 1024
            rows = list(zip(*(table.column(i).to_pylist() for i in range(table.num_columns))))
            columns = {5:['UserID'],6:['SearchPhrase'],9:['RegionID','UserID'],14:['SearchPhrase','UserID']}[a.query]
            source = (synthetic(a.synthetic,a.rows).select(columns) if a.synthetic else pq.read_table(a.parquet,columns=columns).slice(0,a.rows)).to_pylist()
            if a.query == 14: source=[r for r in source if r['SearchPhrase'] is not None and r['SearchPhrase']!='']
            groups = {}
            tuples = set()
            for row in source:
                key = (row[columns[0]],) if a.query in (9,14) else ()
                value = row[columns[-1]]
                users = groups.setdefault(key,set())
                tuples.add(key+(value,))
                if value is not None: users.add(value)
            if a.query in (5,6):
                expected = len(groups.get((),set()))
                assert rows==[(expected,)]
            else:
                expected={k:len(v) for k,v in groups.items()}
                cutoff=sorted(expected.values(),reverse=True)[min(10,len(expected))-1] if expected else 0
                keys=[(row[0],) for row in rows]
                assert len(rows)==min(10,len(expected)) and len(set(keys))==len(rows)
                for key,row in zip(keys,rows): assert row[1]==expected[key] and row[1]>=cutoff
                assert all(rows[i][1]>=rows[i+1][1] for i in range(len(rows)-1))
                assert {k for k,v in expected.items() if v>cutoff}.issubset(set(keys))
            print(json.dumps({'query':a.query,'seconds':seconds,'peak_rss_bytes':peak,
                              'network_request_bytes':network['request'],'network_reply_bytes':network['reply'],
                              'sampled_server_extra_bytes':max(memory)-before_memory,
                              'server_memory_before_bytes':before_memory,
                              'input_rows':a.rows,'matched_rows':len(source),'distinct_tuples':len(tuples),
                              'synthetic':a.synthetic,'groups':len(groups),'correct':True,'rows':rows}))


def synthetic(mode,n):
    import pyarrow as pa
    return pa.table({'RegionID':pa.array([i%17 for i in range(n)],type=pa.int32()),
                     'UserID':pa.array([9007199254740992+(i%31 if mode=='low' else i) for i in range(n)],type=pa.int64()),
                     'SearchPhrase':pa.array(['phrase'+str(i%23 if mode=='low' else i) for i in range(n)])})



if __name__ == '__main__':
    main()
