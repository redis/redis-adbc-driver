"""Dependency-order and ownership regressions; no live cloud resources."""
import copy
import datetime as dt
import unittest

import cleanup


STATE = {"name": "test-run", "database_name": "test-db", "instance_id": "i-test",
         "security_group_id": "sg-test", "security_group_name": "test-run-runner",
         "vpc_id": "vpc-test", "region": "us-east-1", "subscription_id": 123,
         "database_id": 456, "cleanup_deadline_utc": "2026-01-01T00:00:00Z"}
NOW = dt.datetime(2026, 1, 2, tzinfo=dt.timezone.utc)


class CloudError(Exception):
    def __init__(self, code):
        self.response = {"Error": {"Code": code}}


class EC2:
    def __init__(self):
        self.instance = {"InstanceId": "i-test", "VpcId": "vpc-test", "State": {"Name": "running"},
                         "Tags": [{"Key": "BenchmarkRun", "Value": "test-run"},
                                  {"Key": "Name", "Value": "test-run-runner"}]}
        self.group = {"GroupId": "sg-test", "GroupName": "test-run-runner", "VpcId": "vpc-test",
                      "Tags": [{"Key": "BenchmarkRun", "Value": "test-run"}]}
        self.interfaces = []
        self.next_token = None
        self.calls = []
        self.describe_error = None
        self.delete_error = None

    def describe_instances(self, **kw):
        assert kw == {"InstanceIds": ["i-test"]}
        if self.describe_error:
            raise CloudError(self.describe_error)
        return {"Reservations": [] if self.instance is None else [{"Instances": [self.instance]}]}

    def terminate_instances(self, **kw):
        self.calls.append(("terminate", kw))
        self.instance["State"]["Name"] = "shutting-down"

    def describe_security_groups(self, **kw):
        assert kw == {"GroupIds": ["sg-test"]}
        if self.group is None:
            raise CloudError("InvalidGroup.NotFound")
        return {"SecurityGroups": [self.group]}

    def describe_network_interfaces(self, **kw):
        assert kw == {"Filters": [{"Name": "group-id", "Values": ["sg-test"]}]}
        return {"NetworkInterfaces": self.interfaces, "NextToken": self.next_token}

    def delete_security_group(self, **kw):
        self.calls.append(("delete_group", kw))
        if self.delete_error:
            raise CloudError(self.delete_error)
        self.group = None


class Redis:
    def __init__(self):
        self.sub = {"id": 123, "name": "test-run", "status": "active", "numberOfDatabases": 1}
        self.db = {"databaseId": 456, "name": "test-db", "status": "active"}
        self.task = {"status": "processing-completed"}
        self.calls = []
        self.delete_reply = {"taskId": "test-task"}

    def __call__(self, path, method="GET"):
        if method == "DELETE":
            self.calls.append(path)
            return self.delete_reply
        if path.startswith("/tasks/"):
            return self.task
        return self.db if "/databases/" in path else self.sub


class CleanupTest(unittest.TestCase):
    def setUp(self):
        self.ec2, self.api = EC2(), Redis()
        self.state = copy.deepcopy(STATE)
        self.saved = []

    def step(self, **kw):
        return cleanup.step(self.ec2, self.api, self.state,
                            lambda s: self.saved.append(copy.deepcopy(s)), now=kw.pop("now", NOW), **kw)

    def no_runner(self):
        self.ec2.instance = None

    def no_aws(self):
        self.no_runner()
        self.ec2.group = None

    def test_dependency_order_and_verified_absence(self):
        self.assertEqual(self.step(), {"pending": True})
        self.assertEqual(self.ec2.calls[0][0], "terminate")
        self.assertFalse(self.api.calls)
        self.assertEqual(self.step(), {"pending": True})
        self.assertEqual(len(self.ec2.calls), 1)  # already shutting down
        self.ec2.instance["State"]["Name"] = "terminated"
        self.assertEqual(self.step(), {"pending": True})
        self.assertEqual(self.ec2.calls[-1][0], "delete_group")
        self.assertFalse(self.api.calls)
        self.step()
        self.assertEqual(self.api.calls, ["/subscriptions/123/databases/456"])
        self.assertFalse(self.state["cleanup_verified"])
        self.step()  # completed task is insufficient
        self.assertEqual(len(self.api.calls), 1)
        self.api.db = None
        self.api.sub["numberOfDatabases"] = 0
        self.step()
        self.assertEqual(self.api.calls[-1], "/subscriptions/123")
        self.api.sub = None
        self.assertEqual(self.step(), {"verified": True})
        self.assertTrue(self.saved[-1]["cleanup_verified"])
        self.assertEqual(self.step(), {"verified": True})

    def test_absent_runner_empty_response(self):
        self.no_runner()
        self.step()
        self.assertEqual(self.ec2.calls[0][0], "delete_group")

    def test_absent_runner_not_found(self):
        self.ec2.describe_error = "InvalidInstanceID.NotFound"
        self.step()
        self.assertEqual(self.ec2.calls[0][0], "delete_group")

    def test_unknown_aws_error_propagates(self):
        self.ec2.describe_error = "UnauthorizedOperation"
        with self.assertRaises(CloudError):
            self.step()
        self.assertFalse(self.ec2.calls)
        self.assertFalse(self.api.calls)

    def test_group_interfaces_and_pagination_block_teardown(self):
        self.no_runner()
        for interfaces, token in [([{"NetworkInterfaceId": "eni-test"}], None), ([], "more")]:
            self.ec2.interfaces, self.ec2.next_token = interfaces, token
            self.assertEqual(self.step(), {"pending": True})
            self.assertFalse(self.ec2.calls)
            self.assertFalse(self.api.calls)

    def test_group_dependency_retries_bounded(self):
        self.no_runner()
        self.ec2.delete_error = "DependencyViolation"
        for _ in range(3):
            self.assertEqual(self.step(), {"pending": True})
        self.assertEqual(self.step(), {"action_required": True})
        self.assertEqual(len(self.ec2.calls), 3)
        self.assertFalse(self.api.calls)

    def test_group_delete_not_found_race(self):
        self.no_runner()
        self.ec2.delete_error = "InvalidGroup.NotFound"
        self.assertEqual(self.step(), {"pending": True})
        self.assertFalse(self.api.calls)  # verify again before Redis deletion

    def test_all_resource_identity_mismatches_prevent_mutation(self):
        for target, key, value in [("instance", "InstanceId", "i-other"), ("instance", "VpcId", "other"),
                                   ("group", "GroupId", "sg-other"), ("group", "GroupName", "shared"),
                                   ("group", "VpcId", "other"), ("sub", "id", 999), ("sub", "name", "shared"),
                                   ("db", "databaseId", 999), ("db", "name", "shared")]:
            with self.subTest(target=target, key=key):
                self.setUp()
                obj = getattr(self.ec2 if target in ("instance", "group") else self.api, target)
                obj[key] = value
                with self.assertRaises(ValueError):
                    self.step()
                self.assertFalse(self.ec2.calls)
                self.assertFalse(self.api.calls)
        for target in ("instance", "group"):
            self.setUp()
            getattr(self.ec2, target)["Tags"] = []
            with self.assertRaises(ValueError):
                self.step()
            self.assertFalse(self.ec2.calls)

    def test_deadline_and_persisted_completion_gate(self):
        early = dt.datetime(2025, 12, 31, tzinfo=dt.timezone.utc)
        self.assertEqual(self.step(now=early), {"waiting": True})
        self.assertEqual(self.step(now=early, status={"phase": "complete", "artifacts_persisted": False}), {"waiting": True})
        self.assertFalse(self.ec2.calls)
        self.assertEqual(self.step(now=early, status={"phase": "complete", "artifacts_persisted": True}), {"pending": True})

    def test_verify_only_does_not_mutate(self):
        self.assertTrue(self.step(verify_only=True)["configured"])
        self.assertFalse(self.ec2.calls)
        self.assertFalse(self.api.calls)
        self.assertFalse(self.saved)

    def test_destroy_error_requires_support_without_delete(self):
        self.no_aws()
        self.api.db = None
        self.api.sub.update(status="destroy_error", numberOfDatabases=0)
        self.assertEqual(self.step(), {"action_required": True})
        self.assertFalse(self.api.calls)
        self.assertFalse(self.state["cleanup_verified"])

    def test_pending_resource_is_not_deleted_twice(self):
        self.no_aws()
        for status in ("pending-delete", "deleting"):
            self.api.db["status"] = status
            self.assertEqual(self.step(), {"pending": True})
        self.assertFalse(self.api.calls)

    def test_task_error_and_missing_task_require_action(self):
        self.no_aws()
        self.state["database_delete_task"] = "test-task"
        for task in ({"status": "processing-error"}, None):
            self.api.task = task
            self.assertEqual(self.step(), {"action_required": True})
        self.assertFalse(self.api.calls)

    def test_request_uncertainty_is_checkpointed_not_retried(self):
        self.no_aws()
        self.api.delete_reply = {}
        self.assertEqual(self.step(), {"action_required": True})
        self.assertTrue(self.saved[0]["database_delete_requested"])
        self.assertEqual(self.step(), {"action_required": True})
        self.assertEqual(len(self.api.calls), 1)

    def test_other_databases_prevent_subscription_delete(self):
        self.no_aws()
        self.api.db = None
        self.assertEqual(self.step(), {"action_required": True})
        self.assertFalse(self.api.calls)

    def test_completed_task_with_present_resource_eventually_escalates(self):
        self.no_aws()
        self.state["database_delete_task"] = "test-task"
        for _ in range(cleanup.MAX_PENDING_CHECKS - 1):
            self.assertEqual(self.step(), {"pending": True})
        self.assertEqual(self.step(), {"action_required": True})
        self.assertIn("observation limit", self.saved[-1]["cleanup_action_reason"])
        self.assertFalse(self.saved[-1]["cleanup_verified"])
        self.assertFalse(self.api.calls)
        # An eventual absence can still complete cleanup without resetting the
        # budget or treating the prior task success as absence.
        self.api.db = self.api.sub = None
        self.assertEqual(self.step(), {"verified": True})

    def test_interfaces_eventually_escalate_without_mutation(self):
        self.no_runner()
        self.ec2.interfaces = [{"NetworkInterfaceId": "eni-test"}]
        for _ in range(cleanup.MAX_PENDING_CHECKS):
            result = self.step()
        self.assertEqual(result, {"action_required": True})
        self.assertFalse(self.ec2.calls)
        self.assertFalse(self.api.calls)

    def test_eventually_consistent_aws_readback_never_repeats_deletion(self):
        self.step()
        # Simulate stale DescribeInstances readback after termination accepted.
        self.ec2.instance["State"]["Name"] = "running"
        for _ in range(cleanup.MAX_PENDING_CHECKS):
            result = self.step()
        self.assertEqual(result, {"action_required": True})
        self.assertEqual(len(self.ec2.calls), 1)
        self.no_runner()
        original_group = copy.deepcopy(self.ec2.group)
        self.step()
        self.ec2.group = original_group
        for _ in range(cleanup.MAX_PENDING_CHECKS):
            result = self.step()
        self.assertEqual(result, {"action_required": True})
        self.assertEqual([c[0] for c in self.ec2.calls], ["terminate", "delete_group"])
        self.assertFalse(self.api.calls)

    def test_missing_identity_fails_closed(self):
        for key in STATE:
            with self.subTest(key=key):
                self.setUp()
                del self.state[key]
                with self.assertRaises((ValueError, KeyError)):
                    self.step()
                self.assertFalse(self.ec2.calls)
                self.assertFalse(self.api.calls)


if __name__ == "__main__":
    unittest.main()
