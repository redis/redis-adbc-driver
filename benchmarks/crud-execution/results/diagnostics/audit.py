"""Reproduce diagnostic summaries from retained sanitized inputs; no Redis access."""
from collections import Counter, defaultdict
import gzip
import hashlib
import itertools
import json
import math
from pathlib import Path
import statistics

HERE = Path(__file__).resolve().parent
MODES = ('managed', 'unmanaged', 'redis_py')
OPERATIONS = ('point_read', 'insert', 'update', 'delete')


def read(name):
    data = (HERE / name).read_bytes()
    return json.loads(gzip.decompress(data) if name.endswith('.gz') else data)


def equivalent(actual, expected):
    if isinstance(expected, dict):
        assert set(actual) == set(expected)
        for key in expected:
            equivalent(actual[key], expected[key])
    elif isinstance(expected, list):
        assert len(actual) == len(expected)
        for a, e in zip(actual, expected):
            equivalent(a, e)
    elif isinstance(expected, float):
        # Permit machine rounding from median(ns)/1e6 vs median(ns/1e6).
        assert math.isclose(actual, expected, rel_tol=1e-12, abs_tol=1e-12), (actual, expected)
    else:
        assert actual == expected, (actual, expected)


def audit_go():
    data = read('go-baseline-observations.json.gz')
    assert len(data['cases']) == 12
    seen = set()
    summary = []
    for case in data['cases']:
        key = case['mode'], case['operation']
        assert key not in seen
        seen.add(key)
        assert case['complete_fixture_validated'] and not case['test_failed']
        assert [r['sample'] for r in case['samples']] == list(range(-1, 32))
        for row in case['samples']:
            assert row['validated'] and row['elapsed_ns'] > 0
            assert row['warmup'] == (row['sample'] == -1)
        rows = [r for r in case['samples'] if not r['warmup']]
        categories = Counter(c for r in rows for call in r['calls'] for c in call['categories'])
        stages = set(k for r in rows for k in r['stage_ns'])
        summary.append({
            'mode': case['mode'], 'operation': case['operation'],
            'p50_ms': statistics.median(r['elapsed_ns'] for r in rows) / 1e6,
            'hook_calls_per_op': statistics.median(len(r['calls']) for r in rows),
            'commands_per_op': statistics.median(sum(c['commands'] for c in r['calls']) for r in rows),
            'metadata_commands_per_op': sum(v for k, v in categories.items() if k.startswith('metadata')) / 32,
            'categories_per_op': {k: v / 32 for k, v in categories.items()},
            'stage_p50_ms': {k: statistics.median(r['stage_ns'][k] for r in rows) / 1e6 for k in stages},
        })
    assert seen == set(itertools.product(('managed', 'unmanaged', 'raw'), OPERATIONS))
    equivalent(read('go-baseline-summary.json'), summary)


def audit_native(prefix, nested):
    results = read(prefix + '-results.json.gz')
    observation = read(prefix + '-operations.json.gz')
    assert results['status'] == observation['status'] == 'complete'
    assert not results['failures'] and len(results['trials']) == 48
    assert observation['metadata'] == results['metadata']
    assert len(observation['records']) == 1536
    meta = results['metadata']
    assert meta['suite'] == 'oltp' and meta['repetitions'] == 1 and meta['operations'] == 32
    assert meta['tls'] and meta['protocol'] == 3
    assert results['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                    'connections': 32, 'memory_bytes': 4294967296}
    assert meta['stage_diagnostic_wrapper_sha256'] == observation['wrapper_sha256']
    expected = set(itertools.product(OPERATIONS, MODES, (1, 4), (-1, 0)))
    groups = defaultdict(list)
    for row in observation['records']:
        groups[tuple(row[k] for k in ('operation', 'mode', 'clients', 'repeat'))].append(row)
    assert set(groups) == expected
    for trial in results['trials']:
        key = tuple(trial[k] for k in ('operation', 'mode', 'clients', 'repeat'))
        assert key in expected
        expected.remove(key)
        rows = groups[key]
        assert trial['validated'] and trial['status'] == 'validated'
        assert trial['warmup'] == (trial['repeat'] == -1)
        assert len(rows) == 32 and sorted(r['index'] for r in rows) == list(range(32))
        order = [i for worker in range(trial['clients']) for i in range(worker, 32, trial['clients'])]
        latencies = dict(zip(order, trial['latency_ns']))
        assert len(latencies) == 32
        for row in rows:
            assert row['operation_completed'] and not row.get('error_type')
            assert row['warmup'] == trial['warmup']
            assert 0 < row['elapsed_ns'] <= latencies[row['index']]
            assert all(type(v) is int and v > 0 for v in row['stage_ns'].values())
            assert sum(row['stage_ns'].values()) <= row['elapsed_ns']
            assert all(type(v) is int and v > 0 for v in row.get('native_stage_ns', {}).values())
        if nested and trial['mode'] != 'redis_py':
            counts = Counter(k for r in rows for k in r.get('native_stage_ns', {}))
            assert counts['previous_result_close'] == 32 - trial['clients']
            assert counts['adbc_prepare'] == counts['adbc_execute_query'] == 32
            assert counts['arrow_read_all'] == (32 if trial['operation'] == 'point_read' else 0)
    assert not expected
    summary = []
    for mode, operation in sorted(itertools.product(MODES, OPERATIONS)):
        rows = groups[operation, mode, 1, 0]
        field = 'native_stage_ns' if nested else 'stage_ns'
        names = set(k for r in rows for k in r.get(field, {}))
        summary.append({'mode': mode, 'operation': operation, 'samples': 32,
                        'p50_ms': statistics.median(r['elapsed_ns'] for r in rows) / 1e6,
                        ('nested_stages' if nested else 'stage_p50_ms'): {
                            k: ({'samples': sum(k in r.get(field, {}) for r in rows),
                                 'p50_ms': statistics.median(r[field][k] for r in rows if k in r.get(field, {})) / 1e6}
                                if nested else statistics.median(r[field][k] for r in rows) / 1e6)
                            for k in names}})
    equivalent(read('native-nested-summary.json' if nested else 'native-baseline-summary.json'), summary)


def main():
    provenance = read('provenance.json')
    for name, digest in provenance['summary_input_sha256'].items():
        assert hashlib.sha256((HERE / name).read_bytes()).hexdigest() == digest, name
    for name, record in provenance['retained_raw_inputs'].items():
        data = (HERE / name).read_bytes()
        assert data[4:8] == bytes(4) and not (data[3] & 8)  # zero mtime, no filename
        assert hashlib.sha256(data).hexdigest() == record['published_gzip_sha256']
        assert hashlib.sha256(gzip.decompress(data)).hexdigest() == record['json_bytes_sha256']
        assert record['json_bytes_preserved_exactly']
        if record['published_bytes_identical_to_original_source']:
            assert record['original_source_sha256'] == record['published_gzip_sha256']
        elif not record['original_source_was_gzip']:
            assert record['original_source_sha256'] == record['json_bytes_sha256']
    audit_go()
    audit_native('native-baseline', False)
    audit_native('native-nested', True)
    print('PASS: all published diagnostic summaries reproduced; 396 Go + 2×1536 native operations, full grids and nested timing scopes checked')


if __name__ == '__main__':
    main()
