#!/usr/bin/env python3
"""Export a consistent Redis RDB and upload a checked, encrypted S3 backup."""
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
from datetime import datetime, timezone
import boto3


def backup(bucket, client=None):
    client = client or boto3.client('s3')
    stamp = datetime.now(timezone.utc).strftime('%Y/%m/%d/%Y%m%dT%H%M%S.%fZ')
    key = 'backups/' + stamp + '.rdb.gz'
    with tempfile.TemporaryDirectory(prefix='adbc-redis-backup-') as directory:
        rdb = Path(directory) / 'dump.rdb'
        # The password is inherited through the environment, never argv or logs.
        env = {**os.environ, 'REDISCLI_AUTH': os.environ['ANALYTICS_REDIS_BACKUP_PASSWORD']}
        subprocess.run(['redis-cli', '-h', '127.0.0.1', '--user', 'backup', '--rdb', str(rdb)],
                       env=env, check=True, capture_output=True, timeout=300)
        subprocess.run(['redis-check-rdb', str(rdb)], check=True, capture_output=True, timeout=300)
        archive = rdb.with_suffix('.rdb.gz')
        digest = hashlib.sha256()
        with rdb.open('rb') as source, gzip.open(archive, 'wb') as target:
            while chunk := source.read(1024 * 1024):
                digest.update(chunk)
                target.write(chunk)
        manifest = dict(key=key, sha256=digest.hexdigest(), rdb_bytes=rdb.stat().st_size,
                        created_at=datetime.now(timezone.utc).isoformat(),
                        source_commit=os.environ.get('ANALYTICS_SOURCE_COMMIT', 'unknown'))
        client.upload_file(str(archive), bucket, key,
                           ExtraArgs={'ServerSideEncryption': 'AES256', 'ChecksumAlgorithm': 'SHA256'})
        client.put_object(Bucket=bucket, Key=key + '.json', Body=json.dumps(manifest).encode(),
                          ContentType='application/json', ServerSideEncryption='AES256')
        print(json.dumps(manifest), flush=True)
        return manifest


if __name__ == '__main__':
    backup(os.environ['ANALYTICS_BACKUP_BUCKET'])
