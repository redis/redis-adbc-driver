#!/usr/bin/env python3
"""Export a quiesced legacy collector into a private S3 migration baseline.

Run on the legacy host with its existing ANALYTICS_REDIS_URL environment. This
never prints report contents or credentials. The baseline is create-only; an
existing baseline or published serverless manifest must not be overwritten.
"""
import argparse
import json
import os
from datetime import datetime, timezone
import gzip
import tempfile

from collector import Store
from serverless import State, storage, MAX_STATE_BYTES, encode, pack_session, packed


def export_state(store):
    state = State()
    data = state.data
    for row in store.reports():
        state.append_report(row)
    prefix = store.prefix
    for key in store.redis.scan_iter(match=prefix + 'session:*', count=500):
        payload = json.loads(store.redis.get(key))
        data['sessions'][payload['session_id']] = pack_session(payload)
    data['aliases'] = dict(store.redis.hscan_iter(prefix + 'deployment-aliases', count=500))
    data['executions'] = {k: json.loads(v) for k, v in
                          store.redis.hscan_iter(prefix + 'executions', count=500)}
    data['versions'] = sorted(store.redis.sscan_iter(prefix + 'versions', count=500))
    checked = datetime.now(timezone.utc).timestamp()
    data['dns'] = {key[len(prefix + 'dns:'):]: {'name': store.redis.get(key) or '', 'checked_at': checked}
                   for key in store.redis.scan_iter(match=prefix + 'dns:*', count=500)}
    members = store.redis.zrevrange(prefix + 'download-times', 0, 9)
    data['downloads'] = [json.loads(p) for p in store.redis.hmget(prefix + 'downloads', members) if p] if members else []
    if len(encode(data)) > MAX_STATE_BYTES:
        raise ValueError('legacy history exceeds worker budget; do not cut over')
    return data


def export_stream(store, destination):
    """Keep the legacy host's memory bounded even with a large report history."""
    prefix = store.prefix
    written = 0
    encoder = json.JSONEncoder(separators=(',', ':'), allow_nan=False)
    with gzip.GzipFile(fileobj=destination, mode='wb', mtime=0) as output:
        def write(value):
            nonlocal written
            for chunk in encoder.iterencode(value):
                chunk = chunk.encode()
                written += len(chunk)
                if written > MAX_STATE_BYTES:
                    raise ValueError('legacy checkpoint exceeds worker budget; do not cut over')
                output.write(chunk)

        def entries(items, mapping=False):
            output.write(b'{' if mapping else b'[')
            first = True
            for item in items:
                if not first:
                    output.write(b',')
                first = False
                if mapping:
                    key, value = item
                    write(key); output.write(b':'); write(value)
                else:
                    write(item)
            output.write(b'}' if mapping else b']')

        def blocks():
            page = []
            for row in store.reports():
                page.append(row)
                if len(page) == 1000:
                    yield packed(page)
                    page = []
            if page:
                yield packed(page)

        def sessions():
            for key in store.redis.scan_iter(match=prefix + 'session:*', count=500):
                payload = json.loads(store.redis.get(key))
                yield payload['session_id'], pack_session(payload)

        output.write(b'{"format":1,"reports":[],"rejected":[],"report_blocks":')
        entries(blocks())
        output.write(b',"sessions":'); entries(sessions(), True)
        output.write(b',"aliases":'); entries(store.redis.hscan_iter(prefix + 'deployment-aliases', count=500), True)
        output.write(b',"executions":')
        entries(((k, json.loads(v)) for k, v in store.redis.hscan_iter(prefix + 'executions', count=500)), True)
        output.write(b',"versions":'); entries(store.redis.sscan_iter(prefix + 'versions', count=500))
        output.write(b',"downloads":')
        members = store.redis.zrevrange(prefix + 'download-times', 0, 9)
        entries((json.loads(v) for v in store.redis.hmget(prefix + 'downloads', members) if v) if members else [])
        output.write(b',"dns":')
        checked = datetime.now(timezone.utc).timestamp()
        entries(((key[len(prefix + 'dns:'):], {'name': store.redis.get(key) or '', 'checked_at': checked})
                 for key in store.redis.scan_iter(match=prefix + 'dns:*', count=500)), True)
        output.write(b'}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--quiesced', action='store_true', required=True,
                        help='confirm collector and download writers are stopped or redirected')
    args = parser.parse_args()
    destination = storage()
    manifest, _ = destination.get('manifest.json')
    if manifest:
        parser.error('serverless checkpoint already published; migration must happen first')
    source = Store(os.environ.get('ANALYTICS_MIGRATION_REDIS_URL') or os.environ['ANALYTICS_REDIS_URL'])
    try:
        with tempfile.TemporaryFile() as file:
            export_stream(source, file)
            file.seek(0)
            destination.s3.put_object(Bucket=destination.bucket,
                                      Key=destination.prefix + 'baseline.json.gz', Body=file,
                                      ContentType='application/gzip', ServerSideEncryption='AES256',
                                      IfNoneMatch='*')
    finally:
        source.close()
    print('Private migration baseline uploaded; historical data and deployment IDs preserved.')


if __name__ == '__main__':
    main()
