#!/usr/bin/env python3
"""Redis-backed Redis ADBC product usage collector and local dashboard."""
import argparse
from datetime import datetime, timedelta, timezone
import hmac
import hashlib
import ipaddress
import queue
import re
import threading
import dns.resolver
import dns.reversename
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import redis
from socketserver import TCPServer
import uuid

BOUNDS = [1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000]
KINDS = {'select', 'insert', 'update', 'delete', 'merge', 'ddl', 'metadata', 'session', 'other', 'unknown', 'script', 'ingest'}
FEATURES = {'join', 'filter', 'aggregate', 'sort', 'limit', 'distinct', 'cte', 'set_operation', 'window', 'subquery'}
LABELS = {'organization', 'team', 'application', 'environment', 'use_case', 'integration'}
COUNTERS = {'attempts', 'success', 'errors', 'canceled', 'abandoned', 'rows', 'affected'}
OPTIONAL = {'instance_id', 'workload_identity', 'source_hostname', 'installation_id_source', 'executions', 'execution_samples_dropped'}
TOP = OPTIONAL | {'schema_version', 'session_id', 'installation_id', 'sequence', 'started_at', 'observed_at', 'last_query_at', 'driver_version', 'os', 'arch', 'labels', 'connections', 'connection_errors', 'features', 'metrics', 'latency_bounds_ms', 'delivery_failures'}


def number(value):
    if type(value) is not int or not 0 <= value <= 10**15:
        raise ValueError('counter must be a nonnegative bounded integer')
    return value


def text(value):
    if not isinstance(value, str) or len(value.encode()) > 256:
        raise ValueError('invalid label')
    return value


def timestamp(value):
    result = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if result.tzinfo is None:
        raise ValueError('timestamp must include timezone')
    return result


def validate(p):
    if not isinstance(p, dict) or set(p) - TOP or TOP - OPTIONAL - {'last_query_at'} - set(p):
        raise ValueError('unknown or missing fields')
    if p['schema_version'] != 1 or p['latency_bounds_ms'] != BOUNDS:
        raise ValueError('unsupported schema or histogram bounds')
    uuid.UUID(p['session_id'])
    for key in ['installation_id', 'driver_version', 'os', 'arch']:
        text(p[key])
    if not p['installation_id']:
        raise ValueError('installation ID required')
    for key in ['started_at', 'observed_at']:
        timestamp(p[key])
    if 'last_query_at' in p:
        timestamp(p['last_query_at'])
    if not isinstance(p['labels'], dict) or set(p['labels']) - LABELS:
        raise ValueError('unknown labels')
    for value in p['labels'].values():
        text(value)
    for key in ['sequence', 'connections', 'connection_errors', 'delivery_failures']:
        number(p[key])
    if not isinstance(p['features'], dict) or set(p['features']) - FEATURES:
        raise ValueError('unknown features')
    for value in p['features'].values():
        number(value)
    if not isinstance(p['metrics'], dict) or set(p['metrics']) - KINDS:
        raise ValueError('unknown query kinds')
    for m in p['metrics'].values():
        if not isinstance(m, dict) or set(m) != COUNTERS | {'latency', 'ready_latency'}:
            raise ValueError('invalid metrics')
        for key in COUNTERS:
            number(m[key])
        for key in ['latency', 'ready_latency']:
            if not isinstance(m[key], list) or len(m[key]) != len(BOUNDS) + 1:
                raise ValueError('invalid histogram')
            for value in m[key]:
                number(value)
        finished = sum(m[k] for k in ['success', 'errors', 'canceled', 'abandoned'])
        if finished > m['attempts'] or sum(m['latency']) != finished or sum(m['ready_latency']) > m['attempts']:
            raise ValueError('inconsistent execution counts')
    if 'instance_id' in p:
        uuid.UUID(p['instance_id'])
    if 'workload_identity' in p:
        identity = p['workload_identity']
        if not isinstance(identity, dict) or set(identity) != {'provider','key'}:
            raise ValueError('invalid workload identity')
        if identity['provider'] not in {'ecs_service','kubernetes_controller'} or not isinstance(identity['key'],str) or not re.fullmatch('[0-9a-f]{64}',identity['key']):
            raise ValueError('unsupported workload identity')
    if 'source_hostname' in p:
        text(p['source_hostname'])
    if p.get('installation_id_source', 'explicit') not in {'explicit', 'local_file', 'temporary'}:
        raise ValueError('invalid installation ID source')
    number(p.get('execution_samples_dropped', 0))
    executions = p.get('executions', [])
    if not isinstance(executions, list) or len(executions) > 24:
        raise ValueError('too many execution samples')
    ids = set()
    for execution in executions:
        required = {'id', 'kind', 'features', 'started_at', 'completed_at', 'status', 'duration_ms', 'ready_ms', 'rows_returned', 'rows_affected', 'rows_examined', 'indexed_rows', 'rows_represented'}
        if not isinstance(execution, dict) or set(execution) != required:
            raise ValueError('invalid execution fields')
        uuid.UUID(execution['id'])
        if execution['id'] in ids:
            raise ValueError('duplicate execution ID')
        ids.add(execution['id'])
        if execution['kind'] not in KINDS or execution['status'] not in {'success','error','canceled','abandoned'}:
            raise ValueError('invalid execution classification')
        if not isinstance(execution['features'], list) or set(execution['features']) - FEATURES or len(execution['features']) > len(FEATURES):
            raise ValueError('invalid execution features')
        if timestamp(execution['completed_at']) < timestamp(execution['started_at']):
            raise ValueError('invalid execution timestamps')
        for key in ['duration_ms','rows_returned']:
            number(execution[key])
        for key in ['rows_affected','ready_ms','rows_examined','indexed_rows','rows_represented']:
            if execution[key] is not None: number(execution[key])
    return p


def subtract(current, previous):
    """Cumulative reports: reject regressions; retries and old reports are ignored."""
    if isinstance(current, dict):
        if set(previous) - set(current):
            raise ValueError('counter removed')
        return {k: subtract(v, previous.get(k, {} if isinstance(v, dict) else [0] * len(v) if isinstance(v, list) else 0)) for k, v in current.items()}
    if isinstance(current, list):
        return [subtract(a, b) for a, b in zip(current, previous)]
    if current < previous:
        raise ValueError('counter regressed')
    return current - previous


class Store:
    def __init__(self, url, prefix='adbc:{analytics}:'):
        self.redis = redis.Redis.from_url(
            url, decode_responses=True, protocol=3, max_connections=32,
            socket_connect_timeout=2, socket_timeout=5)
        self.prefix = prefix
        self.redis.ping()
        self.lookups = queue.Queue(maxsize=128)
        self.lookup_stop = threading.Event()
        self.lookup_threads = []
        self.lookup_lock = threading.Lock()

    def close(self):
        self.lookup_stop.set()
        for thread in self.lookup_threads: thread.join(timeout=2.5)
        self.redis.close()

    def reverse_dns(self, address):
        if not address:
            return ''
        cached = self.redis.get(self.prefix + 'dns:' + address)
        if cached is not None:
            return cached
        # NX avoids duplicate work; pending entries expire even if the process dies.
        if self.redis.set(self.prefix + 'dns:' + address, '', ex=120, nx=True):
            with self.lookup_lock:
                if not self.lookup_threads:
                    for _ in range(2):
                        thread = threading.Thread(target=self.lookup_dns, daemon=True)
                        self.lookup_threads.append(thread)
                        thread.start()
            try: self.lookups.put_nowait(address)
            except queue.Full: pass
        return ''

    def lookup_dns(self):
        resolver = dns.resolver.Resolver()
        while not self.lookup_stop.is_set():
            try: address = self.lookups.get(timeout=.2)
            except queue.Empty: continue
            name = ''
            try:
                answer = resolver.resolve(dns.reversename.from_address(address), 'PTR', lifetime=2)
                name = str(answer[0].target).rstrip('.')[:253]
            except dns.exception.DNSException:
                pass
            try: self.redis.set(self.prefix + 'dns:' + address, name, ex=3600)
            except redis.exceptions.RedisError: pass

    def deployment_identity(self, payload, client=None):
        if client is None: client = self.redis
        workload = payload.get('workload_identity')
        if workload:
            basis = workload['provider']
            anchor = ['workload', basis, workload['key']]
        else:
            basis = 'installation_id:' + payload.get('installation_id_source','unknown')
            anchor = ['installation', payload['installation_id']]
        # IP, PTR and hostname are deliberately excluded from confirmed aliases.
        alias = hashlib.sha256(json.dumps(anchor, separators=(',',':')).encode()).hexdigest()
        key = self.prefix + 'deployment-aliases'
        client.hsetnx(key, alias, str(uuid.uuid4()))
        deployment = client.hget(key, alias)
        return {'deployment_id': deployment, 'instance_id': payload.get('instance_id',payload['session_id']),
                'instance_kind': 'runtime' if 'instance_id' in payload else 'legacy_handle',
                'basis': basis, 'confidence': 'identifier_match', 'identity_verified': False}

    def ingest(self, payload, received_at=None, source=None):
        p = validate(payload)
        source = source or {}
        session = self.prefix + 'session:' + p['session_id']
        # Optimistic transactions serialize concurrent retries for one session.
        # The hash tag keeps transaction keys in one slot for future cluster use.
        for _ in range(32):
            with self.redis.pipeline() as pipe:
                try:
                    pipe.watch(session)
                    raw = pipe.get(session)
                    old = json.loads(raw) if raw else {}
                    if old and p['sequence'] <= old['sequence']:
                        return False
                    for k in ['installation_id', 'labels', 'started_at', 'driver_version', 'os', 'arch']:
                        if old and p[k] != old[k]:
                            raise ValueError('session identity changed')
                    if old and old.get('source_hostname', '') != p.get('source_hostname', ''):
                        raise ValueError('source hostname changed')
                    if old and any(old.get(k) != p.get(k) for k in ['instance_id','workload_identity']):
                        raise ValueError('runtime or workload identity changed within session')
                    keys = ['connections', 'connection_errors', 'features', 'metrics', 'delivery_failures']
                    delta = subtract({k: p[k] for k in keys}, {k: old[k] for k in keys} if old else {})
                    received = received_at or datetime.now(timezone.utc)
                    identity = self.deployment_identity(p, pipe)
                    pipe.multi()
                    pipe.set(session, json.dumps(p))
                    pipe.xadd(self.prefix + 'reports', {
                        'received_at': received.isoformat(), 'installation': p['installation_id'],
                        'labels': json.dumps(p['labels']), 'delta': json.dumps(delta),
                        'identity': json.dumps(identity),
                        'source': json.dumps({**source, 'hostname': p.get('source_hostname', ''), 'id_source': p.get('installation_id_source','unknown')})})
                    pipe.sadd(self.prefix + 'versions', p['driver_version'])
                    for execution in p.get('executions', []):
                        # Session-qualified IDs prevent cross-session sample overwrites.
                        ident = p['session_id'] + ':' + execution['id']
                        detail = {**execution, 'installation_id': p['installation_id'],
                                  'source_hostname': p.get('source_hostname',''), 'public_ip': source.get('public_ip',''),
                                  'received_at': received.isoformat(), 'application': p['labels'].get('application',''),
                                  'deployment_id': identity['deployment_id'], 'instance_id': identity['instance_id']}
                        pipe.hsetnx(self.prefix + 'executions', ident, json.dumps(detail))
                        pipe.zadd(self.prefix + 'execution-times', {ident: timestamp(execution['completed_at']).timestamp()}, nx=True)
                    pipe.hset(self.prefix + 'sampling', p['session_id'], p.get('execution_samples_dropped',0))
                    pipe.execute()
                    # Keep a bounded recent sample history; cumulative metrics remain intact.
                    stale = self.redis.zrange(self.prefix + 'execution-times', 0, -10001)
                    if stale:
                        with self.redis.pipeline(transaction=True) as cleanup:
                            cleanup.hdel(self.prefix + 'executions', *stale)
                            cleanup.zrem(self.prefix + 'execution-times', *stale)
                            cleanup.execute()
                    return True
                except redis.WatchError:
                    continue
        raise redis.exceptions.RedisError('session contention; retry later')

    def save_download(self, payload):
        member = payload['source'] + ':' + payload['captured_at']
        with self.redis.pipeline(transaction=True) as pipe:
            pipe.hset(self.prefix + 'downloads', member, json.dumps(payload))
            pipe.zadd(self.prefix + 'download-times', {member: timestamp(payload['captured_at']).timestamp()})
            pipe.execute()

    def reports(self):
        # Bounded XRANGE pages avoid blocking Redis with the entire report log.
        cursor = '-'
        last = self.redis.xrevrange(self.prefix + 'reports', count=1)
        if not last:
            return
        end = last[0][0]
        while True:
            page = self.redis.xrange(self.prefix + 'reports', min=cursor, max=end, count=500)
            if not page:
                return
            for _, row in page:
                yield row
            cursor = '(' + page[-1][0]

    def summary(self):
        now = datetime.now(timezone.utc)
        cutoff = now - timedelta(days=28)
        week = now - timedelta(days=7)
        members = self.redis.zrevrange(self.prefix + 'download-times', 0, 9)
        downloads = [json.loads(p) for p in self.redis.hmget(self.prefix + 'downloads', members) if p] if members else []
        orgs, metrics, features, daily, first, activity = {}, {}, {}, {}, {}, {}
        hosts = {}
        deployments, network_matches = {}, {}
        weekly_deployments = set()
        recent = self.redis.zrevrange(self.prefix + 'execution-times', 0, 199)
        executions = [json.loads(p) for p in self.redis.hmget(self.prefix + 'executions', recent) if p] if recent else []
        executions = [e for e in executions if timestamp(e['completed_at']) >= cutoff]
        for execution in executions:
            execution['reverse_dns'] = self.reverse_dns(execution.get('public_ip',''))
        active_week, active_month = set(), set()
        connections = connection_errors = 0
        for row in self.reports():
            received, install = timestamp(row['received_at']), row['installation']
            labels, d = json.loads(row['labels']), json.loads(row['delta'])
            attempts = sum(m['attempts'] for m in d['metrics'].values())
            successes = sum(m['success'] for m in d['metrics'].values())
            if successes:
                first[install] = min(first.get(install, received), received)
                activity.setdefault(install, []).append(received)
            if received < cutoff:
                continue
            source = json.loads(row.get('source', '{}'))
            identity = json.loads(row.get('identity','{}'))
            if identity.get('deployment_id'):
                ident = identity['deployment_id']
                deployment = deployments.setdefault(ident, {'deployment_id':ident, 'bases':set(),
                    'installation_ids':set(), 'instance_ids':set(), 'legacy_handles':set(),
                    'hostnames':set(), 'public_ips':set(), 'applications':set(),
                    'attempts':0, 'last_seen':row['received_at']})
                deployment['bases'].add(identity['basis'])
                deployment['installation_ids'].add(install)
                deployment['instance_ids' if identity['instance_kind']=='runtime' else 'legacy_handles'].add(identity['instance_id'])
                if source.get('hostname'): deployment['hostnames'].add(source['hostname'])
                if source.get('public_ip'):
                    deployment['public_ips'].add(source['public_ip'])
                    network_matches.setdefault(('public_ip', source['public_ip']),set()).add(ident)
                if labels.get('application'): deployment['applications'].add(labels['application'])
                deployment['attempts'] += attempts
                deployment['last_seen'] = max(deployment['last_seen'],row['received_at'])
                if attempts and received >= week: weekly_deployments.add(ident)
            key = (source.get('hostname',''), source.get('public_ip',''))
            host = hosts.setdefault(key, {'hostname': key[0], 'public_ip': key[1], 'installations': set(),
                                        'attempts': 0, 'last_seen': row['received_at'], 'id_sources': set()})
            host['installations'].add(install)
            host['attempts'] += attempts
            host['last_seen'] = max(host['last_seen'], row['received_at'])
            host['id_sources'].add(source.get('id_source','unknown'))
            connections += d['connections']
            connection_errors += d['connection_errors']
            if attempts:
                active_month.add(install)
                if received >= week:
                    active_week.add(install)
            day = daily.setdefault(received.date().isoformat(), {'attempts': 0, 'success': 0})
            day['attempts'] += attempts
            day['success'] += successes
            for k, m in d['metrics'].items():
                total = metrics.setdefault(k, {**{c: 0 for c in COUNTERS}, 'latency': [0] * 15, 'ready_latency': [0] * 15})
                for c in COUNTERS:
                    total[c] += m[c]
                for c in ['latency', 'ready_latency']:
                    total[c] = [a+b for a, b in zip(total[c], m[c])]
            for k, count in d['features'].items():
                features[k] = features.get(k, 0) + count
            if attempts or successes:
                key = tuple(labels.get(k, '') for k in ['organization', 'team', 'application', 'environment', 'integration', 'use_case'])
                org = orgs.setdefault(key, {'organization': key[0], 'team': key[1], 'application': key[2], 'environment': key[3], 'integration': key[4], 'use_case': key[5], 'attempts': 0, 'success': 0, 'errors': 0, 'installations': set()})
                org['attempts'] += attempts
                org['success'] += successes
                org['errors'] += sum(m['errors'] for m in d['metrics'].values())
                org['installations'].add(install)
        eligible = [i for i, at in first.items() if at + timedelta(days=35) <= now]
        retained = sum(any(first[i]+timedelta(days=28) <= at < first[i]+timedelta(days=35) for at in activity[i]) for i in eligible)
        for org in orgs.values():
            org['installations'] = len(org['installations'])
        for host in hosts.values():
            host['installations'] = len(host['installations'])
            host['id_sources'] = sorted(host['id_sources'])
            host['reverse_dns'] = self.reverse_dns(host['public_ip'])
        for deployment in deployments.values():
            deployment['installation_ids'] = len(deployment['installation_ids'])
            deployment['runtime_instances'] = len(deployment.pop('instance_ids'))
            deployment['legacy_handles'] = len(deployment['legacy_handles'])
            for field in ['bases','hostnames','public_ips','applications']:
                deployment[field] = sorted(deployment[field])
            names = {self.reverse_dns(ip) for ip in deployment['public_ips']}
            deployment['reverse_dns'] = sorted(name for name in names if name)
            for name in deployment['reverse_dns']:
                network_matches.setdefault(('reverse_dns', name),set()).add(deployment['deployment_id'])
        associations = [{'match_type':kind, 'value':value, 'deployment_ids':sorted(ids),
                         'confidence':'probable_network_only','merged':False}
                        for (kind,value),ids in sorted(network_matches.items()) if len(ids)>1]
        return {'deployments': list(deployments.values()), 'weekly_active_deployments':len(weekly_deployments),
                'probable_associations':associations[:200],
                'hosts': list(hosts.values()), 'executions': executions,
                'execution_sample_limit': 200, 'execution_history_limit': 10000,
                'window_days': 28, 'weekly_active_installations': len(active_week), 'monthly_active_installations': len(active_month),
                'self_reported_organizations': len({o['organization'] for o in orgs.values() if o['organization']}),
                'self_reported_teams': len({(o['organization'], o['team']) for o in orgs.values() if o['organization'] and o['team']}),
                'connections': connections, 'connection_errors': connection_errors,
                'retention_week4': {'eligible': len(eligible), 'retained': retained, 'rate': retained / len(eligible) if eligible else None},
                'metrics': metrics, 'features': features, 'daily': daily, 'organizations': list(orgs.values()),
                'versions': sorted(self.redis.sscan_iter(self.prefix + 'versions', count=100)), 'downloads': downloads,
                'latency_bounds_ms': BOUNDS}


class CollectorServer(ThreadingHTTPServer):
    def server_bind(self):
        # HTTPServer performs a reverse DNS lookup here. It is unnecessary
        # for this collector and can stall startup on corporate DNS/VPNs.
        TCPServer.server_bind(self)
        self.server_name, self.server_port = self.server_address[:2]


def handler(store, write_token, read_token):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass  # Avoid storing addresses, tokens, or request paths in access logs.

        def reply(self, status, payload):
            data = json.dumps(payload).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Cache-Control', 'no-store')
            self.end_headers()
            self.wfile.write(data)

        def authorized(self, token):
            return hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + token)

        def do_POST(self):
            if self.path != '/v1/usage':
                return self.reply(404, {'error': 'not found'})
            if not self.authorized(write_token):
                return self.reply(401, {'error': 'unauthorized'})
            try:
                length = int(self.headers.get('Content-Length', '0'))
                if not 0 < length <= 65536:
                    return self.reply(413, {'error': 'expected body of 1–65536 bytes'})
                payload = json.loads(self.rfile.read(length))
                # Only trust nginx's overwritten header from a loopback peer.
                address = self.client_address[0]
                if os.environ.get('ANALYTICS_TRUST_PROXY') == '1' and ipaddress.ip_address(address).is_loopback:
                    address = self.headers.get('X-Real-IP', '')
                source = {}
                try:
                    parsed = ipaddress.ip_address(address)
                    if parsed.is_global: source['public_ip'] = str(parsed)
                except ValueError:
                    pass
                accepted = store.ingest(payload, source=source)
            except (ValueError, KeyError, TypeError, AttributeError, OverflowError):
                return self.reply(400, {'error': 'invalid aggregate report'})
            except redis.exceptions.RedisError:
                return self.reply(503, {'error': 'storage unavailable; retry later'})
            self.reply(200, {'accepted': accepted})

        def do_GET(self):
            assets = {'/': ('dashboard.html', 'text/html; charset=utf-8'),
                      '/dashboard.js': ('dashboard.js', 'text/javascript; charset=utf-8'),
                      '/dashboard.css': ('dashboard.css', 'text/css; charset=utf-8')}
            if self.path in assets:
                filename, content_type = assets[self.path]
                data = Path(__file__).with_name(filename).read_bytes()
                self.send_response(200)
                self.send_header('Content-Type', content_type)
                self.send_header('Content-Length', str(len(data)))
                self.send_header('Content-Security-Policy', "default-src 'self'; frame-ancestors 'none'; form-action 'self'")
                self.end_headers()
                return self.wfile.write(data)
            if self.path == '/health':
                try:
                    store.redis.ping()
                except redis.exceptions.RedisError:
                    return self.reply(503, {'status': 'storage unavailable'})
                return self.reply(200, {'status': 'ok'})
            if self.path != '/api/summary':
                return self.reply(404, {'error': 'not found'})
            if not self.authorized(read_token):
                return self.reply(401, {'error': 'unauthorized'})
            try:
                self.reply(200, store.summary())
            except redis.exceptions.RedisError:
                self.reply(503, {'error': 'storage unavailable'})
    return Handler


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--redis-url', default=os.environ.get('ANALYTICS_REDIS_URL'),
                        help='Redis URL (or ANALYTICS_REDIS_URL); use a dedicated database')
    parser.add_argument('--host', default='127.0.0.1')
    parser.add_argument('--port', type=int, default=8787)
    args = parser.parse_args()
    write, read = os.environ.get('ANALYTICS_WRITE_TOKEN'), os.environ.get('ANALYTICS_READ_TOKEN')
    if not write or not read or write == read:
        parser.error('set distinct ANALYTICS_WRITE_TOKEN and ANALYTICS_READ_TOKEN')
    if not args.redis_url:
        parser.error('set ANALYTICS_REDIS_URL or --redis-url')
    store = Store(args.redis_url)
    server = CollectorServer((args.host, args.port), handler(store, write, read))
    print(f'Analytics dashboard: http://{args.host}:{args.port}/', flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
        store.close()


if __name__ == '__main__':
    main()
