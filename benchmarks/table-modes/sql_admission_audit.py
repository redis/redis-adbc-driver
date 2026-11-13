"""Audit the managed SQL admission fix's write trials and concurrency controls."""
import json
from pathlib import Path
import statistics
import sys

COMMIT = '8e178ba17f8775992ab77fc5710d1375528cd4da'
VERSION = 'v0.0.26-dev.sql-admission'
SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
QPF = {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}


def summarize(data, controls):
    assert data['status'] == controls['status'] == 'complete'
    assert data['failures'] == [] and data['reads'] == [] and data['creation'] == []
    meta = data['metadata']
    assert meta['source_sha256'] == SOURCE
    assert meta['write_rows'] == 10000 and meta['columns'] == 105
    assert meta['write_repetitions'] == 3 and meta['protocol'] == 3 and meta['tls']
    for document in (data, controls):
        m = document['metadata']
        assert m['driver_source_commit'] == COMMIT and m['driver_version'] == VERSION
        assert m['driver_sha256'] == meta['driver_sha256'] and m['automatic_retries'] == 0
        assert document['final_qpf'] == QPF
    rows = data['writes']
    assert len(rows) == 12
    summary = []
    for api, writers in [('sql', 1), ('sql', 4), ('arrow', 4)]:
        trials = [r for r in rows if r['api'] == api and r['writers'] == writers]
        assert sorted(r['repeat'] for r in trials) == [-1, 0, 1, 2]
        for r in trials:
            assert r['mode'] == 'managed' and r['operation'] == 'append'
            assert r['warmup'] == (r['repeat'] == -1)
            assert r['validated'] and r['status'] == 'validated' and not r.get('errors')
            assert r['rows'] == 10000 and r['columns'] == 105 and r['batch_rows'] == 1000
            assert not r['setup_in_clock'] and r['index_strategy'] == 'full_field_fixture'
            assert r['seconds'] > 0
        times = [r['seconds'] for r in trials if not r['warmup']]
        summary.append({'api': api, 'writers': writers, 'measured_repetitions': 3,
                        'median_seconds': statistics.median(times),
                        'min_seconds': min(times), 'max_seconds': max(times)})
    cm = controls['metadata']
    assert cm['rows'] == 100 and cm['columns'] == 105 and cm['writers'] == 4 and cm['repetitions'] == 3
    assert len(controls['trials']) == 6
    for api in ('sql_insert', 'arrow_append'):
        trials = [r for r in controls['trials'] if r['api'] == api]
        assert sorted(r['repetition'] for r in trials) == [0, 1, 2]
        for r in trials:
            assert r['successful_writers'] == 4 and r['conflicting_writers'] == 0
            assert r['visible_rows'] == 100 and r['all_visible_values_validated']
            assert len(r['outcomes']) == 4
            assert all(o == {'status': 'success', 'rows': 25} for o in r['outcomes'])
    return summary


def main():
    p = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'sql-admission-2026-11-12'
    summary = summarize(json.loads((p / 'measurements.json').read_text()),
                        json.loads((p / 'concurrency-controls.json').read_text()))
    assert summary == json.loads((p / 'summary.json').read_text())
    print('PASS: 12 complete write trials, 6 complete four-writer controls; coverage and arithmetic')


if __name__ == '__main__':
    main()
