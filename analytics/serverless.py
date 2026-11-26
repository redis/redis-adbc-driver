"""S3-backed ingestion and scheduled summaries; no database or running server.

The HTTP handler acknowledges only durable, immutable report objects. A single
scheduled worker checkpoints accepted cumulative state and publishes all dashboard
filters together through one conditional manifest update. Original reports remain
the recovery log. No request payload, address or token is logged.
"""
import base64
from datetime import datetime, timezone
import gzip
import hashlib
import hmac
import ipaddress
import json
import os
from pathlib import Path
import uuid
from concurrent.futures import ThreadPoolExecutor

import boto3
import dns.resolver
import dns.reversename
from botocore.config import Config
from botocore.exceptions import BotoCoreError, ClientError

from collector import (TRAFFIC_FILTERS, deployment_identity, prepare_report,
                       summarize, timestamp, traffic_class, validate)

MAX_BODY = 65536
MAX_STATE_BYTES = 256 * 1024 * 1024
MAX_OBJECTS = 200000
INVALID = (ValueError, KeyError, TypeError, AttributeError, OverflowError)
MISSING = {'NoSuchKey', '404', 'NotFound'}
CONFLICT = {'PreconditionFailed', 'ConditionalRequestConflict', '412', '409'}
_clients = {}


def encode(value):
    return json.dumps(value, separators=(',', ':'), allow_nan=False).encode()


def session_hash(session):
    return hashlib.sha256(session.encode()).hexdigest()


def packed(value):
    return base64.b64encode(gzip.compress(encode(value), mtime=0)).decode()


def unpacked(value):
    return json.loads(gzip.decompress(base64.b64decode(value)))


def pack_session(payload):
    # Recent executions are stored separately and are never needed to difference
    # the next cumulative report. Pack dormant sessions rather than deserializing
    # their histograms on every scheduled refresh.
    payload = {k: v for k, v in payload.items()
               if k not in {'executions', 'execution_samples_dropped'}}
    return {'sequence': payload['sequence'], 'packed': packed(payload)}


class Aliases:
    def __init__(self, values):
        self.values = values

    def hget(self, key, field):
        return self.values.get(field)

    def hsetnx(self, key, field, value):
        self.values.setdefault(field, value)


class State:
    """Serializable worker state, including the legacy collector's delta history."""
    def __init__(self, data=None):
        self.data = data if data is not None else {
            'format': 1, 'sessions': {}, 'aliases': {}, 'reports': [],
            'executions': {}, 'versions': [], 'downloads': [], 'rejected': []}
        if self.data['format'] != 1:
            raise ValueError('unsupported checkpoint format')
        self.data.setdefault('dns', {})
        self.data.setdefault('report_blocks', [])

    def reports(self):
        for block in self.data['report_blocks']:
            yield from unpacked(block)
        yield from self.data['reports']

    def append_report(self, row):
        self.data['reports'].append(row)
        if len(self.data['reports']) >= 1000:
            self.data['report_blocks'].append(packed(self.data['reports']))
            self.data['reports'] = []

    def ingest(self, envelope):
        payload = envelope['payload']
        previous = self.data['sessions'].get(payload['session_id'])
        if previous and 'packed' in previous:
            previous = unpacked(previous['packed'])
        p, delta = prepare_report(payload, previous)
        if delta is None:
            return False
        received = timestamp(envelope['received_at']).isoformat()
        source = envelope.get('source', {})
        identity = deployment_identity(p, Aliases(self.data['aliases']))
        classification = traffic_class({
            'hostname': p.get('source_hostname', ''),
            **({'traffic_class': p['traffic_class']} if 'traffic_class' in p else {})}, p['labels'])
        self.append_report({
            'received_at': received, 'installation': p['installation_id'],
            'labels': json.dumps(p['labels']), 'delta': json.dumps(delta),
            'identity': json.dumps(identity),
            'source': json.dumps({**source, 'driver_version': p['driver_version'],
                                  'traffic_class': classification,
                                  'hostname': p.get('source_hostname', ''),
                                  'id_source': p.get('installation_id_source', 'unknown')})})
        for execution in p.get('executions', []):
            ident = p['session_id'] + ':' + execution['id']
            self.data['executions'].setdefault(ident, {
                **execution, 'driver_version': p['driver_version'], 'traffic_class': classification,
                'installation_id': p['installation_id'], 'source_hostname': p.get('source_hostname', ''),
                'public_ip': source.get('public_ip', ''), 'received_at': received,
                'application': p['labels'].get('application', ''),
                'deployment_id': identity['deployment_id'], 'instance_id': identity['instance_id']})
        # Same bound and ordering as the legacy execution sample index.
        if len(self.data['executions']) > 10000:
            recent = sorted(self.data['executions'], key=lambda key: (
                timestamp(self.data['executions'][key]['completed_at']), key), reverse=True)[:10000]
            self.data['executions'] = {key: self.data['executions'][key] for key in recent}
        self.data['sessions'][p['session_id']] = pack_session(p)
        self.data['versions'] = sorted(set(self.data['versions']) | {p['driver_version']})
        return True

    def summaries(self, now):
        executions = sorted(self.data['executions'].items(), key=lambda item: (
            timestamp(item[1]['completed_at']), item[0]), reverse=True)[:200]
        return {traffic: {
            **summarize(self.reports(), self.data['downloads'],
                        [e for _, e in executions], self.data['versions'], traffic,
                        reverse_dns=lambda ip: self.data['dns'].get(ip, {}).get('name', ''), now=now),
            'generated_at': now.isoformat(), 'refresh_interval_seconds': 900,
            'rejected_reports': len(self.data['rejected']),
        } for traffic in sorted(TRAFFIC_FILTERS)}


class Storage:
    def __init__(self, s3, bucket, prefix='serverless/'):
        self.s3, self.bucket, self.prefix = s3, bucket, prefix

    def get(self, key, compressed=False):
        try:
            response = self.s3.get_object(Bucket=self.bucket, Key=self.prefix + key)
        except ClientError as error:
            if error.response['Error']['Code'] in MISSING:
                return None, None
            raise
        with response['Body'] as body:
            if compressed:
                with gzip.GzipFile(fileobj=body) as stream:
                    data = stream.read(MAX_STATE_BYTES + 1)
            else:
                data = body.read(MAX_STATE_BYTES + 1)
        if len(data) > MAX_STATE_BYTES:
            raise ValueError('checkpoint or object exceeds worker budget')
        return json.loads(data), response['ETag']

    def put(self, key, value, compressed=False, **conditions):
        body = encode(value)
        if len(body) > MAX_STATE_BYTES:
            raise ValueError('checkpoint exceeds worker budget')
        if compressed:
            body = gzip.compress(body, mtime=0)
        return self.s3.put_object(
            Bucket=self.bucket, Key=self.prefix + key, Body=body,
            ContentType='application/gzip' if compressed else 'application/json',
            ServerSideEncryption='AES256', **conditions)

    def objects(self, prefix):
        count = 0
        pages = self.s3.get_paginator('list_objects_v2').paginate(
            Bucket=self.bucket, Prefix=self.prefix + prefix)
        for page in pages:
            for item in page.get('Contents', []):
                count += 1
                if count > MAX_OBJECTS:
                    raise ValueError('object inventory exceeds worker budget')
                yield item['Key'][len(self.prefix):]

    def ingest(self, payload, source, now):
        validate(payload)
        key = f"reports/{session_hash(payload['session_id'])}/{payload['sequence']:016d}.json"
        try:
            self.put(key, {'payload': payload, 'source': source,
                           'received_at': now.isoformat()}, IfNoneMatch='*')
        except ClientError as error:
            if error.response['Error']['Code'] in {'PreconditionFailed', '412'}:
                return False
            # A concurrent conditional operation may not have committed. Return
            # 503 on 409 so that the caller can retry; never acknowledge a loss.
            raise
        return True

    def refresh(self, context=None, baseline_required=True):
        manifest, etag = self.get('manifest.json')
        if manifest:
            data, _ = self.get(manifest['state'], compressed=True)
            if data is None:
                raise ValueError('published checkpoint missing')
        else:
            data, _ = self.get('baseline.json.gz', compressed=True)
            if data is None and baseline_required:
                raise ValueError('import legacy baseline before enabling summaries')
        state = State(data)
        latest = {session_hash(s): p['sequence'] for s, p in state.data['sessions'].items()}
        rejected = set(state.data['rejected'])
        pending = []
        size = 0
        for key in self.objects('reports/'):
            self.check_time(context)
            _, session, filename = key.split('/')
            if key in rejected or int(filename[:-5]) <= latest.get(session, -1):
                continue
            envelope, _ = self.get(key)
            size += len(encode(envelope))
            if size > MAX_STATE_BYTES:
                raise ValueError('pending reports exceed worker budget')
            pending.append((envelope['received_at'], key, envelope))
        for _, key, envelope in sorted(pending):
            self.check_time(context)
            try:
                state.ingest(envelope)
            except INVALID:
                # Invalid cumulative/session updates are retained in the raw log
                # but never change counters. No payload contents go into logs.
                rejected.add(key)
        state.data['rejected'] = sorted(rejected)
        download_keys = sorted(self.objects('downloads/'), reverse=True)[:10]
        downloads = [self.get(key)[0] for key in download_keys]
        # Include migrated snapshots until superseded by newer scheduled ones.
        combined = {(d['source'], d['captured_at']): d
                    for d in state.data['downloads'] + downloads}
        state.data['downloads'] = sorted(combined.values(), key=lambda d: (
            timestamp(d['captured_at']), d['source']), reverse=True)[:10]
        now = datetime.now(timezone.utc)
        refresh_dns(state, context, now)
        summaries = state.summaries(now)
        self.check_time(context)
        generation = 'generations/' + uuid.uuid4().hex
        self.put(generation + '/state.json.gz', state.data, compressed=True)
        self.put(generation + '/summaries.json', summaries)
        # Readers see either the complete old generation or the complete new one.
        # Failed or overlapping workers cannot publish half of a generation.
        self.put('manifest.json', {'format': 1, 'state': generation + '/state.json.gz',
                                  'previous': [manifest['state'], manifest['summaries']] if manifest else [],
                                  'summaries': generation + '/summaries.json',
                                  'generated_at': now.isoformat()},
                 **({'IfMatch': etag} if etag else {'IfNoneMatch': '*'}))
        # Retain the current and previous generations. In versioned buckets,
        # deleted generations become noncurrent and use the lifecycle policy.
        for key in (manifest or {}).get('previous', []):
            if key.startswith('generations/'):
                try:
                    self.s3.delete_object(Bucket=self.bucket, Key=self.prefix + key)
                except (BotoCoreError, ClientError):
                    pass  # Recovery log and published generation are untouched.
        return {'status': 'ok', 'generated_at': now.isoformat(),
                'processed': len(pending), 'rejected': len(rejected)}

    @staticmethod
    def check_time(context):
        if context and context.get_remaining_time_in_millis() < 30000:
            raise TimeoutError('worker budget exhausted; previous summary retained')


def refresh_dns(state, context, now):
    """Bound PTR work to the scheduled worker; never delay report ingestion."""
    addresses = set()
    for row in state.reports():
        if (now - timestamp(row['received_at'])).days < 28:
            address = json.loads(row.get('source', '{}')).get('public_ip')
            if address:
                addresses.add(address)
    pending = [ip for ip in sorted(addresses)
               if now.timestamp() - state.data['dns'].get(ip, {}).get('checked_at', 0) > 3600][:32]
    Storage.check_time(context)

    def lookup(address):
        name = ''
        try:
            if ipaddress.ip_address(address).is_global:
                answer = dns.resolver.Resolver().resolve(dns.reversename.from_address(address), 'PTR', lifetime=2)
                name = str(answer[0].target).rstrip('.')[:253]
        except (ValueError, dns.exception.DNSException):
            pass
        return address, {'name': name, 'checked_at': now.timestamp()}

    if pending:
        with ThreadPoolExecutor(max_workers=4) as pool:
            state.data['dns'].update(pool.map(lookup, pending))


def storage():
    if 's3' not in _clients:
        _clients['s3'] = boto3.client('s3', config=Config(
            connect_timeout=1, read_timeout=3, retries={'total_max_attempts': 2}))
    return Storage(_clients['s3'], os.environ['ANALYTICS_BUCKET'],
                   os.environ.get('ANALYTICS_PREFIX', 'serverless/'))


def read_token():
    # Reuse the existing token through an encrypted SSM SecureString. Neither
    # Terraform state nor the Lambda environment contains the token itself.
    if 'ssm' not in _clients:
        _clients['ssm'] = boto3.client('ssm')
    return _clients['ssm'].get_parameter(
        Name=os.environ['ANALYTICS_READ_TOKEN_PARAMETER'], WithDecryption=True)['Parameter']['Value']


def reply(status, payload, content_type='application/json'):
    body = encode(payload).decode() if content_type == 'application/json' else payload
    return {'statusCode': status, 'headers': {
        'content-type': content_type, 'cache-control': 'no-store',
        'x-content-type-options': 'nosniff',
        'content-security-policy': "default-src 'self'; frame-ancestors 'none'; form-action 'self'",
    }, 'body': body}


def api(event, context):
    http = event.get('requestContext', {}).get('http', {})
    path, method = event.get('rawPath', ''), http.get('method')
    try:
        if method == 'POST' and path == '/v1/usage':
            try:
                body = event.get('body') or ''
                # Check encoded length before allocating decoded bytes.
                if len(body) > MAX_BODY * 4 // 3 + 4:
                    return reply(413, {'error': 'expected body of 1–65536 bytes'})
                raw = base64.b64decode(body, validate=True) if event.get('isBase64Encoded') else body.encode()
                if not 0 < len(raw) <= MAX_BODY:
                    return reply(413, {'error': 'expected body of 1–65536 bytes'})
                payload = json.loads(raw)
                validate(payload)
                source = {}
                # API Gateway's trusted context replaces all client IP headers.
                address = ipaddress.ip_address(http['sourceIp'])
                if address.is_global:
                    source['public_ip'] = str(address)
            except INVALID:
                return reply(400, {'error': 'invalid aggregate report'})
            accepted = storage().ingest(payload, source, datetime.now(timezone.utc))
            return reply(200, {'accepted': accepted})
        if method != 'GET':
            return reply(404, {'error': 'not found'})
        assets = {'/': ('dashboard.html', 'text/html; charset=utf-8'),
                  '/dashboard.js': ('dashboard.js', 'text/javascript; charset=utf-8'),
                  '/dashboard.css': ('dashboard.css', 'text/css; charset=utf-8')}
        if path in assets:
            filename, content_type = assets[path]
            return reply(200, Path(__file__).with_name(filename).read_text(), content_type)
        if path == '/health':
            manifest, _ = storage().get('manifest.json')
            return reply(200 if manifest else 503, {
                'status': 'ok' if manifest else 'awaiting first summary',
                'generated_at': manifest['generated_at'] if manifest else None})
        if path != '/api/summary':
            return reply(404, {'error': 'not found'})
        authorization = {k.lower(): v for k, v in event.get('headers', {}).items()}.get('authorization', '')
        if not authorization.startswith('Bearer ') or not hmac.compare_digest(authorization.encode(), ('Bearer ' + read_token()).encode()):
            return reply(401, {'error': 'unauthorized'})
        traffic = (event.get('queryStringParameters') or {}).get('traffic', 'all')
        if traffic not in TRAFFIC_FILTERS:
            return reply(400, {'error': 'invalid traffic filter'})
        store = storage()
        manifest, _ = store.get('manifest.json')
        if not manifest:
            return reply(503, {'error': 'awaiting first summary'})
        summaries, _ = store.get(manifest['summaries'])
        if summaries is None:
            return reply(503, {'error': 'summary unavailable'})
        if traffic not in summaries:
            # An older worker's generation can predate a newly added filter.
            return reply(503, {'error': 'summary for this traffic filter is not published yet; retry after the next refresh'})
        return reply(200, summaries[traffic])
    except (BotoCoreError, ClientError):
        return reply(503, {'error': 'storage unavailable; retry later'})


def refresh(event, context):
    return storage().refresh(context, os.environ.get('ANALYTICS_BASELINE_REQUIRED', 'true') == 'true')


def downloads(event, context):
    from downloads import github, pypi
    store = storage()
    errors = []
    for name, fetch in [('github', lambda: github('redis/redis-adbc-driver')),
                        ('pypistats', lambda: pypi('adbc-driver-redis'))]:
        try:
            payload = fetch()
            store.put(f"downloads/{payload['captured_at']}-{name}.json", payload, IfNoneMatch='*')
        except (OSError, ValueError, KeyError, BotoCoreError, ClientError):
            errors.append(name)
    if errors:
        raise RuntimeError('download snapshot failed: ' + ', '.join(errors))
    return {'status': 'ok'}
