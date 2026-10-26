"""Render the pinned full profiles after every profile validates independently."""
import concurrent.futures
import hashlib
import json
from pathlib import Path
import subprocess
import sys

source=Path(sys.argv[1]);output=Path(sys.argv[2]);package=Path(sys.argv[3])
report=json.loads((source/'results.json').read_text())
assert report['status']=='complete' and len(report['profiles'])==8
assert all(p['valid'] for p in report['profiles'])
manifest=json.loads((package/'manifest.json').read_text())
for build in ['baseline','candidate']:
 name='bin/'+build+'-profile.test';p=package/name;h=hashlib.sha256()
 with p.open('rb') as f:
  for chunk in iter(lambda:f.read(8<<20),b''):h.update(chunk)
 assert h.hexdigest()==manifest['files'][name]['sha256']
output.mkdir(parents=True,exist_ok=False)
def render(profile):
 build=profile['build'];q=profile['profile']['query'];stem=build+'-q'+str(q)
 binary=package/'bin'/(build+'-profile.test')
 for kind,flags in [('cpu',[]),('alloc',['-alloc_space'])]:
  command=['go','tool','pprof','-top','-nodecount=40',*flags,str(binary),str(source/(stem+'-'+kind+'.pprof'))]
  result=subprocess.run(command,capture_output=True,text=True)
  (output/(stem+'-'+kind+'-top.txt')).write_text(result.stdout+'\n'+result.stderr)
  if result.returncode:raise RuntimeError(stem+' '+kind+' pprof failed')
 command=['go','tool','pprof','-top','-cum','-nodecount=0','-nodefraction=0',str(binary),str(source/(stem+'-cpu.pprof'))]
 result=subprocess.run(command,capture_output=True,text=True)
 (output/(stem+'-cpu-cumulative.txt')).write_text(result.stdout+'\n'+result.stderr)
 if result.returncode:raise RuntimeError(stem+' cumulative CPU pprof failed')
 return {'query':q,'build':build,**profile['profile']}
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
 rows=list(pool.map(render,report['profiles']))
(output/'profile-summary.json').write_text(json.dumps(sorted(rows,key=lambda r:(r['query'],r['build'])),indent=2)+'\n')
print(json.dumps({'profiles_rendered':len(rows),'text_reports':len(rows)*3}))
