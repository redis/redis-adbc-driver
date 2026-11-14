"""Offline audit of the complete point-only ABBA pilot; never invokes Redis."""
import hashlib
import itertools
import json
import math
from pathlib import Path
import statistics

D = Path(__file__).parent / '2026-11-14'
WINDOWS = ('1-baseline', '2-candidate', '3-candidate', '4-baseline')
KEYS = set(itertools.product(('managed', 'unmanaged', 'redis_py'), (1, 4)))
SETTINGS = {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}


def read(name):
    return json.loads((D / name).read_text())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def p95(values):
    return sorted(values)[math.ceil(len(values) * .95) - 1] / 1e6


def expected(data):
    rows = []
    for mode, clients in itertools.product(('managed', 'unmanaged', 'redis_py'), (1, 4)):
        groups = {k: [r for n in WINDOWS if k in n for r in data[n]['trials']
                      if r['mode'] == mode and r['clients'] == clients and not r['warmup']]
                  for k in ('baseline', 'candidate')}
        samples = {k: [v for r in rs for v in r['latency_ns']] for k, rs in groups.items()}
        assert len(samples['baseline']) == len(samples['candidate']) == 192
        row = {'mode': mode, 'clients': clients, 'samples_per_build': 192,
               'baseline_p50_ms': statistics.median(samples['baseline']) / 1e6,
               'candidate_p50_ms': statistics.median(samples['candidate']) / 1e6,
               'baseline_p95_ms': p95(samples['baseline']), 'candidate_p95_ms': p95(samples['candidate']),
               'baseline_operations_per_second': statistics.median(r['operations_per_second'] for r in groups['baseline']),
               'candidate_operations_per_second': statistics.median(r['operations_per_second'] for r in groups['candidate']),
               'window_p50_ms': [statistics.median([v for r in data[n]['trials']
                                   if r['mode'] == mode and r['clients'] == clients and not r['warmup']
                                   for v in r['latency_ns']]) / 1e6 for n in WINDOWS]}
        row['p50_speedup'] = row['baseline_p50_ms'] / row['candidate_p50_ms']
        rows.append(row)
    return rows


def main():
    provenance = read('provenance.json')
    assert provenance['measured_windows'] == list(WINDOWS)
    assert provenance['automatic_retries'] == provenance['failed_records'] == provenance['discarded_records'] == 0
    assert provenance['post_measurement_production_changes'][0]['commit'] == 'a623a94f'
    harness = {n: sha(D / 'harness' / n) for n in ('run.py', 'scenarios.py')}
    data = {n: read(n + '.json') for n in WINDOWS}
    for name, d in data.items():
        assert d['status'] == 'complete' and not d['failures'] and len(d['trials']) == 24
        build = name.split('-', 1)[1]
        identity = provenance['driver_builds'][build]
        m = d['metadata']
        assert m['driver_source_commit'] == identity['commit']
        assert m['driver_sha256'] == identity['library_sha256']
        assert m['harness_sha256'] == harness['scenarios.py'] and identity['harness_sha256'] == harness
        for key in ('library_sha256', 'cli_sha256'):
            assert provenance['build_artifacts_after_run'][build][key] == identity[key]
        assert m['suite'] == 'oltp' and m['operations'] == 32 and m['point_rows'] == 1000
        assert m['columns'] == 4 and m['repetitions'] == 3 and m['tls'] and m['protocol'] == 3
        assert m['automatic_retries'] == 0 and d['final_qpf'] == SETTINGS
        inventory = {key: [] for key in KEYS}
        for r in d['trials']:
            key = (r['mode'], r['clients'])
            assert key in inventory and r['suite'] == 'oltp' and r['operation'] == 'point_read'
            assert r['validated'] and r['status'] == 'validated' and not r.get('errors')
            assert r['warmup'] == (r['repeat'] == -1) and r['repeat'] in (-1, 0, 1, 2)
            assert r['fixture_rows'] == 1000 and r['columns'] == 4 and r['operations'] == 32
            values = r['latency_ns']
            assert len(values) == 32 and all(type(v) is int and v > 0 for v in values)
            assert r['seconds'] > 0 and math.isfinite(r['seconds'])
            assert r['median_latency_ms'] == statistics.median(values) / 1e6
            assert r['p95_latency_ms'] == p95(values)
            assert r['operations_per_second'] == 32 / r['seconds']
            inventory[key].append(r['repeat'])
        assert all(sorted(repeats) == [-1, 0, 1, 2] for repeats in inventory.values())
    for key in ('packages', 'generator', 'timing_scope', 'addressing'):
        assert all(d['metadata'][key] == data[WINDOWS[0]]['metadata'][key] for d in data.values())
    assert read('comparison.json')['rows'] == expected(data)
    check = read('checks.json')['focused-software-race']
    assert check['source_commit'] == provenance['driver_builds']['candidate']['commit']
    assert check['exit_code'] == check['failed_top_level_tests'] == 0
    assert check['passed_top_level_tests'] == 38 and check['skipped_tests'] == []
    environment = read('environment.json')
    assert environment['distribution'].startswith('Redis Software')
    assert environment['qpf'] == 4 and environment['search_workers'] == 6
    assert environment['routing'] == 'mnp' and environment['routing_connections'] == 32
    assert environment['shard_memory_bytes'] == 4294967296 and environment['eviction_policy'] == 'noeviction'
    assert environment['data_tls'] == 'Certificate and hostname verified' and environment['protocol'] == 3
    assert environment['server_container_cpus'] == 4 and environment['server_container_memory_bytes'] == 8589934592
    manifest = read('sha256.json')
    files = {str(f.relative_to(D)) for f in D.rglob('*') if f.is_file() and f.name != 'sha256.json'}
    assert set(manifest) == files
    for name, digest in manifest.items():
        assert not Path(name).is_absolute() and '..' not in Path(name).parts
        assert sha(D / name) == digest
    print('PASS: all 96 point-only ABBA records, six exact comparisons, 2304 measured observations, source/build/harness/settings/check hashes')


if __name__ == '__main__':
    main()
