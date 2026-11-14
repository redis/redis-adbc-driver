"""Validate complete, matching scenario matrices and export before/after charts.

Offline only. Preserves each build's redis-py baseline; no workload or metric
mixing. Input evidence must retain complete typed-result and post-state checks.
"""
import argparse
import csv
import hashlib
import itertools
import json
import math
from pathlib import Path
import statistics

MODES = ('managed', 'unmanaged', 'redis_py')
OPERATIONS = {'oltp': ('point_read', 'insert', 'update', 'delete'),
              'olap': ('filtered_projection', 'filtered_count_sum', 'grouped_count_sum')}
SETTINGS = {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32,
            'memory_bytes': 4294967296}


def p95(values):
    return sorted(values)[math.ceil(len(values) * .95) - 1]


def audit(data):
    assert data['status'] == 'complete' and not data['failures']
    m = data['metadata']
    assert m['automatic_retries'] == 0 and m['tls'] and m['protocol'] == 3
    assert m['suite'] == 'all' and m['columns'] == 4 and m['repetitions'] == 3
    assert m['operations'] == 32 and m['queries'] == 8
    assert m['point_rows'] == 1000 and m['olap_rows'] == 100000
    assert data['final_qpf'] == SETTINGS
    for name, length in (('driver_source_commit', 40), ('driver_sha256', 64), ('harness_sha256', 64)):
        assert len(m[name]) == length and all(c in '0123456789abcdef' for c in m[name])
    keys = {(suite, op, mode, clients) for suite, ops in OPERATIONS.items()
            for op, mode, clients in itertools.product(ops, MODES, (1, 4))}
    groups = {key: [] for key in keys}
    for r in data['trials']:
        key = tuple(r[k] for k in ('suite', 'operation', 'mode', 'clients'))
        assert key in groups and r['validated'] and r['status'] == 'validated' and not r.get('errors')
        assert r['repeat'] in (-1, 0, 1, 2) and r['warmup'] == (r['repeat'] == -1)
        assert r['fixture_rows'] == m['point_rows' if r['suite'] == 'oltp' else 'olap_rows'] and r['columns'] == 4
        count = m['operations' if r['suite'] == 'oltp' else 'queries']
        samples = r['latency_ns']
        assert r['operations'] == count and len(samples) == count
        assert all(type(v) is int and v > 0 for v in samples)
        assert math.isfinite(r['seconds']) and r['seconds'] > 0
        assert r['median_latency_ms'] == statistics.median(samples) / 1e6
        assert r['p95_latency_ms'] == p95(samples) / 1e6
        assert r['operations_per_second'] == count / r['seconds']
        groups[key].append(r)
    result = {}
    for key, rows in groups.items():
        assert sorted(r['repeat'] for r in rows) == [-1, 0, 1, 2]
        measured = [r for r in rows if not r['warmup']]
        samples = [v for r in measured for v in r['latency_ns']]
        result[key] = {'p50_ms': statistics.median(samples) / 1e6,
                       'p95_ms': p95(samples) / 1e6,
                       'median_operations_per_second': statistics.median(r['operations_per_second'] for r in measured),
                       'samples': len(samples)}
    assert len(data['trials']) == 168
    return result


def compare(before, after):
    a, b = audit(before), audit(after)
    for key in ('harness_sha256', 'packages', 'generator', 'timing_scope', 'addressing'):
        assert before['metadata'][key] == after['metadata'][key], 'Incompatible benchmark: ' + key
    rows = []
    for key in sorted(a):
        suite, op, mode, clients = key
        baseline_key = (suite, op, 'redis_py', clients)
        r = dict(zip(('suite', 'operation', 'mode', 'clients'), key))
        r.update(unit='milliseconds', baseline_p50_ms=a[key]['p50_ms'], candidate_p50_ms=b[key]['p50_ms'],
                 baseline_p95_ms=a[key]['p95_ms'], candidate_p95_ms=b[key]['p95_ms'],
                 baseline_ops_per_second=a[key]['median_operations_per_second'],
                 candidate_ops_per_second=b[key]['median_operations_per_second'], samples_per_build=a[key]['samples'],
                 p50_speedup=a[key]['p50_ms'] / b[key]['p50_ms'],
                 baseline_elapsed_over_redis_py=a[key]['p50_ms'] / a[baseline_key]['p50_ms'],
                 candidate_elapsed_over_redis_py=b[key]['p50_ms'] / b[baseline_key]['p50_ms'])
        rows.append(r)
    return rows


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--baseline', required=True, type=Path)
    p.add_argument('--candidate', required=True, type=Path)
    p.add_argument('--out', required=True, type=Path)
    args = p.parse_args()
    paths = {'baseline': args.baseline, 'candidate': args.candidate}
    data = {k: json.loads(v.read_text()) for k, v in paths.items()}
    rows = compare(data['baseline'], data['candidate'])
    result = {'metric': 'Pooled operation latency after one warmup trial; three measured trials. p95 nearest rank.',
              'scope': 'Persistent application clients; execute plus full typed Arrow fetch or acknowledged mutation. Internal driver setup is included. Setup, independent validation and teardown excluded.',
              'settings': SETTINGS, 'source': {k: d['metadata'] for k, d in data.items()},
              'input_sha256': {k: hashlib.sha256(v.read_bytes()).hexdigest() for k, v in paths.items()},
              'rows': rows}
    args.out.mkdir(parents=True, exist_ok=False)
    (args.out / 'comparison.json').write_text(json.dumps(result, indent=2) + '\n')
    with (args.out / 'comparison.csv').open('w', newline='') as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0])); w.writeheader(); w.writerows(rows)
    print('Validated 168 baseline + 168 candidate records; exported', len(rows), 'comparisons')


if __name__ == '__main__':
    main()
