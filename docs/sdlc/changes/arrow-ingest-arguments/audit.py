"""Audit the complete native ingest-argument comparison and every distribution."""
import itertools
import hashlib
import json
from pathlib import Path
import statistics
import sys

SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
BASELINE = '5f759300c12f5191a36d2f2b5a29555ef14ef486'
VERSIONS = {'baseline': 'v0.1.0-dev.merged-perf-2', 'candidate': 'v0.1.0-dev.ingest-args'}
PAIRS = [('managed', 'sql'), ('unmanaged', 'sql'), ('managed', 'arrow'), ('unmanaged', 'arrow'), ('redis_py', 'hset')]
QPF = {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}


def summarize(data):
    assert data['baseline_source'] == BASELINE
    assert data['source_sha256'] == SOURCE
    out = {}
    for label in ('baseline', 'candidate'):
        result = data[label]
        meta = result['metadata']
        assert result['status'] == 'complete' and result['failures'] == []
        assert not result['reads'] and not result['creation']
        assert len(result['writes']) == 40
        assert meta['driver_source_commit'] == data[label + '_source']
        assert meta['driver_version'] == VERSIONS[label]
        assert meta['source_sha256'] == SOURCE
        assert meta['automatic_retries'] == 0 and meta['protocol'] == 3 and meta['tls']
        assert meta['write_rows'] == 10000 and meta['columns'] == 105
        assert meta['sql_batch_rows'] == 1000 and meta['write_repetitions'] == 3
        assert result['final_qpf'] == QPF
        out[label] = []
        for (mode, api), writers in itertools.product(PAIRS, (1, 4)):
            rows = [r for r in result['writes'] if r['mode'] == mode and r['api'] == api and r['writers'] == writers]
            assert sorted(r['repeat'] for r in rows) == [-1, 0, 1, 2]
            for row in rows:
                assert row['validated'] and row['status'] == 'validated' and not row.get('errors')
                assert row['warmup'] == (row['repeat'] == -1)
                assert row['rows'] == 10000 and row['columns'] == 105
                assert row['batch_rows'] == 1000 and row['operation'] == 'append'
                assert not row['setup_in_clock'] and row['index_strategy'] == 'full_field_fixture'
                assert row['seconds'] > 0 and row['peak_worker_rss_bytes'] > 0
            measured = [r for r in rows if not r['warmup']]
            out[label].append({'mode': mode, 'api': api, 'writers': writers, 'measured_repetitions': 3,
                               'median_seconds': statistics.median(r['seconds'] for r in measured),
                               'min_seconds': min(r['seconds'] for r in measured),
                               'max_seconds': max(r['seconds'] for r in measured),
                               'median_peak_worker_rss_bytes': statistics.median(r['peak_worker_rss_bytes'] for r in measured)})
    return out


def main():
    directory = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent
    data = json.loads((directory / 'native-results.json').read_text())
    assert summarize(data) == data['summary']
    assert hashlib.sha256((directory / 'candidate.patch').read_bytes()).hexdigest() == data['unapplied_patch_sha256']
    for filename, field, values in [('assembly-results.json', 'null_percent', (0, 50, 95)),
                                    ('single-row-results.json', 'shape', ('dense', 'one_nonnull'))]:
        micro = json.loads((directory / filename).read_text())
        expected = set(itertools.product((8, 105), values, ('managed', 'unmanaged'), ('baseline', 'candidate')))
        keys = ('columns', field, 'mode', 'variant')
        assert {tuple(r[k] for k in keys) for r in micro['summary']} == expected
        assert len(micro['samples']) == 3 * len(expected)
        for record in micro['summary']:
            rows = [r for r in micro['samples'] if all(r[k] == record[k] for k in keys)]
            assert len(rows) == 3
            for metric in ('nanoseconds_per_operation', 'bytes_per_operation', 'allocations_per_operation'):
                assert all(r[metric] > 0 for r in rows)
                assert statistics.median(r[metric] for r in rows) == record['median_' + metric]
    print('PASS: all80 native +120 isolated samples; exact matrices, complete results, source/version/input, QPF, patch hash and every elapsed/RSS/allocation distribution')


if __name__ == '__main__':
    main()
