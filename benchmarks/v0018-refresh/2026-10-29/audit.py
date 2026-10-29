"""Independent arithmetic/coverage audit for completed refresh suites."""
from pathlib import Path
from collections import Counter
import importlib.util, tempfile, shutil
ROOT=Path(__file__).resolve().parents[3]
def module(path,name):
    spec=importlib.util.spec_from_file_location(name,ROOT/path)
    value=importlib.util.module_from_spec(spec);spec.loader.exec_module(value);return value

import json, statistics
P=Path(__file__).parent
def read(path):return json.loads((P/path).read_text())
def median(rows,key):return statistics.median(x[key] for x in rows)
out={}
assert {name for name in ['bulk','incremental','cross','union','ipc','qpf','policy'] if (P/name).is_dir()} == {'bulk','incremental','cross','union','ipc','qpf','policy'}
# Reuse the predeclared IPC summarizer and worker-policy gate evaluator.
ipc_summarizer=module('benchmarks/native-ipc/summarize.py','ipc_summary')
policy_gate=module('benchmarks/worker-autotune/controlled/gate.py','policy_gate')
with tempfile.TemporaryDirectory() as temp:
    folder=Path(temp)
    shutil.copy2(P/'ipc/data/results.json',folder/'results.json')
    ipc_summarizer.summarize(folder)
    assert json.loads((folder/'summary.json').read_text()) == read('ipc/data/summary.json')
for factor in [0,2,4,8]:
    prefix=f'policy/data/qpf-{factor}'
    regenerated=policy_gate.evaluate(read(prefix+'/results.json'),read(prefix+'/manifest.json'))
    # Gate tuples become JSON arrays in the saved artifact.
    assert json.loads(json.dumps(regenerated)) == read(prefix+'/gate.json')
u=read('union/results.json')
assert len(u)==36 and all(x['validated'] for x in u)
assert set(Counter((x['input_rows'],x['cardinality'],x['stream']) for x in u).values())=={3}
out['union']={'executions':36,'validated_rows':sum(x['output_rows'] for x in u),'summary':[]}
for n in [10000,50000,100000]:
    vals={str(s):median([x for x in u if x['input_rows']==n and x['cardinality']==n and x['stream']==s],'peak_rss_bytes')/1048576 for s in [False,True]}
    out['union']['summary'].append({'output_rows':2*n,'materialized_mib':vals['False'],'streamed_mib':vals['True'],'reduction_percent':100*(1-vals['True']/vals['False'])})
for name,count in [('bulk',28),('incremental',98),('cross',72)]:
    file=P/name/'data/results.json'
    if not file.exists():continue
    r=json.loads(file.read_text())
    if r['status']!='complete':continue
    t=r['trials']
    grouping={'bulk':['method','writers'],'incremental':['method','readers','batch_rows'],'cross':['engine','mode','workers']}[name]
    repeat_count=5 if name=='cross' else 6
    assert len(r['summary'])=={'bulk':4,'incremental':14,'cross':12}[name]
    for summary in r['summary']:
        case=[x for x in t if all(x[k]==summary[k] for k in grouping)]
        assert sum(x['warmup'] for x in case)==1
        measured=[x for x in case if not x['warmup']]
        assert len(measured)==repeat_count and sorted(x['repeat'] for x in measured)==list(range(repeat_count))
        metrics=['total_index_ready_seconds'] if name=='bulk' else ['seconds','first_batch_seconds']
        for metric in metrics:
            vals=[x[metric] for x in measured]
            stats={'median':statistics.median(vals),'min':min(vals),'max':max(vals)}
            if name=='bulk':assert summary[metric]==stats
            else:
                assert summary[metric]==stats['median']
                if metric=='seconds' or name=='incremental':
                    assert summary['min_'+metric]==stats['min'] and summary['max_'+metric]==stats['max']
    assert len(t)==count and all(x['exact_full_values_validated'] for x in t)
    if name=='cross':assert all(x['rows']==100000 and x['columns']==105 and x['logical_arrow_bytes']==79578019 for x in t)
    if name=='bulk':
        assert r['rows']==100000 and r['columns']==105 and r['logical_arrow_bytes']==79578019
        assert r['parquet_sha256']=='d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0'
        assert r['source_arrow_sha256']=='a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
        for row in r['summary']:
            values=[x['total_index_ready_seconds'] for x in t if not x['warmup'] and (x['method'],x['writers'])==(row['method'],row['writers'])]
            assert len(values)==6 and statistics.median(values)==row['total_index_ready_seconds']['median']
        b={(x['method'],x['writers']):x['total_index_ready_seconds'] for x in r['summary']}
        assert b[('adbc',1)]['max']<b[('redis_py',1)]['min']
        assert max(b[('adbc',4)]['min'],b[('redis_py',4)]['min'])<=min(b[('adbc',4)]['max'],b[('redis_py',4)]['max'])
    if name=='incremental':
        one={x['method']:x for x in r['summary'] if x['readers']==1 and x['batch_rows']==4096}
        a=one['adbc']
        for method in ['redis_py_known','redis_py_discovery']:
            n=one[method]
            assert max(a['min_seconds'],n['min_seconds'])<=min(a['max_seconds'],n['max_seconds'])
            assert a['min_first_batch_seconds']>n['max_first_batch_seconds']
        four=[x for x in r['summary'] if x['readers']==4 and x['batch_rows']==4096]
        assert max(x['min_seconds'] for x in four)<=min(x['max_seconds'] for x in four)
    if name=='cross':
        cases={(x['engine'],x['mode'],x['workers']):x for x in r['summary']}
        for mode in ['persistent','spawned']:
            for workers in [1,4]:
                for engine in ['postgres','clickhouse']:
                    assert cases[('redis',mode,workers)]['min_seconds']>cases[(engine,mode,workers)]['max_seconds']
    out[name]={'executions':count,'measured':sum(not x['warmup'] for x in t),'summary':r['summary']}
ipc=P/'ipc/data/summary.json'
if ipc.exists():
    r=json.loads(ipc.read_text());assert r['operations']==144 and r['measured_operations']==120
    assert len(r['comparisons'])==12 and all(x['elapsed_ranges_overlap'] for x in r['comparisons'])
    out['ipc']=r
q=P/'qpf/data/results.json'
if q.exists():
    r=json.loads(q.read_text());t=r['trials']
    if len(t)==90 and 'restored_qpf' in r['metadata']:
        assert all(x['errors']==0 for x in t)
        assert set(Counter((x['qpf'],x['query'],x['concurrency']) for x in t).values())=={3}
        assert len(r['metadata']['configs'])==9
        assert r['metadata']['restored_qpf']['query_performance_factor']=={'active':True,'scaling_factor':4}
        for c in r['metadata']['configs']:
            factor=c['qpf'];observed=c['observed']
            assert observed['query_performance_factor']=={'active':True,'scaling_factor':factor}
            assert observed['search']['search-workers']==factor*3//2
            assert observed['sched_policy']==('mnp' if factor else 'cmp')
            assert observed['conns']==(32 if factor else 5)
        rows=[]
        for query,n in [('index_group',16),('index_count',16),('full_scan',1)]:
            row={'query':query,'concurrency':n}
            for factor in [0,2,4]:
                v=[x for x in t if (x['query'],x['concurrency'],x['qpf'])==(query,n,factor)]
                row[str(factor)]={'qps':median(v,'qps'),'p95_ms':median(v,'p95_ms'),'qps_range':[min(x['qps'] for x in v),max(x['qps'] for x in v)]}
            rows.append(row)
        out['qpf']={'trials':90,'summary':rows}
policy=P/'policy/data'
if (policy/'completion.json').exists():
    completion=json.loads((policy/'completion.json').read_text())
    if len(completion)==4 and all(x in [0,2] for x in completion.values()):
        records=[]
        timed_executions=0
        for factor in [0,2,4,8]:
            g=json.loads((policy/f'qpf-{factor}/gate.json').read_text())
            assert len(g['cases'])==3 and not g['infrastructure']
            raw=json.loads((policy/f'qpf-{factor}/results.json').read_text())
            assert len(raw)==36
            timed_executions+=sum((row['rounds']+row['warmup_rounds'])*row['concurrency'] for row in raw)
            for row in raw:
                for k in ['qpf_before','qpf_after']:
                    assert row[k]['query_performance_factor']=={'active':True,'scaling_factor':factor}
                    assert row[k]['search']['search-workers']==factor*3//2
                    assert row[k]['sched_policy']==('mnp' if factor else 'cmp')
                    assert row[k]['conns']==(32 if factor else 5)
            for c in g['cases']:records.append({'qpf':factor,**c})
        assert timed_executions==26208
        out['policy']={'cases':records,'passed':sum(c['passed'] for c in records),'total':len(records),'timed_executions':timed_executions}
assert set(out)=={'union','bulk','incremental','cross','ipc','qpf','policy'}
assert out==read('audited-summary.json'), 'Published summary differs from recomputed evidence'
print(json.dumps({'completed':list(out),'counts':{k:{n:v for n,v in r.items() if n in ['executions','measured','trials','passed','total']} for k,r in out.items()}},indent=2))
