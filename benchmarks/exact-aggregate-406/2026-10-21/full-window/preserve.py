"""Preserve only this experiment after all its journaled services have stopped."""
import datetime,hashlib,json,pathlib,subprocess,tarfile
import boto3
root=pathlib.Path('/opt/clickbench/performance-406')
gate=json.loads((root/'window.json').read_text())
units=set(gate.get('active_job_units',[])+gate.get('job_units',[])+gate.get('profile_job_units',[])+gate.get('preflight_job_units',[])+[gate['supervisor_unit']])
audit={}
for unit in sorted(units):
 p=subprocess.run(['systemctl','show',unit,'--property=LoadState,ActiveState,SubState,MainPID,Result,ExecMainStatus'],text=True,capture_output=True)
 state=dict(line.split('=',1) for line in p.stdout.splitlines() if '=' in line)
 assert state.get('ActiveState') in ('inactive','failed') and state.get('MainPID')=='0',unit
 audit[unit]=state
# Exclude this preservation process itself; reject remaining experiment workers.
ps=subprocess.check_output(['ps','-eo','pid,args'],text=True)
owned=[line for line in ps.splitlines() if str(root) in line and any(token in line for token in ['ops/launcher.py','--worker','ops/profiles.py','ops/profile_worker.py','bin/baseline-profile.test','bin/candidate-profile.test'])]
assert not owned,'owned experiment processes still present'
(root/'inactivity-audit.json').write_text(json.dumps({'checked_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'units':audit,'owned_workers_remaining':0},indent=2)+'\n')
control=json.loads((root/'control.json').read_text())
primary=json.loads((root/'work/results.json').read_text())
profiles=json.loads((root/'profiles/results.json').read_text()) if (root/'profiles/results.json').exists() else None
# The private credential file is outside the archive; never print its value.
uri=json.loads(pathlib.Path(gate['credential_file']).read_text())['uri'].encode()
files=[]
for p in sorted(root.rglob('*')):
 if not p.is_file() or p.is_symlink():continue
 rel=p.relative_to(root)
 if rel.parts[0] in ('lib','bin') or p.name in ('run-evidence.tar.gz','report.json'):continue
 if '__pycache__' in rel.parts or 'oracle-spill' in rel.parts:continue
 if p.suffix in ('.json','.log','.py','.md','.txt'):
  assert uri not in p.read_bytes(),'credential detected in artifact'
 files.append((p,rel))
archive=root/'run-evidence.tar.gz'
with tarfile.open(archive,'w:gz') as t:
 for p,rel in files:t.add(p,arcname=str(rel),recursive=False)
bucket=gate['bucket'];prefix=gate['report_prefix'];client=boto3.client('s3',region_name='us-east-1')
def digest(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for chunk in iter(lambda:f.read(8<<20),b''):h.update(chunk)
 return h.hexdigest()
def upload(p,key):
 checksum=digest(p);client.upload_file(str(p),bucket,key,ExtraArgs={'ServerSideEncryption':'AES256','Metadata':{'sha256':checksum}})
 head=client.head_object(Bucket=bucket,Key=key)
 assert head['ContentLength']==p.stat().st_size and head['Metadata']['sha256']==checksum
 return {'key':key,'bytes':p.stat().st_size,'sha256':checksum,'version_id':head.get('VersionId')}
artifacts={}
for name,path in [('package.tar.gz',pathlib.Path('/tmp/full406-package.tar.gz')),('run-evidence.tar.gz',archive),('results.json',root/'work/results.json'),('server-settings.json',root/'server-settings.json')]:artifacts[name]=upload(path,prefix+name)
report={'status':control['status'],'recorded_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'#406 Q2/Q4/Q9/Q10; ordinary statement/internal aggregate workers; not a product ranking','rows':primary['rows'],'primary_status':primary['status'],'primary_trials':len(primary['trials']),'valid_primary_trials':sum(bool(t.get('valid')) for t in primary['trials']),'summary':primary.get('summary'),'profiles_status':profiles.get('status') if profiles else 'not_run','profile_count':len(profiles.get('profiles',[])) if profiles else 0,'failure':primary.get('failure') or control.get('failure'),'provenance':primary['provenance'],'source_sha256':primary['source_sha256'],'artifacts':artifacts,'no_active_jobs':True,'journaled_units':len(units),'backup_manifest_key':gate['backup_manifest_key'],'restore_evidence_key':gate['restore_evidence_key']}
report_path=root/'report.json';report_path.write_text(json.dumps(report,indent=2)+'\n')
record=upload(report_path,prefix+'report.json')
response=client.get_object(Bucket=bucket,Key=record['key'])
assert response.get('VersionId')==record['version_id'] and response['ContentLength']==record['bytes'] and response['Metadata']['sha256']==record['sha256']
returned=response['Body'].read()
assert hashlib.sha256(returned).hexdigest()==record['sha256']
(root/'preservation.json').write_text(json.dumps(record,indent=2)+'\n')
print(json.dumps({'preserved':True,'report':record,'primary_status':primary['status'],'primary_trials':len(primary['trials']),'valid_primary_trials':report['valid_primary_trials'],'profiles_status':report['profiles_status'],'journaled_units_inactive':len(units)}))
