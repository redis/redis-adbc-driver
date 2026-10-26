"""Derive paired full-scale summaries without dropping or selecting trials."""
import json,statistics,sys
from pathlib import Path
p=json.loads(Path(sys.argv[1]).read_text())
assert p['status']=='complete' and len(p['trials'])==24 and all(t['valid'] for t in p['trials'])
def number(value):return isinstance(value,(float,int)) and not isinstance(value,bool)
def describe(values):
 values=[v for v in values if number(v)]
 return {'n':len(values),'median':statistics.median(values),'min':min(values),'max':max(values)} if values else None
def field(t,key):
 d=t.get('execution_diagnostics',{});a=d.get('aggregate_metrics',{})
 return {'seconds':t['seconds'],'client_cpu_seconds':t['client_user_cpu_seconds']+t['client_system_cpu_seconds'],
 'process_peak_rss_bytes':t['process_peak_rss_bytes'],'cgroup_peak_bytes':t.get('isolation',{}).get('trial_memory_peak_bytes'),
 'native_execute_allocated_bytes':d.get('process_allocated_bytes_during_execute'),'redis_commands':d.get('redis_commands'),
 'connection_resp_bytes':d.get('connection_read_bytes',0)+d.get('connection_written_bytes',0),
 'server_cpu_seconds':t['server_delta']['cpu_seconds'],'server_commands':t['server_delta']['commands'],
 'server_request_reply_bytes':t['server_delta']['request_bytes']+t['server_delta']['reply_bytes'],
 'max_primary_sampled_rss_bytes':t['max_primary_sampled_rss_bytes'],
 'max_primary_sampled_used_bytes':t['max_primary_sampled_used_bytes'],
 'processed_rows':d.get('processed_rows'),'returned_tuples':a.get('returned_tuples'),
 'represented_rows':a.get('represented_rows'),'spill_written_bytes':a.get('spill_written_bytes'),
 'spill_read_bytes':a.get('spill_read_bytes'),'merge_wall_seconds':a.get('merge_wall_seconds'),
 'partition_fetch_worker_seconds':a.get('partition_fetch_worker_seconds'),'compact_tuple_worker_seconds':a.get('compact_tuple_worker_seconds'),
 'spill_sort_worker_seconds':a.get('spill_sort_worker_seconds'),'spill_write_worker_seconds':a.get('spill_write_worker_seconds'),
 'socket_read_worker_seconds':d.get('socket_read_worker_seconds'),'redis_round_trip_worker_seconds':d.get('redis_round_trip_worker_seconds')}[key]
keys=['seconds','client_cpu_seconds','process_peak_rss_bytes','cgroup_peak_bytes','native_execute_allocated_bytes','redis_commands','connection_resp_bytes','server_cpu_seconds','server_commands','server_request_reply_bytes','max_primary_sampled_rss_bytes','max_primary_sampled_used_bytes','processed_rows','returned_tuples','represented_rows','spill_written_bytes','spill_read_bytes','merge_wall_seconds','partition_fetch_worker_seconds','compact_tuple_worker_seconds','spill_sort_worker_seconds','spill_write_worker_seconds','socket_read_worker_seconds','redis_round_trip_worker_seconds']
out={'rows':p['rows'],'source_sha256':p['source_sha256'],'builds':p['binaries'],'provenance':p['provenance'],'valid_trials':24,'queries':{},'scope_notes':['All three trials included; no discarded warmups or best-of selection.','Connection/setup excluded from query elapsed; complete Arrow fetch included.','CPU is per-process CPU; server INFO includes monitor/control and cluster traffic.','Worker-duration counters overlap and must not be summed into elapsed time.','Process RSS from getrusage; cgroup MemoryPeak can be unavailable after transient unit exit.']}
for q in [2,4,9,10]:
 pair={}
 for b in ['baseline','candidate']:
  ts=[t for t in p['trials'] if t['query']==q and t['build']==b]
  assert len(ts)==3 and {t['trial'] for t in ts}=={1,2,3}
  pair[b]={'metrics':{k:describe([field(t,k) for t in ts]) for k in keys},'plans':[{'trial':t['trial'],'phase':t.get('execution_diagnostics',{}).get('phase'),'worker_grant':t['worker_grant'],'plan':t.get('execution_diagnostics',{}).get('plan')} for t in ts]}
 pair['speedup']=pair['baseline']['metrics']['seconds']['median']/pair['candidate']['metrics']['seconds']['median']
 pair['elapsed_reduction_percent']=100*(1-1/pair['speedup'])
 out['queries'][str(q)]=pair
Path(sys.argv[2]).write_text(json.dumps(out,indent=2)+'\n')
for q,pair in out['queries'].items():
 b=pair['baseline']['metrics'];c=pair['candidate']['metrics']
 print('Q'+q, 'seconds',b['seconds'],c['seconds'],'speedup',pair['speedup'])
 print(' CPU',b['client_cpu_seconds']['median'],'->',c['client_cpu_seconds']['median'],'alloc GB',b['native_execute_allocated_bytes']['median']/1e9,'->',c['native_execute_allocated_bytes']['median']/1e9)
