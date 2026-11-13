"""Check coverage/correctness evidence and recompute all published summaries."""
import itertools
import json
from pathlib import Path
import statistics
import sys


def summarize(data):
    assert data['status'] == 'complete_with_write_conflicts'
    assert data['failures'] == []
    assert data['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                 'connections': 32, 'memory_bytes': 4294967296}
    assert data['metadata']['driver_source_commit'] == '5c9d583a567e1f6891e6d6ef8703da6189957a04'
    assert data['metadata']['source_sha256'] == 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
    assert data['unmanaged_arrow_ingestion'] == 'NotImplemented (verified refusal)'
    output = {'reads': [], 'writes': []}
    modes = ('managed', 'unmanaged', 'redis_py')
    for suite, axis, repeats, batches in [('reads', 'readers', 6, (512, 4096)),
                                           ('writes', 'writers', 3, (None,))]:
        records = data[suite]
        assert len(records) == len(modes) * 2 * len(batches) * (repeats + 1)
        for mode, workers, batch in itertools.product(modes, (1, 4), batches):
            group = [r for r in records if r['mode'] == mode and r[axis] == workers
                     and (batch is None or r['batch_rows'] == batch)]
            assert sorted(r['repeat'] for r in group) == list(range(-1, repeats))
            assert all(r['warmup'] == (r['repeat'] < 0) for r in group)
            assert all(r['columns'] == 105 and r['rows'] == (100000 if suite == 'reads' else 10000)
                       for r in group)
            measured = [r for r in group if not r['warmup']]
            result = {'mode': mode, axis: workers, 'measured_repetitions': repeats}
            if batch is not None:
                result['batch_rows'] = batch
                assert all(r['tasks'] == (1 if workers == 1 else 8) for r in group)
            if suite == 'writes' and mode == 'managed' and workers == 4:
                for r in group:
                    assert not r['validated'] and r['seconds'] is None
                    assert r['status'] == 'serialization_conflict'
                    assert r['partial_values_validated'] and 0 < r['partial_rows'] < r['rows']
                    assert r['errors'] and all(e['sqlstate'] == '40001' for e in r['errors'])
                result.update(status='serialization_conflict', successful_trials=0,
                              median_seconds=None, partial_rows=[r['partial_rows'] for r in measured])
            else:
                assert all(r['validated'] and r['seconds'] > 0 for r in group)
                seconds = [r['seconds'] for r in measured]
                result.update(status='validated', median_seconds=statistics.median(seconds),
                              min_seconds=min(seconds), max_seconds=max(seconds))
                if suite == 'reads':
                    assert all(0 < r['first_batch_seconds'] < r['seconds'] for r in group)
                    result['median_first_batch_seconds'] = statistics.median(r['first_batch_seconds'] for r in measured)
            output[suite].append(result)
    return output


def check_controls(data):
    expected = {'v0.0.25': 'a8de1c3cce12138ae4c37cdbff1e8ed8875b3bc4',
                'v0.0.26': '5c9d583a567e1f6891e6d6ef8703da6189957a04'}
    assert set(data) == set(expected)
    for version, control in data.items():
        assert control['status'] == 'complete'
        assert control['metadata']['driver_source_commit'] == expected[version]
        assert control['metadata']['automatic_retries'] == 0
        assert len(control['trials']) == 6
        assert sorted((r['api'], r['repetition']) for r in control['trials']) == sorted(
            itertools.product(('sql_insert', 'arrow_append'), range(3)))
        for row in control['trials']:
            assert row['all_visible_values_validated']
            winners = 1 if row['api'] == 'sql_insert' else 4
            assert row['successful_writers'] == winners
            assert row['conflicting_writers'] == 4 - winners
            assert row['visible_rows'] == 25 * winners
            assert sum(o['rows'] for o in row['outcomes']) == row['visible_rows']
            assert all(o['status'] in ('success', 'conflict') for o in row['outcomes'])
            assert all(o['sqlstate'] == '40001' for o in row['outcomes'] if o['status'] == 'conflict')


def main():
    root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / '2026-11-12'
    data = json.loads((root / 'measurements.json').read_text())
    check_controls(json.loads((root / 'concurrency-controls.json').read_text()))
    assert summarize(data) == json.loads((root / 'summary.json').read_text())
    print('PASS: 84 reads, 24 write outcomes, 12 cross-version controls; complete coverage and summary arithmetic')

if __name__ == '__main__':
    main()
