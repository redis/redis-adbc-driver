"""Backup artifacts must be encrypted, checked, and safely restorable."""
import contextlib
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from backup import backup
from restore import restore


class FakeS3:
    def __init__(self):
        self.objects = {}
    def upload_file(self, filename, bucket, key, ExtraArgs):
        assert ExtraArgs['ServerSideEncryption'] == 'AES256'
        assert ExtraArgs['ChecksumAlgorithm'] == 'SHA256'
        self.objects[key] = Path(filename).read_bytes()
    def put_object(self, Bucket, Key, Body, **kwargs):
        assert kwargs['ServerSideEncryption'] == 'AES256'
        self.objects[Key] = Body
    def get_object(self, Bucket, Key):
        return {'Body': io.BytesIO(self.objects[Key])}


class BackupTests(unittest.TestCase):
    def test_snapshot_upload_and_verified_restore(self):
        s3 = FakeS3()
        data = b'REDIS-test-snapshot'
        def command(argv, **kwargs):
            self.assertNotIn('private-password', ' '.join(argv))
            if '--rdb' in argv:
                self.assertEqual(kwargs['env']['REDISCLI_AUTH'], 'private-password')
                Path(argv[-1]).write_bytes(data)
        with patch.dict(os.environ, ANALYTICS_REDIS_BACKUP_PASSWORD='private-password'), patch('backup.subprocess.run', side_effect=command), contextlib.redirect_stdout(io.StringIO()):
            manifest = backup('private-bucket', s3)
        self.assertEqual(manifest['sha256'], hashlib.sha256(data).hexdigest())
        self.assertEqual(gzip.decompress(s3.objects[manifest['key']]), data)
        with tempfile.TemporaryDirectory() as directory, patch('restore.subprocess.run') as check:
            destination = Path(directory) / 'restored.rdb'
            restore('private-bucket', manifest['key'], destination, s3)
            self.assertEqual(destination.read_bytes(), data)
            self.assertEqual(destination.stat().st_mode & 0o777, 0o600)
            check.assert_called_once()
            with self.assertRaises(ValueError):
                restore('private-bucket', manifest['key'], destination, s3)

    def test_corrupt_backup_is_not_installed(self):
        s3 = FakeS3()
        key = 'backups/corrupt.rdb.gz'
        s3.objects[key] = gzip.compress(b'corrupt')
        s3.objects[key+'.json'] = json.dumps(dict(key=key, rdb_bytes=7, sha256='0'*64)).encode()
        with tempfile.TemporaryDirectory() as directory, patch('restore.subprocess.run') as check:
            destination = Path(directory) / 'restored.rdb'
            with self.assertRaises(ValueError):
                restore('private-bucket', key, destination, s3)
            self.assertFalse(destination.exists())
            check.assert_not_called()
