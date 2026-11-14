"""Audit all retained compaction reply-attribution attempts without Redis."""
import hashlib
import json
from collections import Counter
import statistics
from pathlib import Path


def summary(rows):
    copy = [r for r in rows if r['phase'] == 'copy']
    return {
        'hook_ns_median': statistics.median(r['hook_nanoseconds'] for r in rows),
        'copy_hook_ns_sum': sum(r['hook_nanoseconds'] for r in copy),
        'copy_read_ns_sum': sum(r['read_nanoseconds'] for r in copy),
        'valid_commandstats': sum(r.get('commandstats_valid') is True for r in rows),
        'invalid_commandstats': sum(r.get('commandstats_valid') is False for r in rows),
    }


def audit(path):
    evidence = json.loads(path.read_text())
    root = Path(__file__).resolve().parents[2]
    patchdir = root / 'benchmarks/table-modes/compaction-reply-2026-11-13'
    patchmeta = json.loads((patchdir / 'patches.json').read_text())
    assert set(patchmeta) == {'initial.patch', 'corrected.patch'}
    for name, meta in patchmeta.items():
        assert hashlib.sha256((patchdir / name).read_bytes()).hexdigest() == meta['sha256']
    for name, count in [('unit_race', 17), ('corrected_unit_race', 18)]:
        assert evidence[name] == {'tests_including_subtests': count, 'pass': count, 'skip': 0, 'fail': 0}
    assert evidence['production_identity'] == {'baseline': '88774f10', 'non_test_go_and_module_files': 269, 'all_byte_identical': True}
    env = evidence['environment']
    assert env['software_api_versions'] == ['8.2.6-25'] and env['redis_version'] == '8.8.0' and env['modules']['search'] == 80808
    assert env['fixture'] == {'qpf': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32, 'memory_bytes': 4294967296, 'tls_certificate_verified': True, 'resp': 3, 'empty': True}
    assert env['container_cpus'] == 4 and env['container_memory_bytes'] == 8589934592

    cleanup_raw = (patchdir / 'cleanup.json').read_bytes()
    cleanup = evidence['final_cleanup']
    assert cleanup['status'] == 'complete' and cleanup['owner'] == 'root'
    assert hashlib.sha256(cleanup_raw).hexdigest() == cleanup['proof_sha256']
    assert json.loads(cleanup_raw) == cleanup['proof'] == {
        'owned_container_and_network_removed': True,
        'preexisting_containers_preserved': True,
        'preexisting_networks_preserved': True,
        'preexisting_container_count': 1,
        'preexisting_network_count': 31,
        'generated_tls_and_environment_removed': True,
        'private_generated_credentials_removed': True,
    }
    assert cleanup['credential_redaction_matches'] == 0 and cleanup['raw_log_hashes_unchanged']
    profiles = evidence['profiles']
    assert [p['name'] for p in profiles] == ['dense-control', 'dense-commandstats', 'corrected-dense-commandstats']
    assert [len(p['releases']) for p in profiles] == [317, 634, 317]
    assert [p['source'] for p in profiles] == [
        '549cbd4a429b8c17d78c6750a1390c504ebf2ce0',
        '549cbd4a429b8c17d78c6750a1390c504ebf2ce0',
        'd2abe7358b548cc8c42932b8ccea0dd0e70acf87',
    ]
    for p in profiles:
        rows = p['releases']
        assert p['release_observations'] == len(rows)
        assert p['copy_observations'] == sum(r['phase'] == 'copy' for r in rows)
        assert p['summary'] == summary(rows)
        assert p['full_result_validation_complete']
        assert p['phase']['rows'] == p['phase']['unrelated_rows'] == 20000
        assert p['loan_return'] == {'empty': True, 'no_indexes': True, 'clients_closed': True}
        for r in rows:
            assert r['success'] and r['reads'] == r['writes'] == 1
            assert r['pool_misses'] == r['pool_timeouts'] == 0
            assert 0 <= r['write_nanoseconds'] <= r['hook_nanoseconds']
            assert 0 <= r['read_nanoseconds'] <= r['hook_nanoseconds']
            if p['name'] == 'dense-control':
                assert 'commandstats_valid' not in r
            elif p['name'] == 'dense-commandstats':
                assert r['commandstats_valid'] is False
                assert 'script_commandstats' not in r
            else:
                assert r['commandstats_before_nanoseconds'] >= 0 and r['commandstats_after_nanoseconds'] >= 0
                before, after = r['script_commandstats_before'], r['script_commandstats_after']
                delta = {k: after[k] - before[k] for k in before}
                assert delta == r['script_commandstats']
                valid = delta['calls'] == 1 and delta['microseconds'] >= 0 and delta['rejected'] == delta['failed'] == 0
                assert r['commandstats_valid'] is valid
                assert ('commandstats_error' not in r) is valid
    assert profiles[0]['go_exit_code'] == profiles[1]['go_exit_code'] == 0
    assert profiles[1]['diagnostic_valid'] is False
    assert profiles[1]['summary']['invalid_commandstats'] == 634
    corrected = profiles[2]
    valid = corrected['summary']['valid_commandstats'] == 317
    assert corrected['diagnostic_valid'] is valid
    assert (corrected['go_exit_code'] == 0) is valid
    rows = corrected['releases']
    assert Counter(tuple(sorted(r['script_commandstats'].items())) for r in rows) == Counter({
        (('calls', 0), ('failed', 0), ('microseconds', 0), ('rejected', 0)): 316,
        (('calls', 20), ('failed', 0), ('microseconds', 20107), ('rejected', 0)): 1,
    })
    assert sum(r['commandstats_before_nanoseconds'] for r in rows) == 5079655370
    assert sum(r['commandstats_after_nanoseconds'] for r in rows) == 81967877
    assert sum(r['hook_nanoseconds'] for r in rows) == 96385828
    assert sum(r['read_nanoseconds'] for r in rows) == 87266157
    assert sum(r['hook_nanoseconds'] for r in profiles[0]['releases']) == 6296071509

    print('PASS: all three attempts retained, exact release counts, full result checks and counter/timing arithmetic')


if __name__ == '__main__':
    audit(Path(__file__).resolve().parents[2] / 'docs/sdlc/changes/compaction-reply-attribution/results.json')
