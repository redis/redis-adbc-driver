"""Audit the complete merged-main SQL/Arrow/native comparison matrix."""

import itertools
import json
from pathlib import Path
import statistics
import sys


def summarize(data, *, source_commit='ca6858065c4cf02418d55c3aa62eb583590fe252',
              driver_version='v0.1.0-dev.merged-scoped-1'):
    assert data['status'] == 'complete' and not data['failures']
    meta = data['metadata']
    assert meta['driver_source_commit'] == source_commit
    assert meta['driver_version'] == driver_version
    assert meta['source_sha256'] == 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
    assert meta['automatic_retries'] == 0 and meta['tls'] and meta['protocol'] == 3
    assert meta['read_rows'] == 100000 and meta['write_rows'] == 10000 and meta['columns'] == 105
    assert meta['read_repetitions'] == 6 and meta['write_repetitions'] == 3 and meta['sql_batch_rows'] == 1000
    assert data['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp',
                                 'connections': 32, 'memory_bytes': 4294967296}
    result = {}
    cases = {
        'reads': ({'mode': m, 'readers': n, 'batch_rows': b}
                  for m, n, b in itertools.product(('managed', 'unmanaged', 'redis_py'), (1, 4), (512, 4096))),
        'writes': ({'mode': m, 'api': api, 'writers': n, 'operation': 'append'}
                   for (m, api), n in itertools.product(
                       [('managed', 'sql'), ('unmanaged', 'sql'), ('managed', 'arrow'),
                        ('unmanaged', 'arrow'), ('redis_py', 'hset')], (1, 4))),
        'creation': iter([{'mode': m, 'api': 'arrow', 'writers': 1, 'operation': 'create'} for m in ('managed', 'unmanaged')]
                         + [{'mode': m, 'api': 'arrow', 'writers': n, 'operation': 'create_append'} for m, n in itertools.product(('managed', 'unmanaged'), (1, 4))]
                         + [{'mode': 'redis_py', 'api': 'hset', 'writers': n, 'operation': 'create'} for n in (1, 4)]),
    }
    for suite, expected_count in [('reads', 84), ('writes', 40), ('creation', 32)]:
        assert len(data[suite]) == expected_count
        result[suite] = []
        for case in cases[suite]:
            rows = [r for r in data[suite] if all(r[k] == v for k, v in case.items())]
            repeats = list(range(-1, 6 if suite == 'reads' else 3))
            assert sorted(r['repeat'] for r in rows) == repeats
            for row in rows:
                assert row['validated'] and row['warmup'] == (row['repeat'] == -1)
                assert row['rows'] == (100000 if suite == 'reads' else 10000) and row['columns'] == 105
                assert row['seconds'] > 0 and row['peak_worker_rss_bytes'] > 0
                if suite == 'reads':
                    assert row['tasks'] == (1 if row['readers'] == 1 else 8)
                    assert 0 < row['first_batch_seconds'] < row['seconds']
                else:
                    assert row['status'] == 'validated' and not row.get('errors')
                    assert row['batch_rows'] == 1000 and row['setup_in_clock'] == (suite == 'creation')
                    assert row['index_strategy'] == ('membership_only' if suite == 'creation' and row['mode'] != 'managed' else 'full_field_fixture')
            measured = [r for r in rows if not r['warmup']]
            summary = {**case, 'samples': len(measured),
                       'median_seconds': statistics.median(r['seconds'] for r in measured),
                       'min_seconds': min(r['seconds'] for r in measured),
                       'max_seconds': max(r['seconds'] for r in measured),
                       'median_peak_worker_rss_bytes': statistics.median(r['peak_worker_rss_bytes'] for r in measured)}
            if suite == 'reads':
                summary['median_first_batch_seconds'] = statistics.median(r['first_batch_seconds'] for r in measured)
            result[suite].append(summary)
    return result


if __name__ == '__main__':
    directory = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'merged-scoped-2026-11-13'
    data = json.loads((directory / 'measurements.json').read_text())
    assert summarize(data) == json.loads((directory / 'summary.json').read_text())
    provenance = json.loads((directory / 'provenance.json').read_text())
    assert provenance['baseline_source'] == data['metadata']['driver_source_commit']
    assert provenance['driver_version'] == data['metadata']['driver_version']
    assert provenance['driver_sha256'] == data['metadata']['driver_sha256']
    assert provenance['source_sha256'] == data['metadata']['source_sha256']
    assert provenance['cpus'] == 4 and provenance['container_memory_bytes'] == 8589934592
    assert provenance['software_version'] == '8.2.6-25' and provenance['redis_version'] == '8.8.0'
    assert provenance['search_module_version'] == 80808 and provenance['native_gomaxprocs'] == 'unset'
    cleanup = json.loads((directory / 'cleanup.json').read_text())
    assert all(cleanup[k] is True for k in ('owned_container_and_network_removed', 'preexisting_containers_preserved', 'preexisting_networks_preserved', 'generated_tls_and_environment_removed', 'private_generated_credentials_removed'))
    print('PASS: all156 trials, exact matrix/repetitions, source/settings and every elapsed/RSS distribution')
