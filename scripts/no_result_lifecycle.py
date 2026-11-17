#!/usr/bin/env python3
# Copyright (c) 2026 ADBC Drivers Contributors
# Licensed under the Apache License, Version 2.0.
"""Native caller-path correctness checks on an explicitly owned Software fixture.

No timings or speedup claims. Raw errors, endpoint/principal identifiers and
credentials stay out of the JSON result. Supervise the process with a timeout.
"""
import argparse
import concurrent.futures
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import platform
import secrets
import sys
import tempfile
import threading
import traceback
from urllib.parse import urlsplit
import uuid

import adbc_driver_manager as manager
import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import redis

from software_partition_tls import SoftwareAPI, SoftwarePrincipals, qpf_4x_ready, wait_for

PATHS = ('execute', 'executemany')
EXACT = 9007199254740993
SCHEMA = pa.schema([('id', pa.int64()), ('amount', pa.int64()), ('label', pa.string())])
EXPECTED = pa.Table.from_pylist([
    {'id': 1, 'amount': EXACT, 'label': 'a' + chr(0) + 'é'},
    {'id': 2, 'amount': None, 'label': None},
], schema=SCHEMA)
assert 0 in EXPECTED['label'][0].as_py().encode('utf-8')


def error_identity(error):
    state = getattr(error, 'sqlstate', None)
    if isinstance(state, bytes):
        state = state.decode('ascii')
    return {'status': int(error.status_code), 'sqlstate': state}


def invoke(cursor, execution, sql, parameters=None):
    # Caller chooses the no-result route explicitly; never inspect the SQL.
    if execution == 'execute':
        cursor.execute(sql, parameters)
    else:
        cursor.executemany(sql, parameters)


def connect(driver, uri, ca):
    return dbapi.connect(driver=str(driver), autocommit=True, db_kwargs={
        'uri': uri, 'adbc.redis.tls_ca_file': str(ca),
    })


def complete_state(driver, uri, ca, table, expected=EXPECTED):
    with connect(driver, uri, ca) as conn, conn.cursor() as cursor:
        cursor.execute('SELECT id,amount,label FROM ' + table + ' ORDER BY id')
        actual = cursor.fetch_arrow_table()
        assert actual.equals(expected, check_metadata=False), 'complete typed state changed'


def denied_case(driver, uri, ca, table, execution, sql):
    with connect(driver, uri, ca) as conn, conn.cursor() as cursor:
        try:
            invoke(cursor, execution, sql)
        except manager.Error as error:
            # Native principal probes establish the refusal independently.
            message = str(error)
            # Software reports direct ACL refusals as NOPERM, and a refused
            # command inside Lua as ERR ACL failure in script (also covered by
            # rbac_snapshot_payload_test.go). Neither is a transport failure.
            assert ('NOPERM ' in message or 'ERR ACL failure in script: No permissions' in message), 'mutation failed for a reason other than permission'
            result = error_identity(error)
        else:
            raise AssertionError('forbidden mutation succeeded')
        # The same public cursor must remain usable after a refusal.
        cursor.execute('SELECT id,amount,label FROM ' + table + ' ORDER BY id')
        assert cursor.fetch_arrow_table().equals(EXPECTED, check_metadata=False)
    return result


def cancel_case(driver, uri, ca, table, execution):
    entered, unblock = threading.Event(), threading.Event()
    counts = {'entered': 0, 'yielded': 0}
    parameter = pa.record_batch([pa.array([-EXACT], pa.int64())], names=['amount'])

    def producer():
        counts['entered'] += 1
        entered.set()
        if not unblock.wait(15):
            raise TimeoutError('fixture producer was not unblocked')
        counts['yielded'] += 1
        yield parameter

    reader = pa.RecordBatchReader.from_batches(parameter.schema, producer())
    conn = connect(driver, uri, ca)
    cursor = conn.cursor()
    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            pending = pool.submit(invoke, cursor, execution,
                                  'UPDATE ' + table + ' SET amount=? WHERE id=1', reader)
            try:
                assert entered.wait(10), 'execution did not enter the foreign producer'
                assert not pending.done(), 'producer barrier did not hold execution'
                cursor.adbc_cancel()  # The one supported cross-thread cursor operation.
            finally:
                # Foreign get_next cannot be forcibly interrupted. Let its owner
                # return before joining execution and releasing any C/Arrow owner.
                unblock.set()
            try:
                pending.result(timeout=15)
            except manager.Error as error:
                result = error_identity(error)
                assert result == {'status': int(manager.AdbcStatusCode.CANCELLED), 'sqlstate': '57014'}, result
            else:
                raise AssertionError('cancelled mutation succeeded')
        assert counts == {'entered': 1, 'yielded': 1}, 'input consumed more than once'
        cursor.execute('SELECT id,amount,label FROM ' + table + ' ORDER BY id')
        assert cursor.fetch_arrow_table().equals(EXPECTED, check_metadata=False)
    finally:
        unblock.set()
        cursor.close()
        cursor.close()
        reader.close()
        conn.close()
        conn.close()
    complete_state(driver, uri, ca, table)
    return {**result, 'producer_entries': counts['entered'],
            'producer_yields': counts['yielded'], 'reuse_and_explicit_close': True}


def close_case(driver, uri, ca, table, execution):
    updated = EXPECTED.set_column(1, 'amount', pa.array([7, None], pa.int64()))
    conn = connect(driver, uri, ca)
    cursor = conn.cursor()
    try:
        invoke(cursor, execution, 'UPDATE ' + table + ' SET amount=7 WHERE id=1')
        assert cursor.rowcount == 1
        invoke(cursor, execution, 'UPDATE ' + table + ' SET amount=9 WHERE id=-1')
        assert cursor.rowcount == 0
        cursor.execute('SELECT id,amount,label FROM ' + table + ' ORDER BY id')
        retained = cursor.fetch_arrow_table()
    finally:
        cursor.close()
        cursor.close()
        conn.close()
        conn.close()
    assert retained.equals(updated, check_metadata=False), 'Close invalidated retained Arrow values'
    complete_state(driver, uri, ca, table, updated)
    # A separate acknowledged mutation restores this test's logical baseline.
    with connect(driver, uri, ca) as admin, admin.cursor() as reset:
        reset.executemany('UPDATE ' + table + f' SET amount={EXACT} WHERE id=1', None)
        assert reset.rowcount == 1
    complete_state(driver, uri, ca, table)
    return {'affected_rows': [1, 0], 'retained_arrow_valid_after_close': True,
            'explicit_close_idempotent': True, 'fresh_connection_state_valid': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--driver', type=Path, required=True)
    parser.add_argument('--software-api-port', type=int, required=True)
    parser.add_argument('--software-data-port', type=int, required=True)
    parser.add_argument('--owned-software-fixture', action='store_true', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.output.exists():
        raise ValueError('use a new output file and preserve earlier attempts')
    uri, ca = os.environ['REDIS_URI'], Path(os.environ['SSL_CERT_FILE'])
    endpoint = urlsplit(uri)
    if (endpoint.scheme != 'rediss' or endpoint.hostname not in ('localhost', '127.0.0.1')
            or endpoint.port != args.software_data_port or not endpoint.username or not endpoint.password):
        raise ValueError('requires the owned authenticated localhost TLS data endpoint')
    api = SoftwareAPI(args.software_api_port)
    databases = api.call('GET', 'bdbs')
    assert len(databases) == 1 and databases[0]['name'] == 'adbc-test'
    database = databases[0]

    def gate():
        # Principal bindings reconfigure the owned BDB asynchronously. Require
        # effective QPF again after those changes, before running native cases.
        wait_for(lambda: qpf_4x_ready(api.call('GET', f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns")),
                 'Software did not settle at effective 4x QPF')
        current = api.call('GET', f"bdbs/{database['uid']}")
        effective = api.call('GET', f"bdbs/{database['uid']}?fields=status,query_performance_factor,search,sched_policy,conns")
        assert current['eviction_policy'] == 'noeviction' and qpf_4x_ready(effective)
        return {'active_qpf': 4, 'search_workers': effective['search']['search-workers'],
                'sched_policy': effective['sched_policy'], 'conns': effective['conns'], 'noeviction': True}

    results = {'schema_version': 1, 'scope': 'Native correctness only; no performance or full lifetime acceptance.',
               'source': os.environ.get('NO_RESULT_SOURCE_COMMIT'),
               'driver_sha256': hashlib.sha256(args.driver.read_bytes()).hexdigest(),
               'harness_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
               'packages': {name: importlib.metadata.version(name) for name in ('adbc-driver-manager', 'pyarrow', 'redis')},
               'server': {'distribution': 'Redis Software', 'redis_version': database['version'], 'shards': database['shards_count']},
               'client': {'os': platform.system(), 'architecture': platform.machine(),
                          'python': platform.python_version(), 'route': 'Host client through Docker published localhost TLS port'},
               'initial_gate': gate(), 'permission_cases': [], 'cancellation_cases': [], 'close_cases': []}
    token = uuid.uuid4().hex
    schema, prefix, index = 'nr_' + token, 'nr:' + token + ':', 'nr_' + token
    control = redis.Redis.from_url(uri, ssl_ca_certs=str(ca), ssl_cert_reqs='required', protocol=3)
    with tempfile.TemporaryDirectory(prefix='adbc-no-result-') as private:
        directory = Path(private)
        directory.chmod(0o700)
        (directory / 'ca.crt').write_bytes(ca.read_bytes())
        (directory / 'admin.uri').write_text(uri)
        (directory / 'admin.uri').chmod(0o600)
        (directory / 'software.json').write_text(json.dumps({'api_port': args.software_api_port, 'bdb_uid': database['uid']}))
        principals = SoftwarePrincipals(directory, api=api)
        try:
            with connect(args.driver, uri, ca) as conn, conn.cursor() as cursor:
                cursor.execute('CREATE SCHEMA ' + schema)
                cursor.adbc_ingest('managed', EXPECTED, mode='create', db_schema_name=schema)
                cursor.adbc_statement.set_options(**{'adbc.redis.ingest.unmanaged_source': json.dumps({'prefix': prefix, 'index': index})})
                source = EXPECTED.add_column(0, '__key', pa.array([prefix + '1', prefix + '2']))
                cursor.adbc_ingest('unmanaged', source, mode='create', db_schema_name=schema)
            def indexed():
                info = control.execute_command('FT.INFO', index)
                if not isinstance(info, dict):
                    info = dict(zip(info[::2], info[1::2]))
                assert int(info.get(b'hash_indexing_failures', 0)) == 0, 'source fixture indexing failed'
                return int(info[b'num_docs']) == 2 and int(info[b'indexing']) == 0
            wait_for(indexed, 'unmanaged fixture did not finish indexing')
            raw_before = {key: control.hgetall(key) for key in (prefix+'1', prefix+'2')}
            identities = {}
            for denied in ('hset', 'del'):
                name = 'noresult-' + uuid.uuid4().hex + '@example.com'
                principals.create(name, secrets.token_urlsafe(32),
                                  ['+@all', '-' + denied, '~*', '(+' + denied + ' ~adbc:{meta}:*)'])
                with principals.client(name) as principal:
                    try:
                        command = ('HSET', prefix+'probe', 'id', '99') if denied == 'hset' else ('DEL', prefix+'probe')
                        principal.execute_command(*command)
                    except redis.exceptions.NoPermissionError:
                        pass
                    else:
                        raise AssertionError('independent principal probe did not refuse the source mutation')
                identities[denied] = principals.people[name]['uri']
            gate()
            for mode in ('managed', 'unmanaged'):
                table = schema + '.' + mode
                complete_state(args.driver, uri, ca, table)
                for operation in ('insert', 'update', 'delete'):
                    denied = 'del' if mode == 'unmanaged' and operation == 'delete' else 'hset'
                    sql = {'insert': f"INSERT INTO {table} (id,amount,label) VALUES (3,-{EXACT},'denied')",
                           'update': f'UPDATE {table} SET amount=7 WHERE id=1',
                           'delete': f'DELETE FROM {table} WHERE id=2'}[operation]
                    if mode == 'unmanaged' and operation == 'insert':
                        sql = f"INSERT INTO {table} (__key,id,amount,label) VALUES ('{prefix}3',3,-{EXACT},'denied')"
                    cases = [denied_case(args.driver, identities[denied], ca, table, path, sql) for path in PATHS]
                    assert cases[0] == cases[1], 'caller paths changed permission status/SQLSTATE'
                    complete_state(args.driver, uri, ca, table)
                    results['permission_cases'].append({'mode': mode, 'operation': operation, 'paths': list(PATHS),
                        'denied_command': denied, **cases[0], 'complete_state_unchanged': True})
                for path in PATHS:
                    results['cancellation_cases'].append({'mode': mode, 'execution': path,
                        **cancel_case(args.driver, uri, ca, table, path), 'complete_state_unchanged': True})
                    results['close_cases'].append({'mode': mode, 'execution': path,
                        **close_case(args.driver, uri, ca, table, path)})
            assert {key: control.hgetall(key) for key in raw_before} == raw_before
            assert set(control.scan_iter(match=prefix+'*')) == {key.encode() for key in raw_before}
            results['raw_source_unchanged'] = True
        finally:
            principals.cleanup()
            for kind, uid in principals.resources:
                assert all(item['uid'] != uid for item in api.call('GET', kind)), 'temporary principal resource survived cleanup'
            with connect(args.driver, uri, ca) as conn, conn.cursor() as cursor:
                cursor.execute('DROP SCHEMA IF EXISTS ' + schema + ' CASCADE')
            if control.exists(prefix+'1', prefix+'2'):
                control.delete(prefix+'1', prefix+'2')
            if index.encode() in control.execute_command('FT._LIST'):
                control.execute_command('FT.DROPINDEX', index)
            assert not list(control.scan_iter(match=prefix+'*'))
            control.close()
    results['final_gate'] = gate()
    results['cleanup'] = {'temporary_principals_removed': True, 'owned_schema_and_source_removed': True}
    args.output.write_text(json.dumps(results, indent=2)+'\n')
    print('Native no-result permission/cancellation/Close checks passed; owned fixture objects removed.')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # API/driver exceptions can echo private resource identifiers or SQL.
        # Keep CI diagnostics fixed; opt into a private local traceback file.
        if private_log := os.environ.get('NO_RESULT_PRIVATE_FAILURE_LOG'):
            with open(private_log, 'w', opener=lambda path, flags: os.open(path, flags, 0o600)) as log:
                traceback.print_exc(file=log)
        print(type(error).__name__ + ': native no-result lifecycle validation failed', file=sys.stderr)
        raise SystemExit(1) from None
