"""Audit the post-Arrow measurement matrix and recompute distributions."""
import itertools
import json
from pathlib import Path
import statistics
import sys

MODES = ('managed', 'unmanaged', 'redis_py')
COMMIT = '426fdb88bbff8561baf5f8f3535edfe14f172985'
SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'


def summarize(data):
    assert data['status'] in ('complete', 'complete_with_write_conflicts')
    assert data['failures'] == []
    meta = data['metadata']
    assert meta['driver_source_commit'] == COMMIT
    assert meta['source_sha256'] == SOURCE
    assert meta['driver_version'] == 'v0.0.26-dev.arrow-bench'
    assert meta['read_rows'] == 100000 and meta['write_rows'] == 10000 and meta['columns'] == 105
    assert meta['read_repetitions'] == 6 and meta['write_repetitions'] == 3
    assert meta['automatic_retries'] == 0 and meta['protocol'] == 3 and meta['tls']
    assert data['unmanaged_arrow_ingestion'] == 'Supported (empty typed append verified)'
    assert data['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                 'connections': 32, 'memory_bytes': 4294967296}
    cases = {
        'reads': [{'mode': m, 'readers': n, 'batch_rows': b}
                  for m, n, b in itertools.product(MODES, (1, 4), (512, 4096))],
        'writes': [{'mode': m, 'api': api, 'writers': n, 'operation': 'append'}
                   for n in (1, 4) for m, api in [('managed', 'sql'), ('unmanaged', 'sql'),
                   ('managed', 'arrow'), ('unmanaged', 'arrow'), ('redis_py', 'hset')]],
        'creation': ([{'mode': m, 'api': 'arrow', 'writers': 1, 'operation': 'create'} for m in ('managed', 'unmanaged')] +
                     [{'mode': m, 'api': 'arrow', 'writers': n, 'operation': 'create_append'} for n in (1, 4) for m in ('managed', 'unmanaged')] +
                     [{'mode': 'redis_py', 'api': 'hset', 'writers': n, 'operation': 'create'} for n in (1, 4)])}
    out = {}
    for suite, keys in cases.items():
        repeats = 6 if suite == 'reads' else 3
        records = data[suite]
        assert len(records) == len(keys) * (repeats + 1), (suite, len(records))
        out[suite] = []
        for key in keys:
            rows = [r for r in records if all(r[k] == v for k, v in key.items())]
            assert sorted(r['repeat'] for r in rows) == list(range(-1, repeats))
            for r in rows:
                assert r['warmup'] == (r['repeat'] < 0)
                assert r['rows'] == (100000 if suite == 'reads' else 10000) and r['columns'] == 105
                if suite != 'reads':
                    assert r['setup_in_clock'] == (suite == 'creation') and r['batch_rows'] == 1000
                    expected_index = 'membership_only' if suite == 'creation' and r['mode'] != 'managed' else 'full_field_fixture'
                    assert r['index_strategy'] == expected_index
                else:
                    assert r['tasks'] == (1 if r['readers'] == 1 else 8)
                if r['validated']:
                    assert r['seconds'] > 0
                else:
                    assert suite == 'writes' and r['mode'] == 'managed' and r['api'] == 'sql' and r['writers'] == 4
                    assert r['status'] == 'serialization_conflict' and r['seconds'] is None
                    assert r['partial_values_validated'] and 0 < r['partial_rows'] < r['rows']
                    assert r['errors'] and all(e['sqlstate'] == '40001' for e in r['errors'])
            measured = [r for r in rows if not r['warmup']]
            good = [r for r in measured if r['validated']]
            result = {**key, 'measured_repetitions': repeats, 'successful_trials': len(good),
                      'failed_trials': repeats - len(good)}
            # Do not publish a success latency when any measured trial failed.
            if len(good) == repeats:
                values = [r['seconds'] for r in good]
                result.update(median_seconds=statistics.median(values), min_seconds=min(values), max_seconds=max(values))
                if suite == 'reads':
                    assert all(0 < r['first_batch_seconds'] < r['seconds'] for r in rows)
                    result['median_first_batch_seconds'] = statistics.median(r['first_batch_seconds'] for r in measured)
            else:
                result.update(median_seconds=None, status='incomplete_workload',
                              partial_rows=[r['partial_rows'] for r in measured if not r['validated']])
            out[suite].append(result)
    assert (data['status'] == 'complete_with_write_conflicts') == any(not r['validated'] for r in data['writes'])
    return out


def main():
    p = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'post-arrow-2026-11-12'
    data = json.loads((p / 'measurements.json').read_text())
    actual = summarize(data)
    assert actual == json.loads((p / 'summary.json').read_text())
    print('PASS: 84 reads, 40 write outcomes, 32 creation trials; complete coverage, validation accounting and arithmetic')


if __name__ == '__main__':
    main()
