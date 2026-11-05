"""Protocol, aggregation, access control and download pagination regressions."""
import copy
import os
from unittest.mock import patch
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
import json
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
        self.store = None
        if 'ANALYTICS_TEST_REDIS_URL' in os.environ:
            self.store = Store(os.environ['ANALYTICS_TEST_REDIS_URL'], prefix='adbc:{analytics-test-' + uuid.uuid4().hex + '}:')
            self.addCleanup(self.cleanup_store)

    def cleanup_store(self):
        for key in self.store.redis.scan_iter(match=self.store.prefix + '*', count=100):
            self.store.redis.unlink(key)
        self.store.close()

    def require_store(self):
        if self.store is None:
            self.skipTest('set ANALYTICS_TEST_REDIS_URL to a dedicated Redis database')

    def test_cumulative_retry_restart_and_out_of_order(self):
        self.require_store()
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
        self.require_store()
        now = datetime.now(timezone.utc)
        for install, days in [('retained', [42, 12]), ('lost', [42]), ('young', [20])]:
            for age in days:
                p = report(); p['installation_id'] = install
                self.store.ingest(p, received_at=now-timedelta(days=age))
        r = self.store.summary()['retention_week4']
        self.assertEqual(r, {'eligible': 2, 'retained': 1, 'rate': .5})

    def test_http_auth_and_rejection(self):
        self.require_store()
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

    def test_concurrent_retries_are_counted_once(self):
        self.require_store()
        p = report()
        with ThreadPoolExecutor(max_workers=8) as pool:
            accepted = list(pool.map(lambda _: self.store.ingest(copy.deepcopy(p)), range(24)))
        self.assertEqual(sum(accepted), 1)
        self.assertEqual(self.store.summary()['metrics']['select']['attempts'], 1)

    def test_download_snapshots_roundtrip(self):
        self.require_store()
        p = dict(source='github', captured_at=datetime.now(timezone.utc).isoformat(), total_asset_downloads=10)
        self.store.save_download(p)
        self.store.save_download(p)
        self.assertEqual(self.store.summary()['downloads'], [p])

    def test_execution_samples_sources_and_retry_deduplication(self):
        self.require_store()
        p = report()
        p['source_hostname'] = 'worker.example.test'
        p['installation_id_source'] = 'local_file'
        p['executions'] = [dict(id=str(uuid.uuid4()), kind='select', features=['filter'],
            started_at=p['started_at'], completed_at=p['observed_at'], status='success', duration_ms=123,
            ready_ms=20, rows_returned=3, rows_affected=0, rows_examined=1000, indexed_rows=1000000, rows_represented=None)]
        self.store.redis.set(self.store.prefix+'dns:8.8.8.8','dns.google',ex=60)
        self.store.ingest(p,source={'public_ip':'8.8.8.8'})
        p['sequence'] += 1
        self.store.ingest(p,source={'public_ip':'8.8.8.8'})
        summary=self.store.summary()
        self.assertEqual(len(summary['executions']),1)
        e=summary['executions'][0]
        self.assertEqual((e['rows_examined'],e['rows_returned'],e['indexed_rows']),(1000,3,1000000))
        self.assertEqual(e['reverse_dns'],'dns.google')
        self.assertEqual(summary['hosts'][0]['hostname'],'worker.example.test')
        self.assertEqual(summary['hosts'][0]['installations'],1)
        self.assertEqual(summary['hosts'][0]['id_sources'],['local_file'])
        self.assertEqual(summary['weekly_active_installations'],1)
        bad=copy.deepcopy(p);bad['executions'][0]['sql']='SELECT secret'
        with self.assertRaises(ValueError): validate(bad)
        bad=copy.deepcopy(p);bad['executions'][0]['rows_examined']=-1
        with self.assertRaises(ValueError): validate(bad)
        p['executions'][0]['rows_examined']=None
        validate(p)

    def test_proxy_source_header_requires_explicit_trust(self):
        self.require_store()
        server=CollectorServer(('127.0.0.1',0),handler(self.store,'write','read'))
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        self.addCleanup(server.server_close);self.addCleanup(server.shutdown)
        url='http://127.0.0.1:'+str(server.server_port)+'/v1/usage'
        self.store.redis.set(self.store.prefix+'dns:8.8.8.8','dns.google',ex=60)
        for trust in ['0','1']:
            p=report();p['source_hostname']='host-'+trust
            with patch.dict(os.environ,ANALYTICS_TRUST_PROXY=trust):
                req=urllib.request.Request(url,data=json.dumps(p).encode(),headers={'Authorization':'Bearer write','X-Real-IP':'8.8.8.8'})
                with urllib.request.urlopen(req,timeout=3) as response: self.assertEqual(response.status,200)
        hosts={host['hostname']:host for host in self.store.summary()['hosts']}
        self.assertEqual(hosts['host-0']['public_ip'],'')
        self.assertEqual(hosts['host-1']['public_ip'],'8.8.8.8')

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
