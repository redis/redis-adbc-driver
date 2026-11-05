#!/usr/bin/env python3
"""Verify an S3 RDB backup into a new file; never overwrite a running database."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import boto3


def restore(bucket, key, destination, client=None):
    client = client or boto3.client('s3')
    destination = Path(destination)
    if destination.exists():
        raise ValueError('restore destination must not exist')
    response = client.get_object(Bucket=bucket, Key=key + '.json')
    with response['Body'] as body:
        manifest = json.load(body)
    if manifest['key'] != key or not 0 < manifest['rdb_bytes'] <= 2**40:
        raise ValueError('invalid backup manifest')
    response = client.get_object(Bucket=bucket, Key=key)
    digest, size = hashlib.sha256(), 0
    with tempfile.TemporaryDirectory(dir=destination.parent, prefix='.adbc-restore-') as directory:
        pending = Path(directory) / 'dump.rdb'
        with response['Body'] as body, gzip.GzipFile(fileobj=body) as source, pending.open('wb') as target:
            while chunk := source.read(1024 * 1024):
                size += len(chunk)
                if size > manifest['rdb_bytes']:
                    raise ValueError('backup exceeds manifest size')
                digest.update(chunk)
                target.write(chunk)
        if size != manifest['rdb_bytes'] or digest.hexdigest() != manifest['sha256']:
            raise ValueError('backup checksum mismatch')
        subprocess.run(['redis-check-rdb', str(pending)], check=True, capture_output=True, timeout=300)
        # Link exclusively: never overwrite an existing destination, even on a race.
        os.link(pending, destination)
        os.chmod(destination, 0o600)
    return manifest


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bucket', required=True)
    parser.add_argument('--key', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    print(json.dumps(restore(args.bucket, args.key, args.output)))
