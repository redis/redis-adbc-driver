"""Fault checks for owned Software management operations; no live cluster needed."""
import argparse
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
import urllib.error

from software_partition_tls import SoftwareAPI, SoftwarePrincipals, prepare


class ManagementSafety(unittest.TestCase):
    def error(self, code):
        return urllib.error.HTTPError('https://localhost:9443/v1/bdbs/1', code,
            'fixture error', {}, io.BytesIO(b'{"password":"must-not-appear"}'))

    def test_retry_only_rejected_database_put(self):
        api = SoftwareAPI(9443)
        with patch('urllib.request.urlopen', side_effect=[self.error(409), io.BytesIO(b'{}')]) as call, patch('time.sleep'):
            self.assertEqual(api.call('PUT', 'bdbs/1', {'roles_permissions': []}), {})
            self.assertEqual(call.call_count, 2)
        for method, path, code in [('POST', 'users', 409), ('PUT', 'redis_acls/1', 409), ('PUT', 'bdbs/1/actions/import', 409), ('GET', 'bdbs/1', 404)]:
            with self.subTest(method=method, path=path):
                with patch('urllib.request.urlopen', side_effect=self.error(code)) as call:
                    with self.assertRaises(RuntimeError) as error:
                        api.call(method, path, {})
                    self.assertEqual(call.call_count, 1)
                    self.assertNotIn('must-not-appear', str(error.exception))
        with patch('urllib.request.urlopen', side_effect=self.error(409)) as call, patch('time.monotonic', side_effect=[0, 121]):
            with self.assertRaises(RuntimeError):
                api.call('PUT', 'bdbs/1', {})
            self.assertEqual(call.call_count, 1)

    def test_container_ownership_before_api_or_certificates(self):
        for container in [
            {'Config': {'Labels': {'com.docker.compose.service': 'other'}}, 'NetworkSettings': {'Ports': {}}},
            {'Config': {'Labels': {'com.docker.compose.service': 'redis-software'}},
             'NetworkSettings': {'Ports': {'9443/tcp': [{'HostIp': '0.0.0.0', 'HostPort': '19443'}]}}},
        ]:
            with tempfile.TemporaryDirectory() as directory:
                args = argparse.Namespace(directory=Path(directory) / 'fresh', container='owned', api_port=19443, db_port=6379, shards=3)
                with patch('subprocess.check_output', return_value=json.dumps([container])), patch('software_partition_tls.SoftwareAPI') as api:
                    with self.assertRaises(ValueError):
                        prepare(args)
                    api.assert_not_called()
                    self.assertFalse(args.directory.exists())

    def test_cleanup_continues_after_failure_and_preserves_other_roles(self):
        principals = SoftwarePrincipals.__new__(SoftwarePrincipals)
        principals.config = {'bdb_uid': 1}
        principals.resources = [('roles', 2), ('redis_acls', 3), ('users', 4)]
        principals.api = Mock()
        def call(method, path, body=None):
            if method == 'GET':
                return {'status': 'active', 'roles_permissions': [
                    {'role_uid': 1, 'redis_acl_uid': 1}, {'role_uid': 2, 'redis_acl_uid': 3}]}
            if path == 'users/4':
                raise RuntimeError('injected user deletion failure')
        principals.api.call.side_effect = call
        with self.assertRaisesRegex(RuntimeError, 'users'):
            principals.cleanup()
        principals.api.call.assert_any_call('PUT', 'bdbs/1', {'roles_permissions': [{'role_uid': 1, 'redis_acl_uid': 1}]})
        principals.api.call.assert_any_call('DELETE', 'redis_acls/3')
        principals.api.call.assert_any_call('DELETE', 'roles/2')


if __name__ == '__main__':
    unittest.main()
