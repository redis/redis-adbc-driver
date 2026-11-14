"""Offline audit of the archived unselected native managed-write subset."""
from pathlib import Path
import json,math,statistics,hashlib
P=Path(__file__).parent/'2026-11-14-native'
runs=[]
keys={(op,c) for op in ('insert','update','delete') for c in (1,4)}
for i,k in enumerate(('baseline','candidate','candidate','baseline'),1):
 d=json.loads((P/f'{i}-{k}.json').read_text());assert d['status']=='complete' and not d['failures'] and len(d['trials'])==24
 m=d['metadata'];assert m['suite']=='managed-write-subset' and m['automatic_retries']==0 and m['point_rows']==1000 and m['columns']==4 and m['operations']==32 and m['repetitions']==3
 assert m['subset']=={'operations':['insert','update','delete'],'modes':['managed'],'clients':[1,4],'case_count':6,'expected_records':24,'full_168_record_matrix':False}
 assert d['final_qpf']=={'factor':4,'workers':6,'routing':'mnp','connections':32,'memory_bytes':4294967296}
 grouped={key:[] for key in keys}
 for t in d['trials']:
  assert t['suite']=='oltp' and t['mode']=='managed' and t['validated'] and t['status']=='validated' and not t.get('errors') and t['fixture_rows']==1000 and t['columns']==4 and t['operations']==32
  xs=t['latency_ns'];assert len(xs)==32 and all(type(x)is int and x>0 for x in xs)
  assert t['warmup']==(t['repeat']==-1)
  assert t['median_latency_ms']==statistics.median(xs)/1e6 and t['p95_latency_ms']==sorted(xs)[math.ceil(.95*len(xs))-1]/1e6 and t['operations_per_second']==32/t['seconds']
  grouped[(t['operation'],t['clients'])].append(t)
 for vs in grouped.values():assert sorted(v['repeat'] for v in vs)==[-1,0,1,2]
 assert json.loads((P/f'{i}-{k}-exit.json').read_text())['exit_code']==0
 assert all(json.loads((P/'cleanup.json').read_text())[str(i)].values())
 runs.append({'run':i,'build':k,'data':d})
expected=[]
for op,c in sorted(keys):
 row={'operation':op,'clients':c}
 for k in ('baseline','candidate'):
  ts=[t for run in runs if run['build']==k for t in run['data']['trials'] if (t['operation'],t['clients'])==(op,c) and not t['warmup']]
  xs=[v for t in ts for v in t['latency_ns']];assert len(ts)==6 and len(xs)==192
  row[k]={'trials':len(ts),'observations':len(xs),'p50_ms':statistics.median(xs)/1e6,'p95_ms':sorted(xs)[math.ceil(.95*len(xs))-1]/1e6,'median_operations_per_second':statistics.median(t['operations_per_second'] for t in ts)}
 row['p50_speedup']=row['baseline']['p50_ms']/row['candidate']['p50_ms'];expected.append(row)
assert json.loads((P/'comparison.json').read_text())==expected
identity=json.loads((P/'provenance.json').read_text())
for run in runs:
 m=run['data']['metadata'];build=run['build'];assert m['driver_source_commit']==identity['source_builds'][build]['commit'] and m['driver_sha256']==identity['source_builds'][build]['library_sha256']
 assert m['wrapper_sha256']==identity['harness_sha256']['managed_subset.py']
 assert m['frozen_harness_sha256']=={k:identity['harness_sha256'][k] for k in ('run.py','scenarios.py')}
for name,digest in json.loads((P/'sha256.json').read_text()).items():assert hashlib.sha256((P/name).read_bytes()).hexdigest()==digest
print('PASS: four complete 24-record managed-write SUBSETS, six cases, all measured/warmup samples, complete final states and cleanup')
