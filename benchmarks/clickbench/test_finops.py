import copy
import datetime as dt
import unittest

from finops import policy, policy_name, verify

NOW = dt.datetime(2026, 10, 13, tzinfo=dt.timezone.utc)
CONFIG = {"account_id": "123456789012", "region": "us-east-1", "run_tag": "test-run",
          "expires_at": "2026-10-14T00:00:00Z", "instance_id": "i-test",
          "finops_principals": ["arn:aws:iam::123456789012:user/finops"],
          "cleanup_principal": "arn:aws:iam::123456789012:role/budget-cleanup",
          "cleanup_schedule_evidence": {"rule": "verified-schedule"},
          "redis_subscription_id": 123, "redis_database_id": 456}


class EC2:
    def describe_instances(self, **kwargs):
        return {"Reservations": [{"Instances": [{"State": {"Name": "running"},
            "Tags": [{"Key": "BenchmarkRun", "Value": CONFIG["run_tag"]}]}]}]}


class IAM:
    def get_user_policy(self, **kwargs):
        return {"PolicyDocument": policy(CONFIG)}

    def simulate_principal_policy(self, **kwargs):
        context = {c["ContextKeyName"]: c["ContextKeyValues"][0] for c in kwargs["ContextEntries"]}
        protected = (kwargs["PolicySourceArn"] in CONFIG["finops_principals"]
                     and context.get("ec2:ResourceTag/BenchmarkRun") == CONFIG["run_tag"]
                     and dt.datetime.fromisoformat(context["aws:CurrentTime"]) < dt.datetime(2026, 10, 14, tzinfo=dt.timezone.utc))
        return {"EvaluationResults": [{"EvalDecision": "explicitDeny" if protected else "allowed"}]}


class Finops(unittest.TestCase):
    def test_policy_scope_and_expiry(self):
        statement = policy(CONFIG)["Statement"][0]
        self.assertEqual(statement["Action"], "ec2:TerminateInstances")
        self.assertEqual(statement["Resource"], "arn:aws:ec2:us-east-1:123456789012:instance/*")
        self.assertEqual(statement["Condition"]["StringEquals"], {"ec2:ResourceTag/BenchmarkRun": "test-run"})
        self.assertEqual(statement["Condition"]["DateLessThan"]["aws:CurrentTime"], "2026-10-14T00:00:00+00:00")
        self.assertNotEqual(policy_name(CONFIG), policy_name(dict(CONFIG, run_tag="other")))
        with self.assertRaises(ValueError):
            policy(dict(CONFIG, run_tag="*"))

    def test_all_principals_and_budget_cleanup(self):
        evidence = verify(IAM(), EC2(), CONFIG, NOW)
        self.assertTrue(evidence["verified"])
        self.assertEqual(len(evidence["checks"]), 6)
        self.assertEqual(evidence["redis_database_id"], 456)
        with self.assertRaises(ValueError):
            verify(IAM(), EC2(), CONFIG, NOW + dt.timedelta(days=1))
        bad = copy.deepcopy(CONFIG); bad["finops_principals"].append(bad["cleanup_principal"])
        with self.assertRaises(ValueError):
            verify(IAM(), EC2(), bad, NOW)

    def test_fail_closed_on_ineffective_or_missing_context(self):
        class Broken(IAM):
            def simulate_principal_policy(self, **kwargs):
                return {"EvaluationResults": [{"EvalDecision": "allowed"}]}
        with self.assertRaises(ValueError):
            verify(Broken(), EC2(), CONFIG, NOW)
        class Missing(IAM):
            def simulate_principal_policy(self, **kwargs):
                return {"EvaluationResults": [{"EvalDecision": "explicitDeny", "MissingContextValues": ["owner"]}]}
        with self.assertRaises(ValueError):
            verify(Missing(), EC2(), CONFIG, NOW)


if __name__ == "__main__":
    unittest.main()
