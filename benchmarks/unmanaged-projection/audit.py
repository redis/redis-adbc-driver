"""Offline audit of complete guarded/native unmanaged projection diagnostics."""
import hashlib
import json
import math
from pathlib import Path
import statistics

ROOT = Path(__file__).resolve().parent
BUNDLE = ROOT / '2026-11-14'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(name):
    return json.loads((BUNDLE / name).read_text())


def summarize(records):
    values = sorted(value for record in records for value in record['latency_ns'])
    return {'records': len(records), 'samples': len(values),
            'p50_ms': statistics.median(values) / 1e6,
            'p95_ms': values[math.ceil(len(values) * .95) - 1] / 1e6,
            'median_finite_queries_per_second': statistics.median(record['operations'] / record['seconds'] for record in records),
            'median_allocated_bytes_per_32_queries': statistics.median(record['allocated_bytes'] for record in records),
            'median_allocations_per_32_queries': statistics.median(record['allocations'] for record in records)}


def audit():
    sql = load('sql.json')
    assert sql['order'] == ['A1', 'B1', 'B2', 'A2']
    records = sql['records']
    assert len(records) == 32
    keys = set()
    for record in records:
        key = record['pass'], record['clients'], record['repeat']
        assert key not in keys
        keys.add(key)
        assert record['pass'] in sql['order'] and record['clients'] in (1, 4)
        assert record['repeat'] in (-1, 0, 1, 2)
        assert record['warmup'] == (record['repeat'] == -1) and record['validated'] is True
        assert record['build'] == ('baseline' if record['pass'].startswith('A') else 'candidate')
        assert record['fixture_rows'] == 100000 and record['matched_rows'] == 10000 and record['operations'] == 32
        assert len(record['latency_ns']) == 32 and all(type(value) is int and value > 0 for value in record['latency_ns'])
        assert math.isfinite(record['seconds']) and record['seconds'] > 0
        assert record['commands']['ft.aggregate'] == 32
        assert record['commands']['ft.cursor'] == 64
        if record['build'] == 'baseline':
            assert record['commands']['evalsha_ro'] == 320000
        else:
            assert record['commands'].get('evalsha_ro', 0) == 0
            assert record['commands']['hmget'] == 320064
        assert record['allocated_bytes'] > 0 and record['allocations'] > 0
    assert len(keys) == 4 * 2 * 4
    expected = []
    for clients in (1, 4):
        row = {'clients': clients}
        for build in ('baseline', 'candidate'):
            chosen = [record for record in records if record['build'] == build and record['clients'] == clients and not record['warmup']]
            row[build] = summarize(chosen)
            assert row[build]['records'] == 6 and row[build]['samples'] == 192
        row['p50_speedup'] = row['baseline']['p50_ms'] / row['candidate']['p50_ms']
        expected.append(row)
    assert load('comparison.json')['rows'] == expected

    retrieval = load('retrieval.json')
    assert retrieval['endpoint_cpu_usable'] is retrieval['endpoint_network_stats_usable'] is False
    assert len(retrieval['records']) == 16
    keys = set()
    for record in retrieval['records']:
        key = record['mode'], record['clients'], record['repeat']
        assert key not in keys
        keys.add(key)
        assert record['mode'] in ('guarded_projection', 'native_hgetall') and record['clients'] in (1, 4)
        assert record['repeat'] in (-1, 0, 1, 2) and record['warmup'] == (record['repeat'] == -1)
        assert record['validated'] is True and len(record['latency_ns']) == 32
        assert all(type(value) is int and value > 0 for value in record['latency_ns'])
        command = 'evalsha_ro' if record['mode'] == 'guarded_projection' else 'hgetall'
        assert record['commands'][command] == 320000
    assert len(keys) == 2 * 2 * 4

    provenance = load('provenance.json')
    assert provenance['builds_finished_before_timing'] is True
    assert provenance['post_measurement_production_changes'] is True
    assert provenance['post_measurement_change_scope']
    assert provenance['automatic_retries'] == provenance['failed_profile_records'] == provenance['discarded_profile_records'] == 0
    assert provenance['software_qpf'] == 4 and provenance['search_workers'] == 6
    assert provenance['routing'] == 'mnp' and provenance['routing_connections'] == 32
    assert provenance['memory_bytes'] == 4294967296 and provenance['eviction_policy'] == 'noeviction'
    assert provenance['tls_certificate_hostname_verified'] is True and provenance['resp'] == 3
    harness = digest(BUNDLE / 'source/sql_profile_test.go.txt')
    for build in ('baseline', 'candidate'):
        source = provenance['sources'][build]
        assert source['profile_test_sha256'] == harness
        assert source['production_file_sha256'] == digest(BUNDLE / ('source/' + build + '_projection_read.go.txt'))
        assert source['test_binary_sha256'] == source['test_binary_sha256_after']
        assert len(source['commit']) == 40 and len(source['test_binary_sha256']) == 64
    environment = load('environment.json')
    assert environment['distribution'].startswith('Redis Software')
    assert environment['server_versions']['redis_version']
    assert environment['software_node_versions'][0]['software_version']
    assert environment['server_container_cpus'] == 4
    assert environment['server_container_memory_bytes'] == 8589934592
    assert environment['qpf'] == 4 and environment['search_workers'] == 6
    assert environment['routing'] == 'mnp' and environment['routing_connections'] == 32
    assert environment['data_tls'] == 'Certificate and hostname verified' and environment['protocol'] == 3
    check = load('checks.json')['native_projection_race']
    assert check['source_commit'] == provenance['sources']['candidate']['commit']
    assert check['exit_code'] == check['skipped_top_level_tests'] == check['failed_top_level_tests'] == 0
    assert check['passed_top_level_tests'] == 23

    manifest = load('sha256.json')
    files = {str(path.relative_to(BUNDLE)) for path in BUNDLE.rglob('*') if path.is_file() and path.name != 'sha256.json'}
    assert set(manifest) == files
    for name, value in manifest.items():
        assert '..' not in Path(name).parts and not Path(name).is_absolute()
        assert digest(BUNDLE / name) == value
    print('PASS: complete 32 SQL ABBA records / 1024 queries, 16 retrieval records, exact comparisons, source/harness/binary identities, checks and', len(files), 'file hashes')


if __name__ == '__main__':
    audit()
