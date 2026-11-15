"""Strict offline audit for complete native known-key projection width runs."""
import argparse
import itertools
import json
import math
from pathlib import Path
import statistics

WIDTHS = ('none', 'registered', 'unregistered', 'both')
MODES = ('unmanaged', 'redis_py')


def value_bytes(i, width, columns, payload_bytes):
    # Independent encoding arithmetic; no native driver/PyArrow/harness imports.
    values = [len(str(i)), len(str(i % 16)), len(str(9007199254740993 + i)),
              len(f'row-{i:08d}'.encode()) + 1 + len('label')]
    whole = sum(values) + len(str(i))  # ordinal control field
    whole += payload_bytes * ((width in ('registered', 'both')) + (width in ('unregistered', 'both')))
    return sum(values[:columns]), whole


def summarize(data):
    assert data['status'] == 'complete' and not data['failures']
    meta = data['metadata']
    assert meta['suite'] == 'unmanaged_known_key_projection'
    assert meta['widths'] == list(WIDTHS) and meta['modes'] == list(MODES)
    assert meta['projection_columns'] == [1, 4] and meta['clients'] == [1, 4]
    assert meta['payload_bytes'] == 65536
    assert meta['tls'] and meta['protocol'] == 3 and meta['automatic_retries'] == 0
    assert len(meta['driver_source_commit']) == 40
    assert all(len(meta[k]) == 64 for k in ['driver_sha256', 'harness_sha256', 'cli_sha256'])
    assert set(meta['packages']) == {'pyarrow', 'adbc-driver-manager', 'redis', 'hiredis'}
    assert data['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}
    assert type(meta['repetitions']) is int and meta['repetitions'] >= 1
    assert type(meta['operations']) is int and meta['operations'] >= 4 and meta['operations'] % 4 == 0
    rows, operations, repetitions = meta['point_rows'], meta['operations'], meta['repetitions']
    assert rows >= operations and rows % 61 != 0
    expected = set(itertools.product(WIDTHS, MODES, (1, 4), (1, 4)))
    groups = {key: [] for key in expected}
    for trial in data['trials']:
        key = tuple(trial[name] for name in ['width', 'mode', 'projection_columns', 'clients'])
        assert key in groups
        assert trial['validated'] and trial['status'] == 'validated' and not trial.get('errors')
        assert trial['operations'] == operations and trial['fixture_rows'] == rows
        assert trial['warmup'] == (trial['repeat'] == -1)
        assert math.isfinite(trial['seconds']) and trial['seconds'] > 0
        values = trial['latency_ns']
        assert len(values) == operations and all(type(v) is int and v > 0 for v in values)
        assert trial['median_latency_ms'] == statistics.median(values) / 1e6
        assert trial['p95_latency_ms'] == sorted(values)[math.ceil(len(values) * .95) - 1] / 1e6
        assert trial['operations_per_second'] == operations / trial['seconds']
        ids = [(i * 61) % rows + 1 for i in range(operations)]
        selected, whole = zip(*(value_bytes(i, key[0], key[2], meta['payload_bytes']) for i in ids))
        assert trial['byte_oracle']['selected_value_bytes'] == sum(selected)
        assert trial['byte_oracle']['whole_hash_value_bytes'] == sum(whole)
        groups[key].append(trial)
    assert len(data['trials']) == len(expected) * (repetitions + 1)
    fixtures = data['fixtures']
    assert len(fixtures) == len(WIDTHS) and {r['width'] for r in fixtures} == set(WIDTHS)
    for fixture in fixtures:
        assert fixture['source_preserved'] and fixture['source_rows'] == rows
        assert fixture['complete_source_value_bytes'] == sum(value_bytes(i, fixture['width'], 4, meta['payload_bytes'])[1] for i in range(1, rows + 1))
    summaries = []
    for key, trials in sorted(groups.items()):
        assert sorted(t['repeat'] for t in trials) == list(range(-1, repetitions))
        measured = [t for t in trials if not t['warmup']]
        values = [v for t in measured for v in t['latency_ns']]
        summaries.append(dict(zip(['width', 'mode', 'projection_columns', 'clients'], key)) | {
            'p50_ms': statistics.median(values) / 1e6,
            'p95_ms': sorted(values)[math.ceil(len(values) * .95) - 1] / 1e6,
            'min_ms': min(values) / 1e6, 'max_ms': max(values) / 1e6,
            'median_operations_per_second': statistics.median(t['operations_per_second'] for t in measured),
            'measured_trials': len(measured), 'latency_samples': len(values),
            'byte_oracle': measured[0]['byte_oracle']})
    return summaries


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--results', type=Path, required=True)
    parser.add_argument('--out', type=Path)
    args = parser.parse_args()
    data = json.loads(args.results.read_text())
    summary = summarize(data)
    if args.out:
        with args.out.open('x') as file:
            json.dump({'metadata': data['metadata'], 'summary': summary}, file, indent=2)
            file.write('\n')
    print('PASS:', len(data['trials']), 'complete validated trials;', len(summary), 'cases; full-source and selected-value byte oracles')


if __name__ == '__main__':
    main()
