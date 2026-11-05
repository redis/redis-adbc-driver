#!/usr/bin/env python3
"""Redis-backed Redis ADBC product usage collector and local dashboard."""
import argparse
from datetime import datetime, timedelta, timezone
import hmac
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
TOP = {'schema_version', 'session_id', 'installation_id', 'sequence', 'started_at', 'observed_at', 'last_query_at', 'driver_version', 'os', 'arch', 'labels', 'connections', 'connection_errors', 'features', 'metrics', 'latency_bounds_ms', 'delivery_failures'}


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
    if not isinstance(p, dict) or set(p) - TOP or TOP - {'last_query_at'} - set(p):
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

    def close(self):
        self.redis.close()

    def ingest(self, payload, received_at=None):
        p = validate(payload)
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
                    keys = ['connections', 'connection_errors', 'features', 'metrics', 'delivery_failures']
                    delta = subtract({k: p[k] for k in keys}, {k: old[k] for k in keys} if old else {})
                    received = received_at or datetime.now(timezone.utc)
                    pipe.multi()
                    pipe.set(session, json.dumps(p))
                    pipe.xadd(self.prefix + 'reports', {
                        'received_at': received.isoformat(), 'installation': p['installation_id'],
                        'labels': json.dumps(p['labels']), 'delta': json.dumps(delta)})
                    pipe.sadd(self.prefix + 'versions', p['driver_version'])
                    pipe.execute()
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
        return {'window_days': 28, 'weekly_active_installations': len(active_week), 'monthly_active_installations': len(active_month),
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
                accepted = store.ingest(payload)
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
