"""Read-only audit of the unapplied fused Arrow input experiment."""
import hashlib
import itertools
import json
from pathlib import Path
import re
import statistics

P = Path(__file__).parent / 'direct-arrow-2026-11-13'
BASE = '88774f10cf16fc4260e6ad5077097f4de8828406'
CANDIDATE = '255e58919e7ad2327752a07cd30380968aad6798'


def load(name):
    return json.loads((P / name).read_text())


micro = load('construction-results.json')
assert micro['source'] == CANDIDATE and len(micro['samples']) == 24
parsed = []
pattern = (r'BenchmarkArrowInputConstruction/columns=(\d+)/rows=(\d+)/fused=(true|false)'
           r'-\d+\s+(\d+)\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
for m in re.finditer(pattern, (P / 'construction.txt').read_text()):
    parsed.append(dict(zip(('columns', 'rows', 'fused', 'iterations', 'ns_per_op',
                            'bytes_per_op', 'allocs_per_op'),
                           [int(m[1]), int(m[2]), m[3] == 'true', int(m[4]),
                            float(m[5]), int(m[6]), int(m[7])])))
assert parsed == micro['samples']
assert len(micro['summary']) == 4
for width, rows in itertools.product((8, 105), (63, 1024)):
    cell, = [r for r in micro['summary'] if (r['columns'], r['rows']) == (width, rows)]
    for flag, key in ((False, 'control'), (True, 'fused')):
        samples = [r for r in parsed if (r['columns'], r['rows'], r['fused']) == (width, rows, flag)]
        assert len(samples) == 3
        for metric in ('ns_per_op', 'bytes_per_op', 'allocs_per_op'):
            assert cell[key][metric] == statistics.median(r[metric] for r in samples)
    assert cell['seconds_change_pct'] == (cell['fused']['ns_per_op'] / cell['control']['ns_per_op'] - 1) * 100
    assert cell['control']['allocs_per_op'] == cell['fused']['allocs_per_op']

native = load('native-results.json')
assert native['source_baseline'] == BASE and native['source_candidate'] == CANDIDATE
assert len(native['summary']) == 8
provenance = load('provenance.json')
assert provenance['baseline_source'] == BASE and provenance['candidate_source'] == CANDIDATE
for variant, source, version in (('baseline', BASE, 'v0.1.0-dev.allocation-base'),
                                 ('candidate', CANDIDATE, 'v0.1.0-dev.direct-arrow-1')):
    data = native['variants'][variant]
    meta = data['metadata']
    assert meta['driver_source_commit'] == source and meta['driver_version'] == version
    assert meta['read_rows'] == 100000 and meta['columns'] == 105 and meta['read_repetitions'] == 6
    assert meta['protocol'] == 3 and meta['tls'] is True and meta['automatic_retries'] == 0
    assert len(data['samples']) == 56
    assert data['final_qpf'] == dict(factor=4, workers=6, routing='mnp', connections=32, memory_bytes=4294967296)
    p = provenance['native'][variant]
    assert p['source'] == source and p['version'] == version
    assert p['driver_sha256'] == meta['driver_sha256'] and p['source_data_sha256'] == meta['source_sha256']
    assert p['native_gomaxprocs'] == 'unset'
    assert hashlib.sha256((P / 'read_two_modes.py').read_bytes()).hexdigest() == p['wrapper_sha256']
    f = p['fixture']
    assert f['qpf'] == 4 and f['workers'] == 6 and f['routing'] == 'mnp' and f['connections'] == 32
    assert f['resp'] == 3 and f['tls_certificate_verified'] and f['empty']
    assert f['memory_bytes'] == 4294967296
    for row in data['samples']:
        assert row['validated'] and row['rows'] == 100000 and row['columns'] == 105
        assert row['warmup'] == (row['repeat'] < 0)
        assert row['tasks'] == (1 if row['readers'] == 1 else 8)
for mode, readers, batch in itertools.product(('managed', 'unmanaged'), (1, 4), (512, 4096)):
    cell, = [r for r in native['summary'] if (r['mode'], r['readers'], r['batch_rows']) == (mode, readers, batch)]
    for variant in ('baseline', 'candidate'):
        samples = [r for r in native['variants'][variant]['samples']
                   if (r['mode'], r['readers'], r['batch_rows']) == (mode, readers, batch)]
        assert sorted(r['repeat'] for r in samples) == list(range(-1, 6))
        for metric in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
            values = [r[metric] for r in samples if not r['warmup']]
            actual = cell[variant][metric]
            assert all(v > 0 for v in values)
            assert actual == dict(values=values, median=statistics.median(values), min=min(values), max=max(values))
    for metric in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
        assert cell[metric + '_change_pct'] == (cell['candidate'][metric]['median'] / cell['baseline'][metric]['median'] - 1) * 100
    assert cell['candidate']['peak_worker_rss_bytes']['min'] <= cell['baseline']['peak_worker_rss_bytes']['max']
    assert cell['baseline']['peak_worker_rss_bytes']['min'] <= cell['candidate']['peak_worker_rss_bytes']['max']
assert sum(r['seconds_change_pct'] > 0 for r in native['summary'] if r['mode'] == 'managed') == 3
assert all(r['seconds_change_pct'] < 0 for r in native['summary'] if r['mode'] == 'unmanaged')

checks = load('checks.json')
assert checks['source'] == CANDIDATE
for name, top, total in (('unit', 21, 65), ('race', 21, 65), ('software', 10, 33)):
    c = checks['checks'][name]
    assert c['source'] == CANDIDATE and c['exit_code'] == 0 and not c['skips'] and not c['failures']
    assert len(c['passes']) == total and sum('/' not in s for s in c['passes']) == top
assert len(checks['initial_controller_selection_attempts']) == 2
for attempt in checks['initial_controller_selection_attempts']:
    assert attempt['exit_code'] == 0 and attempt['controller_exit_code'] == 1
    assert len(attempt['skips']) == 4 and not attempt['failures']
assert checks['informational_cli_probe']['exit_code'] == 2
for name in ('micro', 'build'):
    assert checks['checks'][name]['exit_code'] == 0
loan = load('loan-return.json')
assert all(loan['software'].values())
for variant in ('baseline', 'candidate'):
    assert all(loan['native'][variant][k] for k in ('empty', 'no_indexes', 'clients_closed', 'effective_qpf_tls_resp3_reverified'))
    assert loan['native'][variant]['controller_exit_code'] == loan['native'][variant]['gate_after_exit_code'] == 0
cleanup = load('cleanup-proof.json')
assert all(cleanup[k] is True for k in (
    'owned_container_and_network_removed', 'preexisting_containers_preserved',
    'preexisting_networks_preserved', 'generated_tls_and_environment_removed',
    'private_generated_credentials_removed'))
assert cleanup['preexisting_container_count'] == 1 and cleanup['preexisting_network_count'] == 31
assert hashlib.sha256((P / 'cleanup-proof.json').read_bytes()).hexdigest() == loan['final_cleanup_proof_sha256']
assert loan['credential_redactions_required'] == 0
patch = (P / 'candidate.patch').read_bytes()
assert hashlib.sha256(patch).hexdigest() == provenance['candidate_go_patch_sha256']
for name in ('go/scan_arrow.go', 'go/scan_typed.go', 'go/scan_input_test.go'):
    assert ('diff --git a/' + name + ' b/' + name).encode() in patch
for path, expected in load('sha256.json').items():
    assert hashlib.sha256((P / path).read_bytes()).hexdigest() == expected, path
print('PASS: all24 construction/all112 native samples and eight cells, checks/attempts/source hashes; candidate remains unapplied')
