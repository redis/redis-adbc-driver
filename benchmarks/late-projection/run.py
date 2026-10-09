"""Measure ordered residual SELECT against an isolated, preloaded ClickBench table.

Load the public 100K fixture with ../exact-distinct/run.py --phase load using
--table it_213_issue214. This runner checks all projected cells and accepts
SQL's unspecified membership among equal EventTime values at LIMIT edges.
"""
import argparse
import collections
import json
import os
from pathlib import Path
import subprocess
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'exact-distinct'))
from proxy import Proxy


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--driver', type=Path, required=True)
    p.add_argument('--baseline', type=Path)
    p.add_argument('--parquet', type=Path, required=True)
    p.add_argument('--output', type=Path)
    p.add_argument('--case', choices=['q24', 'offset', 'rare', 'none'])
    a = p.parse_args()
    if a.baseline:
        results = []
        for trial in range(3):
            drivers = [('baseline', a.baseline), ('late', a.driver)]
            if trial % 2: drivers.reverse()
            for label, driver in drivers:
                for case in ['q24', 'offset', 'rare', 'none']:
                    cmd = [sys.executable, __file__, '--driver', str(driver.resolve()),
                           '--parquet', str(a.parquet), '--case', case]
                    result = json.loads(subprocess.check_output(cmd, text=True))
                    result.update(strategy=label, trial=trial+1)
                    results.append(result)
                    print(json.dumps(result), flush=True)
                    a.output.write_text(json.dumps(results, indent=2)+'\n')
        return
    import adbc_driver_manager.dbapi as dbapi
    import pyarrow.parquet as pq
    from urllib.parse import urlsplit, urlunsplit
    pattern, offset = {'q24': ('google', 0), 'offset': ('produkty', 10),
                       'rare': ('gaw.ru', 0), 'none': ('issue214AbsentSentinel', 0)}[a.case]
    sql = f"SELECT * FROM it_213_issue214 WHERE URL LIKE '%{pattern}%' ORDER BY EventTime LIMIT 10 OFFSET {offset}"
    proxy = Proxy(os.environ['REDIS_URI']); proxy.start()
    parsed = urlsplit(os.environ['REDIS_URI'])
    uri = urlunsplit(parsed._replace(netloc='127.0.0.1:'+str(proxy.port)))
    with dbapi.connect(driver=str(a.driver.resolve()), autocommit=True, db_kwargs={'uri':uri}) as conn:
        with conn.cursor() as cur:
            before = proxy.snapshot()
            started = time.perf_counter()
            cur.execute(sql)
            execute_seconds = time.perf_counter()-started
            table = cur.fetch_arrow_table()
            assert table.num_columns == 105
            seconds = time.perf_counter()-started
            wire = {k:v-before[k] for k,v in proxy.snapshot().items()}
    # Cast reference fields to the result's exact SQL types (seconds -> us).
    import pyarrow as pa
    import pyarrow.compute as pc
    source = pq.read_table(a.parquet).slice(0,100000)
    cols = []
    for f in table.schema:
        col = source[f.name]
        if pa.types.is_timestamp(f.type) and pa.types.is_integer(col.type):
            col = pc.multiply(pc.cast(col,pa.int64()),1000000)
        if pa.types.is_date(f.type) and pa.types.is_integer(col.type):
            col = pc.cast(col,pa.int32())
        cols.append(pc.cast(col,f.type))
    candidates = [r for r in pa.Table.from_arrays(cols,schema=table.schema).to_pylist()
                  if r['URL'] is not None and pattern in r['URL']]
    candidates.sort(key=lambda r:r['EventTime'])
    selected = candidates[offset:offset+10]
    actual = table.to_pylist()
    assert len(actual)==len(selected)
    assert [r['EventTime'] for r in actual]==[r['EventTime'] for r in selected]
    counts = collections.Counter(tuple(r.values()) for r in candidates)
    for r in actual:
        key=tuple(r.values()); assert counts[key]>0; counts[key]-=1
    # Every strict interior row must occur; only boundary tie membership may vary.
    if selected:
        interior = collections.Counter(tuple(r.values()) for r in candidates
                                       if selected[0]['EventTime'] < r['EventTime'] < selected[-1]['EventTime'])
        got = collections.Counter(tuple(r.values()) for r in actual)
        assert not interior-got
    print(json.dumps({'case':a.case,'sql':sql,'seconds':seconds,'execute_seconds':execute_seconds,
                      'wire_bytes':wire,'rows':len(actual),'candidate_rows':len(candidates),
                      'validated':True}))

if __name__ == '__main__': main()
