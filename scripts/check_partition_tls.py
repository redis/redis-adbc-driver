#!/usr/bin/env python3
"""Verify native partitions over trusted TLS with independent restricted workers.

Requires REDIS_TLS_ADMIN_URI, SSL_CERT_FILE (fixture CA path) and a TLS-only disposable fixture
with aggregate cap 137. Uses REDIS_ADBC_LIBRARY, or the installed native wheel.
Never prints connection URIs, credentials or descriptor payloads.
"""
import argparse
import base64
import decimal
import hashlib
import importlib.metadata
import json
import os
import platform
from pathlib import Path
import secrets
import socket
import ssl
import subprocess
import sys
import time
import threading
from urllib.parse import quote, urlsplit, urlunsplit

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import redis


WORKER = r'''
import base64, json, os, sys
import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
p = json.load(sys.stdin)
assert not any(name in os.environ for name in ('REDIS_TLS_ADMIN_URI', 'REDIS_URI', 'REDIS_ADMIN_URI', 'REDIS_WORKER_URI'))
with dbapi.connect(driver=p['driver'], db_kwargs={'uri':p['uri'], 'adbc.redis.tls_ca_file':p['ca_file'], 'adbc.redis.stream_batch_rows':'10', 'adbc.redis.cluster':p['cluster_mode']}, autocommit=True) as conn:
    with conn.cursor() as cur:
        cur.adbc_read_partition(base64.b64decode(p['partition']))
        if p.get('cancel') or p.get('release'):
            batches = cur.fetch_record_batch()
            assert batches.read_next_batch().num_rows == 10
            if p.get('cancel'):
                conn.adbc_cancel()
                try:
                    batches.read_all()
                except OSError as exc:
                    assert 'cancel' in str(exc).lower()
                else:
                    raise AssertionError('cancel did not stop the partition reader')
            batches.close()
            print('cancelled' if p.get('cancel') else 'released')
        else:
            result = cur.fetch_arrow_table()
            sink = pa.BufferOutputStream()
            with pa.ipc.new_stream(sink, result.schema) as writer:
                writer.write_table(result)
            print(base64.b64encode(sink.getvalue()).decode())
'''


def verified_uri(value):
    uri = urlsplit(value)
    if (uri.scheme != 'rediss' or uri.query or uri.fragment or not uri.hostname
            or not uri.username or not uri.password):
        raise ValueError('use a rediss URI with explicit credentials and no query/fragment overrides')
    return uri


def fixture_difference(actual, expected, schema):
    """Only fixture IDs/counts, never raw values, SQL, credentials or descriptors."""
    rows = actual.to_pylist()
    expected_by_id = {row['id']:row for row in expected}
    ids = [row['id'] for row in rows]
    return {'expected_rows':len(expected), 'actual_rows':len(rows),
        'schema_matches':actual.schema.equals(schema),
        'missing_ids':sorted(set(expected_by_id)-set(ids)),
        'unexpected_ids':sorted(set(ids)-set(expected_by_id)),
        'duplicate_ids':sorted({i for i in ids if ids.count(i)>1}),
        'changed_value_ids':sorted({row['id'] for row in rows
            if row['id'] in expected_by_id and row != expected_by_id[row['id']]})}


def cluster_distribution(control, index, minimum_primaries, primary_clients=None):
    """Prove owned fixture keys span the advertised primary slot owners."""
    slots = control.execute_command('CLUSTER', 'SLOTS')
    owners, ranges = {}, []
    for start, end, primary, *_ in slots:
        identity = tuple(primary[:2])
        owners.setdefault(identity, len(owners))
        ranges.append((int(start), int(end), owners[identity]))
    assert len(owners) >= minimum_primaries
    assert ranges and all(0 <= start <= end <= 16383 for start, end, _ in ranges)
    ordered = sorted(ranges)
    assert ordered[0][0] == 0 and ordered[-1][1] == 16383
    assert all(left[1] + 1 == right[0] for left, right in zip(ordered, ordered[1:]))
    result = control.execute_command('FT.SEARCH', index, '*', 'NOCONTENT', 'LIMIT', 0, 2000, 'DIALECT', 2)
    if isinstance(result, dict):
        total, keys = result['total_results'], [row['id'] for row in result['results']]
    else:
        total, keys = result[0], result[1:]
    assert int(total) == 1099 and len(keys) == 1099 and len(set(keys)) == 1099
    counts = [0] * len(owners)
    keys_by_owner = [[] for _ in owners]
    for key in keys:
        slot = redis.cluster.key_slot(key.encode())
        matches = [owner for start, end, owner in ranges if start <= slot <= end]
        assert len(matches) == 1
        counts[matches[0]] += 1
        keys_by_owner[matches[0]].append(key)
    assert all(count > 0 for count in counts)
    if primary_clients is not None:
        assert set(primary_clients) == {identity[1] for identity in owners}
        for identity, owner in owners.items():
            with primary_clients[identity[1]].pipeline(transaction=False) as pipe:
                for key in keys_by_owner[owner]:
                    pipe.exists(key)
                assert pipe.execute() == [1] * len(keys_by_owner[owner])
    return {'primary_count': len(owners), 'slot_range_count': len(ranges),
            'minimum_primary_count': minimum_primaries,
            'physical_row_presence_verified': primary_clients is not None,
            'owned_rows_per_primary': counts, 'covered_slots': 16384}


class RedisPrincipals:
    def __init__(self, control):
        self.control, self.names = control, []

    def create(self, name, password, acl):
        self.control.execute_command('ACL', 'SETUSER', name, 'reset', 'on', '>' + password, *acl)
        self.names.append(name)

    def command(self, name, permission):
        self.control.execute_command('ACL', 'SETUSER', name, permission)

    def cleanup(self):
        if self.names:
            self.control.acl_deluser(*self.names)


def main(argv=None, cloud_principals_factory=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--require-wheel', action='store_true')
    parser.add_argument('--cluster-mode', choices=('auto', 'false', 'true'), default='auto')
    parser.add_argument('--minimum-cluster-primaries', type=int, default=1)
    backend = parser.add_mutually_exclusive_group()
    backend.add_argument('--software-fixture', type=Path)
    backend.add_argument('--cloud-fixture', type=Path)
    backend.add_argument('--oss-cluster-fixture', type=Path)
    args = parser.parse_args(argv)
    if args.minimum_cluster_primaries < 1:
        parser.error('minimum cluster primaries must be positive')
    if args.minimum_cluster_primaries > 1 and args.cluster_mode == 'false':
        parser.error('cluster primary requirement needs Cluster API routing')
    if args.oss_cluster_fixture:
        if args.cluster_mode == 'false':
            parser.error('OSS Cluster fixture requires Cluster API routing')
        args.minimum_cluster_primaries = max(3, args.minimum_cluster_primaries)
    if args.output.exists():
        parser.error('choose a fresh report output')
    admin_uri = os.environ['REDIS_TLS_ADMIN_URI']
    endpoint = verified_uri(admin_uri)
    ca = Path(os.environ['SSL_CERT_FILE']).resolve(strict=True)
    override = os.environ.get('REDIS_ADBC_LIBRARY')
    if args.require_wheel and override:
        raise ValueError('installed-wheel validation must not override the native library')
    if override:
        driver = str(Path(override).resolve(strict=True))
    else:
        from adbc_driver_redis import _driver_path
        driver = _driver_path()
        if args.require_wheel and 'site-packages' not in Path(driver).parts:
            raise ValueError('wheel library must load from the installed package')
    control = redis.Redis.from_url(admin_uri, ssl_ca_certs=str(ca), ssl_cert_reqs='required', decode_responses=True)
    suffix = secrets.token_hex(6)
    schema, table = 'partition_tls_' + suffix, 'rows'
    qualified = schema + '.' + table
    planner, worker = 'planner_' + suffix, 'worker_' + suffix
    if args.software_fixture:
        planner += '@partition.invalid'
        worker += '@partition.invalid'
    if args.cloud_fixture:
        # Cloud/account password validation can reject an all-hex ACL password.
        planner_password, worker_password = ('Aa1!' + secrets.token_urlsafe(21) for _ in range(2))
    else:
        planner_password, worker_password = secrets.token_hex(24), secrets.token_hex(24)
    def user_uri(user, password):
        host = endpoint.hostname
        if ':' in host:
            host = '[' + host + ']'
        return urlunsplit(('rediss', quote(user) + ':' + quote(password) + '@' + host + ':' + str(endpoint.port or 6379), endpoint.path, '', ''))
    planner_uri, worker_uri = user_uri(planner, planner_password), user_uri(worker, worker_password)
    if args.cloud_fixture:
        if cloud_principals_factory is None:
            raise ValueError('use cloud_partition_tls.py to supply API credentials privately')
        principals = cloud_principals_factory(args.cloud_fixture, schema)
    elif args.software_fixture:
        from software_partition_tls import SoftwarePrincipals
        principals = SoftwarePrincipals(args.software_fixture, schema)
    elif args.oss_cluster_fixture:
        from oss_cluster_partition_tls import ClusterPrincipals
        principals = ClusterPrincipals(control, args.oss_cluster_fixture, admin_uri, ca)
    else:
        principals = RedisPrincipals(control)

    read_commands = ['+ping', '+info', '+ft._list', '+ft.info', '+ft.aggregate', '+ft.cursor',
                     '+ft.search', '+ft.profile', '+get', '+exists', '+hget', '+hmget',
                     '+hgetall', '+smembers', '+sismember']
    if args.cluster_mode == 'true':
        read_commands += ['+cluster|slots', '+command', '+asking']
    report = {'kind': 'local native partition TLS/independent ACL validation',
              'status': 'running', 'library_sha256': hashlib.sha256(Path(driver).read_bytes()).hexdigest(),
              'installed_wheel': not bool(override), 'python': sys.version.split()[0],
              'pyarrow': pa.__version__, 'certificate_verification': True,
              'platform': platform.platform(), 'machine': platform.machine(),
              'packages': {name: importlib.metadata.version(name) for name in ('adbc-driver-manager', 'redis')},
              'aggregate_cap': 137, 'cluster_mode': args.cluster_mode, 'checks': []}
    schema_created = False
    def check(name):
        report['checks'].append(name)
        if args.cloud_fixture:
            print('Cloud partition check passed: ' + name, flush=True)
    def connect(uri):
        return dbapi.connect(driver=driver, db_kwargs={'uri': uri, 'adbc.redis.tls_ca_file':str(ca),
            'adbc.redis.cluster': args.cluster_mode}, autocommit=True)
    def invoke(part, cancel=False, release=False, uri=worker_uri, env=None, ca_file=None):
        # Workers receive only their own principal in stdin. Do not inherit
        # planner/admin connection variables or unrelated host credentials.
        worker_env = {name: value for name, value in os.environ.items() if name in (
            'PATH', 'HOME', 'SYSTEMROOT', 'TMPDIR', 'TMP', 'TEMP',
            'LD_LIBRARY_PATH', 'DYLD_LIBRARY_PATH')}
        worker_env.update(env or {})
        result = subprocess.run([sys.executable, '-c', WORKER], input=json.dumps({
            'driver': driver, 'uri': uri, 'ca_file':str(ca_file or ca), 'cluster_mode': args.cluster_mode, 'partition': base64.b64encode(part).decode(), 'cancel': cancel, 'release': release,
        }), text=True, capture_output=True, timeout=60, env=worker_env)
        # Child errors may contain query/connection data; do not echo stderr.
        return result
    def read(part):
        result = invoke(part)
        if result.returncode:
            raise AssertionError('independent TLS partition worker failed')
        return pa.ipc.open_stream(base64.b64decode(result.stdout)).read_all()
    def assert_failed(part, marker, uri=worker_uri, env=None, ca_file=None):
        result = invoke(part, uri=uri, env=env, ca_file=ca_file)
        if result.returncode == 0 or marker.lower() not in result.stderr.lower():
            raise AssertionError('expected native worker rejection: ' + marker)
    try:
        control.ping()
        try:
            cluster_enabled = control.info('cluster').get('cluster_enabled')
        except redis.ResponseError:
            cluster_enabled = None
        report['server_cluster_enabled'] = cluster_enabled
        if args.minimum_cluster_primaries > 1 and not (args.cluster_mode == 'true' or
                (args.cluster_mode == 'auto' and cluster_enabled == 1)):
            raise ValueError('cluster primary requirement needs Cluster API routing')
        if args.cluster_mode == 'auto' and cluster_enabled == 1:
            read_commands += ['+cluster|slots', '+command', '+asking']
        if args.software_fixture:
            report['topology'] = {'deployment': 'Redis Software TLS proxy', **principals.config}
        if args.oss_cluster_fixture:
            report['topology'] = principals.config
            check('verified_three_primary_tls_caps')
        report['redis_version'] = control.info('server')['redis_version']
        report['modules'] = control.execute_command('MODULE', 'LIST')
        if args.cloud_fixture:
            report['kind'] = 'Redis Cloud Pro native partition TLS/independent ACL validation'
            report['topology'] = principals.config
            actual_cap = int(control.config_get('search-max-aggregate-results')['search-max-aggregate-results'])
            assert actual_cap >= 137
            report['server_aggregate_cap'] = actual_cap
        else:
            assert control.config_get('search-max-aggregate-results')['search-max-aggregate-results'] == '137'
        check('verified_admin_tls_and_real_cap')
        # A mismatched hostname is rejected by the same CA/hostname TLS policy.
        context = ssl.create_default_context(cafile=str(ca))
        try:
            with socket.create_connection((endpoint.hostname, endpoint.port or 6379), timeout=5) as sock:
                context.wrap_socket(sock, server_hostname='not-the-fixture.invalid')
        except ssl.SSLCertVerificationError:
            check('hostname_mismatch_rejected')
        else:
            raise AssertionError('TLS hostname verification was bypassed')
        principals.create(planner, planner_password, ['~*', '+@all'])
        principals.create(worker, worker_password, ['%R~adbc:*', '%R~' + schema + ':*', *read_commands])
        data = pa.table({
            'id': pa.array(range(1100), pa.int64()),
            'label': [None if i % 3 == 0 else 'a\x00b' for i in range(1100)],
            'amount': pa.array([decimal.Decimal('123456789.12')] * 1100, pa.decimal128(18, 2)),
            'payload': pa.array([b'\x00\xff'] * 1100, pa.binary()),
        })
        with connect(planner_uri) as owner:
            with owner.cursor() as cur:
                cur.execute('CREATE SCHEMA ' + schema)
                schema_created = True
                cur.adbc_ingest(table, data, db_schema_name=schema)
                cur.execute('DELETE FROM ' + qualified + ' WHERE id=2')
                deadline = time.monotonic() + 10
                while True:
                    raw = control.execute_command('FT.INFO', 'idx:' + schema + ':' + table)
                    info = dict(zip(raw[::2], raw[1::2])) if isinstance(raw, list) else raw
                    if float(info['indexing']) == 0 and float(info['percent_indexed']) >= 1:
                        break
                    if time.monotonic() >= deadline:
                        raise AssertionError('fixture did not finish indexing')
                    time.sleep(0.01)
                if args.oss_cluster_fixture:
                    from partition_tls_resharding import wait_search_convergence
                    report['initial_search_convergence_attempts'] = wait_search_convergence(
                        principals.clients, 'idx:' + schema + ':' + table, 1099,
                        row_ids=[i for i in range(1, 1101) if i != 3])
                cur.adbc_statement.set_options(**{'adbc.redis.partition_count': 4, 'adbc.redis.partition_scan_cap': 137})
                broad, broad_schema = cur.adbc_execute_partitions('SELECT * FROM ' + qualified)
                direct, direct_schema = cur.adbc_execute_partitions('SELECT * FROM ' + qualified + ' WHERE __rowid IN(1,1,3,1100)')
                cur.adbc_statement.set_options(**{'adbc.redis.partition_count': 1})
                cancellable, _ = cur.adbc_execute_partitions('SELECT * FROM ' + qualified)
        if args.cluster_mode == 'true' or (args.cluster_mode == 'auto' and cluster_enabled == 1):
            primary_clients = (dict(zip(sorted(principals.nodes), principals.clients))
                               if args.oss_cluster_fixture else None)
            report['cluster_distribution'] = cluster_distribution(control, 'idx:' + schema + ':' + table,
                                                                  args.minimum_cluster_primaries, primary_clients)
            check('fixture_rows_span_advertised_cluster_primaries')
        assert len(broad) == 4 and len(direct) == 3
        for descriptor in broad + direct + cancellable:
            for secret in (admin_uri, endpoint.password, planner_uri, worker_uri, planner_password, worker_password, str(ca)):
                assert secret.encode() not in descriptor
        check('closed_planner_and_credential_free_descriptors')
        # Build expected values directly from the fixture, independently of SQL.
        expected = data.take(pa.array([i for i in range(1100) if i != 2]))
        actual = pa.concat_tables([read(part) for part in broad]).sort_by('id')
        assert actual.schema.equals(broad_schema) and actual.equals(expected)
        check('restricted_cross_process_search_full_values_schema')
        actual = pa.concat_tables([read(part) for part in direct]).sort_by('id')
        assert actual.schema.equals(direct_schema) and actual.equals(data.take(pa.array([0, 1099])))
        check('restricted_cross_process_direct_hash_duplicates_deleted')
        # Native Go TLS must reject an untrusted CA, not only Python's client.
        wrong_ca = ca.parent / 'untrusted.crt'
        if not wrong_ca.is_file():
            raise ValueError('fixture must provide untrusted.crt for the negative native TLS check')
        assert_failed(broad[0], 'certificate', ca_file=wrong_ca)
        check('native_untrusted_ca_rejected')
        # Serve a validly signed leaf with a mismatched SAN to the real native
        # worker. No Redis protocol is needed: TLS must fail before authentication.
        wrong_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        wrong_context.load_cert_chain(ca.parent / 'wrong-host.crt', ca.parent / 'wrong-host.key')
        listener = socket.socket()
        listener.bind(('127.0.0.1', 0))
        listener.listen()
        listener.settimeout(0.2)
        stopped = threading.Event()
        def serve_wrong_host():
            while not stopped.is_set():
                try:
                    sock, _ = listener.accept()
                except socket.timeout:
                    continue
                except OSError:
                    return
                with sock:
                    sock.settimeout(5)
                    try:
                        with wrong_context.wrap_socket(sock, server_side=True):
                            pass
                    except ssl.SSLError:
                        pass
        thread = threading.Thread(target=serve_wrong_host, daemon=True)
        thread.start()
        try:
            wrong_uri = urlunsplit(('rediss', quote(worker) + ':' + quote(worker_password)
                + '@127.0.0.1:' + str(listener.getsockname()[1]), '/0', '', ''))
            assert_failed(broad[0], 'certificate', uri=wrong_uri)
            check('native_hostname_mismatch_rejected')
        finally:
            stopped.set()
            listener.close()
            thread.join(timeout=5)

        worker_controls = (principals.clients_for_uri(worker_uri) if args.oss_cluster_fixture else [
            redis.Redis.from_url(worker_uri, ssl_ca_certs=str(ca), ssl_cert_reqs='required', decode_responses=True)])
        try:
            for worker_control in worker_controls:
                commands = [('CONFIG', 'SET', 'search-max-aggregate-results', '137'),
                            ('HSET', schema + ':forbidden', 'id', '1')]
                if args.software_fixture or args.cloud_fixture:
                    # The managed proxy filters unauthorized CONFIG GET to an empty reply
                    # instead of NOPERM. The worker cannot discover or raise the cap.
                    assert worker_control.config_get('search-max-aggregate-results') == {}
                    check('cloud_proxy_config_get_filtered_empty' if args.cloud_fixture else 'software_proxy_config_get_filtered_empty')
                else:
                    commands.insert(0, ('CONFIG', 'GET', '*'))
                for command in commands:
                    try:
                        worker_control.execute_command(*command)
                    except redis.exceptions.NoPermissionError:
                        pass
                    else:
                        raise AssertionError('worker administrative/write command was allowed')
        finally:
            for worker_control in worker_controls:
                worker_control.close()
        check('worker_config_mutation_and_writes_denied' if args.software_fixture or args.cloud_fixture else 'worker_config_and_writes_denied')
        assert_failed(b'{}', 'invalid Redis ADBC partition')
        check('malformed_descriptor_rejected')
        principals.command(worker, '-ft.info')
        assert_failed(broad[0], 'FT.INFO permission')
        assert read(direct[0]).num_rows == 1
        principals.command(worker, '+ft.info')
        check('search_readiness_acl_required_direct_hash_independent')
        principals.command(worker, '-hmget')
        assert_failed(direct[0], 'NOPERM')
        principals.command(worker, '+hmget')
        check('direct_hash_reader_obeys_worker_acl')
        canceled = invoke(cancellable[0], cancel=True)
        assert canceled.returncode == 0 and canceled.stdout.strip() == 'cancelled'
        check('connection_cancel_in_independent_tls_worker')
        released = invoke(cancellable[0], release=True)
        assert released.returncode == 0 and released.stdout.strip() == 'released'
        check('early_release_in_independent_tls_worker')
        if args.oss_cluster_fixture:
            from partition_tls_resharding import check_resharding
            report['resharding'] = check_resharding(principals, control, schema, worker,
                worker_uri, admin_uri, driver, ca, args.cluster_mode, direct[0], data.take(pa.array([0])))
            check('persistent_workers_follow_ask_and_moved_with_restricted_acl')
            # Check every value after ownership converges, including Search reads.
            actual = pa.concat_tables([read(part) for part in broad]).sort_by('id')
            report['post_reshard_result'] = fixture_difference(actual, expected.to_pylist(), broad_schema)
            assert actual.schema.equals(broad_schema) and actual.equals(expected)
            report['post_reshard_distribution'] = cluster_distribution(control,
                'idx:' + schema + ':' + table, args.minimum_cluster_primaries, primary_clients)
            check('post_reshard_search_full_values_and_physical_owners')
        with connect(planner_uri) as owner:
            with owner.cursor() as cur:
                cur.execute("UPDATE " + qualified + " SET label='updated' WHERE id=0")
                report['mutation_rowcounts'] = {'update':cur.rowcount}
                cur.execute('DELETE FROM ' + qualified + ' WHERE id=5')
                report['mutation_rowcounts']['delete'] = cur.rowcount
                cur.execute('INSERT INTO ' + qualified + ' (id) VALUES(1100)')
                report['mutation_rowcounts']['insert'] = cur.rowcount
        live_expected = [row for row in data.to_pylist() if row['id'] not in (2, 5)]
        live_expected[0]['label'] = 'updated'
        live = pa.concat_tables([read(part) for part in broad]).sort_by('id')
        report['live_result'] = fixture_difference(live, live_expected, broad_schema)
        assert live.schema.equals(broad_schema) and live.to_pylist() == live_expected
        check('live_updates_deletes_and_later_insert_exclusion')
        with connect(planner_uri) as owner:
            with owner.cursor() as cur:
                cur.execute('TRUNCATE ' + qualified)
        assert_failed(broad[0], 'partition target changed')
        check('stale_target_rejected')
        from retained_partition_tls import check_retained_tls
        retained_cluster = ('true' if cluster_enabled == 1 else 'false') if args.cluster_mode == 'auto' else args.cluster_mode
        report['retained'] = check_retained_tls(principals, control, schema, planner_uri,
            worker, admin_uri, driver, ca, retained_cluster, bool(args.oss_cluster_fixture))
        check('retained_global_sql_minimal_acl_lifecycle_and_replay')
        report['status'] = 'passed'
    finally:
        # Only this generated schema/users belong to this run. Keep cleanup
        # failures visible rather than report a successful fixture lifecycle.
        try:
            if schema_created:
                with connect(planner_uri) as owner:
                    with owner.cursor() as cur:
                        cur.execute('DROP TABLE IF EXISTS ' + qualified)
                        cur.execute('DROP SCHEMA IF EXISTS ' + schema)
        except BaseException:
            report['status'] = 'failed'
            raise
        finally:
            try:
                principals.cleanup()
            except BaseException:
                report['status'] = 'failed'
                raise
            finally:
                control.close()
                if report['status'] != 'passed':
                    report['status'] = 'failed'
                args.output.parent.mkdir(parents=True, exist_ok=True)
                args.output.write_text(json.dumps(report, indent=2) + '\n')
    print('Native TLS partitions:', len(report['checks']), 'checks passed; independent restricted credentials, verified certificates.')


if __name__ == '__main__':
    main()
