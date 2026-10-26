"""Read-only Go profiling helper; credentials arrive through the isolated stdin."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit, urlunsplit, parse_qsl, urlencode

job=json.loads(Path(sys.argv[1]).read_text())
uri=json.load(sys.stdin)['uri']
parts=urlsplit(uri);query=dict(parse_qsl(parts.query))
query.update(protocol='3',read_timeout='20m',write_timeout='20m')
uri=urlunsplit(parts._replace(query=urlencode(query)))
base=Path(job['stem'])
env=dict(os.environ)
env.update(REDIS_URI=uri, REDIS_406_PROFILE_SCHEMA=job['schema'],
           REDIS_406_PROFILE_QUERY=str(job['query']), REDIS_406_PROFILE_WORKERS=str(job['workers']),
           REDIS_406_PROFILE_DIAGNOSTICS=str(base)+'-diagnostics.json')
command=[job['binary'],'-test.run','^TestPerformance406Profile$','-test.count=1','-test.timeout=20m','-test.v',
         '-test.cpuprofile',str(base)+'-cpu.pprof','-test.memprofile',str(base)+'-alloc.pprof']
result={'query':job['query'],'status':'failed'}
try:
    completed=subprocess.run(command,env=env,text=True,capture_output=True,timeout=job['timeout'])
    raw=completed.stdout+'\n'+completed.stderr
    for line in raw.splitlines():
        if 'allocated_bytes' in line and '{' in line:
            result['profile']=json.loads(line[line.index('{'):])
    # Retain complete diagnostic text while removing any URI/credential echoes.
    raw=raw.replace(uri,'[redacted connection URI]')
    parsed=urlsplit(uri)
    for value in (parsed.username,parsed.password):
        if value:
            raw=raw.replace(value,'[redacted]').replace(unquote(value),'[redacted]')
    raw=re.sub(r'\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s\"\'<>]+','[redacted URL]',raw)
    Path(str(base)+'-profile.log').write_text(raw)
    result['exit_code']=completed.returncode
    if completed.returncode==0 and 'profile' in result:result['status']='complete'
except BaseException as error:
    result['error_type']=type(error).__name__
finally:
    Path(job['output']).write_text(json.dumps(result,indent=2)+'\n')
