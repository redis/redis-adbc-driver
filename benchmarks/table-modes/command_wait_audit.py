"""Audit complete command-wait controls without Redis, Go or native libraries."""
from collections import Counter
import hashlib
import json
from pathlib import Path
import statistics


def summary(job):
    rows = job['releases']
    result = {'name': job['name'], 'mode': job['mode'], 'observations': len(rows),
              'job_milliseconds': job['phase']['elapsed_milliseconds']}
    for key in ('hook_nanoseconds', 'read_nanoseconds', 'write_nanoseconds', 'prelude_nanoseconds'):
        values = [r[key] for r in rows]
        result[key] = {'median': statistics.median(values), 'minimum': min(values),
                       'maximum': max(values), 'sum': sum(values)}
    values = [r['prelude_nanoseconds'] + r['hook_nanoseconds'] for r in rows]
    result['combined_nanoseconds'] = {'median': statistics.median(values),
                                      'minimum': min(values), 'maximum': max(values), 'sum': sum(values)}
    return result


def audit():
    root = Path(__file__).resolve().parents[2]
    folder = root/'benchmarks/table-modes/command-wait-2026-11-13'
    data = json.loads((folder/'results.json').read_text())
    for name, digest in json.loads((folder/'sha256.json').read_text()).items():
        assert hashlib.sha256((folder/name).read_bytes()).hexdigest() == digest
    provenance = data['provenance']
    assert provenance['baseline'] == '06c76c0b68258862d091487c3eb280b961f4fb8d'
    assert provenance['measured_source'] == 'dc6cceb5c9304a1f2bad740ec43e00cb162dff3b'
    assert provenance['initial_unexecuted_source'] == '0408724dc1615650df1c2c90f7163384ae517ec9'
    assert provenance['production_changed'] is False and provenance['gomaxprocs'] == 4
    assert provenance['go_toolchain'] == 'go1.27.3'
    assert hashlib.sha256((folder/'candidate.patch').read_bytes()).hexdigest() == provenance['go_patch_sha256']
    assert data['focused_tests'] == {'unit': {'pass': 14, 'skip': 0, 'fail': 0},
                                     'race': {'pass': 14, 'skip': 0, 'fail': 0}}
    environment = data['environment']
    assert environment['software_api_versions'] == ['8.2.6-25'] and environment['redis_version'] == '8.8.0'
    assert environment['modules']['search'] == 80808
    assert environment['container_cpus'] == 4 and environment['container_memory_bytes'] == 8589934592
    assert environment['fixture'] == {'qpf': 4, 'workers': 6, 'routing': 'mnp', 'connections': 32,
                                       'memory_bytes': 4294967296, 'tls_certificate_verified': True,
                                       'resp': 3, 'empty': True}
    cases = [('control-before', 'control'), ('same-ping', 'same_ping'),
             ('separate-time', 'separate_time'), ('pacing-20ms', 'pacing_20ms'),
             ('separate-ping', 'separate_ping'), ('same-time', 'same_time'), ('control-after', 'control')]
    assert [(p['name'], p['mode']) for p in data['profiles']] == cases
    assert len(data['checks']) == 24 and all(c['exit_code'] == 0 for c in data['checks'])
    expected_stages = ['unit', 'race', 'verify-before']
    for name, _ in cases:
        expected_stages.extend((name, name+'-reset', name+'-verify'))
    assert [c['name'] for c in data['checks']] == expected_stages
    assert data['summary'] == [summary(p) for p in data['profiles']]
    for job in data['profiles']:
        assert job['source'] == provenance['measured_source'] and job['go_exit_code'] == 0
        assert job['diagnostic_valid'] is True and job['full_target_values_validated'] is True
        assert job['unrelated_aggregate_validated'] is True
        assert job['phase']['rows'] == job['phase']['unrelated_rows'] == 20000
        assert job['phase']['phases']['copy']['steps'] == 314
        assert job['loan_return'] == {'empty': True, 'no_indexes': True, 'clients_closed': True}
        rows = job['releases']
        assert len(rows) == 317 and Counter(r['phase'] for r in rows) == {'copy': 314, 'index': 1, 'install': 2}
        dials = job['transport']['connections']
        ids = {d['ordinal'] for d in dials}
        assert len(ids) == len(dials) and all(i > 0 for i in ids)
        assert all(d['verified_tls'] is True and d['tls_version'] == 772 and
                   d['tcp_nodelay_known'] is False for d in dials)
        assert job['transport']['mode'] == job['mode']
        for row in rows:
            assert row['mode'] == job['mode'] and row['success'] is True
            assert row['reads'] == row['writes'] == 1 and row['pool_misses'] == row['pool_timeouts'] == 0
            assert row['command_wait_valid'] is True and row['release_connection'] in ids
            assert row['release_read_bytes'] > 0 and row['release_write_bytes'] > 0
            assert 0 <= row['read_nanoseconds'] <= row['hook_nanoseconds']
            assert 0 <= row['write_nanoseconds'] <= row['hook_nanoseconds']
            assert row['prelude_nanoseconds'] >= 0
            if job['mode'] == 'control':
                assert row['prelude_nanoseconds'] == 0 and 'probe_connection' not in row
            elif job['mode'] == 'pacing_20ms':
                assert row['pacing_success'] is True
                assert row['pacing_nanoseconds'] == row['prelude_nanoseconds'] >= 20000000
                assert row['probe_reads'] == row['probe_writes'] == row['probe_read_bytes'] == row['probe_write_bytes'] == 0
                assert row['probe_connection'] == 0 and row['connection_relation'] == 'not_applicable'
            else:
                assert row['probe_success'] is True and row['probe_reads'] == row['probe_writes'] == 1
                assert row['probe_connection'] in ids and row['probe_read_bytes'] > 0 and row['probe_write_bytes'] > 0
                assert row['probe_nanoseconds'] == row['prelude_nanoseconds'] > 0
                assert (row['probe_connection'] != row['release_connection']) == job['mode'].startswith('separate')
    assert data['loan_return'] == {'all_processes_clients_ended': True, 'empty': True, 'no_indexes': True,
                                   'effective_qpf_tls_resp3_reverified_each_job': True,
                                   'container_identity_limits_unchanged_at_loan_boundaries': True,
                                   'all_seven_jobs_valid': True, 'release_observations': 2219}
    trace = json.loads((folder/'trace-capability.json').read_text())
    assert trace['capture_performed'] is False and trace['workload_rerun'] is False
    assert trace['clock_calibration_performed'] is False and trace['packet_timestamp_delivery_observed'] is False
    assert trace['default_exec_net_raw_effective'] is False and trace['root_exec_net_raw_effective'] is True
    assert trace['protocol_zero_raw_socket_opened_and_closed'] is True
    assert trace['temporary_socket_accepts_timestampns_option'] is True
    assert trace['tools_present'] == {'tcpdump': False, 'tshark': False, 'libpcap': False, 'dpkt': False, 'scapy': False}
    print('PASS: all seven fixed jobs/2219 releases, exact phases/physical connections, controls and paired timing arithmetic')


if __name__ == '__main__':
    audit()
