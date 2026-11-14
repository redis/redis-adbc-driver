"""Audit owned-string ingestion evidence without Redis, Go or Arrow imports."""
from collections import Counter
import hashlib
import json
from pathlib import Path
import statistics


BASE = '06c76c0b68258862d091487c3eb280b961f4fb8d'
CANDIDATE = '85cf660bd8bacc6ff1e2ed9d6f3289ff946bdfec'
SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'


def distribution(values):
    return dict(samples=values, median=statistics.median(values),
                minimum=min(values), maximum=max(values))


def regression(control, candidate, threshold):
    return (statistics.median(candidate) / statistics.median(control) > 1 + threshold
            and min(candidate) > max(control))


def audit():
    root = Path(__file__).resolve().parents[2]
    folder = root/'benchmarks/table-modes/ingest-strings-2026-11-13'
    read = lambda name: json.loads((folder/name).read_text())
    native, micro, summary = read('native-results.json'), read('construction-results.json'), read('summary.json')
    provenance, checks = read('provenance.json'), read('checks.json')
    expected = Counter({(mode, shape, writers, rep): 1
                        for mode in ('managed', 'unmanaged')
                        for shape in ('one_stream', 'one_batch')
                        for writers in (1, 4) for rep in (-1, 0, 1, 2)})
    assert set(native) == {'baseline', 'candidate'}
    assert provenance['baseline_commit'] == BASE and provenance['measured_candidate'] == CANDIDATE
    assert provenance['gate'] == {
        'mixed_allocation_reduction_minimum': .5,
        'micro_time_or_bytes_median_increase': .03,
        'native_time_median_increase': .03,
        'native_rss_median_increase': .01,
        'range_required_for_regression': 'candidate minimum strictly greater than baseline/control maximum'}
    for variant, source, version in [('baseline', BASE, 'v0.1.0-dev.scoped-base'),
                                      ('candidate', CANDIDATE, 'v0.1.0-dev.ingest-strings-1')]:
        run = native[variant]
        assert run['status'] == 'complete' and run['failures'] == [] and run['reads'] == []
        assert len(run['writes']) == 32
        assert Counter((r['mode'], r['shape'], r['writers'], r['repeat']) for r in run['writes']) == expected
        metadata = run['metadata']
        assert metadata['driver_source_commit'] == source and metadata['driver_version'] == version
        assert metadata['source_sha256'] == SOURCE and metadata['automatic_retries'] == 0
        assert metadata['protocol'] == 3 and metadata['tls'] is True
        assert metadata['harness_source_commit'] == CANDIDATE and metadata['write_repetitions'] == 3
        assert provenance[variant]['driver_sha256'] == metadata['driver_sha256']
        assert provenance[variant]['source_head'] == source
        assert provenance[variant]['native_gomaxprocs'] == 'unset'
        assert run['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                   'connections': 32, 'memory_bytes': 4294967296}
        for row in run['writes']:
            assert row['validated'] is True and row['rows'] == 10000 and row['columns'] == 105
            assert row['api'] == 'arrow' and row['warmup'] is (row['repeat'] == -1)
            assert row['seconds'] > 0 and row['peak_worker_rss_bytes'] > 0
            assert row['public_calls'] == row['writers']
            count = 10000 // row['writers']
            assert row['input_batches'] == (row['writers'] if row['shape'] == 'one_batch'
                                            else row['writers'] * ((count + 999) // 1000))
            assert row['largest_input_batch'] == (count if row['shape'] == 'one_batch' else 1000)
    assert len(summary['native']) == 8
    for cell in summary['native']:
        key = cell['mode'], cell['shape'], cell['writers']
        arms = {v: [r for r in run['writes'] if not r['warmup'] and
                    (r['mode'], r['shape'], r['writers']) == key] for v, run in native.items()}
        for metric, threshold in [('seconds', .03), ('peak_worker_rss_bytes', .01)]:
            values = {v: [r[metric] for r in rows] for v, rows in arms.items()}
            assert all(len(rows) == 3 for rows in values.values())
            stored = cell[metric]
            assert all(stored[v] == distribution(rows) for v, rows in values.items())
            assert stored['change_fraction'] == statistics.median(values['candidate']) / statistics.median(values['baseline']) - 1
            assert stored['gate_failed'] is regression(values['baseline'], values['candidate'], threshold)
            assert stored['gate_failed'] is False
    assert len(micro) == 48
    assert Counter((r['mixed'], r['columns'], r['rows'], r['variant']) for r in micro) == Counter({
        (mixed, width, rows, variant): 3 for mixed in (False, True)
        for width in (8, 105) for rows in (63, 1024) for variant in ('clone', 'arena')})
    assert len(summary['construction']) == 8
    for cell in summary['construction']:
        key = cell['mixed'], cell['columns'], cell['rows']
        arms = {v: [r for r in micro if (r['mixed'], r['columns'], r['rows']) == key and
                    r['variant'] == v] for v in ('clone', 'arena')}
        for metric in ('nanoseconds', 'bytes', 'allocations'):
            values = {v: [r[metric] for r in rows] for v, rows in arms.items()}
            stored = cell[metric]
            assert all(stored[v] == distribution(rows) for v, rows in values.items())
            assert stored['change_fraction'] == statistics.median(values['arena']) / statistics.median(values['clone']) - 1
            fails = (statistics.median(values['arena']) > .5 * statistics.median(values['clone'])
                     if cell['mixed'] else False) if metric == 'allocations' else regression(values['clone'], values['arena'], .03)
            assert stored['gate_failed'] is fails and not fails
    assert [c['stage'] for c in checks] == ['unit-01', 'unit-02', 'race-01', 'micro-01', 'live-01',
                                          'build-01', 'baseline-native-01', 'candidate-native-01']
    assert checks[0]['go_or_native_exit'] == checks[0]['controller']['exit_code'] == 1
    assert checks[0]['source'] == 'e780302370bdf478963d6373a7fb224d177b0406'
    assert checks[0]['controller']['completed'] is False
    for item in checks[1:]:
        assert item['controller'] == {'exit_code': 0, 'completed': True}
        assert item['go_or_native_exit'] == 0 and item['source'] == CANDIDATE
    for item, count, tops in [(checks[1], 66, 6), (checks[2], 66, 6), (checks[4], 14, 8)]:
        assert item['pass_events'] == len(item['passed_cases']) == count
        assert len(item['top_level']) == tops and item['skips'] == item['failures'] == 0
    environment = read('environment.json')
    assert environment['software_api_versions'] == ['8.2.6-25'] and environment['redis_version'] == '8.8.0'
    assert environment['modules']['search'] == 80808 and environment['container_cpus'] == 4
    assert environment['container_memory_bytes'] == 8589934592
    assert environment['fixture'] == {'qpf': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32,
                                      'memory_bytes': 4294967296, 'tls_certificate_verified': True,
                                      'resp': 3, 'empty': True}
    assert hashlib.sha256((folder/'helper.py.txt').read_bytes()).hexdigest() == provenance['baseline']['helper_sha256'] == provenance['candidate']['helper_sha256']
    for name, digest in read('sha256.json').items():
        assert hashlib.sha256((folder/name).read_bytes()).hexdigest() == digest
    cleanup = read('cleanup.json')
    assert cleanup == {
        'owned_container_and_network_removed': True,
        'preexisting_containers_preserved': True,
        'preexisting_networks_preserved': True,
        'preexisting_container_count': 1,
        'preexisting_network_count': 31,
        'generated_tls_and_environment_removed': True,
        'private_generated_credentials_removed': True}
    print('PASS: all64 native/all8 cells +48 construction samples,146 passing cases, original failed compile attempt, source/settings and original quantitative gates')


if __name__ == '__main__':
    audit()
