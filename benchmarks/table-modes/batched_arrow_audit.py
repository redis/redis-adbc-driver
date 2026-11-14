"""Read-only audit of all rejected batched-Arrow construction/native samples."""
import collections
import hashlib
import itertools
import json
from pathlib import Path
import re
import statistics

P = Path(__file__).parent / 'batched-arrow-2026-11-13'
BASE = '24f654b76b98f3d8960af94a3ca0230185d467a1'
CANDIDATE = '2caafbc3cef7005523d4f0eca1631c2646128d4e'


def load(name):
    return json.loads((P / name).read_text())


def close(actual, expected):
    assert abs(actual - expected) < 1e-9, (actual, expected)


micro = load('construction-results.json')
assert micro['source_commit'] == CANDIDATE and len(micro['samples']) == 24
parsed = []
pattern = (r'BenchmarkBatchedArrowConstruction/columns_(\d+)/rows_(\d+)/'
           r'batched_(true|false)-\d+\s+\d+\s+(\d+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
for match in re.finditer(pattern, (P / 'construction.txt').read_text()):
    parsed.append(dict(zip(('columns', 'rows', 'batched', 'ns_per_op', 'bytes_per_op', 'allocs_per_op'),
                           [int(match[1]), int(match[2]), match[3] == 'true',
                            int(match[4]), int(match[5]), int(match[6])])))
assert parsed == micro['samples']
assert len(micro['summary']) == 4
for columns, rows in itertools.product((8, 105), (63, 1024)):
    cell, = [s for s in micro['summary'] if (s['columns'], s['rows']) == (columns, rows)]
    for batched, label in ((False, 'scalar'), (True, 'batched')):
        samples = [s for s in parsed if (s['columns'], s['rows'], s['batched']) == (columns, rows, batched)]
        assert len(samples) == 3
        for key in ('ns_per_op', 'bytes_per_op', 'allocs_per_op'):
            assert all(s[key] > 0 for s in samples)
            close(cell[label][key], statistics.median(s[key] for s in samples))
    for key, change in (('ns_per_op', 'time_change_pct'), ('bytes_per_op', 'bytes_change_pct')):
        close(cell[change], (cell['batched'][key] / cell['scalar'][key] - 1) * 100)
    if columns == 8:
        assert cell['scalar']['bytes_per_op'] == cell['batched']['bytes_per_op']
        assert cell['scalar']['allocs_per_op'] == cell['batched']['allocs_per_op']

native = load('native-results.json')
assert native['source_baseline'] == BASE and native['source_candidate'] == CANDIDATE
assert len(native['summary']) == 8
for variant, commit in (('baseline', BASE), ('candidate', CANDIDATE)):
    data = native['variants'][variant]
    assert data['metadata']['driver_source_commit'] == commit
    assert len(data['samples']) == 56
    for row in data['samples']:
        assert row['validated'] is True and row['rows'] == 100000 and row['columns'] == 105
        assert row['warmup'] == (row['repeat'] < 0)
        assert row['tasks'] == (1 if row['readers'] == 1 else 8)
for mode, readers, batch in itertools.product(('managed', 'unmanaged'), (1, 4), (512, 4096)):
    cell, = [s for s in native['summary'] if (s['mode'], s['readers'], s['batch_rows']) == (mode, readers, batch)]
    for variant in ('baseline', 'candidate'):
        samples = [r for r in native['variants'][variant]['samples']
                   if (r['mode'], r['readers'], r['batch_rows']) == (mode, readers, batch)]
        assert sorted(r['repeat'] for r in samples) == list(range(-1, 6))
        measured = [r for r in samples if not r['warmup']]
        assert len(measured) == 6
        for key in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
            values = [r[key] for r in measured]
            assert all(v > 0 for v in values)
            actual = cell[variant][key]
            assert actual['values'] == values
            close(actual['median'], statistics.median(values))
            close(actual['min'], min(values))
            close(actual['max'], max(values))
    for key in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
        close(cell[key + '_change_pct'],
              (cell['candidate'][key]['median'] / cell['baseline'][key]['median'] - 1) * 100)
# Verify the two specifically reported disjoint RSS distributions.
for mode, readers in (('managed', 1), ('unmanaged', 4)):
    cell, = [s for s in native['summary'] if (s['mode'], s['readers'], s['batch_rows']) == (mode, readers, 4096)]
    assert cell['candidate']['peak_worker_rss_bytes']['min'] > cell['baseline']['peak_worker_rss_bytes']['max']

checks = load('checks.json')
assert checks['source_commit'] == CANDIDATE and checks['failed_attempts'] == 0
for name, top, all_cases in (('unit', 20, 58), ('race', 20, 58), ('software', 6, 21)):
    check = checks['checks'][name]
    assert check['exit_code'] == check['skipped'] == check['failed'] == 0
    assert check['top_level_passed'] == top and check['passed_including_subtests'] == all_cases
    assert len(check['cases']) == all_cases and all(c['Action'] == 'pass' for c in check['cases'])
    assert sum('/' not in c['Test'] for c in check['cases']) == top

cleanup = load('cleanup-proof.json')
assert all(cleanup[k] is True for k in (
    'owned_container_and_network_removed', 'preexisting_containers_preserved',
    'preexisting_networks_preserved', 'generated_tls_and_environment_removed',
    'private_generated_credentials_removed'))
assert cleanup['preexisting_container_count'] == 1 and cleanup['preexisting_network_count'] == 31
wrapper = load('repro-wrapper-checks.json')
assert len(wrapper['cases']) == 5 and all(c['passed'] and c['mock_only'] for c in wrapper['cases'])

provenance = load('provenance.json')
assert provenance['baseline_source'] == BASE and provenance['candidate_source'] == CANDIDATE
assert provenance['fixture']['qpf'] == 4 and provenance['fixture']['resp'] == 3
assert provenance['fixture']['tls_certificate_verified'] is True
assert provenance['native_gomaxprocs'] == 'unset'
patch = (P / 'candidate.patch').read_text()
assert '+++ b/go/scan_arrow_batch.go' in patch and '+++ b/go/scan_arrow_batch_test.go' in patch
for path, expected in load('sha256.json').items():
    assert hashlib.sha256((P / path).read_bytes()).hexdigest() == expected, path
print('Audited all 24 construction and 112 native trials, all eight cells, checks and source/evidence hashes.')
