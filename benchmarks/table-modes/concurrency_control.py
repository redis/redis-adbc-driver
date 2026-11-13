"""Small same-API control for SQL INSERT versus Arrow append concurrency.

Run each release in a separate invocation with a fresh task-owned database.
This checks conflicts/correctness, not production throughput.
"""
import argparse
import concurrent.futures
import hashlib
import importlib.util
import json
import multiprocessing
import os
from pathlib import Path
import sys
import uuid

import pyarrow as pa

SPEC=importlib.util.spec_from_file_location('table_mode_benchmark',Path(__file__).with_name('run.py'))
bench=importlib.util.module_from_spec(SPEC);sys.modules[SPEC.name]=bench;SPEC.loader.exec_module(bench)


def worker(payload):
    cfg, api, lo, count, barrier = payload
    table=bench.source(cfg['source']).slice(lo,count)
    numbered=table.append_column(bench.ORDINAL,pa.array(range(lo+1,lo+count+1),type=pa.int64()))
    sql='INSERT INTO bench.managed ('+','.join('"'+n+'"' for n in numbered.column_names)+') VALUES '+','.join(
        '('+','.join(bench.literal(v) for v in row.values())+')' for row in numbered.to_pylist())
    with bench.connect(cfg) as c,c.cursor() as q:
        barrier.wait(timeout=60)
        try:
            if api=='sql_insert':q.execute(sql);written=q.rowcount
            else:written=q.adbc_ingest('managed',numbered,mode='append',db_schema_name='bench')
            assert written==count,written
            return {'status':'success','rows':written}
        except Exception as error:
            state=getattr(error,'sqlstate',None)
            return {'status':'conflict' if state=='40001' else 'error','rows':0,
                    'error_type':type(error).__name__,'sqlstate':state,
                    'message':str(error).replace(cfg['uri'],'[private endpoint]')}


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--driver',type=Path,required=True);p.add_argument('--version',required=True)
    p.add_argument('--source',type=Path,required=True);p.add_argument('--output',type=Path,required=True)
    p.add_argument('--source-commit',required=True);p.add_argument('--repetitions',type=int,default=3)
    p.add_argument('--dedicated-empty-server',action='store_true',required=True)
    a=p.parse_args()
    if a.output.exists():p.error('Preserve prior results; choose a new output file')
    b=bench.Benchmark.__new__(bench.Benchmark)
    b.cfg={'driver':str(a.driver.resolve()),'source':str(a.source.resolve()),
           'uri':os.environ['REDIS_BENCH_URI'],'ca':os.environ['SSL_CERT_FILE']}
    b.owner=uuid.uuid4().hex
    with bench.client(b.cfg) as r:
        assert r.dbsize()==0 and not r.execute_command('FT._LIST')
    b.gate()
    with bench.connect(b.cfg) as c:assert a.version in str(c.adbc_get_info())
    with bench.client(b.cfg) as r:r.set(bench.MARKER,b.owner)
    expected=bench.source(a.source).slice(0,100)
    results={'metadata':{'driver_version':a.version,'driver_source_commit':a.source_commit,
                        'driver_sha256':hashlib.sha256(a.driver.read_bytes()).hexdigest(),
                        'rows':100,'columns':105,'writers':4,'repetitions':a.repetitions,
                        'start':'barrier after all four connections and inputs are ready',
                        'automatic_retries':0,'scope':'Correctness/conflict control; no throughput claim'},'trials':[]}
    ctx=multiprocessing.get_context('spawn')
    with ctx.Manager() as manager:
        for repetition in range(a.repetitions):
            for api in ['sql_insert','arrow_append']:
                b.reset();bench.setup_managed(b.cfg,expected.slice(0,0))
                barrier=manager.Barrier(4)
                payloads=[(b.cfg,api,i*25,25,barrier) for i in range(4)]
                with concurrent.futures.ProcessPoolExecutor(4,mp_context=ctx) as pool:
                    outcomes=list(pool.map(worker,payloads))
                assert all(o['status']!='error' for o in outcomes),outcomes
                visible=b.validate_partial_managed(expected)
                assert visible==sum(o['rows'] for o in outcomes)
                record={'api':api,'repetition':repetition,'successful_writers':sum(o['status']=='success' for o in outcomes),
                        'conflicting_writers':sum(o['status']=='conflict' for o in outcomes),
                        'visible_rows':visible,'all_visible_values_validated':True,'outcomes':outcomes}
                results['trials'].append(record)
                a.output.write_text(json.dumps(results,indent=2)+'\n');print(json.dumps(record),flush=True)
    results['final_qpf']=b.gate();results['status']='complete'
    a.output.write_text(json.dumps(results,indent=2)+'\n')

if __name__=='__main__':main()
