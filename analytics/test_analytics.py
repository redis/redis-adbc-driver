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

from collector import BOUNDS, COUNTERS, CollectorServer, Store, handler, validate, traffic_class, workload_size
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
            self.store.redis.set(self.store.prefix+"dns:8.8.8.8","dns.google",ex=60)
            self.store.redis.set(self.store.prefix+"dns:1.1.1.1","one.one.one.one",ex=60)
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

    def test_fixed_diagnostic_allowlist_and_traffic(self):
        self.assertEqual(traffic_class({'hostname':'runnervmabc'}, {}), 'ci_inferred')
        self.assertEqual(traffic_class({'hostname':'runnervmabc','traffic_class':'application'}, {}), 'application')
        self.assertEqual(traffic_class({}, {}), 'unknown')
        self.assertEqual(traffic_class({'hostname':'runnervmabc','traffic_class':'unknown'}, {'environment':'ci'}), 'unknown')
        self.assertEqual(workload_size({'rows_returned':10}), 'unmeasured')
        self.assertEqual(workload_size({'rows_examined':0}), '0')
        self.assertEqual(workload_size({'rows_examined':101,'rows_represented':10001}), '10k–1m')
        p=report();p['traffic_class']='benchmark'
        m=p['metrics']['select'];m['execution_paths']={'aggregate_row_fallback':1};m['group_limit_retries']=2
        validate(p)
        for field,value in [('execution_paths',{'private table':1}),('error_classes',{'raw error':1}),('row_fallbacks',-1),('execution_paths',{'driver_scan':2})]:
            bad=copy.deepcopy(p);bad['metrics']['select'][field]=value
            with self.subTest(field=field,value=value),self.assertRaises(ValueError):validate(bad)
        p['traffic_class']='customer-name'
        with self.assertRaises(ValueError):validate(p)

    def test_version_traffic_grouping_and_diagnostic_deduplication(self):
        self.require_store()
        p=report();p['traffic_class']='application'
        p['metrics']['select'].update(execution_paths={'driver_scan':1},group_limit_retries=2,row_fallbacks=1)
        self.assertTrue(self.store.ingest(p));self.assertFalse(self.store.ingest(p))
        other=report();other['traffic_class']='ci';other['driver_version']='v0.2.0'
        self.store.ingest(other)
        second=copy.deepcopy(p);second['sequence']=2
        second['metrics']['select'].update(attempts=2,success=2,latency=[2]+[0]*14,ready_latency=[2]+[0]*14,execution_paths={'driver_scan':2},group_limit_retries=3)
        self.store.ingest(second)
        summary=self.store.summary('application')
        self.assertEqual(summary['metrics']['select']['attempts'],2)
        self.assertEqual(summary['metrics']['select']['execution_paths'],{'driver_scan':2})
        self.assertEqual(summary['metrics']['select']['group_limit_retries'],3)
        self.assertEqual(summary['metrics']['select']['row_fallbacks'],1)
        self.assertEqual(len(summary['comparisons']),1)
        self.assertEqual(summary['comparisons'][0]['driver_version'],'v0.1.0')
        self.assertEqual(self.store.summary('ci')['metrics']['select']['attempts'],1)
        self.assertFalse(self.store.summary('benchmark')['metrics'])
        changed=copy.deepcopy(second);changed['sequence']=3;changed['traffic_class']='ci'
        with self.assertRaises(ValueError):self.store.ingest(changed)
        # Old stream entries genuinely have no version; do not invent it.
        legacy=report()
        delta={key:legacy[key] for key in ['connections','connection_errors','features','metrics','delivery_failures']}
        self.store.redis.xadd(self.store.prefix+'reports',{'received_at':datetime.now(timezone.utc).isoformat(),'installation':'legacy','labels':'{}','delta':json.dumps(delta),'source':json.dumps({'hostname':'runnervmlegacy'})})
        legacy_summary=self.store.summary('ci_inferred')
        self.assertEqual(legacy_summary['comparisons'][0]['driver_version'],'unknown')
        self.assertEqual(legacy_summary['metrics']['select']['execution_paths'],{})

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
        with request('/api/summary?traffic=application', 'read') as response:
            self.assertEqual(json.load(response)['metrics'], {})
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            request('/api/summary?traffic=private', 'read')
        self.assertEqual(ctx.exception.code, 400)
        ctx.exception.close()
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            request('/api/summary?traffic=ci')
        self.assertEqual(ctx.exception.code, 401)
        ctx.exception.close()
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

    def test_automatic_deployment_association_and_network_changes(self):
        self.require_store()
        def workload(install, instance, key='a'*64):
            p=report();p['installation_id']=install;p['instance_id']=instance
            p['workload_identity']={'provider':'ecs_service','key':key}
            return p
        first=workload('container-a',str(uuid.uuid4()))
        second=workload('replacement-container',str(uuid.uuid4()))
        self.store.ingest(first,source={'public_ip':'8.8.8.8'})
        self.store.ingest(second,source={'public_ip':'1.1.1.1'})
        summary=self.store.summary()
        self.assertEqual(len(summary['deployments']),1)
        d=summary['deployments'][0]
        uuid.UUID(d['deployment_id'])
        self.assertEqual(d['installation_ids'],2)
        self.assertEqual(d['runtime_instances'],2)
        self.assertEqual(d['public_ips'],['1.1.1.1','8.8.8.8'])
        self.assertEqual(summary['weekly_active_deployments'],1)
        self.assertEqual(summary['weekly_active_installations'],2)
        other=workload('container-a',str(uuid.uuid4()),key='b'*64)
        self.store.ingest(other,source={'public_ip':'8.8.8.8'})
        self.assertEqual(len(self.store.summary()['deployments']),2)

    def test_network_only_matches_do_not_merge_deployments(self):
        self.require_store()
        for install in ['new-container-one','new-container-two']:
            p=report();p['installation_id']=install;p['instance_id']=str(uuid.uuid4())
            self.store.ingest(p,source={'public_ip':'8.8.8.8'})
        self.store.redis.set(self.store.prefix+'dns:8.8.8.8','shared.example.test',ex=60)
        summary=self.store.summary()
        self.assertEqual(len(summary['deployments']),2)
        self.assertEqual(summary['weekly_active_deployments'],2)
        self.assertTrue(summary['probable_associations'])
        for association in summary['probable_associations']:
            self.assertEqual(association['confidence'],'probable_network_only')
            self.assertFalse(association['merged'])
            self.assertEqual(len(association['deployment_ids']),2)

    def test_same_uuid_across_networks_and_handles(self):
        self.require_store()
        instance=str(uuid.uuid4())
        for address in ['8.8.8.8','1.1.1.1']:
            p=report();p['instance_id']=instance
            self.store.ingest(p,source={'public_ip':address})
        summary=self.store.summary()
        self.assertEqual(len(summary['deployments']),1)
        self.assertEqual(summary['deployments'][0]['runtime_instances'],1)
        self.assertEqual(summary['deployments'][0]['installation_ids'],1)
        self.assertFalse(summary['probable_associations'])

    def test_temporary_ids_associate_without_mounted_storage(self):
        self.require_store()
        instance=str(uuid.uuid4());events=[]
        for _ in range(2):
            p=report();p['installation_id']=str(uuid.uuid4());p['instance_id']=instance;p['installation_id_source']='temporary'
            self.store.ingest(p);events.append(p)
        summary=self.store.summary()
        self.assertEqual(len(summary['deployments']),1)
        self.assertEqual(summary['deployments'][0]['installation_ids'],2)
        self.assertEqual(summary['deployments'][0]['runtime_instances'],1)
        # An existing installation identity also carries continuity into a new runtime.
        p=copy.deepcopy(events[0]);p['session_id']=str(uuid.uuid4());p['instance_id']=str(uuid.uuid4())
        self.store.ingest(p)
        self.assertEqual(len(self.store.summary()['deployments']),1)

    def test_deployment_alias_creation_is_concurrent_and_stable(self):
        self.require_store()
        payloads=[]
        for index in range(16):
            p=report();p['installation_id']='container-'+str(index);p['instance_id']=str(uuid.uuid4())
            p['workload_identity']={'provider':'kubernetes_controller','key':'f'*64};payloads.append(p)
        with ThreadPoolExecutor(max_workers=8) as pool:
            self.assertTrue(all(pool.map(self.store.ingest,payloads)))
        summary=self.store.summary()
        self.assertEqual(len(summary['deployments']),1)
        self.assertEqual(summary['deployments'][0]['runtime_instances'],16)
        p=copy.deepcopy(payloads[0]);p['sequence']+=1;p['instance_id']=str(uuid.uuid4())
        with self.assertRaises(ValueError):self.store.ingest(p)
        p=report();p['workload_identity']={'provider':'ecs_service','key':'raw-service-name'}
        with self.assertRaises(ValueError):validate(p)

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
