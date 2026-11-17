#!/usr/bin/env python3
"""Instrumented Linux Python/C-driver point reads, never a performance gate.

Use only a dedicated, owned Software fixture verified through make redis-up.
Raw exception traces are deliberately private. Run with an outer timeout.
"""
import argparse
import ctypes
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import platform
import sys
import time
import traceback
from urllib.parse import parse_qs, urlsplit
import uuid

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import redis

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'scripts'))
from software_partition_tls import SoftwareAPI, qpf_4x_ready, wait_for


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--software-api-port', type=int, required=True)
    parser.add_argument('--software-data-port', type=int, required=True)
    parser.add_argument('--owned-software-fixture', action='store_true', required=True)
    parser.add_argument('--route', choices=('linux-container-shared-software-network-namespace',), required=True)
    args = parser.parse_args()
    if platform.system() != 'Linux' or args.output.exists():
        raise ValueError('Use a Linux client and a new output filename')
    manifest = json.loads((args.build/'manifest-private.json').read_text())
    library = args.build/'probe.so'
    assert manifest['build_exit_code'] == 0
    assert hashlib.sha256(library.read_bytes()).hexdigest() == manifest['library_sha256']
    uri, ca = os.environ['REDIS_URI'], os.environ['SSL_CERT_FILE']
    endpoint = urlsplit(uri)
    assert (endpoint.scheme == 'rediss' and endpoint.hostname == 'localhost'
            and endpoint.port == args.software_data_port and endpoint.username and endpoint.password)
    assert parse_qs(endpoint.query).get('protocol') == ['3']
    api = SoftwareAPI(args.software_api_port)
    databases = api.call('GET', 'bdbs')
    assert len(databases) == 1 and databases[0]['name'] == 'adbc-test'
    database = databases[0]

    def gate():
        effective_path = f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns"
        wait_for(lambda: qpf_4x_ready(api.call('GET', effective_path)), 'Software QPF not ready')
        effective = api.call('GET', effective_path)
        current = api.call('GET', f"bdbs/{database['uid']}")
        assert qpf_4x_ready(effective) and current['eviction_policy'] == 'noeviction'
        assert current['tls_mode'] == 'enabled' and not current.get('bigstore')
        return {'active_qpf': 4, 'search_workers': 6, 'sched_policy': 'mnp',
                'conns': 32, 'noeviction': True}

    result = {'schema_version': 1, 'status': 'started', 'source_commit': manifest['source_commit'],
              'library_sha256': manifest['library_sha256'], 'overlaid_files': manifest['overlaid_files'],
              'builder_sha256': manifest['builder_sha256'], 'race': manifest['race'],
              'checkptr': manifest['checkptr'],
              'go_version': manifest['go_version'],
              'harness_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              'packages': {p: importlib.metadata.version(p) for p in ('adbc-driver-manager', 'pyarrow', 'redis')},
              'client': {'os': platform.system(), 'architecture': platform.machine(),
                         'python': platform.python_version(), 'route': args.route},
              'server': {'distribution': 'Redis Software', 'redis_version': database['version'],
                         'shards': database['shards_count']},
              'scope': ('Instrumented native Python DBAPI execute plus complete fetch. Foreground calls carry '
                        'this ExecuteQuery context; unmarked overlapping calls remain separate. Hook time '
                        'includes pool, transport and decoding; it is not packet RTT or server CPU. '
                        'Overlapping/nested intervals are not additive. ctypes/observer overhead is included. '
                        'No uninstrumented baseline/candidate comparison or performance acceptance.'),
              'initial_gate': gate(), 'samples': []}
    dll = ctypes.CDLL(str(library))
    dll.RedisNativeProofBegin.argtypes = []
    dll.RedisNativeProofBegin.restype = ctypes.c_int
    dll.RedisNativeProofEnd.argtypes = []
    dll.RedisNativeProofEnd.restype = ctypes.c_void_p
    dll.RedisNativeProofFree.argtypes = [ctypes.c_void_p]
    dll.RedisNativeProofFree.restype = None

    def finish_sample():
        pointer = dll.RedisNativeProofEnd()
        assert pointer
        try:
            return json.loads(ctypes.string_at(pointer))
        finally:
            dll.RedisNativeProofFree(pointer)

    control = redis.Redis.from_url(uri, ssl_ca_certs=ca, ssl_cert_reqs='required', protocol=3)
    token = uuid.uuid4().hex
    schema, prefix, index = 'np_'+token, 'np:'+token+':', 'np_'+token
    columns = ['id', 'amount', 'label']
    expected = pa.table({'id': pa.array(range(1, 65), pa.int64()),
                         'amount': pa.array([None if i % 7 == 0 else 9007199254740993+i for i in range(1, 65)], pa.int64()),
                         'label': pa.array([None if i % 5 == 0 else 'row\0é'+str(i) for i in range(1, 65)], pa.string())})

    def connect():
        return dbapi.connect(driver=str(library), autocommit=True, db_kwargs={
            'uri': uri, 'adbc.redis.tls_ca_file': ca, 'adbc.redis.cluster': 'false'})

    def save():
        args.output.write_text(json.dumps(result, indent=2)+'\n')

    save()
    try:
        with connect() as conn, conn.cursor() as cur:
            cur.execute('CREATE SCHEMA '+schema)
            cur.adbc_ingest('managed', expected, mode='create', db_schema_name=schema)
            cur.adbc_statement.set_options(**{'adbc.redis.ingest.unmanaged_source': json.dumps({'prefix': prefix, 'index': index})})
            source = expected.add_column(0, '__key', pa.array([prefix+str(i) for i in range(1, 65)]))
            cur.adbc_ingest('unmanaged', source, mode='create', db_schema_name=schema)
        raw_before = {prefix+str(i): control.hgetall(prefix+str(i)) for i in range(1, 65)}
        assert all(raw_before.values())
        with connect() as conn, conn.cursor() as cur:
            cur.execute('SELECT id,__rowid FROM '+schema+'.managed ORDER BY id')
            managed_ids = cur.fetch_arrow_table().to_pydict()['__rowid']
            assert len(managed_ids) == 64
            for mode in ('managed', 'unmanaged'):
                for sample in range(-1, 32):
                    ordinal = ((sample if sample >= 0 else 32)*17) % 64
                    predicate = ('__rowid='+str(managed_ids[ordinal]) if mode == 'managed'
                                 else "__key='"+prefix+str(ordinal+1)+"'")
                    sql = 'SELECT '+','.join(columns)+' FROM '+schema+'.'+mode+' WHERE '+predicate
                    assert dll.RedisNativeProofBegin() == 1
                    assert dll.RedisNativeProofBegin() == 0, 'overlapping sample was accepted'
                    started = time.perf_counter_ns()
                    try:
                        cur.execute(sql)
                        executed = time.perf_counter_ns()
                        actual = cur.fetch_arrow_table()
                        fetched = time.perf_counter_ns()
                    finally:
                        observations = finish_sample()
                    assert observations['dropped_calls'] == 0
                    assert observations['foreground_inflight_at_end'] == 0
                    assert any(call['foreground'] for call in observations['calls'])
                    assert actual.equals(expected.slice(ordinal, 1), check_metadata=False)
                    result['samples'].append({'mode': mode, 'sample': sample, 'warmup': sample < 0,
                        'execute_ns': executed-started, 'fetch_ns': fetched-executed,
                        'complete_typed_row_validated': True, **observations})
                cur.execute('SELECT '+','.join(columns)+' FROM '+schema+'.'+mode+' ORDER BY id')
                assert cur.fetch_arrow_table().equals(expected, check_metadata=False)
        assert {key: control.hgetall(key) for key in raw_before} == raw_before
        assert set(control.scan_iter(match=prefix+'*')) == {key.encode() for key in raw_before}
        result.update(complete_typed_fixture_validated=True, unmanaged_source_unchanged=True)
    finally:
        with connect() as conn, conn.cursor() as cur:
            cur.execute('DROP SCHEMA IF EXISTS '+schema+' CASCADE')
        keys = [prefix+str(i) for i in range(1, 65)]
        control.delete(*keys)
        if index.encode() in control.execute_command('FT._LIST'):
            control.execute_command('FT.DROPINDEX', index)
        assert not list(control.scan_iter(match=prefix+'*'))
        control.close()
        result['owned_objects_removed'] = True
        save()
    result.update(status='passed', final_gate=gate())
    save()
    print('Native proof attribution and typed oracles passed; owned objects removed.')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        if private_log := os.environ.get('NATIVE_PROOF_PRIVATE_FAILURE_LOG'):
            with open(private_log, 'w', opener=lambda path, flags: os.open(path, flags, 0o600)) as log:
                traceback.print_exc(file=log)
        print('Native proof attribution failed: '+type(error).__name__, file=sys.stderr)
        raise SystemExit(1)
