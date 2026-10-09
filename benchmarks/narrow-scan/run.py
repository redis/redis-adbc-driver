"""Compare Q4/Q5/Q16 with original and modified drivers on an official fixture.

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
    p.add_argument('--table', default='it_211_hits')
    p.add_argument('--output', type=Path)
    p.add_argument('--query', type=int)
    p.add_argument('--rows', type=int, default=100000)
    a = p.parse_args()
    if not re.fullmatch(r'it_211_[a-z0-9_]+', a.table):
        p.error('use a dedicated table name beginning it_211_')
    if a.phase == 'query':
        if a.baseline is None or a.output is None:
            p.error('--baseline and --output are required for query')
        results = []
        for trial in range(3):
            drivers = [('baseline', a.baseline), ('narrow', a.driver)]
            if trial % 2: drivers.reverse()
            for label, driver in drivers:
                for query in (4, 5, 16):
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
            # Independent references use Python integer arithmetic and exact
            # counts from the source UserID column, including a tie-aware Q16.
            users = pq.read_table(a.parquet, columns=['UserID']).column(0).slice(0,a.rows).to_pylist()
            counts = Counter(u for u in users if u is not None)
            sql = re.sub(r'\bhits\b', a.table, a.queries.read_text().splitlines()[a.query-1])
            begin = time.perf_counter()
            cur.execute(sql)
            table = cur.fetch_arrow_table()
            seconds = time.perf_counter()-begin
            rows = list(zip(*(table.column(i).to_pylist() for i in range(table.num_columns))))
            if a.query == 4:
                expected = sum(u*c for u,c in counts.items())/sum(counts.values())
                assert len(rows) == 1 and math.isclose(rows[0][0], expected, rel_tol=1e-12)
            elif a.query == 5:
                assert rows == [(len(counts),)]
            else:
                cutoff = sorted(counts.values(), reverse=True)[min(10,len(counts))-1]
                assert len(rows) == min(10,len(counts))
                assert len(set(u for u,c in rows)) == len(rows)
                assert all(counts[u] == c and c >= cutoff for u,c in rows)
                assert all(rows[i][1] >= rows[i+1][1] for i in range(len(rows)-1))
                assert {u for u,c in counts.items() if c > cutoff}.issubset({u for u,c in rows})
            print(json.dumps({'query': a.query, 'seconds': seconds, 'correct': True, 'rows': rows}))


if __name__ == '__main__':
    main()
