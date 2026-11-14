"""Read-only audit of the separately scoped unmanaged Arrow input experiment."""
from collections import Counter
import hashlib
import itertools
import json
import math
from pathlib import Path
import re
import statistics

P = Path(__file__).parent/'unmanaged-arrow-input-2026-11-13'
BASE = '06c76c0b68258862d091487c3eb280b961f4fb8d'
CANDIDATE = '67264020f524f62a431ef00585a218c6a63660f5'
SUPPLEMENT = '1e112158028d60757c2555f11d8376d206da9332'


def load(name):
    return json.loads((P/name).read_text())


native = load('native-results.json')
provenance = load('provenance.json')
assert native['baseline_source'] == provenance['baseline_source'] == BASE
assert native['candidate_source'] == provenance['candidate_source'] == CANDIDATE
assert provenance['supplement_source'] == SUPPLEMENT
cases = {(m, n, b, r) for m, n, b, r in itertools.product(
    ('managed', 'unmanaged'), (1, 4), (512, 4096), range(-1, 6))}
for variant, source, version in (('baseline', BASE, 'v0.1.0-dev.scoped-base'),
                                 ('candidate', CANDIDATE, 'v0.1.0-dev.unmanaged-arrow-1')):
    data = native['variants'][variant]
    meta = data['metadata']
    assert data['status'] == 'complete' and not data['failures']
    assert len(data['reads']) == 56 and not data['writes'] and not data['creation']
    assert Counter((r['mode'], r['readers'], r['batch_rows'], r['repeat'])
                   for r in data['reads']) == Counter({k: 1 for k in cases})
    assert meta['driver_source_commit'] == source and meta['driver_version'] == version
    assert meta['source_sha256'] == 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
    assert meta['read_repetitions'] == 6 and meta['protocol'] == 3 and meta['tls'] is True
    assert meta['automatic_retries'] == 0
    assert data['final_qpf'] == dict(factor=4, workers=6, routing='mnp', connections=32, memory_bytes=4294967296)
    p = provenance['native'][variant]
    assert p['source'] == source and p['version'] == version
    assert p['driver_sha256'] == meta['driver_sha256'] and p['source_data_sha256'] == meta['source_sha256']
    assert p['native_gomaxprocs'] == 'unset'
    assert hashlib.sha256((P/'read_two_modes.py').read_bytes()).hexdigest() == p['wrapper_sha256']
    assert p['fixture'] == dict(qpf=4, workers=6, routing='mnp', connections=32,
                                memory_bytes=4294967296, tls_certificate_verified=True, resp=3, empty=True)
    for row in data['reads']:
        assert row['validated'] and row['rows'] == 100000 and row['columns'] == 105
        assert row['warmup'] is (row['repeat'] == -1)
        assert row['tasks'] == (1 if row['readers'] == 1 else 8)
        assert 0 < row['first_batch_seconds'] < row['seconds'] and row['peak_worker_rss_bytes'] > 0

assert len(native['summary']) == 8
failures, ratios = [], []
for mode, readers, batch in itertools.product(('managed', 'unmanaged'), (1, 4), (512, 4096)):
    cell, = [c for c in native['summary'] if (c['mode'], c['readers'], c['batch_rows']) == (mode, readers, batch)]
    for variant in ('baseline', 'candidate'):
        rows = [r for r in native['variants'][variant]['reads'] if not r['warmup'] and
                (r['mode'], r['readers'], r['batch_rows']) == (mode, readers, batch)]
        for metric in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
            values = [r[metric] for r in rows]
            assert cell[variant][metric] == dict(values=values, median=statistics.median(values), min=min(values), max=max(values))
    for metric in ('seconds', 'first_batch_seconds', 'peak_worker_rss_bytes'):
        assert cell[metric+'_change_pct'] == (cell['candidate'][metric]['median']/cell['baseline'][metric]['median']-1)*100
    if mode == 'unmanaged':
        ratios.append(cell['baseline']['seconds']['median']/cell['candidate']['seconds']['median'])
        if cell['seconds_change_pct'] >= 0:
            failures.append('unmanaged_median_not_lower')
    if mode == 'managed' and cell['seconds_change_pct'] > 3 and cell['candidate']['seconds']['min'] > cell['baseline']['seconds']['max']:
        failures.append('managed_clear_elapsed_regression')
    if cell['peak_worker_rss_bytes_change_pct'] > 1 and cell['candidate']['peak_worker_rss_bytes']['min'] > cell['baseline']['peak_worker_rss_bytes']['max']:
        failures.append('clear_RSS_regression')
geo = math.prod(ratios)**.25-1
if geo < .02:
    failures.append('geometric_mean_below_2pct')
assert native['gate'] == dict(accepted=not failures, failures=failures,
                               unmanaged_geometric_mean_speedup=geo, all_four_unmanaged_medians_lower=True)
assert not failures

micro = load('construction-results.json')
assert micro['source'] == CANDIDATE and len(micro['samples']) == 24
pattern = (r'BenchmarkUnmanagedArrowInputConstruction/columns=(\d+)/rows=(\d+)/fused=(true|false)'
           r'-\d+\s+(\d+)\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
parsed = [dict(zip(('columns', 'rows', 'fused', 'iterations', 'ns_per_op', 'bytes_per_op', 'allocs_per_op'),
                   [int(m[1]), int(m[2]), m[3] == 'true', int(m[4]), float(m[5]), int(m[6]), int(m[7])]))
          for m in re.finditer(pattern, (P/'construction.txt').read_text())]
assert parsed == micro['samples'] and len(micro['summary']) == 4
for width, rows in itertools.product((8, 105), (63, 1024)):
    c, = [r for r in micro['summary'] if (r['columns'], r['rows']) == (width, rows)]
    for flag, key in ((False, 'control'), (True, 'fused')):
        samples = [r for r in parsed if (r['columns'], r['rows'], r['fused']) == (width, rows, flag)]
        assert len(samples) == 3
        for metric in ('ns_per_op', 'bytes_per_op', 'allocs_per_op'):
            assert c[key][metric] == statistics.median(r[metric] for r in samples)
    assert c['seconds_change_pct'] == (c['fused']['ns_per_op']/c['control']['ns_per_op']-1)*100
    assert c['fused']['allocs_per_op']-c['control']['allocs_per_op'] == (106 if width == 105 else 0)

checks = load('checks.json')
assert checks['measured_source'] == CANDIDATE and checks['supplement_source'] == SUPPLEMENT
for name, source, count, tops in (('unit', CANDIDATE, 70, 24), ('race', CANDIDATE, 70, 24),
                                 ('software', CANDIDATE, 27, 10), ('actual_nul_unit', SUPPLEMENT, 1, 1),
                                 ('actual_nul_race', SUPPLEMENT, 1, 1), ('actual_nul_software', SUPPLEMENT, 1, 1)):
    c = checks['checks'][name]
    assert c['source'] == source and c['exit_code'] == c['skip'] == c['fail'] == 0
    assert c['pass'] == count and len(c['cases']) == count
    assert sum('/' not in r['test'] for r in c['cases']) == tops
    assert all(r['action'] == 'pass' for r in c['cases'])
    if source == SUPPLEMENT:
        assert c['production_identical_to'] == CANDIDATE and c['production_file_count'] == 270
for name in ('micro', 'build'):
    assert checks['checks'][name]['source'] == CANDIDATE and checks['checks'][name]['exit_code'] == 0
identity = load('production-identity.json')
assert identity['measured_source'] == CANDIDATE and identity['supplement_source'] == SUPPLEMENT
assert identity['non_test_Go_and_module_files'] == len(identity['sha256']) == 270 and identity['all_unchanged']
assert identity['sha256']['go/scan_typed.go'] == provenance['frozen']['source_files']['go/scan_typed.go']
loan = load('loan-return.json')
assert all(loan[k] is True for k in ('empty', 'index_free', 'all_clients_processes_ended', 'effective_qpf_tls_resp3_verified'))
assert loan['native_trials_validated'] == 112 and loan['supplemental_source'] == SUPPLEMENT
for name, expected in load('sha256.json').items():
    assert hashlib.sha256((P/name).read_bytes()).hexdigest() == expected, name
print('PASS: all112 native/all8 cells and gate, all24 micro samples, 167+3 correctness pass events and production identity')
