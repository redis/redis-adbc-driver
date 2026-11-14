"""Offline audit of complete managed-readiness diagnostic observations."""
from pathlib import Path
import hashlib,json,math,statistics
P=Path(__file__).parent/'2026-11-14'
read=lambda n:json.loads((P/n).read_text())
runs=[read(f'{i}-{k}.json') for i,k in enumerate(('baseline','candidate','candidate','baseline'),1)]
for i,r in enumerate(runs,1):
 d=r['data'];assert r['run']==i and r['exit_code']==0 and len(d['samples'])==len(d['foreground_calls'])==96 and d['caller_retries']==0
 assert d['public_columns']==4 and d['control_columns']==1 and d['rows_per_fixture']==1000
 for op in ('insert','update','delete'):assert sorted(x['index'] for x in d['samples'] if x['operation']==op)==list(range(1,33))
 for ph in d['foreground_calls'].values():
  assert ph['pipeline_shapes'].get('zcard,ft.info',0)==int(r['build']=='candidate')
  assert ph['round_trips'].get('ft.info',{}).get('calls',0)==int(r['build']=='baseline')
  assert ph['round_trips']['zcard']['calls']==(2 if r['build']=='baseline' else 1)
expected=[]
for op in ('insert','update','delete'):
 row={'operation':op}
 for k in ('baseline','candidate'):
  xs=[s['milliseconds'] for r in runs if r['build']==k for s in r['data']['samples'] if s['operation']==op]
  assert len(xs)==64 and all(x>0 and math.isfinite(x) for x in xs)
  row[k]={'observations':len(xs),'p50_ms':statistics.median(xs),'p95_ms':sorted(xs)[math.ceil(.95*len(xs))-1]}
 row['p50_speedup']=row['baseline']['p50_ms']/row['candidate']['p50_ms'];expected.append(row)
assert read('comparison.json')==expected
v=read('provenance.json');assert v['automatic_retries']==v['discarded_observations']==0 and v['all_final_states_validated']
for k in ('baseline','candidate'):assert v['identities'][k]['test_binary_sha256']==v['test_hash_after_runs'][k]
for name,digest in read('sha256.json').items():assert hashlib.sha256((P/name).read_bytes()).hexdigest()==digest
print('PASS: all 384 observations, exact proof exchange change, retained slower cases, source/binary/input hashes and comparison arithmetic')
