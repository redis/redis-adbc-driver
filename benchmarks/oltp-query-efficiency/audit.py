"""Audit the archived before/after matrices, charts, provenance and cleanup."""
import csv
import hashlib
import json
from pathlib import Path

from compare import compare


def main():
    root = Path(__file__).resolve().parent
    bundle = root / '2026-11-13'
    read = lambda name: json.loads((bundle / name).read_text())
    before, after, charts = read('baseline.json'), read('candidate.json'), read('comparison.json')
    expected = compare(before, after)
    assert charts['rows'] == expected and len(expected) == 42
    for name, filename in (('baseline', 'baseline.json'), ('candidate', 'candidate.json')):
        assert charts['input_sha256'][name] == hashlib.sha256((bundle / filename).read_bytes()).hexdigest()
    with (bundle / 'comparison.csv').open(newline='') as f:
        reader = csv.DictReader(f)
        assert reader.fieldnames == list(expected[0])
        assert list(reader) == [{k: str(v) for k, v in row.items()} for row in expected]
    provenance, cleanup = read('provenance.json'), read('cleanup.json')
    assert provenance['compare_source_sha256'] == hashlib.sha256((root / 'compare.py').read_bytes()).hexdigest()
    for build, data in (('baseline', before), ('candidate', after)):
        identity, metadata = provenance['driver_builds'][build], data['metadata']
        assert identity['commit'] == metadata['driver_source_commit']
        assert identity['library_sha256'] == metadata['driver_sha256']
        assert identity['harness_sha256']['scenarios.py'] == metadata['harness_sha256']
    assert provenance['library_sha256_after_run'] == provenance['driver_builds']['candidate']['library_sha256']
    assert provenance['cli_sha256_after_run'] == provenance['driver_builds']['candidate']['cli_sha256']
    assert provenance['post_measurement_production_changes'] is False
    assert all(cleanup[k] for k in ('owned_container_and_network_removed', 'preexisting_containers_preserved',
               'preexisting_networks_preserved', 'generated_environment_removed', 'preexisting_tls_preserved',
               'private_generated_credentials_removed'))
    assert cleanup['tls_generated_by_this_task'] is False
    checks = read('checks.json')
    assert all(checks[k]['exit_code'] == checks[k]['failed_top_level_tests'] == 0 for k in
               ('ordinary-software', 'integrated-race', 'final-race', 'snapshot-software', 'unmanaged-aggregate-profile'))
    for k in ('ordinary-software', 'integrated-race', 'final-race', 'snapshot-software', 'unmanaged-aggregate-profile'):
        assert checks[k]['skipped_top_level_tests'] == len(checks[k]['skipped_tests'])
    assert len(read('statement-profile.json')['samples']) == 20
    attribution = read('unmanaged-aggregate-profile.json')
    assert len(attribution['samples']) == 8 and len(attribution['planned_phases']) == 2
    for sample in attribution['samples']:
        assert sample['validated'] and sample['fixture_rows'] == 100000 and sample['public_columns'] == 4
        assert sample['plan']['kind'] == 'typed_aggregate_inputs' and sample['plan']['live_unmanaged']
    for phase in attribution['planned_phases']:
        assert phase['validated']
    for name, digest in read('sha256.json').items():
        assert hashlib.sha256((bundle / name).read_bytes()).hexdigest() == digest, name
    print('PASS: 168 baseline + 168 candidate records, 42 JSON/CSV comparisons, profiles, provenance, checks and cleanup')


if __name__ == '__main__':
    main()
