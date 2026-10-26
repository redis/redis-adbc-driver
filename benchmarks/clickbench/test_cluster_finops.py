import datetime as dt
import json
import unittest
from unittest.mock import Mock
from cluster_finops import checked_config, cleanup_payload, verify

class ClusterProtection(unittest.TestCase):
    def setUp(self):
        self.now=dt.datetime(2026,10,21,20,tzinfo=dt.timezone.utc)
        self.config={'protection_mode':'ec2-api-termination-protection','account_id':'735486936198','region':'us-east-1','run_tag':'clickbench-test',
            'instance_id':'i-00000000000000001','instance_ids':['i-00000000000000001','i-00000000000000002'],
            'security_group_id':'sg-test','expires_at':(self.now+dt.timedelta(hours=12)).isoformat(),
            'cleanup_rule_name':'test-rule','cleanup_function_arn':'test-function','cleanup_principal':'test-role','cleanup_code_sha256':'code'}
        self.ec2,self.events,self.functions,self.iam=[Mock() for _ in range(4)]
        self.ec2.describe_instances.return_value={'Reservations':[{'Instances':[{'InstanceId':i,'InstanceType':'c6a.4xlarge','State':{'Name':'running'},
            'Tags':[{'Key':'BenchmarkRun','Value':'clickbench-test'},{'Key':'owner','Value':'albert.wong@redis.com'}]} for i in self.config['instance_ids']]}]}
        tags=[{'Key':k,'Value':v} for k,v in {'BenchmarkRun':'clickbench-test','owner':'albert.wong@redis.com','RedisLabsIdentifier':'clickbench-test','skip_deletion':'true','ExpiresAt':self.config['expires_at']}.items()]
        for n,instance in enumerate(self.ec2.describe_instances.return_value['Reservations'][0]['Instances']):
            instance['Tags']=tags
            instance['BlockDeviceMappings']=[{'Ebs':{'VolumeId':f'vol-{n}','DeleteOnTermination':True}}]
        self.ec2.describe_volumes.return_value={'Volumes':[{'VolumeId':f'vol-{n}','Tags':tags} for n in range(2)]}
        self.ec2.describe_security_groups.return_value={'SecurityGroups':[{'Tags':tags}]}
        self.ec2.describe_instance_attribute.return_value={'DisableApiTermination':{'Value':True}}
        self.events.describe_rule.return_value={'State':'ENABLED','ScheduleExpression':'rate(5 minutes)','Arn':'rule'}
        self.events.list_targets_by_rule.return_value={'Targets':[{'Arn':'test-function','Input':'{}'}]}
        self.functions.get_function_configuration.return_value={'Role':'test-role','CodeSha256':'code','FunctionArn':'test-function','Environment':{'Variables':{'RUN_CONFIG':json.dumps(cleanup_payload(self.config))}}}
        self.iam.simulate_principal_policy.return_value={'EvaluationResults':[{'EvalDecision':'allowed'},{'EvalDecision':'allowed'}]}
    def check(self):return verify(self.config,self.ec2,self.events,self.functions,self.iam,self.now)
    def test_every_node_is_checked(self):
        self.assertTrue(self.check()['verified'])
        self.assertEqual(self.ec2.describe_instance_attribute.call_count,2)
        self.assertEqual(self.iam.simulate_principal_policy.call_count,2)
    def test_one_unprotected_node_rejects_cluster(self):
        self.ec2.describe_instance_attribute.side_effect=[{'DisableApiTermination':{'Value':True}},{'DisableApiTermination':{'Value':False}}]
        with self.assertRaisesRegex(ValueError,'protection is disabled'):self.check()
    def test_incomplete_cleanup_permissions_rejected(self):
        self.iam.simulate_principal_policy.return_value={'EvaluationResults':[{'EvalDecision':'allowed'},{'EvalDecision':'implicitDeny'}]}
        with self.assertRaisesRegex(ValueError,'cleanup cannot'):self.check()
    def test_changed_cleanup_deadline_rejected(self):
        env=self.functions.get_function_configuration.return_value['Environment']['Variables']
        payload=cleanup_payload(self.config);payload['expires_at']=(self.now+dt.timedelta(hours=13)).isoformat();env['RUN_CONFIG']=json.dumps(payload)
        with self.assertRaisesRegex(ValueError,'deadline differs'):self.check()
    def test_verify_only_scheduled_input_rejected(self):
        self.events.list_targets_by_rule.return_value['Targets'][0]['Input']='{"verify_only":true}'
        with self.assertRaisesRegex(ValueError,'suppress deadline'):self.check()
    def test_untagged_volume_is_rejected(self):
        self.ec2.describe_volumes.return_value['Volumes'][1]['Tags']=[]
        with self.assertRaisesRegex(ValueError,'reaper-exclusion'):self.check()
    def test_duplicate_nodes_and_expiry_rejected(self):
        self.config['instance_ids'].append(self.config['instance_id'])
        with self.assertRaises(ValueError):checked_config(self.config,self.now)
        self.config['instance_ids'].pop();self.config['expires_at']=self.now.isoformat()
        with self.assertRaises(ValueError):checked_config(self.config,self.now)

if __name__=='__main__':unittest.main()
