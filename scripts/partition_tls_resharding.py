"""Deterministic ASK/MOVED checks, only within the declared disposable TLS cluster."""
import base64
from contextlib import ExitStack
import json
import os
import queue
import subprocess
import sys
import threading
import time
from urllib.parse import unquote, urlsplit

import pyarrow as pa
import redis
from oss_cluster_partition_tls import declared_primaries


WORKER = r'''
import base64, json, sys
import adbc_driver_manager.dbapi as dbapi
import pyarrow as pa
p = json.loads(sys.stdin.readline())
with dbapi.connect(driver=p['driver'], db_kwargs={
    'uri':p['uri'], 'adbc.redis.tls_ca_file':p['ca'],
    'adbc.redis.cluster':p['cluster_mode'], **p.get('options', {})}, autocommit=True) as conn:
    for line in sys.stdin:
        try:
            with conn.cursor() as cur:
                cur.adbc_read_partition(base64.b64decode(p['partition']))
                result = cur.fetch_arrow_table()
                sink = pa.BufferOutputStream()
                with pa.ipc.new_stream(sink, result.schema) as writer:
                    writer.write_table(result)
            response = {'status':'ok', 'arrow':base64.b64encode(sink.getvalue()).decode()}
        except Exception as exc:
            # Never send raw driver errors, URIs or descriptors to diagnostics.
            response = {'status':'error', 'permission_denied':'NOPERM' in str(exc),
                        'revoked':'revoked' in str(exc).lower()}
        print(json.dumps(response), flush=True)
'''


class RepeatedWorker:
    """Fresh process, one persistent native connection, bounded handshake reads."""
    def __init__(self, driver, uri, ca, cluster_mode, partition, options=None):
        env = {name:value for name,value in os.environ.items() if name in (
            'PATH', 'HOME', 'SYSTEMROOT', 'TMPDIR', 'TMP', 'TEMP',
            'LD_LIBRARY_PATH', 'DYLD_LIBRARY_PATH')}
        self.process = subprocess.Popen([sys.executable, '-c', WORKER],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            text=True, env=env)
        self.responses = queue.Queue()
        def collect():
            try:
                for line in self.process.stdout:
                    self.responses.put(line)
            finally:
                self.responses.put(None)
        self.thread = threading.Thread(target=collect, daemon=True)
        self.thread.start()
        try:
            self.process.stdin.write(json.dumps({'driver':driver, 'uri':uri, 'ca':str(ca),
                'cluster_mode':cluster_mode, 'partition':base64.b64encode(partition).decode(),
                'options':options or {}})+'\n')
            self.process.stdin.flush()
        except BaseException:
            self.close()
            raise

    def read(self, expected=None, denied=False, revoked=False, permission=False):
        self.process.stdin.write('read\n')
        self.process.stdin.flush()
        try:
            line = self.responses.get(timeout=30)
        except queue.Empty:
            raise AssertionError('persistent native worker exceeded 30-second read deadline') from None
        if line is None:
            raise AssertionError('persistent native worker exited before returning a result')
        reply = json.loads(line)
        if denied:
            assert reply['status'] == 'error', 'native worker unexpectedly succeeded with permission denied'
            if permission:
                assert reply['permission_denied'], 'native worker did not report NOPERM'
        elif revoked:
            assert reply['status'] == 'error' and reply['revoked'], 'native worker did not report revocation'
        else:
            assert reply['status'] == 'ok', 'persistent native partition read failed'
            actual = pa.ipc.open_stream(base64.b64decode(reply['arrow'])).read_all()
            assert actual.schema.equals(expected.schema) and actual.equals(expected)

    def close(self):
        try:
            self.process.stdin.close()
        except OSError:
            pass  # An exited child must still be reaped and its stdout closed.
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=5)
        self.thread.join(timeout=5)
        self.process.stdout.close()


class SlotMigration:
    def __init__(self, principals, key, owned_prefix, admin_uri):
        # No arbitrary endpoint, slot or key list from callers may expand scope.
        if not key.startswith(owned_prefix):
            raise ValueError('migration key is outside this run')
        self.slot = redis.cluster.key_slot(key.encode())
        self.clients = dict(zip(sorted(principals.nodes), principals.clients))
        slots = principals.clients[0].execute_command('CLUSTER', 'SLOTS')
        nodes = declared_primaries(slots, principals.config['ports'])
        if nodes != principals.nodes:
            raise ValueError('primary identities changed before migration')
        owners = [primary[1] for start,end,primary,*_ in slots if start <= self.slot <= end]
        if len(owners) != 1:
            raise ValueError('migration slot must have one declared owner')
        self.source_port = owners[0]
        self.target_port = next(port for port in sorted(self.clients) if port != self.source_port)
        self.source, self.target = self.clients[self.source_port], self.clients[self.target_port]
        self.source_id, self.target_id = nodes[self.source_port], nodes[self.target_port]
        self.keys = self.source.execute_command('CLUSTER', 'GETKEYSINSLOT', self.slot, 2000)
        if key not in self.keys or not self.keys or len(self.keys) >= 2000 or any(
                not item.startswith(owned_prefix) for item in self.keys):
            raise ValueError('refusing to migrate a slot containing unrelated or unbounded keys')
        endpoint = urlsplit(admin_uri)
        self.username, self.password = unquote(endpoint.username), unquote(endpoint.password)

    def begin(self):
        self.target.execute_command('CLUSTER', 'SETSLOT', self.slot, 'IMPORTING', self.source_id)
        self.source.execute_command('CLUSTER', 'SETSLOT', self.slot, 'MIGRATING', self.target_id)
        # tls-cluster=yes makes server-side MIGRATE use the fixture's trusted TLS.
        self.source.execute_command('MIGRATE', '127.0.0.1', self.target_port, '', 0, 5000,
            'AUTH2', self.username, self.password, 'KEYS', *self.keys)
        assert self.source.execute_command('CLUSTER', 'COUNTKEYSINSLOT', self.slot) == 0

    def finish(self):
        if self.source.execute_command('CLUSTER', 'COUNTKEYSINSLOT', self.slot) != 0:
            raise RuntimeError('cannot finalize migration while source still owns keys')
        # Destination first, then source, then remaining primary, per Redis's protocol.
        for port in [self.target_port, self.source_port, *(
                port for port in sorted(self.clients) if port not in (self.source_port,self.target_port))]:
            self.clients[port].execute_command('CLUSTER', 'SETSLOT', self.slot, 'NODE', self.target_id)
        for client in self.clients.values():
            owners = [primary[1:3] for start,end,primary,*_ in client.execute_command('CLUSTER','SLOTS')
                      if start <= self.slot <= end]
            assert owners == [[self.target_port,self.target_id]]

    def assert_redirect(self, key, kind):
        try:
            self.source.exists(key)
        except kind as exc:
            assert exc.slot_id == self.slot and exc.host == '127.0.0.1' and exc.port == self.target_port
        else:
            raise AssertionError('source did not emit the required redirect')


def wait_search_convergence(clients, index, expected_rows, timeout=30, *, row_ids=None):
    """Require stable cap-bounded HASH loads on every Search coordinator."""
    expected_ids = set(range(1, expected_rows + 1) if row_ids is None else row_ids)
    if len(expected_ids) != expected_rows or not expected_ids or min(expected_ids) < 1:
        raise ValueError('readiness requires exact unique positive fixture row IDs')
    deadline = time.monotonic() + timeout
    attempts, stable = 0, 0
    while True:
        attempts += 1
        ready = True
        for client in clients:
            try:
                raw = client.execute_command('FT.AGGREGATE', index, '*',
                    'GROUPBY', 0, 'REDUCE', 'COUNT', 0, 'AS', 'counted')
                count = (raw['results'][0]['extra_attributes']['counted'] if isinstance(raw,dict)
                         else dict(zip(raw[1][::2],raw[1][1::2]))['counted'])
                ready = ready and int(count) == expected_rows
                # Counts and cached SORTABLE IDs can be correct while LOAD *
                # cannot read a migrated HASH yet. Match native whole-row reads,
                # including authoritative __rowid presence, on every coordinator.
                for lo in range(1, max(expected_ids) + 1, 137):
                    hi = lo + 136
                    raw, cursor = client.execute_command('FT.AGGREGATE', index,
                        '@__rowid:[' + str(lo) + ' ' + str(hi) + ']',
                        'LOAD', 1, '@__key', 'LOAD', '*', 'TIMEOUT', 0,
                        'WITHCURSOR', 'COUNT', 1000, 'DIALECT', 2, NEVER_DECODE=True)
                    if cursor:
                        client.execute_command('FT.CURSOR', 'DEL', index, cursor)
                        ready = False
                    rows = ([r.get('extra_attributes', r.get(b'extra_attributes'))
                             for r in raw.get('results', raw.get(b'results'))] if isinstance(raw,dict)
                            else [dict(zip(r[::2],r[1::2])) for r in raw[1:]])
                    ids = [int(value) for row in rows
                           if (value := row.get('__rowid', row.get(b'__rowid'))) is not None]
                    wanted = {i for i in expected_ids if lo <= i <= hi}
                    ready = ready and len(rows) == len(ids) == len(wanted) and set(ids) == wanted
            except redis.ResponseError as exc:
                if 'Query requires unavailable slots' not in str(exc):
                    raise
                ready = False
        stable = stable + 1 if ready else 0
        if stable == 3:
            return attempts
        if time.monotonic() >= deadline:
            raise AssertionError('Search topology did not converge on all primaries within deadline')
        time.sleep(0.1)


def check_resharding(principals, control, schema, worker_name, worker_uri, admin_uri,
                     driver, ca, cluster_mode, descriptor, expected):
    index = 'idx:' + schema + ':rows'
    row_id = json.loads(descriptor)['lo']
    raw = control.execute_command('FT.SEARCH', index, '@__rowid:['+str(row_id)+' '+str(row_id)+']',
                                  'NOCONTENT', 'LIMIT', 0, 2, 'DIALECT', 2)
    keys = [row['id'] for row in raw['results']] if isinstance(raw,dict) else raw[1:]
    assert len(keys) == 1
    key = keys[0]
    migration = SlotMigration(principals, key, schema + ':', admin_uri)
    workers = []
    cleanup = ExitStack()
    finished = False
    try:
        for _ in range(2):
            worker = RepeatedWorker(driver, worker_uri, ca, cluster_mode, descriptor)
            cleanup.callback(worker.close)
            workers.append(worker)
            worker.read(expected)  # Cache the original slot route on both connections.
        try:
            migration.begin()
            migration.assert_redirect(key, redis.exceptions.AskError)
            # Prove that ASKING is part of the restricted Cluster API ACL contract.
            principals.command(worker_name, '-asking')
            worker_controls = principals.clients_for_uri(worker_uri)
            try:
                for client in worker_controls:
                    try:
                        client.execute_command('ASKING')
                    except redis.exceptions.NoPermissionError:
                        pass
                    else:
                        raise AssertionError('restricted worker ASKING was not denied')
            finally:
                for client in worker_controls:
                    client.close()
            workers[0].read(denied=True)
            principals.command(worker_name, '+asking')
            workers[0].read(expected)
        finally:
            migration.finish()
            finished = True
        migration.assert_redirect(key, redis.exceptions.MovedError)
        # The second connection has not read since pre-migration warmup.
        for worker in workers:
            worker.read(expected)
        assert migration.target.exists(key) == 1
        convergence_attempts = wait_search_convergence(principals.clients, index, 1099,
            row_ids=[i for i in range(1, 1101) if i != 3])
        return {'slot':migration.slot, 'source_port':migration.source_port,
                'target_port':migration.target_port, 'migrated_keys':len(migration.keys),
                'ask_observed':True, 'moved_observed':True, 'asking_acl_denial_verified':True,
                'persistent_native_connections':2, 'exact_arrow_values_and_schema':True,
                'ownership_finalized':finished, 'search_convergence_attempts':convergence_attempts}
    finally:
        try:
            principals.command(worker_name, '+asking')
        finally:
            cleanup.close()
