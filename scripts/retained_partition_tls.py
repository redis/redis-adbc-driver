"""Retained native deployment checks using only a caller-owned TLS fixture."""
import base64
from contextlib import ExitStack
import decimal
import json
import os
import secrets
import subprocess
import sys
from urllib.parse import quote, urlsplit, urlunsplit

import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
import redis

from partition_tls_resharding import RepeatedWorker, SlotMigration


LIFECYCLE = r'''
import base64,json,os,sys
import adbc_driver_manager.dbapi as dbapi
p=json.load(sys.stdin)
assert not any(k in os.environ for k in ('REDIS_URI','REDIS_ADMIN_URI','REDIS_TLS_ADMIN_URI','REDIS_WORKER_URI'))
with dbapi.connect(driver=p['driver'],db_kwargs=p['options'],autocommit=True) as conn:
    with conn.cursor() as cur:
        try:
            cur.execute('SELECT 1')
        except dbapi.Error as exc:
            assert 'reader-only' in str(exc)
        else:
            raise AssertionError('reader-only SQL succeeded')
        cur.adbc_read_partition(base64.b64decode(p['partition']))
        with cur.fetch_record_batch() as reader:
            assert reader.read_next_batch().num_rows == 1
            if p['cancel']:
                conn.adbc_cancel()
                try:
                    reader.read_all()
                except OSError as exc:
                    assert 'cancel' in str(exc).lower()
                else:
                    raise AssertionError('cancel did not stop retained replay')
print('passed')
'''


def check_retained_tls(principals, control, schema, planner_uri, worker_name,
                       admin_uri, driver, ca, cluster_mode, cluster_fixture=False):
    namespace = 'tls_' + secrets.token_hex(12)
    prefixes = [f'adbc:{{retained:{namespace}}}:']
    table = schema + '.retained_rows'
    options = {'uri': planner_uri, 'adbc.redis.tls_ca_file': str(ca),
               'adbc.redis.cluster': cluster_mode,
               'adbc.redis.partition_result_namespace': namespace}
    verified_policy = getattr(principals, 'config', {}).get('verified_eviction_policy')
    if verified_policy is not None:
        if verified_policy not in ('noeviction', 'volatile-lru', 'volatile-lfu', 'volatile-random', 'volatile-ttl', 'volatile-lrm'):
            raise ValueError('managed fixture API does not guarantee persistent control keys')
        options['adbc.redis.partition_control_persistence'] = 'guaranteed'
    worker = worker_name.replace('@', '_retained@') if '@' in worker_name else worker_name + '_retained'
    password = 'Aa1!' + secrets.token_urlsafe(21)
    endpoint = urlsplit(admin_uri)
    uri = urlunsplit(('rediss', quote(worker) + ':' + quote(password) + '@' +
                     endpoint.netloc.split('@')[-1], endpoint.path, '', ''))
    worker_options = {'adbc.redis.partition_reader_only': 'true',
                      'adbc.redis.partition_result_namespace': namespace,
                      'adbc.redis.stream_batch_rows': '1'}
    routing = ['+cluster|slots', '+command', '+asking'] if cluster_mode == 'true' else []
    principals.create(worker, password, ['%R~' + prefixes[0] + '*', '+ping', '+time', '+getrange', *routing])
    router = (redis.RedisCluster.from_url(admin_uri, ssl_ca_certs=str(ca),
              decode_responses=True, protocol=3) if cluster_mode == 'true' else control)
    report = {'status': 'running', 'checks': [], 'minimal_worker_commands': ['PING', 'TIME', 'GETRANGE', *routing],
              'control_persistence_verification': 'managed-api' if verified_policy else 'redis-config',
              'verified_eviction_policy': verified_policy}
    data = pa.table({'id': pa.array([2**53 + i for i in range(31)], pa.int64()),
                     'grp': pa.array([i % 3 for i in range(31)], pa.int64()),
                     'label': [None if i % 4 == 0 else 'exact\x00é' for i in range(31)],
                     'payload': pa.array([b'\x00\xff'] * 31, pa.binary()),
                     'amount': pa.array([decimal.Decimal('123.45')] * 31, pa.decimal128(20, 2))})

    def connect(settings=options):
        return dbapi.connect(driver=driver, db_kwargs=settings, autocommit=True)

    def revoke(part):
        job = json.loads(part)['result']['job_id']
        with connect() as conn, conn.cursor() as cur:
            cur.execute("DROP PARTITION RESULT '" + job + "'")

    try:
        # Managed proxies may filter CONFIG. Their fixture management API has
        # independently verified policy before declaring persistent controls.
        with connect() as conn, conn.cursor() as cur:
            cur.adbc_ingest('retained_rows', data, db_schema_name=schema)
            queries = [
                f'SELECT grp,COUNT(*) AS n,SUM(amount) AS total FROM {table} GROUP BY grp ORDER BY grp',
                f'SELECT DISTINCT grp FROM {table} ORDER BY grp DESC LIMIT 2 OFFSET 1',
                f'SELECT id,ROW_NUMBER() OVER (PARTITION BY grp ORDER BY id) AS rn FROM {table} ORDER BY id',
                f'WITH x AS (SELECT id,grp FROM {table}) SELECT a.id FROM x a JOIN x b ON a.id=b.id WHERE b.grp=1 ORDER BY a.id',
            ]
            for sql in queries:
                cur.execute(sql)
                expected = cur.fetch_arrow_table()
                cur.adbc_statement.set_options(**{'adbc.redis.partition_result_mode': 'retained',
                    'adbc.redis.partition_count': 1, 'adbc.redis.stream_batch_rows': 4})
                parts, output_schema = cur.adbc_execute_partitions(sql)
                assert len(parts) == 1 and output_schema.equals(expected.schema)
                with ExitStack() as stack:
                    child = RepeatedWorker(driver, uri, ca, cluster_mode, parts[0], worker_options)
                    stack.callback(child.close)
                    child.read(expected)
                revoke(parts[0])
            cur.adbc_statement.set_options(**{'adbc.redis.partition_count': 1})
            parts, output_schema = cur.adbc_execute_partitions(f'SELECT * FROM {table} ORDER BY id')
            assert len(parts) == 1 and output_schema.equals(data.schema)
        part = parts[0]
        report['checks'].append('global_sql_exact_schema_and_values_independent_minimal_acl')
        # All planner handles are gone. Sources can be dropped before replay.
        with connect() as conn, conn.cursor() as cur:
            cur.execute('DROP TABLE ' + table)
        with ExitStack() as stack:
            workers = [RepeatedWorker(driver, uri, ca, cluster_mode, part, worker_options) for _ in range(2)]
            for child in workers:
                stack.callback(child.close)
                child.read(data)
            report['checks'].append('typed_replay_after_planner_close_and_source_drop')
            # A descriptor/configuration pair cannot grant another tenant's ACL.
            other = json.loads(part)
            other['result']['namespace'] = 'other_' + namespace
            denied = RepeatedWorker(driver, uri, ca, cluster_mode, json.dumps(other).encode(),
                {**worker_options, 'adbc.redis.partition_result_namespace': 'other_' + namespace})
            stack.callback(denied.close)
            denied.read(denied=True, permission=True)
            report['checks'].append('different_namespace_acl_denied')
            for cancel in (False, True):
                env = {k:v for k,v in os.environ.items() if k in ('PATH','HOME','SYSTEMROOT','TMPDIR','TMP','TEMP','LD_LIBRARY_PATH','DYLD_LIBRARY_PATH')}
                payload = {'driver': driver, 'options': {**options, **worker_options, 'uri': uri},
                           'partition': base64.b64encode(part).decode(), 'cancel': cancel}
                result = subprocess.run([sys.executable, '-c', LIFECYCLE], input=json.dumps(payload),
                    text=True, capture_output=True, env=env, timeout=60)
                assert result.returncode == 0 and result.stdout.strip() == 'passed', 'retained native lifecycle check failed'
                workers[0].read(data)
            report['checks'].append('early_release_and_cancel_leave_shared_result_replayable')
            if cluster_fixture:
                with connect() as conn, conn.cursor() as cur:
                    cur.execute('CREATE TABLE ' + table + ' (id BIGINT)')
                    cur.execute('INSERT INTO ' + table + ' VALUES (1)')
                job = json.loads(part)['result']['job_id']
                key = prefixes[0] + 'job:' + job + ':manifest'
                migration = SlotMigration(principals, key, prefixes[0], admin_uri)
                try:
                    migration.begin()
                    migration.assert_redirect(key, redis.exceptions.AskError)
                    principals.command(worker, '-asking')
                    for probe in principals.clients_for_uri(uri):
                        try:
                            try:
                                probe.execute_command('ASKING')
                            except redis.exceptions.NoPermissionError:
                                pass
                            else:
                                raise AssertionError('retained worker ASKING was allowed')
                        finally:
                            probe.close()
                    workers[0].read(denied=True)
                    principals.command(worker, '+asking')
                    workers[0].read(data)
                    # A new capture must refuse active slot migration. Its own
                    # result namespace is in a different slot from this replay.
                    while True:
                        pending = 'pending_' + secrets.token_hex(12)
                        prefix = f'adbc:{{retained:{pending}}}:'
                        if redis.cluster.key_slot(prefix.encode()) != migration.slot:
                            break
                    prefixes.append(prefix)
                    with connect({**options, 'adbc.redis.partition_result_namespace': pending}) as conn, conn.cursor() as cur:
                        cur.adbc_statement.set_options(**{'adbc.redis.partition_result_mode': 'retained'})
                        try:
                            cur.adbc_execute_partitions('SELECT id FROM ' + table)
                        except dbapi.Error as exc:
                            assert 'migrat' in str(exc).lower() or 'topology' in str(exc).lower()
                        else:
                            raise AssertionError('retained publication accepted an active migration')
                finally:
                    try:
                        principals.command(worker, '+asking')
                    finally:
                        migration.finish()
                migration.assert_redirect(key, redis.exceptions.MovedError)
                for child in workers:
                    child.read(data)
                report['routing'] = {'ask': True, 'moved': True, 'asking_denial': True,
                                     'active_migration_publication_rejected': True,
                                     'migrated_keys': len(migration.keys), 'persistent_workers': 2}
                report['checks'].append('retained_ask_moved_and_active_migration_publication_guard')
            revoke(part)
            for child in workers:
                child.read(revoked=True)
            report['checks'].append('explicit_revoke_observed_by_independent_workers')
        report['status'] = 'passed'
        return report
    finally:
        try:
            with connect() as conn, conn.cursor() as cur:
                cur.execute('DROP TABLE IF EXISTS ' + table)
        finally:
            try:
                clients = (principals.clients if cluster_fixture else
                           [router.get_redis_connection(node) for node in router.get_primaries()]
                           if router is not control else [control])
                for prefix in prefixes:
                    for client in clients:
                        for key in list(client.scan_iter(match=prefix + '*', count=100)):
                            router.delete(key)
                        assert not list(client.scan_iter(match=prefix + '*', count=100))
                report['owned_namespace_cleanup_verified'] = True
            finally:
                if router is not control:
                    router.close()
