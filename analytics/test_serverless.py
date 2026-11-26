"""Durable acknowledgement, checkpoint recovery and legacy migration parity."""
import copy
from datetime import datetime, timezone
import hashlib
import gzip
import io
import json
import os
from unittest.mock import patch
import unittest
import uuid

from botocore.exceptions import ClientError
from collector import Store, TRAFFIC_FILTERS
from migrate_serverless import export_state, export_stream
from serverless import State, Storage, api, encode
from test_analytics import report


def error(code):
    return ClientError({'Error': {'Code': code}}, 'S3TestOperation')


class MemoryS3:
    """Exercise S3's conditional object boundary without a running database."""
    def __init__(self):
        self.values = {}
        self.fail_suffix = None
        self.reads = []

    def put_object(self, Bucket, Key, Body, **kwargs):
        if self.fail_suffix and Key.endswith(self.fail_suffix):
            raise error('ServiceUnavailable')
        old = self.values.get(Key)
        if kwargs.get('IfNoneMatch') == '*' and old:
            raise error('PreconditionFailed')
        if kwargs.get('IfMatch') and (not old or old[1] != kwargs['IfMatch']):
            raise error('PreconditionFailed')
        etag = '"' + hashlib.sha256(Body).hexdigest() + '"'
        self.values[Key] = (bytes(Body), etag)
        return {'ETag': etag}

    def get_object(self, Bucket, Key):
        self.reads.append(Key)
        if Key not in self.values:
            raise error('NoSuchKey')
        body, etag = self.values[Key]
        return {'Body': io.BytesIO(body), 'ETag': etag}

    def delete_object(self, Bucket, Key):
        self.values.pop(Key, None)

    def get_paginator(self, operation):
        return self

    def paginate(self, Bucket, Prefix):
        keys = sorted(key for key in self.values if key.startswith(Prefix))
        # Multiple pages catch implementations that silently use only page one.
        for start in range(0, len(keys), 2):
            yield {'Contents': [{'Key': key} for key in keys[start:start + 2]]}


class ServerlessTests(unittest.TestCase):
    def setUp(self):
        self.s3 = MemoryS3()
        self.store = Storage(self.s3, 'private-test-bucket')
        self.now = datetime.now(timezone.utc)

    def ingest(self, p):
        return self.store.ingest(p, {}, self.now)

    def summary(self):
        manifest, _ = self.store.get('manifest.json')
        summaries, _ = self.store.get(manifest['summaries'])
        return summaries['all']

    def event(self, method='POST', path='/v1/usage', body=None):
        return {'requestContext': {'http': {'method': method, 'sourceIp': '8.8.8.8'}},
                'rawPath': path, 'body': json.dumps(body) if body is not None else '',
                'headers': {'X-Real-IP': '1.1.1.1', 'X-Forwarded-For': '1.1.1.1'}}

    def test_durable_create_only_acknowledgement_and_storage_failure(self):
        p = report()
        with patch('serverless.storage', return_value=self.store):
            self.assertEqual(api(self.event(body=p), None)['statusCode'], 200)
            self.assertFalse(json.loads(api(self.event(body=p), None)['body'])['accepted'])
            key = next(iter(self.s3.values))
            envelope, _ = self.store.get(key.removeprefix('serverless/'))
            self.assertEqual(envelope['source'], {'public_ip': '8.8.8.8'})
            self.s3.fail_suffix = '.json'
            self.assertEqual(api(self.event(body=report()), None)['statusCode'], 503)
            self.assertEqual(len(self.s3.values), 1)

    def test_wire_privacy_body_limits_and_dashboard_auth(self):
        with patch('serverless.storage', return_value=self.store), patch('serverless.read_token', return_value='private'):
            p = report(); p['sql'] = 'private SQL'
            self.assertEqual(api(self.event(body=p), None)['statusCode'], 400)
            self.assertFalse(self.s3.values)
            event = self.event(); event['body'] = 'x' * 65537
            self.assertEqual(api(event, None)['statusCode'], 413)
            event['body'] = '%%%'; event['isBase64Encoded'] = True
            self.assertEqual(api(event, None)['statusCode'], 400)
            event = self.event('GET', '/api/summary')
            self.assertEqual(api(event, None)['statusCode'], 401)
            event['headers']['Authorization'] = 'Bearer private'
            self.assertEqual(api(event, None)['statusCode'], 503)
            self.store.refresh(baseline_required=False)
            self.assertEqual(api(event, None)['statusCode'], 200)
            event['queryStringParameters'] = {'traffic': 'secret'}
            self.assertEqual(api(event, None)['statusCode'], 400)
            self.assertEqual(api(self.event('GET', '/../collector.py'), None)['statusCode'], 404)

    def test_retry_order_and_incremental_refresh(self):
        p = report()
        self.ingest(p)
        self.store.refresh(baseline_required=False)
        second = copy.deepcopy(p)
        second['sequence'] = 3
        second['connections'] = 3
        self.ingest(second)
        self.assertFalse(self.ingest(p))
        # This arrives after sequence 3 but is an older cumulative snapshot.
        second['sequence'] = 2; second['connections'] = 2
        self.ingest(second)
        self.store.refresh()
        self.assertEqual(self.summary()['connections'], 3)
        self.assertEqual(self.summary()['metrics']['select']['attempts'], 1)
        self.s3.reads.clear()
        self.store.refresh()
        self.assertFalse(any('/reports/' in key for key in self.s3.reads))
        self.assertEqual(self.summary()['connections'], 3)

    def test_publish_failure_keeps_previous_generation_and_retry_counts_once(self):
        self.ingest(report())
        self.store.refresh(baseline_required=False)
        before = self.summary()
        p = report(); self.ingest(p)
        self.s3.fail_suffix = '/summaries.json'
        with self.assertRaises(ClientError):
            self.store.refresh()
        self.assertEqual(self.summary(), before)
        self.s3.fail_suffix = None
        self.store.refresh()
        self.assertEqual(self.summary()['metrics']['select']['attempts'], 2)
        self.store.refresh()
        self.assertEqual(self.summary()['metrics']['select']['attempts'], 2)

    def test_regressed_counters_and_changed_identity_are_quarantined(self):
        p = report(); self.ingest(p)
        self.store.refresh(baseline_required=False)
        p['sequence'] = 2; p['connections'] = 0
        self.ingest(p)
        p['sequence'] = 3; p['labels']['team'] = 'Changed'
        self.ingest(p)
        self.ingest(report())
        self.store.refresh()
        self.assertEqual(self.summary()['connections'], 2)
        self.assertEqual(self.summary()['rejected_reports'], 2)
        self.store.refresh()
        self.assertEqual(self.summary()['rejected_reports'], 2)

    def test_baseline_required_and_state_budget_failure(self):
        with self.assertRaisesRegex(ValueError, 'baseline'):
            self.store.refresh()
        self.assertNotIn('serverless/manifest.json', self.s3.values)
        self.ingest(report())
        with patch('serverless.MAX_STATE_BYTES', 10), self.assertRaises(ValueError):
            self.store.refresh(baseline_required=False)
        self.assertNotIn('serverless/manifest.json', self.s3.values)

    def test_manifest_conflict_retains_winning_generation(self):
        self.store.refresh(baseline_required=False)
        manifest, etag = self.store.get('manifest.json')
        self.store.put('manifest.json', {**manifest, 'winner': True}, IfMatch=etag)
        with self.assertRaises(ClientError):
            self.store.put('manifest.json', manifest, IfMatch=etag)
        self.assertTrue(self.store.get('manifest.json')[0]['winner'])

    def test_runtime_identity_and_filters(self):
        runtime = str(uuid.uuid4())
        for category in ['ci', 'application']:
            p = report(); p.update(instance_id=runtime, installation_id_source='temporary',
                                   installation_id=str(uuid.uuid4()), traffic_class=category)
            self.ingest(p)
        self.store.refresh(baseline_required=False)
        manifest, _ = self.store.get('manifest.json')
        summaries, _ = self.store.get(manifest['summaries'])
        self.assertEqual(set(summaries), TRAFFIC_FILTERS)
        self.assertEqual(summaries['all']['weekly_active_deployments'], 1)
        self.assertEqual(summaries['ci']['connections'], 1)
        self.assertEqual(summaries['application']['connections'], 1)

    def test_exclusion_presets_hide_ci_and_benchmark_traffic(self):
        for category, host in [('application', 'app'), ('unknown', 'other'), ('benchmark', 'bench'),
                               ('ci', 'ci-host'), (None, 'runnervm1abc')]:
            p = report(); p.update(installation_id=str(uuid.uuid4()), source_hostname=host)
            if category:
                p['traffic_class'] = category
            self.ingest(p)
        self.store.refresh(baseline_required=False)
        manifest, _ = self.store.get('manifest.json')
        summaries, _ = self.store.get(manifest['summaries'])
        self.assertEqual(set(summaries), TRAFFIC_FILTERS)
        connections = {traffic: summary['connections'] for traffic, summary in summaries.items()}
        self.assertEqual(connections['all'], 5)
        self.assertEqual(connections['ci_inferred'], 1)
        self.assertEqual(connections['exclude_ci'], 3)  # application, unknown and benchmark
        self.assertEqual(connections['exclude_ci_benchmark'], 2)  # application and unknown
        self.assertEqual(summaries['exclude_ci_benchmark']['traffic_filter'], 'exclude_ci_benchmark')
        with patch('serverless.storage', return_value=self.store), patch('serverless.read_token', return_value='private'):
            event = self.event('GET', '/api/summary')
            event['headers']['Authorization'] = 'Bearer private'
            event['queryStringParameters'] = {'traffic': 'exclude_ci_benchmark'}
            response = api(event, None)
            self.assertEqual(response['statusCode'], 200)
            self.assertEqual(json.loads(response['body'])['connections'], 2)
            # A generation published by an older worker lacks the presets.
            del summaries['exclude_ci_benchmark']
            self.store.put(manifest['summaries'], summaries)
            response = api(event, None)
            self.assertEqual(response['statusCode'], 503)
            self.assertIn('not published yet', json.loads(response['body'])['error'])

    def test_legacy_migration_and_future_reports_match_redis(self):
        if 'ANALYTICS_TEST_REDIS_URL' not in os.environ:
            self.skipTest('requires the verified Redis Software fixture')
        legacy = Store(os.environ['ANALYTICS_TEST_REDIS_URL'], 'adbc:{s3-parity-' + uuid.uuid4().hex + '}:')
        try:
            p = report(); legacy.ingest(p)
            baseline = export_state(legacy)
            stream = io.BytesIO()
            export_stream(legacy, stream)
            streamed = State(json.loads(gzip.decompress(stream.getvalue())))
            self.assertEqual(list(streamed.reports()), baseline['reports'])
            self.assertEqual(streamed.data['sessions'], baseline['sessions'])
            self.store.put('baseline.json.gz', baseline, compressed=True, IfNoneMatch='*')
            p['sequence'] = 2; p['connections'] = 4
            legacy.ingest(copy.deepcopy(p), received_at=self.now)
            self.ingest(p)
            self.store.refresh()
            expected = legacy.summary()
            actual = self.summary()
            for field in ['generated_at', 'refresh_interval_seconds', 'rejected_reports']:
                actual.pop(field)
            self.assertEqual(actual, expected)
            before = copy.deepcopy(actual)
            # A restored baseline must keep session counters and alias IDs,
            # and must not count an already migrated report again.
            self.assertEqual(before['connections'], 4)
            self.assertEqual(before['deployments'][0]['deployment_id'],
                             json.loads(baseline['reports'][0]['identity'])['deployment_id'])
        finally:
            for key in legacy.redis.scan_iter(match=legacy.prefix + '*'):
                legacy.redis.unlink(key)
            legacy.close()

    def test_packed_history_preserves_exact_receipts_and_counters(self):
        state = State()
        p = report()
        # Force a full history block and a tail without losing exact receipt times.
        for sequence in range(1, 1003):
            p['sequence'] = sequence
            p['connections'] = sequence
            state.ingest({'payload': p, 'source': {}, 'received_at': self.now.isoformat()})
        self.assertEqual(len(state.data['report_blocks']), 1)
        self.assertEqual(len(state.data['reports']), 2)
        self.assertEqual(len(list(state.reports())), 1002)
        restored = State(json.loads(encode(state.data)))
        self.assertEqual(restored.summaries(self.now)['all']['connections'], 1002)
        self.assertFalse(restored.ingest({'payload': p, 'source': {}, 'received_at': self.now.isoformat()}))


if __name__ == '__main__':
    unittest.main()
