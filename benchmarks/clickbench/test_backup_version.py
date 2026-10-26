import unittest
from backup_version import verify_snapshot_head


class S3Error(Exception):
    def __init__(self, code):
        self.response = {'Error': {'Code': code}}


class FakeS3:
    def __init__(self, head, code=None):
        self.head, self.code, self.calls = head, code, []

    def head_object(self, **kwargs):
        self.calls.append(kwargs)
        if self.code and 'VersionId' in kwargs:
            raise S3Error(self.code)
        return self.head


class BackupVersionTest(unittest.TestCase):
    def setUp(self):
        self.record = {'key': 'rdb/shard', 'version_id': 'saved-version', 'bytes': 123, 'sha256': 'digest'}
        self.head = {'VersionId': 'saved-version', 'ContentLength': 123, 'Metadata': {'sha256': 'digest'}}

    def test_explicit_version(self):
        s3 = FakeS3(self.head)
        self.assertEqual(verify_snapshot_head(s3, 'bucket', self.record)['method'], 'explicit_version')
        self.assertEqual(len(s3.calls), 1)

    def test_access_denied_fallback_requires_same_version(self):
        s3 = FakeS3(self.head, '403')
        self.assertEqual(verify_snapshot_head(s3, 'bucket', self.record)['method'], 'current_head_exact_version_match')
        self.assertNotIn('VersionId', s3.calls[-1])

    def test_fallback_rejects_replaced_or_changed_object(self):
        for key, value in [('VersionId', 'new-version'), ('ContentLength', 124), ('Metadata', {'sha256': 'changed'})]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                verify_snapshot_head(FakeS3({**self.head, key: value}, 'AccessDenied'), 'bucket', self.record)

    def test_other_errors_do_not_fallback(self):
        s3 = FakeS3(self.head, 'NoSuchVersion')
        with self.assertRaises(S3Error):
            verify_snapshot_head(s3, 'bucket', self.record)
        self.assertEqual(len(s3.calls), 1)


if __name__ == '__main__':
    unittest.main()
