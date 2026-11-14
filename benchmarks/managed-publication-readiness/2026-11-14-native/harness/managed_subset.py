"""Managed-write subset of unchanged ScenarioBenchmark, not a full matrix."""
import argparse,hashlib,json
from pathlib import Path
import scenarios as original
import run as base

p=argparse.ArgumentParser(description=__doc__)
for key in ('driver','cli','work'):p.add_argument('--'+key,type=Path,required=True)
p.add_argument('--version',required=True)
a=p.parse_args();a.repeats=3;a.operations=32;a.queries=8;a.point_rows=1000;a.olap_rows=100000;a.suite='oltp';a.dedicated_empty_server=True
b=original.ScenarioBenchmark(a)
cases=[(op,clients) for op in ('insert','update','delete') for clients in (1,4)]
b.results['metadata'].update(suite='managed-write-subset',subset={'operations':['insert','update','delete'],'modes':['managed'],'clients':[1,4],'case_count':6,'expected_records':24,'full_168_record_matrix':False},wrapper_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),frozen_harness_sha256={n:hashlib.sha256((Path(__file__).parent/n).read_bytes()).hexdigest() for n in ('run.py','scenarios.py')})
b.save()
for repeat in range(-1,3):
 for op,clients in cases if repeat%2 else reversed(cases):
  mapping=b.setup('managed',1000)
  b.run_trial('oltp',op,'managed',clients,repeat,mapping)
b.results['final_qpf']=b.gate();b.results['status']='complete';b.save()
assert len(b.results['trials'])==24 and not b.results['failures']
# run_trial has closed every thread/client and checked complete mutation states.
with base.client(b.cfg) as r:
 assert r.get(base.MARKER)==b.owner.encode()
 for index in r.execute_command('FT._LIST'):r.execute_command('FT.DROPINDEX',index)
 r.flushdb();assert r.dbsize()==0 and not r.execute_command('FT._LIST')
(b.work/'cleanup.json').write_text(json.dumps({'all_clients_exited':True,'ownership_marker_verified':True,'owned_data_and_indexes_removed':True},indent=2)+'\n')
print('complete24-recordmanaged-writeSUBSET; owneddata/indexescleared',flush=True)
