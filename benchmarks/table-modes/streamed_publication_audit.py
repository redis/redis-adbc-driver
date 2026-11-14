"""Audit every explicit publication-unit observation and its timing summaries."""

import itertools
import json
from pathlib import Path
import statistics
import sys


def summarize(data):
    assert data['status'] == 'complete' and data['failures'] == [] and data['reads'] == []
    assert len(data['writes']) == 24
    meta = data['metadata']
    assert meta['driver_source_commit'] == '24f654b76b98f3d8960af94a3ca0230185d467a1'
    assert meta['driver_version'] == 'v0.1.0-dev.next-four-base'
    assert meta['source_sha256'] == 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
    assert meta['write_rows'] == 10000 and meta['columns'] == 105
    assert meta['automatic_retries'] == 0 and meta['protocol'] == 3 and meta['tls']
    assert meta['input_batch_rows'] == 1000 and meta['full_input_materialized']
    assert data['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                 'connections': 32, 'memory_bytes': 4294967296}
    result = []
    for writers, shape in itertools.product((1, 4), ('many_calls', 'one_stream', 'one_batch')):
        rows = [r for r in data['writes'] if r['writers'] == writers and r['shape'] == shape]
        assert sorted(r['repeat'] for r in rows) == [-1, 0, 1, 2]
        count = 10000 // writers
        for row in rows:
            assert row['validated'] and row['rows'] == 10000 and row['columns'] == 105
            assert row['mode'] == 'managed' and row['api'] == 'arrow'
            assert row['warmup'] == (row['repeat'] == -1) and row['seconds'] > 0
            assert row['peak_worker_rss_bytes'] > 0
            calls = writers * ((count + 999) // 1000) if shape == 'many_calls' else writers
            assert row['public_calls'] == calls
            assert row['input_batches'] == (writers if shape == 'one_batch' else writers * ((count + 999) // 1000))
            assert row['largest_input_batch'] == (count if shape == 'one_batch' else 1000)
        measured = [r for r in rows if not r['warmup']]
        result.append({'writers': writers, 'shape': shape, 'samples': len(measured),
                       'median_seconds': statistics.median(r['seconds'] for r in measured),
                       'min_seconds': min(r['seconds'] for r in measured),
                       'max_seconds': max(r['seconds'] for r in measured),
                       'median_peak_worker_rss_bytes': statistics.median(r['peak_worker_rss_bytes'] for r in measured)})
    return result


if __name__ == '__main__':
    directory = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'streamed-publication-2026-11-13'
    data = json.loads((directory / 'measurements.json').read_text())
    assert summarize(data) == json.loads((directory / 'summary.json').read_text())
    print('PASS: all24 trials, exact source/call/batch/repetition coverage and elapsed/RSS distributions')
