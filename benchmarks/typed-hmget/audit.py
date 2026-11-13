"""Audit actual nullable HMGET parser and complete driver experiment evidence."""
import hashlib
import itertools
import json
from pathlib import Path
import statistics
import sys

DIRECTORY = Path(__file__).parent / '2026-11-13'
SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
DRIVER = '3213aa548d2543b60b0c54fe73f6c7584a2e800e'
BASELINE = '5f759300c12f5191a36d2f2b5a29555ef14ef486'
UPSTREAM = {
    'pinned': ('6f2b2639e563b11709e06f42bc0addbfb5a1abb6', '30e29d1afd15fe21d4aa11da4846739a530cb34d'),
    'master': ('464d41ab25abaaa1138d9f44381e43c332356e99', '1f0ca9a6bd67199ef4ce29a46060d9b08fdaeeb5'),
}


def audit(directory):
    data = json.loads((directory / 'results.json').read_text())
    assert data['status'] == 'complete' and data['source_sha256'] == SOURCE
    freeze = data['freeze']
    assert freeze['driver_base_commit'] == DRIVER and freeze['driver_clean']
    assert freeze['overlay_json_sha256'] == 'd09ac40cc737506d1f65862794aec858f254c92daa71e274ce109871b2e87656'
    assert freeze['modfile_sha256'] == 'ef64e3d92c4c9317ee8c15ddbd5f3780bd0bd1bd13eacf5d8809290075e727a7'
    assert freeze['sumfile_sha256'] == '71cd4040a7d8b07f36e0f77e29ce3900824ea6f70abf347a2da6db4bdf7530a2'
    for name, (base, commit) in UPSTREAM.items():
        assert freeze['upstream'][name]['base_commit'] == base
        assert freeze['upstream'][name]['commit'] == commit and freeze['upstream'][name]['clean']
    for name, expected in data['patch_sha256'].items():
        assert hashlib.sha256((directory / 'patches' / name).read_bytes()).hexdigest() == expected
    assert data['patch_sha256']['upstream-v9.23.patch'] == freeze['upstream']['pinned']['patch_sha256']
    assert data['patch_sha256']['upstream-master.patch'] == freeze['upstream']['master']['patch_sha256']
    assert data['patch_sha256']['driver-adapter.patch'] == data['reproduction_hashes']['adapter_patch_sha256']
    parser = data['parser']
    assert parser['exit_code'] == 0 and parser['source_commit'] == UPSTREAM['master'][1]
    assert parser['environment']['gomaxprocs'] == '1' and 'go1.27.3' in parser['environment']['go']
    assert len(parser['observations']) == 36
    summary = []
    for rows, buffer, mode in itertools.product((256, 1024, 4096), (32768, 1048576), ('generic', 'nullable')):
        values = [x for x in parser['observations'] if (x['rows'], x['buffer_bytes'], x['mode']) == (rows, buffer, mode)]
        assert len(values) == 3 and all(x['iterations'] > 0 and x['ns_per_op'] > 0 and x['bytes_per_op'] > 0 and x['allocs_per_op'] > 0 for x in values)
        summary.append({'rows': rows, 'buffer_bytes': buffer, 'mode': mode, 'samples': len(values), **{('median_' + k): statistics.median(x[k] for x in values) for k in ('ns_per_op', 'bytes_per_op', 'allocs_per_op')}})
    assert summary == parser['summary']
    native = data['native']
    assert native['loan_return'] == {'empty': True, 'no_indexes': True, 'clients_closed': True}
    assert native['environment']['cpus'] == 4 and native['environment']['container_memory_bytes'] == 8589934592
    assert native['environment']['tls_certificate_verified'] and native['environment']['resp'] == 3
    for variant, commit, version in [('baseline', BASELINE, 'v0.1.0-dev.merged-perf-2'), ('candidate', DRIVER, 'v0.1.0-dev.typed-hmget')]:
        run = native[variant]
        assert run['status'] == 'complete' and run['exit_code'] == 0 and run['writes'] == [] and run['failures'] == []
        meta = run['metadata']
        assert meta['driver_source_commit'] == commit and meta['driver_version'] == version
        assert meta['source_sha256'] == SOURCE and meta['automatic_retries'] == 0 and meta['protocol'] == 3 and meta['tls']
        assert run['build']['driver_base_commit'] == commit and run['build']['version'] == version
        assert run['build']['driver_sha256'] == meta['driver_sha256']
        assert len(run['reads']) == 28 and run['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}
        summary = []
        for readers, batch in itertools.product((1, 4), (512, 4096)):
            rows = [r for r in run['reads'] if r['readers'] == readers and r['batch_rows'] == batch]
            assert sorted(r['repeat'] for r in rows) == list(range(-1, 6))
            for row in rows:
                assert row['warmup'] == (row['repeat'] == -1) and row['validated'] and row['mode'] == 'managed'
                assert row['rows'] == 100000 and row['columns'] == 105 and row['tasks'] == (1 if readers == 1 else 8)
                assert row['peak_worker_rss_bytes'] > 0 and 0 < row['first_batch_seconds'] < row['seconds']
            measured = [r for r in rows if not r['warmup']]
            summary.append({'readers': readers, 'batch_rows': batch, 'samples': len(measured), 'median_seconds': statistics.median(r['seconds'] for r in measured), 'min_seconds': min(r['seconds'] for r in measured), 'max_seconds': max(r['seconds'] for r in measured), 'median_first_batch_seconds': statistics.median(r['first_batch_seconds'] for r in measured), 'median_peak_worker_rss_bytes': statistics.median(r['peak_worker_rss_bytes'] for r in measured)})
        assert summary == native['summary'][variant]
    assert native['baseline']['metadata']['packages'] == native['candidate']['metadata']['packages']
    for name in ('typed-api-correctness-01.log', 'master-api-race-03.log', 'master-baseline-build-01.log'):
        assert data['checks'][name]['exit_code'] != 0
    assert all(data['cleanup'][key] for key in ('owned_container_and_network_removed', 'preexisting_containers_preserved', 'preexisting_networks_preserved', 'generated_tls_and_environment_removed', 'private_generated_credentials_removed'))
    print('PASS: all 36 actual-API parser observations and 56 complete native trials; compound provenance, patch hashes, QPF and arithmetic')


if __name__ == '__main__':
    audit(Path(sys.argv[1]) if len(sys.argv) > 1 else DIRECTORY)
