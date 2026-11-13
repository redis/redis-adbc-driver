"""Audit the complete unmanaged string-sizing native and factored results."""
import itertools
import hashlib
import json
from pathlib import Path
import statistics
import sys

BASELINE='5f759300c12f5191a36d2f2b5a29555ef14ef486'
CANDIDATE='16e3d7b17ef340199a76d0d46980440d88280e4f'
SOURCE='a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
VERSIONS={'baseline':'v0.1.0-dev.merged-perf-2','candidate':'v0.1.0-dev.string-sizing'}
PAIRS=[('unmanaged','sql'),('unmanaged','arrow'),('redis_py','hset')]
QPF={'factor':4,'workers':6,'routing':'mnp','connections':32,'memory_bytes':4294967296}
PHASES=[('combined','baseline'),('combined','sizing_only'),('combined','v1'),('construction','baseline_shared_v2'),('construction','v1'),('sizing','baseline'),('sizing','fast_shared_v1_v2')]


def summarize(data):
    assert data['baseline_source']==BASELINE and data['candidate_source']==CANDIDATE
    assert data['source_sha256']==SOURCE
    summary={}
    for label,commit in [('baseline',BASELINE),('candidate',CANDIDATE)]:
        result=data[label];meta=result['metadata']
        assert result['status']=='complete' and not result['failures']
        assert not result['reads'] and not result['creation'] and len(result['writes'])==24
        assert meta['driver_source_commit']==commit and meta['driver_version']==VERSIONS[label]
        assert meta['source_sha256']==SOURCE and meta['protocol']==3 and meta['tls'] and meta['automatic_retries']==0
        assert meta['write_rows']==10000 and meta['columns']==105 and meta['write_repetitions']==3 and meta['sql_batch_rows']==1000
        assert result['final_qpf']==QPF
        summary[label]=[]
        for (mode,api),writers in itertools.product(PAIRS,(1,4)):
            rows=[r for r in result['writes'] if r['mode']==mode and r['api']==api and r['writers']==writers]
            assert sorted(r['repeat'] for r in rows)==[-1,0,1,2]
            for row in rows:
                assert row['validated'] and row['status']=='validated' and not row.get('errors')
                assert row['warmup']==(row['repeat']==-1)
                assert row['rows']==10000 and row['columns']==105 and row['batch_rows']==1000
                assert row['operation']=='append' and not row['setup_in_clock'] and row['index_strategy']=='full_field_fixture'
                assert row['seconds']>0 and row['peak_worker_rss_bytes']>0
            measured=[r for r in rows if not r['warmup']]
            summary[label].append({'mode':mode,'api':api,'writers':writers,'measured_repetitions':3,'median_seconds':statistics.median(r['seconds'] for r in measured),'min_seconds':min(r['seconds'] for r in measured),'max_seconds':max(r['seconds'] for r in measured),'median_peak_worker_rss_bytes':statistics.median(r['peak_worker_rss_bytes'] for r in measured)})
    return summary


def audit_factor(factor):
    keys=('rows_per_operation','columns','shape','phase','variant')
    expected={(n,c,s,p,v) for n,c,s,(p,v) in itertools.product((1,256),(8,105),('dense','half_null','target95_null','one_nonnull'),PHASES)}
    assert factor['metadata']['source_commit']==CANDIDATE
    assert len(factor['samples'])==336 and len(factor['summary'])==112
    assert {tuple(r[k] for k in keys) for r in factor['summary']}==expected
    for record in factor['summary']:
        rows=[r for r in factor['samples'] if all(r[k]==record[k] for k in keys)];assert len(rows)==3
        for metric in ('nanoseconds_per_operation','bytes_per_operation','allocations_per_operation'):
            assert all(r[metric]>=0 for r in rows)
            assert statistics.median(r[metric] for r in rows)==record['median_'+metric]
        assert record['median_nanoseconds_per_operation']>0


def main():
    directory=Path(sys.argv[1]) if len(sys.argv)>1 else Path(__file__).parent
    native=json.loads((directory/'native-results.json').read_text())
    assert summarize(native)==native['summary']
    assert hashlib.sha256((directory/'candidate.patch').read_bytes()).hexdigest()==native['unapplied_patch_sha256']
    audit_factor(json.loads((directory/'factor-results.json').read_text()))
    print('PASS: all 48 native + 336 factor samples; complete matrices, provenance/QPF, exact outcomes and every elapsed/RSS/allocation distribution')


if __name__=='__main__':main()
