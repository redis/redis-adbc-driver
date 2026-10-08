"""Compare Q9/Q17/Q33 with original and modified drivers on an official fixture.

Requires pyarrow and adbc-driver-manager. Use an isolated Redis server.
Supply the official PostgreSQL create.sql and queries.sql from ClickBench.
"""
import argparse
from collections import Counter
import json
import math
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
    p.add_argument('--table', default='it_212_hits')
    p.add_argument('--output', type=Path)
    p.add_argument('--query', type=int)
    p.add_argument('--rows', type=int, default=100000)
    a = p.parse_args()
    if not re.fullmatch(r'it_212_[a-z0-9_]+', a.table):
        p.error('use a dedicated table name beginning it_212_')
    if a.phase == 'query':
        if a.baseline is None or a.output is None:
            p.error('--baseline and --output are required for query')
        results = []
        for trial in range(3):
            drivers = [('baseline', a.baseline), ('incremental', a.driver)]
            if trial % 2: drivers.reverse()
            for label, driver in drivers:
                for query in (9, 17, 33):
                    cmd = [sys.executable, __file__, '--phase', 'trial', '--driver', str(driver.resolve()),
                           '--parquet', str(a.parquet), '--ddl', str(a.ddl), '--queries', str(a.queries),
                           '--table', a.table, '--query', str(query), '--rows', str(a.rows)]
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
    with dbapi.connect(driver=str(a.driver.resolve()), autocommit=True,
                      db_kwargs={'uri': uri, 'adbc.redis.aggregate_pushdown': 'exact'}) as conn:
        with conn.cursor() as cur:
            if a.phase == 'cleanup':
                cur.execute('DROP TABLE '+a.table)
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
            begin = time.perf_counter()
            cur.execute(sql)
            table = cur.fetch_arrow_table()
            seconds = time.perf_counter()-begin
            # Capture worker process peak RSS before reading reference data.
            peak = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
            if sys.platform != 'darwin': peak *= 1024
            rows = list(zip(*(table.column(i).to_pylist() for i in range(table.num_columns))))
            columns = {9:['RegionID','UserID'],17:['UserID','SearchPhrase'],
                       33:['WatchID','ClientIP','IsRefresh','ResolutionWidth']}[a.query]
            source = pq.read_table(a.parquet,columns=columns).slice(0,a.rows).to_pylist()
            groups = {}
            for row in source:
                if a.query == 9:
                    key = (row['RegionID'],)
                    users = groups.setdefault(key,set())
                    if row['UserID'] is not None: users.add(row['UserID'])
                elif a.query == 17:
                    key = (row['UserID'],row['SearchPhrase'])
                    groups[key] = groups.get(key,0)+1
                else:
                    key = (row['WatchID'],row['ClientIP'])
                    value = groups.setdefault(key,[0,0,0,0])
                    value[0]+=1
                    if row['IsRefresh'] is not None: value[1]+=row['IsRefresh']
                    if row['ResolutionWidth'] is not None:
                        value[2]+=row['ResolutionWidth'];value[3]+=1
            if a.query == 9: expected={k:(len(v),) for k,v in groups.items()}
            elif a.query == 17: expected={k:(v,) for k,v in groups.items()}
            else: expected={k:(v[0],v[1],v[2]/v[3] if v[3] else None) for k,v in groups.items()}
            cutoff=sorted((v[0] for v in expected.values()),reverse=True)[min(10,len(expected))-1]
            key_width=len(columns) if a.query==17 else (1 if a.query==9 else 2)
            keys=[tuple(row[:key_width]) for row in rows]
            assert len(rows)==min(10,len(expected)) and len(set(keys))==len(rows)
            for key,row in zip(keys,rows):
                want=expected[key]
                got=row[key_width:]
                assert want[0]>=cutoff
                assert all(math.isclose(g,w,rel_tol=1e-12) if isinstance(w,float) else g==w for g,w in zip(got,want))
            assert all(rows[i][key_width]>=rows[i+1][key_width] for i in range(len(rows)-1))
            assert {k for k,v in expected.items() if v[0]>cutoff}.issubset(set(keys))
            print(json.dumps({'query':a.query,'seconds':seconds,'peak_rss_bytes':peak,
                              'input_rows':a.rows,'groups':len(expected),'correct':True,'rows':rows}))



if __name__ == '__main__':
    main()
