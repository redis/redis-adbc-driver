"""Protocol, aggregation, access control and download pagination regressions."""
import copy
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
import uuid

from collector import BOUNDS, COUNTERS, CollectorServer, Store, handler, validate
from downloads import github, pypi


def report(attempts=1, success=1):
    now = datetime.now(timezone.utc).isoformat()
    metric = {k: 0 for k in COUNTERS}
    metric.update(attempts=attempts, success=success, rows=success * 10,
                  latency=[success] + [0]*14, ready_latency=[attempts] + [0]*14)
    return dict(schema_version=1, session_id=str(uuid.uuid4()), installation_id='stable-install',
                sequence=1, started_at=now, observed_at=now, last_query_at=now,
                driver_version='v0.1.0', os='linux', arch='amd64',
                labels=dict(organization='Example', team='Analytics', application='BI'),
                connections=1, connection_errors=0, features={'filter': attempts},
                metrics={'select': metric}, latency_bounds_ms=BOUNDS, delivery_failures=0)


class AnalyticsTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.store = Store(Path(self.directory.name) / 'analytics.sqlite3')

    def test_cumulative_retry_restart_and_out_of_order(self):
        p = report()
        self.assertTrue(self.store.ingest(p))
        self.assertFalse(self.store.ingest(p))
        second = copy.deepcopy(p)
        second['sequence'] = 3
        second['metrics']['select'].update(attempts=3, success=2, rows=20,
                                          latency=[2]+[0]*14, ready_latency=[3]+[0]*14)
        second['features']['filter'] = 3
        self.assertTrue(self.store.ingest(second))
        self.assertFalse(self.store.ingest(p))
        restarted = report()
        self.store.ingest(restarted)
        summary = self.store.summary()
        self.assertEqual(summary['metrics']['select']['attempts'], 4)
        self.assertEqual(summary['metrics']['select']['success'], 3)
        self.assertEqual(summary['weekly_active_installations'], 1)
        self.assertEqual(summary['self_reported_organizations'], 1)
        self.assertEqual(summary['self_reported_teams'], 1)
        self.assertEqual(summary['organizations'][0]['installations'], 1)
        self.assertEqual(summary['features']['filter'], 4)
        regressed = copy.deepcopy(second)
        regressed['sequence'] = 4
        regressed['connections'] = 0
        with self.assertRaises(ValueError):
            self.store.ingest(regressed)
        changed = copy.deepcopy(second)
        changed['sequence'] = 4
        changed['labels']['team'] = 'Changed'
        with self.assertRaises(ValueError):
            self.store.ingest(changed)

    def test_payload_allowlist_and_consistency(self):
        cases = []
        p = report(); p['sql'] = 'SELECT secret'; cases.append(p)
        p = report(); p['labels']['password'] = 'secret'; cases.append(p)
        p = report(); p['metrics']['secret table'] = p['metrics']['select']; cases.append(p)
        p = report(); p['metrics']['select']['attempts'] = -1; cases.append(p)
        p = report(); p['metrics']['select']['success'] = 2; cases.append(p)
        p = report(); p['metrics']['select']['latency'] = []; cases.append(p)
        p = report(); p['connections'] = True; cases.append(p)
        p = report(); p['labels']['team'] = 'x'*257; cases.append(p)
        for p in cases:
            with self.subTest(payload=p), self.assertRaises(ValueError):
                validate(p)

    def test_retention_needs_full_window_and_success(self):
        now = datetime.now(timezone.utc)
        for install, days in [('retained', [42, 12]), ('lost', [42]), ('young', [20])]:
            for age in days:
                p = report(); p['installation_id'] = install
                self.store.ingest(p)
                with self.store.connect() as db:
                    db.execute('UPDATE reports SET received_at=? WHERE session=?', ((now-timedelta(days=age)).isoformat(), p['session_id']))
        r = self.store.summary()['retention_week4']
        self.assertEqual(r, {'eligible': 2, 'retained': 1, 'rate': .5})

    def test_http_auth_and_rejection(self):
        server = CollectorServer(('127.0.0.1', 0), handler(self.store, 'write', 'read'))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        base = 'http://127.0.0.1:' + str(server.server_port)
        def request(path, token=None, data=None):
            headers = {'Authorization': 'Bearer '+token} if token else {}
            return urllib.request.urlopen(urllib.request.Request(base+path, data=data, headers=headers), timeout=3)
        for token in [None, 'write']:
            with self.assertRaises(urllib.error.HTTPError) as ctx:
                request('/api/summary', token)
            self.assertEqual(ctx.exception.code, 401)
            ctx.exception.close()
        with request('/v1/usage', 'write', json.dumps(report()).encode()) as response:
            self.assertTrue(json.load(response)['accepted'])
        with request('/api/summary', 'read') as response:
            self.assertEqual(json.load(response)['metrics']['select']['success'], 1)
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            request('/v1/usage', 'write', b'{"sql":"private"}')
        self.assertEqual(ctx.exception.code, 400)
        ctx.exception.close()
        with request('/') as response:
            self.assertIn(b'Adoption analytics', response.read())
        for path, mime in [('/dashboard.js', 'text/javascript'), ('/dashboard.css', 'text/css')]:
            with request(path) as response:
                self.assertIn(mime, response.headers['Content-Type'])
                self.assertGreater(len(response.read()), 100)
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            request('/../collector.py')
        self.assertEqual(ctx.exception.code, 404)
        ctx.exception.close()

    def test_download_asset_pagination(self):
        calls = []
        def get(url, token=None):
            calls.append(url)
            if '/assets?' in url:
                return [dict(id=i, name=f'wheel-{i}', download_count=2) for i in range(100)] if url.endswith('&page=1') else [dict(id=101, name='last', download_count=3)]
            return [dict(id=12, tag_name='v1')]
        payload = github('redis/redis-adbc-driver', get)
        self.assertEqual(payload['total_asset_downloads'], 203)
        self.assertEqual(len(payload['assets']), 101)
        self.assertEqual(len(calls), 3)
        payload = pypi('adbc-driver-redis', lambda url: {'data': {'last_day': 1, 'last_week': 7, 'last_month': 30}})
        self.assertEqual(payload['data']['last_month'], 30)


if __name__ == '__main__':
    unittest.main()
