#!/usr/bin/env python3
"""Audit the frozen native diagnostic inventory, identities and call shapes.

Standard library only. This cannot qualify a performance or lifetime gate.
"""
import hashlib
import json
from pathlib import Path
import statistics


def audit_record(record, harness, source, checkptr):
    assert record['source_commit'] == source
    assert record['status'] == 'passed' and not record['race'] and record['checkptr'] == checkptr
    assert record['client']['os'] == 'Linux' and record['client']['architecture'] == 'aarch64'
    assert record['server'] == {'distribution': 'Redis Software', 'redis_version': '8.8.0', 'shards': 3}
    assert record['initial_gate'] == record['final_gate'] == {
        'active_qpf': 4, 'search_workers': 6, 'sched_policy': 'mnp', 'conns': 32, 'noeviction': True}
    for key in ('complete_typed_fixture_validated', 'unmanaged_source_unchanged', 'owned_objects_removed'):
        assert record[key] is True
    for name, key in (('native_proofs.py', 'harness_sha256'), ('native_proofs_build.py', 'builder_sha256')):
        assert hashlib.sha256((harness/name).read_bytes()).hexdigest() == record[key], name
    assert len(record['samples']) == 66
    for mode, commands in (('managed', 19), ('unmanaged', 18)):
        samples = [s for s in record['samples'] if s['mode'] == mode]
        assert sorted(s['sample'] for s in samples) == list(range(-1, 32))
        for sample in samples:
            assert sample['warmup'] == (sample['sample'] == -1)
            assert sample['complete_typed_row_validated'] is True
            assert sample['inflight_at_end'] == sample['foreground_inflight_at_end'] == sample['dropped_calls'] == 0
            foreground = [c for c in sample['calls'] if c['foreground']]
            unmarked = [c for c in sample['calls'] if not c['foreground']]
            assert [c['categories'] for c in unmarked] == [['control_release']]
            assert [c['categories'] for c in foreground] == [
                ['control_acquire'], ['hget', 'zcard', 'metadata_root_hlen', 'control_valid'],
                ['metadata_root_hgetall'],
                ['hmget', 'metadata_bucket_hlen', 'metadata_directory_zcard', 'metadata_bucket_hget'],
                ['hmget', 'exists', 'metadata_object_get'],
                ['ft.info', 'zcard'] if mode == 'managed' else ['ft.info'],
                ['control_valid'], ['hmget'], ['control_valid'], ['control_valid']]
            assert sum(len(c['categories']) for c in foreground) == commands
            for call in sample['calls']:
                assert not call['error'] and call['start_ns'] >= 0 and call['elapsed_ns'] >= 0
                assert call['start_ns']+call['elapsed_ns'] <= sample['window_ns']
        measured = [s for s in samples if not s['warmup']]
        print(('checkptr' if checkptr else 'normal'), mode, '32 measured + 1 warmup; 10 context calls;', commands, 'commands;',
              'instrumented execute p50 ms:', round(statistics.median(s['execute_ns']/1e6 for s in measured), 4))


def main():
    here = Path(__file__).resolve().parent
    harness = here.parent.parent
    data = (here/'observations.json').read_bytes()
    record = json.loads(data)
    attempts = json.loads((here/'attempts.json').read_text())
    audit_record(record, harness, '55da25218f3606f2f8fb04d74f527903870805a4', False)
    post_data = (here/'postmerge-checkptr.json').read_bytes()
    post = json.loads(post_data)
    audit_record(post, harness, '292ee2544fa6d48baec7091eab9e2cc28f385a3d', True)
    assert [a['attempt'] for a in attempts['attempts']] == [1, 2, 3, 4]
    assert hashlib.sha256(data).hexdigest() == attempts['published_observations_sha256']
    assert attempts['attempts'][1]['race_status'] == 'runtime_initialization_failed'
    assert attempts['attempts'][3]['checkptr_status'] == 'aborted_before_native_samples'
    assert hashlib.sha256(post_data).hexdigest() == attempts['postmerge_published_observations_sha256']
    print('Complete native diagnostic audited; no performance or safety acceptance inferred.')


if __name__ == '__main__':
    main()
