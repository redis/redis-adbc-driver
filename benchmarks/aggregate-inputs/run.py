"""Native 1/10 expression SUM and official Q30 comparison on preloaded fixture."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'exact-distinct'))
from proxy import Proxy


def query(width,queries):
    if width==90:
        return re.sub(r'\bhits\b','it_213_issue215',queries.read_text().splitlines()[29])
    return 'SELECT '+', '.join(f'SUM(ResolutionWidth + {i})' for i in range(1,width+1))+' FROM it_213_issue215'


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--driver',type=Path,required=True)
    p.add_argument('--baseline',type=Path)
    p.add_argument('--parquet',type=Path,required=True)
    p.add_argument('--queries',type=Path,required=True)
    p.add_argument('--width',type=int,choices=[1,10,90])
    p.add_argument('--output',type=Path)
    a=p.parse_args()
    if a.baseline:
        results=[]
        for trial in range(3):
            drivers=[('baseline',a.baseline),('typed',a.driver)]
            if trial%2:drivers.reverse()
            for label,driver in drivers:
                for width in [1,10,90]:
                    output=subprocess.check_output([sys.executable,__file__,'--driver',str(driver.resolve()),
                        '--parquet',str(a.parquet),'--queries',str(a.queries),'--width',str(width)],text=True)
                    result=json.loads(output);result.update(strategy=label,trial=trial+1)
                    results.append(result);print(json.dumps(result),flush=True)
                    a.output.write_text(json.dumps(results,indent=2)+'\n')
        return
    import adbc_driver_manager.dbapi as dbapi
    import pyarrow.parquet as pq
    import resource
    from urllib.parse import urlsplit,urlunsplit
    sql=query(a.width,a.queries)
    proxy=Proxy(os.environ['REDIS_URI']);proxy.start()
    parsed=urlsplit(os.environ['REDIS_URI'])
    uri=urlunsplit(parsed._replace(netloc='127.0.0.1:'+str(proxy.port)))
    with dbapi.connect(driver=str(a.driver.resolve()),autocommit=True,
                      db_kwargs={'uri':uri,'adbc.redis.aggregate_pushdown':'exact'}) as conn:
        with conn.cursor() as cur:
            before=proxy.snapshot();cpu=resource.getrusage(resource.RUSAGE_SELF)
            start=time.perf_counter();cur.execute(sql);table=cur.fetch_arrow_table()
            seconds=time.perf_counter()-start
            after=resource.getrusage(resource.RUSAGE_SELF)
            cpu_seconds=after.ru_utime+after.ru_stime-cpu.ru_utime-cpu.ru_stime
            wire={k:v-before[k] for k,v in proxy.snapshot().items()}
    values=pq.read_table(a.parquet,columns=['ResolutionWidth']).slice(0,100000).column(0).to_pylist()
    values=[int(x) for x in values if x is not None]
    offsets=range(90) if a.width==90 else range(1,a.width+1)
    expected=[sum(values)+offset*len(values) for offset in offsets]
    actual=[table.column(i)[0].as_py() for i in range(table.num_columns)]
    assert table.num_rows==1 and actual==expected,(actual,expected)
    print(json.dumps({'width':a.width,'sql':sql,'seconds':seconds,'cpu_seconds':cpu_seconds,
                      'process_peak_rss_bytes':after.ru_maxrss*(1 if sys.platform=='darwin' else 1024),
                      'wire_bytes':wire,'expected':expected,'actual':actual,'validated':True}))

if __name__=='__main__':main()
