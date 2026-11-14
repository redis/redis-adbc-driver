"""Audit retained compaction proof profiles and separate release attribution."""
from pathlib import Path
import json
import statistics
import sys

BASE='24f654b76b98f3d8960af94a3ca0230185d467a1'
PROFILE='263bdc1c2585cdce74dd87f92a0bfc9dce0d9ad4'
EMPTY='197bc1e6ee5b264c58e16618f8f529bac95b7133'

def audit(directory):
    d=json.loads((directory/'results.json').read_text())
    assert d['status']=='complete' and d['base_commit']==BASE and d['measured_commit']==PROFILE
    assert not d['production_changed'] and len(d['production_source_sha256'])==269
    e=d['environment']
    assert (e['qpf'],e['workers'],e['routing'],e['connections'],e['memory_bytes'])==(4,6,'mnp',32,4294967296)
    assert e['tls_certificate_verified'] and e['resp']==3 and e['container_cpus']==4 and e['container_memory_bytes']==8589934592
    assert 'go1.27.3' in d['go_toolchain'] and d['gomaxprocs']==4
    assert len(d['stages'])==4 and all(x['exit_code']==0 for x in d['stages'])
    assert len(d['profiles'])==6 and d['measurement_attempts']=={'profile_subprocesses':3,'profile_cases':6,'failed':0,'restarted':0,'discarded':0}
    summary=[]
    for paced in (True,False):
        rows=[x for x in d['profiles'] if x['paced']==paced]
        assert sorted(x['repeat'] for x in rows)==[0,1,2]
        for x in rows:
            assert x['dense'] and x['rows']==x['unrelated_rows']==20000
            phase=x['phases']['copy'];r=phase['round_trips'];shapes=phase['pipeline_shapes']
            assert phase['steps']==314
            extra=int(paced and x['repeat']==0)
            assert sum(v['calls'] for v in r.values())==4718+extra
            assert sum(v['commands'] for v in r.values())==30681+extra
            assert r['copy_commit']['calls']==313+extra
            assert r['copy_commit']['script_header_bytes']==12520+5152*extra
            for shape in ('hget,zcard,hlen','hmget,hlen,zcard,hget','hmget,exists','ft.info,zcard,ft.info,zcard,ft.info,zcard,ft.info,zcard'):
                assert shapes[shape]==314
            assert shapes['hgetall*32']==1 and shapes['hgetall*64']==312
            assert shapes['zrange*4']==1 and shapes['zlexcount,zrangebylex,zlexcount,zrangebylex,zlexcount,zrangebylex,zlexcount,zrangebylex']==313
            assert sum(shapes.values())==sum(v['calls'] for k,v in r.items() if k.startswith('pipeline_'))
            shape_commands=0
            for shape,n in shapes.items():
                count=sum(int(part.rsplit('*',1)[1]) if '*' in part else 1 for part in shape.split(','))
                shape_commands+=count*n
            assert shape_commands==sum(v['commands'] for k,v in r.items() if k.startswith('pipeline_'))
        times=[x['elapsed_milliseconds'] for x in rows]
        release=[x['phases']['copy']['round_trips']['control_release']['milliseconds'] for x in rows]
        summary.append({'paced':paced,'samples':3,'elapsed_milliseconds':{'median':statistics.median(times),'min':min(times),'max':max(times)},'release_hook_milliseconds':{'median':statistics.median(release),'min':min(release),'max':max(release)}})
    assert d['summary']==summary
    assert d['extra_call']['cause'].startswith('Unproven:')
    empty=d['empty_release']
    assert empty['measured_commit']==EMPTY and empty['exit_code']==0 and len(empty['samples'])==64
    for arm in (True,False):
        rows=[x for x in empty['samples'] if x['delegated']==arm]
        assert sorted(x['repeat'] for x in rows)==list(range(32))
        assert all(x['success'] and x['reads']==x['writes']==1 and x['pool_misses']==x['pool_timeouts']==0 for x in rows)
        assert all(0<x['hook_nanoseconds']<=x['caller_nanoseconds'] for x in rows)
    dense=d['dense_release']
    assert dense['measured_commit']=='b0ebc65cb28a4c7c1959eb45175a72f2c3a2f13c' and dense['exit_code']==0
    profile=dense['profile'];assert profile['dense'] and not profile['paced'] and profile['rows']==profile['unrelated_rows']==20000
    copy=profile['phases']['copy'];assert copy['steps']==314
    assert sum(x['calls'] for x in copy['round_trips'].values())==4718
    assert sum(x['commands'] for x in copy['round_trips'].values())==30681
    releases=dense['releases'];assert len(releases)==317
    assert {phase:sum(x['phase']==phase for x in releases) for phase in ('copy','index','install')}=={'copy':314,'index':1,'install':2}
    assert all(x['success'] and x['reads']==x['writes']==1 and x['pool_misses']==x['pool_timeouts']==0 for x in releases)
    summary={}
    for arm in (True,False):
        rows=[x for x in empty['samples'] if x['delegated']==arm]
        summary['delegated' if arm else 'direct']={k:{'median':statistics.median(x[k] for x in rows),'min':min(x[k] for x in rows),'max':max(x[k] for x in rows)} for k in ('hook_nanoseconds','read_nanoseconds','write_nanoseconds','caller_nanoseconds')}
    rows=[x for x in releases if x['phase']=='copy']
    summary['dense_first']=rows[0]
    summary['dense_remaining']={k:{'median':statistics.median(x[k] for x in rows[1:]),'min':min(x[k] for x in rows[1:]),'max':max(x[k] for x in rows[1:]),'sum':sum(x[k] for x in rows[1:])} for k in ('hook_nanoseconds','read_nanoseconds','write_nanoseconds')}
    summary['dense_all']={k:sum(x[k] for x in rows) for k in ('hook_nanoseconds','read_nanoseconds','write_nanoseconds')}
    assert summary==d['release_summary']
    assert summary['dense_all']['read_nanoseconds']/summary['dense_all']['hook_nanoseconds']>0.998
    for run in (d,empty,dense):
        assert run['loan_return']=={'empty':True,'no_indexes':True,'clients_closed':True}
    assert all(d['owner_cleanup'][key] for key in ('owned_container_and_network_removed','preexisting_containers_preserved','preexisting_networks_preserved','generated_tls_and_environment_removed','private_generated_credentials_removed'))
    assert (d['owner_cleanup']['preexisting_container_count'],d['owner_cleanup']['preexisting_network_count'])==(1,31)
    assert d['owner_reported_privacy']=={'secret_scan_matches':0,'raw_artifact_hashes_changed':False}
    assert not d['review']['human_approval_inferred']
    print('PASS: six unchanged dense profiles, supplemental count accounting and 64 empty/317 dense release observations; all attempts retained')

if __name__=='__main__':
    audit(Path(sys.argv[1]) if len(sys.argv)>1 else Path(__file__).resolve().parents[2]/'docs/sdlc/changes/compaction-proof-roundtrips')
