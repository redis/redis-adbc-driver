"""Cloud management ownership and failure checks; no live account needed."""
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
import urllib.error

from cloud_partition_tls import CloudAPI, CloudPrincipals, UnknownOutcome, verify_owned, verified_topology


class CloudManagementSafety(unittest.TestCase):
    def test_cluster_distribution_resp2_and_resp3_exact_keys(self):
        from check_partition_tls import cluster_distribution
        keys = ['owned-row:' + str(i) for i in range(1099)]
        responses = [[1099, *keys], {'total_results':1099,
            'results':[{'id': key, 'values':[]} for key in keys]}]
        for response in responses:
            with self.subTest(response_type=type(response).__name__):
                control = Mock()
                control.execute_command.side_effect = [
                    [[0, 8191, ['a', 1]], [8192, 16383, ['b', 2]]], response]
                result = cluster_distribution(control, 'owned-index', 2)
                self.assertEqual(result['primary_count'], 2)
                self.assertEqual(result['covered_slots'], 16384)
                self.assertEqual(sum(result['owned_rows_per_primary']), 1099)
                self.assertTrue(all(n > 0 for n in result['owned_rows_per_primary']))

    def test_cluster_requirement_cannot_pass_in_proxy_mode(self):
        from check_partition_tls import main
        with patch('sys.stderr', new_callable=io.StringIO):
            with self.assertRaises(SystemExit) as error:
                main(['--output', 'unused.json', '--cluster-mode', 'false',
                      '--minimum-cluster-primaries', '2'])
        self.assertEqual(error.exception.code, 2)

    def test_cluster_distribution_rejects_incomplete_slot_map(self):
        from check_partition_tls import cluster_distribution
        control = Mock()
        control.execute_command.return_value = [[0, 8190, ['a', 1]], [8192, 16383, ['b', 2]]]
        with self.assertRaises(AssertionError):
            cluster_distribution(control, 'owned-index', 2)
        control.execute_command.assert_called_once_with('CLUSTER', 'SLOTS')
        control.execute_command.reset_mock()
        control.execute_command.return_value = [[0, 16383, ['a', 1]]]
        with self.assertRaises(AssertionError):
            cluster_distribution(control, 'owned-index', 2)
        control.execute_command.assert_called_once_with('CLUSTER', 'SLOTS')

    def test_required_topology_uses_live_shard_count(self):
        database = {'clustering': {'numberOfShards': 1}, 'redisVersion': '8.6'}
        with self.assertRaisesRegex(ValueError, 'fewer primary shards'):
            verified_topology(database, 2)
        with self.assertRaisesRegex(ValueError, 'positive'):
            verified_topology(database, 0)
        database.update(clustering={'numberOfShards': 2}, supportOSSClusterApi=True,
                        useExternalEndpointForOSSClusterApi=True)
        self.assertEqual(verified_topology(database, 2), {'shards': 2, 'minimum_shards': 2,
            'oss_cluster_api': True, 'external_cluster_endpoints': True, 'redis_version': '8.6'})

    def test_no_post_replay_or_response_secret_disclosure(self):
        api = CloudAPI({'account': 'account-secret', 'user': 'user-secret'})
        error = urllib.error.HTTPError('https://api.redislabs.com/v1/acl/users', 409,
            'conflict', {}, io.BytesIO(b'{"password":"must-not-appear"}'))
        with patch('urllib.request.urlopen', side_effect=error) as call:
            with self.assertRaises(RuntimeError) as raised:
                api.command('POST', 'acl/users', {'password': 'must-not-appear'})
            self.assertEqual(call.call_count, 1)
            self.assertNotIn('must-not-appear', str(raised.exception))
        api.call = Mock(side_effect=[{'taskId': 'task-1'}, {'status': 'processing-error',
            'response': {'password': 'must-not-appear'}}])
        with self.assertRaises(RuntimeError) as raised:
            api.command('POST', 'acl/users', {})
        self.assertNotIn('must-not-appear', str(raised.exception))
        self.assertEqual(api.pending, set())
        self.assertEqual(api.call.call_args_list[0].args[0], 'POST')
        self.assertEqual(api.call.call_args_list[1].args[0], 'GET')

    def test_shared_subscription_rejected_before_any_mutation(self):
        config = {'disposable': True, 'subscription_name': 'codex-adbc-owned',
                  'database_name': 'codex-adbc-owned', 'subscription_id': 1, 'database_id': 2}
        api = Mock()
        api.call.side_effect = [dict(name='codex-adbc-owned', status='active'),
            {'subscription': [{'databases': [{'databaseId': 2}, {'databaseId': 3}]}]}]
        with self.assertRaisesRegex(ValueError, 'exactly'):
            verify_owned(api, config)
        api.command.assert_not_called()
        self.assertTrue(all(call.args[0] == 'GET' for call in api.call.call_args_list))
        api.reset_mock()
        api.call.side_effect = None
        api.call.return_value = dict(name='someone-elses-subscription', status='active')
        with self.assertRaisesRegex(ValueError, 'ownership'):
            verify_owned(api, config)
        api.command.assert_not_called()

    def test_role_scoped_to_only_owned_database(self):
        principals = CloudPrincipals.__new__(CloudPrincipals)
        principals.config = {'subscription_id': 11, 'database_id': 22}
        principals.people = {}
        principals.create_owned = Mock(return_value=33)
        principals.client = Mock()
        principals.client.return_value.ping.return_value = True
        with tempfile.TemporaryDirectory() as tmp:
            principals.directory = Path(tmp)
            (principals.directory / 'admin.uri').write_text('rediss://default:admin@fixture.invalid:10000/0')
            principals.create('worker-unique', 'worker-secret', ['%R~adbc:*', '+ping'])
        body = principals.create_owned.call_args_list[1].args[1]
        self.assertEqual(body['redisRules'], [{'ruleName': 'worker-unique-rule',
            'databases': [{'subscriptionId': 11, 'databaseId': 22}]}])
        self.assertNotIn('admin', principals.people['worker-unique']['uri'])

    def test_partial_post_reconciled_cleanup_continues_after_failure(self):
        principals = CloudPrincipals.__new__(CloudPrincipals)
        principals.owned, principals.api = [], Mock()
        principals.uncertain = set()
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        principals.directory = Path(directory.name)
        principals.api.pending = set()
        principals.find = Mock(return_value=None)
        principals.api.command.side_effect = RuntimeError('ambiguous response')
        with self.assertRaises(RuntimeError):
            principals.create_owned('users', {'name': 'fresh-unique'})
        self.assertEqual(principals.owned, [('users', 'fresh-unique')])
        principals.owned = [('redisRules', 'rule'), ('roles', 'role'), ('users', 'fresh-unique')]
        principals.find.side_effect = lambda kind, name: {'id': 7} if name == 'fresh-unique' else None
        with self.assertRaisesRegex(RuntimeError, 'users'):
            principals.cleanup()
        self.assertEqual([c.args for c in principals.find.call_args_list[-3:]],
                         [('users', 'fresh-unique'), ('roles', 'role'), ('redisRules', 'rule')])
        principals.owned = []
        principals.find = Mock(return_value={'id': 999})
        with self.assertRaisesRegex(ValueError, 'already exists'):
            principals.create_owned('users', {'name': 'pre-existing'})
        self.assertEqual(principals.owned, [])

    def test_unknown_creation_cannot_be_reported_as_cleaned(self):
        principals = CloudPrincipals.__new__(CloudPrincipals)
        principals.owned, principals.uncertain = [], set()
        principals.resource_ids = {}
        principals.api = Mock()
        principals.api.pending = set()
        principals.api.command.side_effect = UnknownOutcome('response lost')
        principals.find = Mock(return_value=None)
        with tempfile.TemporaryDirectory() as directory:
            principals.directory = Path(directory)
            with self.assertRaises(UnknownOutcome):
                principals.create_owned('users', {'name': 'fresh-unknown'})
            with patch('cloud_partition_tls.wait_for', side_effect=TimeoutError('unresolved')):
                with self.assertRaisesRegex(RuntimeError, 'cleanup failed'):
                    principals.cleanup()
            report = json.loads((principals.directory / 'cloud-acl-cleanup.json').read_text())
            self.assertEqual(report['status'], 'failed')
            self.assertEqual(report['owned_principal_names'], [['users', 'fresh-unknown']])

    def test_interrupted_journal_blocks_new_run_and_can_be_recovered(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = {'disposable': True, 'subscription_id': 11, 'database_id': 22,
                'subscription_name': 'codex-adbc-owned', 'database_name': 'codex-adbc-owned',
                'endpoint': 'fixture.invalid:10000', 'redis_version': '8.6', 'shards': 1}
            (root / 'cloud.json').write_text(json.dumps(config))
            (root / 'admin.uri').write_text('rediss://default:secret@fixture.invalid:10000/0')
            journal = {'status': 'running', 'owned_principal_names': [['users', 'worker-owned']],
                'uncertain_creations': [['users', 'worker-owned']], 'pending_tasks': ['task-1']}
            (root / 'cloud-acl-cleanup.json').write_text(json.dumps(journal))
            api = Mock()
            api.pending = set()
            with patch('cloud_partition_tls.verify_owned', return_value={'publicEndpoint': config['endpoint'],
                    'clustering': {'numberOfShards': 1}, 'redisVersion': '8.6'}):
                with self.assertRaisesRegex(ValueError, 'unfinished'):
                    CloudPrincipals(root, 'schema', api)
                principals = CloudPrincipals(root, None, api, recover=True)
            self.assertEqual(principals.owned, [('users', 'worker-owned')])
            self.assertEqual(principals.uncertain, {('users', 'worker-owned')})
            self.assertEqual(api.pending, {'task-1'})

    def test_permission_update_omits_unchanged_name(self):
        principals = CloudPrincipals.__new__(CloudPrincipals)
        principals.schema = 'owned-schema'
        principals.people = {'worker': {'acl': ['+hmget'], 'rule': 123, 'rule_name': 'owned-rule'}}
        principals.api = Mock()
        principals.client = Mock()
        import redis
        principals.client.return_value.execute_command.side_effect = redis.exceptions.NoPermissionError()
        principals.command('worker', '-hmget')
        principals.api.command.assert_called_once_with('PUT', 'acl/redisRules/123', {'redisRule': '+hmget -hmget'})

    def test_interrupted_journal_replacement_preserves_previous_record(self):
        principals = CloudPrincipals.__new__(CloudPrincipals)
        principals.owned, principals.uncertain = [('users', 'new-user')], set()
        principals.api = Mock()
        principals.api.pending = set()
        with tempfile.TemporaryDirectory() as directory:
            principals.directory = Path(directory)
            previous = {'status': 'running', 'owned_principal_names': [['roles', 'earlier-role']]}
            journal = principals.directory / 'cloud-acl-cleanup.json'
            journal.write_text(json.dumps(previous))
            with patch.object(Path, 'replace', side_effect=OSError('interrupted replacement')):
                with self.assertRaises(OSError):
                    principals.journal()
            self.assertEqual(json.loads(journal.read_text()), previous)

    def test_cleanup_uses_created_id_even_after_name_changes(self):
        principals = CloudPrincipals.__new__(CloudPrincipals)
        principals.owned = [('users', 'original-owned-name')]
        principals.resource_ids = {'users/original-owned-name': 123}
        principals.uncertain = set()
        principals.api = Mock()
        principals.api.pending = set()
        principals.find_id = Mock(side_effect=[{'id': 123, 'name': 'renamed-owned'}, None])
        principals.find = Mock()
        with tempfile.TemporaryDirectory() as directory:
            principals.directory = Path(directory)
            principals.cleanup()
        principals.api.command.assert_called_once_with('DELETE', 'acl/users/123')
        principals.find.assert_not_called()

    def test_management_trust_survives_test_ca_environment(self):
        import ssl
        api = CloudAPI({'account': 'account-secret', 'user': 'user-secret'})
        context = api.context
        with patch.dict('os.environ', {'SSL_CERT_FILE': '/not-a-real-test-ca.pem'}):
            with patch('urllib.request.urlopen', return_value=io.BytesIO(b'{}')) as call:
                api.call('GET', 'subscriptions/11')
        self.assertIs(call.call_args.kwargs['context'], context)
        self.assertTrue(context.check_hostname)
        self.assertEqual(context.verify_mode, ssl.CERT_REQUIRED)


if __name__ == '__main__':
    unittest.main()
