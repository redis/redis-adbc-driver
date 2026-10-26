"""All-primary ownership and partial-failure checks without a live server."""
import json
import base64
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import Mock, MagicMock, patch
import redis
import pyarrow as pa

from oss_cluster_partition_tls import ClusterPrincipals, declared_primaries
from partition_tls_resharding import SlotMigration, RepeatedWorker, wait_search_convergence
from check_partition_tls import worker_failure_summary, native_search_ready


class ClusterManagementSafety(unittest.TestCase):
    @staticmethod
    def arrow_worker_result(table):
        sink = pa.BufferOutputStream()
        with pa.ipc.new_stream(sink, table.schema) as writer:
            writer.write_table(table)
        return subprocess.CompletedProcess([], 0,
            stdout=base64.b64encode(sink.getvalue()).decode(), stderr='')

    def test_native_readiness_requires_missing_migrated_row_to_return(self):
        expected = pa.table({'id':[0, 1], 'label':['migrated', 'other']})
        invoke = Mock(return_value=self.arrow_worker_result(expected.slice(1)))
        self.assertFalse(native_search_ready(invoke, [b'private'], expected))
        invoke.return_value = self.arrow_worker_result(expected)
        self.assertTrue(native_search_ready(invoke, [b'private'], expected))

    def test_native_readiness_never_retries_corrupt_values_or_schema(self):
        expected = pa.table({'id':[0, 1], 'label':['migrated', 'other']})
        for actual in (pa.table({'id':[0, 0], 'label':['migrated', 'migrated']}),
                       pa.table({'id':[2], 'label':['unexpected']}),
                       pa.table({'id':[0], 'label':['changed']}),
                       pa.table({'id':[0], 'extra':['wrong schema']})):
            with self.subTest(schema=actual.schema):
                invoke = Mock(return_value=self.arrow_worker_result(actual))
                with self.assertRaisesRegex(AssertionError, 'invalid fixture values'):
                    native_search_ready(invoke, [b'private'], expected)

    def test_native_readiness_only_tolerates_known_topology_error(self):
        invoke = Mock(return_value=subprocess.CompletedProcess([], 1,
            stdout='', stderr='Query requires unavailable slots'))
        self.assertFalse(native_search_ready(invoke, [b'private'], pa.table({'id':[0]})))
        invoke.return_value.stderr = 'NOPERM private credentials'
        with self.assertRaisesRegex(AssertionError, 'categories=permission_denied'):
            native_search_ready(invoke, [b'private'], pa.table({'id':[0]}))

    def test_raw_readiness_cannot_bypass_native_worker_and_deadline(self):
        client = Mock(spec=redis.Redis)
        client.execute_command.side_effect = lambda *args, **kwargs: (
            [{'results':[{'extra_attributes':{'__rowid':'1'}}]}, 0]
            if 'WITHCURSOR' in args else {'results':[{'extra_attributes':{'counted':'1'}}]})
        probe = Mock(return_value=False)
        with self.assertRaisesRegex(AssertionError, 'within deadline'):
            wait_search_convergence([client], 'owned-index', 1, timeout=0, native_probe=probe)
        probe.assert_called_once()
        probe.side_effect = [False, True, False, True, True, True]
        with patch('partition_tls_resharding.time.sleep'):
            self.assertEqual(wait_search_convergence([client], 'owned-index', 1,
                native_probe=probe), 6)

    def test_worker_diagnostics_classify_without_echoing_private_errors(self):
        result = subprocess.CompletedProcess([], 1, stdout='private descriptor',
            stderr='rediss://user:secret@private-host Query requires unavailable slots')
        self.assertEqual(worker_failure_summary(result),
                         'exit=1; categories=search_topology_unavailable')
        result.stderr = 'private SQL and credentials'
        self.assertEqual(worker_failure_summary(result), 'exit=1; categories=unclassified')

    def test_sortable_membership_cannot_hide_missing_hash_payload(self):
        client = Mock(spec=redis.Redis)
        def reply(*args, **kwargs):
            if 'WITHCURSOR' not in args:
                return {'results': [{'extra_attributes': {'counted': '2'}}]}
            # A stale index still has both SORTABLE IDs, but only one HASH can
            # be loaded on the shard serving that index entry after migration.
            ids = [2] if ('LOAD', '*') in list(zip(args, args[1:])) else [1, 2]
            return [{'results': [{'extra_attributes': {'__rowid': str(i)}} for i in ids]}, 0]
        client.execute_command.side_effect = reply
        with patch('partition_tls_resharding.time.sleep'), patch(
                'partition_tls_resharding.time.monotonic', side_effect=[0, 0, 0, 1]):
            with self.assertRaisesRegex(AssertionError, 'within deadline'):
                wait_search_convergence([client], 'owned-index', 2, timeout=0.5)

    def test_count_only_readiness_does_not_prove_migrated_row_membership(self):
        client=Mock(spec=redis.Redis)
        client.execute_command.side_effect=lambda *args, **kwargs: (
            [{'results':[{'extra_attributes':{'__rowid':'2'}}]},0] if 'WITHCURSOR' in args
            else {'results':[{'extra_attributes':{'counted':'2'}}]})
        with self.assertRaisesRegex(AssertionError,'within deadline'):
            wait_search_convergence([client],'owned-index',2,timeout=0)
        self.assertTrue(any('WITHCURSOR' in call.args for call in client.execute_command.call_args_list),
                        'readiness accepted global count without reading cursor membership')

    def test_readiness_requires_three_complete_cursor_rounds_and_closes_extra_cursor(self):
        client=Mock(spec=redis.Redis)
        client.execute_command.side_effect=lambda *args, **kwargs: (
            [{'results':[{'extra_attributes':{'__rowid':'1'}},
                         {'extra_attributes':{'__rowid':'2'}}]},0] if 'WITHCURSOR' in args
            else {'results':[{'extra_attributes':{'counted':'2'}}]})
        with patch('partition_tls_resharding.time.sleep'):
            self.assertEqual(wait_search_convergence([client],'owned-index',2),3)
        client.execute_command.side_effect=lambda *args, **kwargs: (
            [{'results':[{'extra_attributes':{'__rowid':'1'}}]},12] if 'WITHCURSOR' in args
            else 'OK' if args[:2]==('FT.CURSOR','DEL')
            else {'results':[{'extra_attributes':{'counted':'1'}}]})
        with self.assertRaisesRegex(AssertionError,'within deadline'):
            wait_search_convergence([client],'owned-index',1,timeout=0)
        client.execute_command.assert_any_call('FT.CURSOR','DEL','owned-index',12)

    def test_search_convergence_is_bounded_and_does_not_hide_other_errors(self):
        client=Mock(spec=redis.Redis)
        client.execute_command.side_effect=redis.ResponseError('Query requires unavailable slots')
        with self.assertRaisesRegex(AssertionError,'within deadline'):
            wait_search_convergence([client],'owned-index',1099,timeout=0)
        client.execute_command.side_effect=redis.exceptions.NoPermissionError('not permitted')
        with self.assertRaises(redis.exceptions.NoPermissionError):
            wait_search_convergence([client],'owned-index',1099)
        client.execute_command.side_effect=None
        client.execute_command.side_effect=lambda *args, **kwargs: (
            [{'results':[]},0] if 'WITHCURSOR' in args
            else {'results':[{'extra_attributes':{'counted':'1098'}}]})
        with self.assertRaisesRegex(AssertionError,'within deadline'):
            wait_search_convergence([client],'owned-index',1099,timeout=0)

    def test_unresponsive_worker_is_killed_even_when_stdin_is_broken(self):
        worker=RepeatedWorker.__new__(RepeatedWorker)
        worker.process=Mock()
        worker.thread=Mock()
        worker.process.stdin.close.side_effect=BrokenPipeError()
        worker.process.wait.side_effect=[subprocess.TimeoutExpired('worker',5),0]
        worker.close()
        worker.process.kill.assert_called_once()
        self.assertEqual(worker.process.wait.call_count,2)
        worker.process.stdout.close.assert_called_once()

    def migration_fixture(self, keys=None):
        ports=[16441,16442,16443]
        slots=[[0,5460,['127.0.0.1',16441,'a']],
               [5461,10922,['127.0.0.1',16442,'b']],
               [10923,16383,['127.0.0.1',16443,'c']]]
        principals=Mock()
        principals.nodes=dict(zip(ports,'abc'))
        principals.config={'ports':ports}
        principals.clients=[Mock(spec=redis.Redis) for _ in ports]
        for client in principals.clients:
            client.execute_command.side_effect=lambda *args: (
                slots if args==('CLUSTER','SLOTS') else keys if args[:2]==('CLUSTER','GETKEYSINSLOT') else 0)
        return principals

    def test_migration_refuses_unrelated_keys_before_any_admin_mutation(self):
        for keys in [['owned:1','foreign:1'], ['foreign:1'], ['owned:1']*2000]:
            with self.subTest(keys=len(keys)):
                principals=self.migration_fixture(keys)
                with self.assertRaisesRegex(ValueError,'refusing to migrate'):
                    SlotMigration(principals,'owned:1','owned:',
                                  'rediss://default:private@localhost:16441/0')
                for client in principals.clients:
                    self.assertFalse(any(call.args[:2]==('CLUSTER','SETSLOT') or call.args[:1]==('MIGRATE',)
                        for call in client.execute_command.call_args_list))

    def test_changed_primary_identity_blocks_migration(self):
        principals=self.migration_fixture(['owned:1'])
        principals.nodes[16442]='replaced-node'
        with self.assertRaisesRegex(ValueError,'identities changed'):
            SlotMigration(principals,'owned:1','owned:',
                          'rediss://default:private@localhost:16441/0')
        for client in principals.clients:
            self.assertFalse(any(call.args[:2]==('CLUSTER','SETSLOT')
                for call in client.execute_command.call_args_list))

    def test_incomplete_migration_cannot_reassign_ownership(self):
        principals=self.migration_fixture(['owned:1'])
        migration=SlotMigration(principals,'owned:1','owned:',
                                'rediss://default:private@localhost:16441/0')
        migration.source.execute_command.side_effect=None
        migration.source.execute_command.return_value=1
        with self.assertRaisesRegex(RuntimeError,'source still owns keys'):
            migration.finish()
        for client in principals.clients:
            self.assertFalse(any(call.args[:2]==('CLUSTER','SETSLOT')
                for call in client.execute_command.call_args_list))

    def test_invalid_ports_fail_before_fixture_or_container_creation(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture=Path(directory)/'must-not-exist'
            for port in ['abc','016441','1023','60000']:
                with self.subTest(port=port):
                    result=subprocess.run(['bash',str(Path(__file__).with_name('start_partition_tls_cluster.sh')),
                        str(fixture),'never-create-a-container',port],capture_output=True,text=True)
                    self.assertEqual(result.returncode,2)
                    self.assertFalse(fixture.exists())

    def test_unready_primary_closes_clients_before_acl_writes(self):
        ports=[16441,16442,16443]
        slots=[[0,5460,['127.0.0.1',16441,'a']],
               [5461,10922,['127.0.0.1',16442,'b']],
               [10923,16383,['127.0.0.1',16443,'c']]]
        control=Mock();control.execute_command.return_value=slots
        clients=[Mock(spec=redis.Redis) for _ in ports]
        for index,client in enumerate(clients):
            node_id='abc'[index]
            state='fail' if index==2 else 'ok'
            client.execute_command.side_effect=lambda *args,node_id=node_id,state=state: (
                node_id if args==('CLUSTER','MYID') else
                'cluster_state:'+state+'\r\n' if args==('CLUSTER','INFO') else [])
            client.config_get.return_value={'search-max-aggregate-results':'137'}
        with tempfile.TemporaryDirectory() as directory:
            fixture=Path(directory)
            (fixture/'cluster.json').write_text(json.dumps({'deployment':'owned OSS Cluster TLS','ports':ports}))
            with patch.object(ClusterPrincipals,'clients_for_uri',return_value=clients):
                with self.assertRaisesRegex(ValueError,'every primary must be ready'):
                    ClusterPrincipals(control,fixture,'rediss://default:private@localhost:16441/0',fixture/'ca.crt')
        for client in clients:
            client.close.assert_called_once()
            self.assertFalse(any(call.args[:1]==('ACL',) for call in client.execute_command.call_args_list))

    def test_missing_physical_rows_cannot_pass_slot_mapping(self):
        from check_partition_tls import cluster_distribution
        keys=['owned-row:'+str(i) for i in range(1099)]
        control=Mock()
        control.execute_command.side_effect=[[[0,16383,['127.0.0.1',16441,'node']]], [1099,*keys]]
        primary=MagicMock()
        primary.pipeline.return_value.__enter__.return_value.execute.return_value=[0]*1099
        with self.assertRaises(AssertionError):
            cluster_distribution(control,'owned-index',1,{16441:primary})
        primary.pipeline.assert_called_once_with(transaction=False)

    def fixture(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        principals = ClusterPrincipals.__new__(ClusterPrincipals)
        principals.directory = Path(directory.name)
        principals.names = []
        principals.clients = [Mock(), Mock(), Mock()]
        for client in principals.clients:
            client.acl_getuser.return_value = None
        return principals

    def test_foreign_or_missing_primary_rejected(self):
        slots = [[0, 5460, ['127.0.0.1', 16441, 'a']],
                 [5461, 10922, ['127.0.0.1', 16442, 'b']],
                 [10923, 16383, ['127.0.0.1', 16443, 'c']]]
        self.assertEqual(declared_primaries(slots, [16441,16442,16443]),
                         {16441:'a',16442:'b',16443:'c'})
        for invalid in [slots[:2], [slots[0],slots[1],[10923,16383,['other.invalid',16443,'c']]],
                        [slots[0],slots[1],[10923,16383,['127.0.0.1',16444,'c']]]]:
            with self.subTest(slots=invalid), self.assertRaises(ValueError):
                declared_primaries(invalid,[16441,16442,16443])

    def test_partial_creation_still_cleans_every_primary(self):
        principals = self.fixture()
        principals.clients[1].execute_command.side_effect = RuntimeError('primary unavailable')
        with self.assertRaises(RuntimeError):
            principals.create('fresh-owned','private-password',['+ping'])
        self.assertEqual(principals.names,['fresh-owned'])
        principals.cleanup()
        for client in principals.clients:
            client.acl_deluser.assert_called_once_with('fresh-owned')
            client.close.assert_called_once()
        report=json.loads((principals.directory/'cluster-acl-cleanup.json').read_text())
        self.assertTrue(report['absence_verified_on_all_primaries'])

    def test_existing_user_on_one_primary_is_never_adopted(self):
        principals = self.fixture()
        principals.clients[1].acl_getuser.return_value = {'commands':'+@all'}
        with self.assertRaisesRegex(ValueError,'adopt'):
            principals.create('preexisting','private-password',['+ping'])
        self.assertEqual(principals.names,[])
        for client in principals.clients:
            client.execute_command.assert_not_called()

    def test_permission_change_reaches_every_primary_and_rejects_unowned(self):
        principals = self.fixture()
        with self.assertRaisesRegex(ValueError,'not owned'):
            principals.command('foreign','-hmget')
        principals.names=['owned']
        principals.command('owned','-hmget')
        for client in principals.clients:
            client.execute_command.assert_called_once_with('ACL','SETUSER','owned','-hmget')

    def test_cleanup_failure_remains_visible_and_continues(self):
        principals = self.fixture()
        principals.names=['owned']
        principals.clients[0].acl_deluser.side_effect = RuntimeError('node unavailable')
        with self.assertRaisesRegex(RuntimeError,'cleanup failed'):
            principals.cleanup()
        for client in principals.clients:
            client.acl_deluser.assert_called_once_with('owned')
            client.close.assert_called_once()
        report=json.loads((principals.directory/'cluster-acl-cleanup.json').read_text())
        self.assertEqual(report['status'],'failed')
        self.assertEqual(report['failed_primary_indexes'],[0])
        self.assertFalse(report['absence_verified_on_all_primaries'])
