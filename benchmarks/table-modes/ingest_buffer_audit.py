"""Audit every frozen ingestion-reuse observation without Redis or Go."""
from collections import Counter
import hashlib
import json
from pathlib import Path
import statistics

ROOT = Path(__file__).resolve().parents[2]
FOLDER = ROOT / 'benchmarks/table-modes/ingest-buffer-2026-11-13'
BASELINE = '88774f10cf16fc4260e6ad5077097f4de8828406'
CANDIDATE = '553960585f29a5f7b1151742e97d12168f5a9d69'
SOURCE = 'a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2'
HELPER = 'a4e01c712fc90f4b87c97eb783baf8e4a3b972e548f280b256236b132697f49b'
PATCH = '6dad5c6fde7d06297606128b13466e3f89e17e00c8a258c2a05e8f60bb768d66'


def load(name):
    return json.loads((FOLDER / name).read_text())


def audit():
    data, provenance = load('native-results.json'), load('provenance.json')
    assert hashlib.sha256((FOLDER / 'candidate.patch').read_bytes()).hexdigest() == PATCH
    assert hashlib.sha256((ROOT / 'benchmarks/table-modes/ingest_buffer_compare.py').read_bytes()).hexdigest() == HELPER
    wanted = Counter((mode, shape, writers, repeat) for mode in ('managed', 'unmanaged')
                     for shape in ('one_stream', 'one_batch') for writers in (1, 4)
                     for repeat in (-1, 0, 1, 2))
    for variant, run in data.items():
        assert variant in ('baseline', 'candidate') and run['status'] == 'complete'
        assert run['reads'] == [] and run['failures'] == []
        assert len(run['writes']) == 32
        assert Counter((r['mode'], r['shape'], r['writers'], r['repeat']) for r in run['writes']) == wanted
        meta = run['metadata']
        assert meta['driver_source_commit'] == (BASELINE if variant == 'baseline' else CANDIDATE)
        assert meta['driver_source_commit'] == provenance[variant]['source_head']
        assert meta['driver_version'] == provenance[variant]['version']
        assert meta['driver_sha256'] == provenance[variant]['driver_sha256']
        assert meta['source_sha256'] == SOURCE and meta['protocol'] == 3 and meta['tls'] is True
        assert meta['automatic_retries'] == 0 and meta['full_input_materialized'] is True
        assert meta['write_rows'] == 10000 and meta['columns'] == 105
        assert meta['harness_source_commit'] == CANDIDATE
        assert provenance[variant]['helper_sha256'] == HELPER
        assert provenance[variant]['native_gomaxprocs'] == 'unset'
        assert all(v is None for v in provenance[variant]['inherited_runtime'].values())
        assert run['final_qpf'] == {'factor': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296}
        for r in run['writes']:
            assert r['api'] == 'arrow' and r['validated'] is True and r['warmup'] == (r['repeat'] == -1)
            assert r['rows'] == 10000 and r['columns'] == 105 and r['public_calls'] == r['writers']
            assert r['seconds'] > 0 and r['peak_worker_rss_bytes'] > 0
            stream = r['shape'] == 'one_stream'
            assert r['input_batches'] == ((10 if r['writers'] == 1 else 12) if stream else r['writers'])
            assert r['largest_input_batch'] == (1000 if stream else 10000 // r['writers'])
    summary = []
    for mode in ('managed', 'unmanaged'):
        for shape in ('one_stream', 'one_batch'):
            for writers in (1, 4):
                row = {'mode': mode, 'shape': shape, 'writers': writers}
                for variant, run in data.items():
                    rs = [r for r in run['writes'] if r['mode'] == mode and r['shape'] == shape and r['writers'] == writers and not r['warmup']]
                    assert len(rs) == 3
                    row[variant] = {key: {'median': statistics.median(r[key] for r in rs), 'minimum': min(r[key] for r in rs), 'maximum': max(r[key] for r in rs)} for key in ('seconds', 'peak_worker_rss_bytes')}
                summary.append(row)
    assert summary == load('summary.json')
    micro = load('construction-results.json')
    assert len(micro) == 24
    assert Counter((r['columns'], r['kind'], r['variant']) for r in micro) == Counter({(width, kind, variant): 3 for width in (8, 105) for kind in ('integer', 'mixed') for variant in ('fresh', 'reuse')})
    assert all(r['iterations'] > 0 and r['ns_per_op'] > 0 and r['bytes_per_op'] > 0 and r['allocs_per_op'] > 0 for r in micro)
    assert load('cleanup-proof.json') == {
        'owned_container_and_network_removed': True,
        'preexisting_containers_preserved': True,
        'preexisting_networks_preserved': True,
        'preexisting_container_count': 1,
        'preexisting_network_count': 31,
        'generated_tls_and_environment_removed': True,
        'private_generated_credentials_removed': True,
    }
    checks = load('checks.json')
    for stage, total, top in [('unit', 59, 3), ('race', 59, 3), ('live', 14, 8)]:
        c = checks[stage]
        assert c['passed'] == total == len(c['cases'])
        assert c['top_level'] == top == sum('/' not in r['name'] for r in c['cases'])
        assert c['failed'] == c['skipped'] == 0
    assert checks['controller_cleanup_attempt']['tests_repeated'] is False
    assert checks['controller_cleanup_attempt']['cleanup_empty_gate_passed'] is True
    for file, digest in load('sha256.json').items():
        assert hashlib.sha256((FOLDER / file).read_bytes()).hexdigest() == digest
    print('PASS: all64 native/all8 cells +24 conversion samples,132 correctness pass cases, exact source/settings/helper/patch and full arithmetic')


if __name__ == '__main__':
    audit()
