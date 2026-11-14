"""Audit a complete archived scenario pair without changing historical defaults.

Offline only: validates 168 records/build, all 42 comparisons, CSV, frozen
harness/build provenance, checks, environment, cleanup and complete file hashes.
The older audit.py continues to audit the original November 13 bundle unchanged.
"""
import argparse
import csv
import hashlib
import json
from pathlib import Path

import compare

ROOT = Path(__file__).resolve().parent
REQUIRED = frozenset(('baseline.json', 'candidate.json', 'comparison.json',
                      'comparison.csv', 'provenance.json', 'checks.json',
                      'environment.json', 'cleanup.json', 'report.md',
                      'harness/run.py', 'harness/scenarios.py'))


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def hexadecimal(value, length):
    assert type(value) is str and len(value) == length and all(c in '0123456789abcdef' for c in value)


def safe_path(bundle, name):
    path = Path(name)
    assert type(name) is str and name and not path.is_absolute() and '..' not in path.parts
    result = bundle / path
    assert result.resolve().is_relative_to(bundle.resolve()) and result.is_file(), name
    return result


def read(bundle, name):
    return json.loads(safe_path(bundle, name).read_text())


def audit_bundle(directory, required_checks=()):
    bundle = Path(directory).resolve()
    before, after = (read(bundle, name) for name in ('baseline.json', 'candidate.json'))
    expected = compare.compare(before, after)
    assert len(expected) == 42
    actual = read(bundle, 'comparison.json')
    assert actual['rows'] == expected
    assert actual['source'] == {'baseline': before['metadata'], 'candidate': after['metadata']}
    assert actual['settings'] == compare.SETTINGS
    assert actual['metric'] == 'Pooled operation latency after one warmup trial; three measured trials. p95 nearest rank.'
    assert actual['scope'] == 'Persistent application clients; execute plus full typed Arrow fetch or acknowledged mutation. Internal driver setup is included. Setup, independent validation and teardown excluded.'
    for build in ('baseline', 'candidate'):
        assert actual['input_sha256'][build] == sha256(bundle / (build + '.json'))
    with (bundle / 'comparison.csv').open(newline='') as stream:
        rows = csv.DictReader(stream)
        assert rows.fieldnames == list(expected[0])
        assert list(rows) == [{k: str(v) for k, v in row.items()} for row in expected]

    provenance = read(bundle, 'provenance.json')
    assert provenance['compare_source_sha256'] == sha256(ROOT / 'compare.py')
    assert provenance['post_measurement_production_changes'] is False
    for key in ('automatic_retries', 'failed_native_trial_records', 'discarded_native_trial_records'):
        assert type(provenance[key]) is int and provenance[key] == 0
    assert set(provenance['driver_builds']) == set(provenance['build_artifacts_after_run']) == {'baseline', 'candidate'}
    harness = {name: sha256(safe_path(bundle, 'harness/' + name)) for name in ('run.py', 'scenarios.py')}
    for build, data in (('baseline', before), ('candidate', after)):
        identity = provenance['driver_builds'][build]
        metadata = data['metadata']
        hexadecimal(identity['commit'], 40)
        for key in ('library_sha256', 'cli_sha256'):
            hexadecimal(identity[key], 64)
            assert provenance['build_artifacts_after_run'][build][key] == identity[key]
        assert identity['commit'] == metadata['driver_source_commit']
        assert identity['library_sha256'] == metadata['driver_sha256']
        assert identity['harness_sha256'] == harness
        assert metadata['harness_sha256'] == harness['scenarios.py']
        assert type(metadata['driver_version']) is str and metadata['driver_version'].startswith('v')
        if 'driver_version' in identity:
            assert identity['driver_version'] == metadata['driver_version']

    checks = read(bundle, 'checks.json')
    assert type(checks) is dict and checks and set(required_checks) <= checks.keys()
    for name, check in checks.items():
        # Each checkpoint retains its actual source. Earlier broad checks are
        # not silently relabeled as later guarded/frozen measurement commits.
        hexadecimal(check['source_commit'], 40)
        assert check['exit_code'] == check['failed_top_level_tests'] == 0, name
        assert type(check['passed_top_level_tests']) is int and check['passed_top_level_tests'] > 0, name
        assert type(check['skipped_tests']) is list
        assert type(check['skipped_top_level_tests']) is int and check['skipped_top_level_tests'] == len(check['skipped_tests'])
        assert all(type(test) is str and test for test in check['skipped_tests'])

    environment = read(bundle, 'environment.json')
    assert environment['distribution'].startswith('Redis Software')
    assert environment['server_versions']['redis_version']
    assert any(module['name'].lower() == 'search' and module['version'] > 0 for module in environment['server_versions']['modules'])
    assert environment['software_node_versions'] and all(node['software_version'] for node in environment['software_node_versions'])
    assert environment['server_container_cpus'] == 4 and environment['server_container_memory_bytes'] == 8589934592
    assert environment['qpf'] == 4 and environment['search_workers'] == 6
    assert environment['routing'] == 'mnp' and environment['routing_connections'] == 32
    assert environment['shard_memory_bytes'] == 4294967296 and environment['eviction_policy'] == 'noeviction'
    assert environment['data_tls'] == 'Certificate and hostname verified' and environment['protocol'] == 3

    cleanup = read(bundle, 'cleanup.json')
    for key in ('owned_container_and_network_removed', 'preexisting_containers_preserved',
                'preexisting_networks_preserved', 'generated_environment_removed',
                'private_generated_credentials_removed'):
        assert cleanup[key] is True, key
    assert type(cleanup['tls_generated_by_this_task']) is bool
    if cleanup['tls_generated_by_this_task']:
        assert cleanup['generated_tls_removed'] is True
    else:
        assert cleanup['preexisting_tls_preserved'] is True

    manifest = read(bundle, 'sha256.json')
    files = {str(path.relative_to(bundle)) for path in bundle.rglob('*') if path.is_file() and path != bundle / 'sha256.json'}
    assert REQUIRED <= files and set(manifest) == files, 'Incomplete evidence hash manifest'
    for name, digest in manifest.items():
        hexadecimal(digest, 64)
        assert sha256(safe_path(bundle, name)) == digest, name
    return {'baseline_records': len(before['trials']), 'candidate_records': len(after['trials']),
            'comparison_rows': len(expected), 'checkpoints': len(checks), 'hashed_files': len(files)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bundle', type=Path, default=ROOT / '2026-11-14')
    parser.add_argument('--require-check', action='append', default=[], help='Require this named recorded checkpoint; repeat as needed.')
    args = parser.parse_args()
    result = audit_bundle(args.bundle, args.require_check)
    print('PASS: 168 baseline + 168 candidate records, 42 exact JSON/CSV comparisons, frozen harness/build identities,',
          result['checkpoints'], 'actual-source checkpoints, environment, cleanup and', result['hashed_files'], 'file hashes')


if __name__ == '__main__':
    main()
