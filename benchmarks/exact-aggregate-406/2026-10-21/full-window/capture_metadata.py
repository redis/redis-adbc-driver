"""Read only the benchmark host and an allowlist of Redis configuration."""
import datetime
import importlib.util
import json
import os
from pathlib import Path
import platform
import shutil
import sys
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

config=json.loads(Path(sys.argv[1]).read_text())
spec=importlib.util.spec_from_file_location('aggregate406',config['runner'])
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
uri=json.loads(Path(config['credential_file']).read_text())['uri']
parts=urlsplit(uri);query=dict(parse_qsl(parts.query));query['protocol']='3'
uri=urlunsplit(parts._replace(query=urlencode(query)))

def plain(value):
    if isinstance(value,bytes):return value.decode('utf-8')
    if isinstance(value,dict):return {plain(k):plain(v) for k,v in value.items()}
    if isinstance(value,(list,tuple)):return [plain(v) for v in value]
    return value

info_keys=['redis_version','redis_mode','os','arch_bits','io_threads_active',
           'used_memory','used_memory_rss','maxmemory','maxmemory_policy',
           'evicted_keys','rdb_bgsave_in_progress','aof_enabled','loading','cluster_enabled']
patterns=['maxmemory','maxmemory-policy','save','appendonly','io-threads',
          'io-threads-do-reads','search-*']
stats=module.ServerStats(uri)
try:
    memory={}
    if Path('/proc/meminfo').exists():
        for line in Path('/proc/meminfo').read_text().splitlines():
            key,value=line.split(':',1)
            if key in ('MemTotal','MemAvailable'):memory[key]=int(value.split()[0])*1024
    report={'captured_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'platform':platform.platform(),'cpus':os.cpu_count(),'memory_bytes':memory,
            'disk':dict(zip(('total','used','free'),shutil.disk_usage(config['root']))),
            'scope':'Read-only preflight; allowlisted configuration, no connection credentials',
            'primaries':[]}
    for node,client,modules in zip(stats.nodes,stats.clients,stats.modules):
        info=client.info()
        settings=plain(client.execute_command('CONFIG','GET',*patterns))
        if isinstance(settings,list):settings=dict(zip(settings[::2],settings[1::2]))
        report['primaries'].append({'endpoint':node,'modules':modules,
                                   'info':{k:info.get(k) for k in info_keys},
                                   'configuration':settings})
    module.save(config['metadata_output'],report)
    print(json.dumps({'metadata_saved':True,'primaries':len(report['primaries']),
                      'cpus':report['cpus'],'memory_bytes':memory}),flush=True)
except Exception as error:
    print(json.dumps({'metadata_saved':False,'error_type':type(error).__name__}),flush=True)
    sys.exit(1)
finally:
    stats.close()
