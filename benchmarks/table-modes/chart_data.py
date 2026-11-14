"""Audit validated scenario/table-mode evidence and emit chart-ready JSON/CSV.

This command never connects to Redis or runs ClickBench. Historical product
results remain a separately labeled series, without mixing builds or metrics.
"""
import argparse
import csv
import hashlib
import itertools
import json
import math
from pathlib import Path
import statistics

MODES = ('managed','unmanaged','redis_py')
POINT = ('point_read','insert','update','delete')
QUERIES = ('filtered_projection','filtered_count_sum','grouped_count_sum')


def percentile(values, fraction):
    assert values and all(math.isfinite(x) and x > 0 for x in values)
    return sorted(values)[max(0, math.ceil(len(values)*fraction)-1)]


def scenario_summary(data):
    assert data['status']=='complete' and not data['failures']
    m=data['metadata'];assert m['automatic_retries']==0 and m['tls'] and m['protocol']==3
    assert data['final_qpf']=={'factor':4,'workers':6,'routing':'mnp','connections':32,'memory_bytes':4294967296}
    expected=set()
    for suite,operations in [('oltp',POINT),('olap',QUERIES)]:
        if m['suite'] not in ('all',suite):continue
        expected.update((suite,op,mode,n) for op,mode,n in itertools.product(operations,MODES,(1,4)))
    groups={key:[] for key in expected}
    for row in data['trials']:
        key=tuple(row[k] for k in ('suite','operation','mode','clients'));assert key in groups
        assert row['validated'] and row['status']=='validated' and not row.get('errors')
        assert row['warmup']==(row['repeat']==-1)
        assert row['columns']==4 and row['fixture_rows']==m['point_rows' if row['suite']=='oltp' else 'olap_rows']
        assert row['operations']==m['operations' if row['suite']=='oltp' else 'queries']
        values=row['latency_ns'];assert len(values)==row['operations'] and all(type(x) is int and x>0 for x in values)
        assert math.isfinite(row['seconds']) and row['seconds']>0
        assert row['median_latency_ms']==statistics.median(values)/1e6
        assert row['p95_latency_ms']==percentile(values,.95)/1e6
        assert row['operations_per_second']==row['operations']/row['seconds']
        groups[key].append(row)
    result=[]
    for key,rows in sorted(groups.items()):
        assert sorted(r['repeat'] for r in rows)==list(range(-1,m['repetitions']))
        measured=[r for r in rows if not r['warmup']];values=[v for r in measured for v in r['latency_ns']]
        result.append(dict(zip(('suite','operation','mode','clients'),key)) | {
            'p50_ms':statistics.median(values)/1e6,'p95_ms':percentile(values,.95)/1e6,
            'min_ms':min(values)/1e6,'max_ms':max(values)/1e6,
            'median_operations_per_second':statistics.median(r['operations_per_second'] for r in measured),
            'trials':len(measured),'latency_samples':len(values),'fixture_rows':rows[0]['fixture_rows'],'columns':4})
    assert len(data['trials'])==len(expected)*(m['repetitions']+1)
    return result


def table_summary(data):
    # Reuse the established independent full-matrix audit rather than silently
    # accepting a subset or a successful-looking summary file.
    from merged_scoped_audit import summarize
    return summarize(data)


def make_charts(table_data,scenario_data):
    a=table_data['metadata'];b=scenario_data['metadata']
    for key in ('driver_source_commit','driver_sha256','driver_version'):
        assert a[key]==b[key], 'Do not combine different driver builds: '+key
    scenarios=scenario_summary(scenario_data);tables=table_summary(table_data)
    charts={k:[] for k in ('oltp','bulk_ingestion','olap_scans','olap_queries','data_movement','slide4_overview')}
    def add(chart,scenario,mode,value,unit,baseline,**extra):
        assert value>0 and baseline>0 and math.isfinite(value) and math.isfinite(baseline)
        charts[chart].append({'scenario':scenario,'mode':mode,'value':value,'unit':unit,
                              'redis_py_value':baseline,'elapsed_multiple':value/baseline,**extra})
    def point(op,mode,n):return next(r for r in scenarios if r['operation']==op and r['mode']==mode and r['clients']==n)
    for r in scenarios:
        ref=point(r['operation'],'redis_py',r['clients'])
        extra={k:r[k] for k in ('clients','p95_ms','trials','latency_samples','fixture_rows','columns','median_operations_per_second')}
        add('oltp' if r['suite']=='oltp' else 'olap_queries',r['operation'],r['mode'],r['p50_ms'],'ms',ref['p50_ms'],**extra)
    def existing(suite,mode,n,api=None,batch=4096,operation=None):
        return next(r for r in tables[suite] if r['mode']==mode and r.get('readers',r.get('writers'))==n and
                    (r.get('api')==api if api else r.get('batch_rows')==batch) and (not operation or r['operation']==operation))
    for api,n,mode in itertools.product(('sql','arrow'),(1,4),MODES):
        r=existing('writes',mode,n,'hset' if mode=='redis_py' else api);ref=existing('writes','redis_py',n,'hset')
        add('bulk_ingestion',api+'_append',mode,r['median_seconds'],'s',ref['median_seconds'],clients=n,
            fixture_rows=10000,columns=105,trials=r['samples'],batch_rows=1000,
            min_value=r['min_seconds'],max_value=r['max_seconds'])
    for n,batch,mode in itertools.product((1,4),(512,4096),MODES):
        r=existing('reads',mode,n,batch=batch);ref=existing('reads','redis_py',n,batch=batch)
        add('olap_scans','full_scan',mode,r['median_seconds'],'s',ref['median_seconds'],clients=n,
            batch_rows=batch,first_batch_seconds=r['median_first_batch_seconds'],fixture_rows=100000,
            columns=105,trials=r['samples'],min_value=r['min_seconds'],max_value=r['max_seconds'])
    for mode in MODES:
        r=existing('creation',mode,1,'hset' if mode=='redis_py' else 'arrow',operation='create');ref=existing('creation','redis_py',1,'hset',operation='create')
        add('data_movement','arrow_to_redis_create',mode,r['median_seconds'],'s',ref['median_seconds'],clients=1,
            fixture_rows=10000,columns=105,trials=r['samples'],setup_included=True)
        for n in (1,4):
            r=existing('reads',mode,n);ref=existing('reads','redis_py',n)
            add('data_movement','redis_to_arrow',mode,r['median_seconds'],'s',ref['median_seconds'],clients=n,
                fixture_rows=100000,columns=105,trials=r['samples'],setup_included=False)
        r=point('point_read',mode,1);ref=point('point_read','redis_py',1)
        add('slide4_overview','point_read',mode,r['p50_ms'],'ms',ref['p50_ms'],clients=1,
            fixture_rows=r['fixture_rows'],columns=4,trials=r['trials'],latency_samples=r['latency_samples'])
        for scenario,suite,api,rows in [('full_scan','reads',None,100000),('sql_insert','writes','sql',10000)]:
            r=existing(suite,mode,1,'hset' if mode=='redis_py' and api else api);ref=existing(suite,'redis_py',1,'hset' if api else None)
            add('slide4_overview',scenario,mode,r['median_seconds'],'s',ref['median_seconds'],clients=1,
                fixture_rows=rows,columns=105,trials=r['samples'])
    return {'schema_version':1,'driver':{k:a[k] for k in ('driver_source_commit','driver_sha256','driver_version')},
            'scope':'Current chart series use the exact same driver build. Separate runs and synthetic/105-column fixtures remain labeled. Ratios are driver elapsed / redis-py elapsed; below 1 is faster. P50/P95 pool operation observations across measured rounds; QPS is median closed-loop trial throughput.',
            'charts':charts,'scenario_summary':scenarios,
            'caveats':['Managed snapshots/publication differ from live unmanaged HASHes and raw commands.',
                       'Known-key point access excludes address lookup; managed uses internal row IDs.',
                       'SQL INSERT refuses existing keys; HSET can overwrite. No multi-statement transaction claim.',
                       'Existing-table setup excluded; create-and-fill includes table/index work with differing index semantics.',
                       'Finite sample p95 and closed-loop throughput are not production capacity/SLA measurements.']}


def add_historical(document,data):
    assert data['source']['source_rows']==99997497
    assert all(r['status']=='valid' for r in data['checks'])
    valid={(r['query'],r['trial']) for r in data['checks']}
    values=[]
    for r in data['comparisons']:
        assert (r['query'],2) in valid and (r['query'],3) in valid
        values.append({'query':r['query'],'product':'redis_adbc','seconds':r['redis_seconds']})
        for product,b in r['baselines'].items():values.append({'query':r['query'],'product':product,'seconds':b['seconds']})
    document['historical_clickbench']={'historical':True,'observed_at':data['observed_at'],'scope':data['scope'],
        'rows':99997497,'distribution':'Redis Open Source cluster; not current Software 4x QPF',
        'hardware':'Redis: 48 c6a.4xlarge primaries + 1 client; each other product: one c6a.4xlarge. Unequal resources.',
        'metric':'minimum of two independently validated hot trials, not medians or a full-suite score','values':values}


def write_outputs(document,output):
    output.mkdir(parents=True,exist_ok=False)
    (output/'charts.json').write_text(json.dumps(document,indent=2)+'\n')
    rows=[{'chart':chart,**row} for chart,values in document['charts'].items() for row in values]
    columns=list(dict.fromkeys(k for r in rows for k in r))
    with (output/'charts.csv').open('w',newline='') as f:
        writer=csv.DictWriter(f,fieldnames=columns);writer.writeheader();writer.writerows(rows)
    return len(rows)


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--table-modes',type=Path,required=True);p.add_argument('--scenarios',type=Path,required=True)
    p.add_argument('--historical-clickbench',type=Path);p.add_argument('--out',type=Path,required=True)
    a=p.parse_args();paths={'table_modes':a.table_modes,'scenarios':a.scenarios}
    d=make_charts(json.loads(a.table_modes.read_text()),json.loads(a.scenarios.read_text()))
    if a.historical_clickbench:
        paths['historical_clickbench']=a.historical_clickbench;add_historical(d,json.loads(a.historical_clickbench.read_text()))
    d['input_sha256']={k:hashlib.sha256(v.read_bytes()).hexdigest() for k,v in paths.items()}
    print('Generated',write_outputs(d,a.out),'validated chart rows')


if __name__=='__main__':main()
