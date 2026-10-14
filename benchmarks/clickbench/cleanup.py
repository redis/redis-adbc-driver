"""Idempotent, exact-resource cleanup for a scheduled ClickBench deployment.

Package this file as the cleanup Lambda and use cleanup.handler. Each invocation
advances one asynchronous step; only observed resource absence verifies cleanup.
"""
import datetime as dt
import json
import os
import urllib.error
import urllib.request


MAX_PENDING_CHECKS = 30


def error_code(exc):
    return getattr(exc, "response", {}).get("Error", {}).get("Code")


def utc(value):
    result = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if result.tzinfo is None:
        raise ValueError("Cleanup deadline requires a timezone")
    return result.astimezone(dt.timezone.utc)


def validate_state(state):
    for key in ("name", "database_name", "instance_id", "security_group_id",
                "security_group_name", "vpc_id", "region"):
        if not isinstance(state.get(key), str) or not state[key]:
            raise ValueError(f"Missing exact cleanup identity: {key}")
    for key in ("subscription_id", "database_id"):
        if type(state.get(key)) is not int or state[key] <= 0:
            raise ValueError(f"Missing exact cleanup identity: {key}")
    utc(state["cleanup_deadline_utc"])


def runner(ec2, state):
    try:
        reply = ec2.describe_instances(InstanceIds=[state["instance_id"]])
    except Exception as exc:
        if error_code(exc) == "InvalidInstanceID.NotFound":
            return None
        raise
    instances = [i for r in reply["Reservations"] for i in r["Instances"]]
    if not instances:
        return None
    if len(instances) != 1:
        raise ValueError("Unexpected runner lookup")
    instance = instances[0]
    tags = {t["Key"]: t["Value"] for t in instance.get("Tags", [])}
    if (instance["InstanceId"] != state["instance_id"] or
            instance["VpcId"] != state["vpc_id"] or
            tags.get("BenchmarkRun") != state["name"] or
            tags.get("Name") != state["name"] + "-runner"):
        raise ValueError("Runner ownership differs")
    return instance


def security_group(ec2, state):
    try:
        reply = ec2.describe_security_groups(GroupIds=[state["security_group_id"]])
    except Exception as exc:
        if error_code(exc) == "InvalidGroup.NotFound":
            return None
        raise
    groups = reply["SecurityGroups"]
    if len(groups) != 1:
        raise ValueError("Unexpected security group lookup")
    group = groups[0]
    tags = {t["Key"]: t["Value"] for t in group.get("Tags", [])}
    if (group["GroupId"] != state["security_group_id"] or
            group["GroupName"] != state["security_group_name"] or
            group["VpcId"] != state["vpc_id"] or
            tags.get("BenchmarkRun") != state["name"]):
        raise ValueError("Security group ownership differs")
    return group


def redis_resources(api, state):
    path = "/subscriptions/" + str(state["subscription_id"])
    sub = api(path)
    db = api(path + "/databases/" + str(state["database_id"]))
    if sub is not None and (sub.get("id") != state["subscription_id"] or
                            sub.get("name") != state["name"]):
        raise ValueError("Redis subscription ownership differs")
    if db is not None and (db.get("databaseId") != state["database_id"] or
                           db.get("name") != state["database_name"]):
        raise ValueError("Redis database ownership differs")
    return path, sub, db


def step(ec2, api, state, save, status=None, now=None, verify_only=False):
    """api returns None only on HTTP 404; save durably checkpoints the state."""
    validate_state(state)
    now = now or dt.datetime.now(dt.timezone.utc)
    complete = (status or {}).get("phase") == "complete" and (status or {}).get("artifacts_persisted") is True
    if not verify_only and now < utc(state["cleanup_deadline_utc"]) and not complete:
        return {"waiting": True}

    instance = runner(ec2, state)
    group = security_group(ec2, state)
    path, sub, db = redis_resources(api, state)
    if verify_only:
        return {"configured": True, "runner_present": instance is not None,
                "security_group_present": group is not None,
                "subscription_present": sub is not None, "database_present": db is not None}

    def checkpoint(phase, **result):
        state["cleanup_phase"] = phase
        state["cleanup_verified"] = False
        state.pop("cleanup_action_reason", None)
        if result.get("pending"):
            checks = state.setdefault("cleanup_pending_checks", {})
            checks[phase] = checks.get(phase, 0) + 1
            if checks[phase] >= MAX_PENDING_CHECKS:
                state["cleanup_action_reason"] = f"{phase}: observation limit reached"
                result = {"action_required": True}
        save(state)
        return result

    if instance is not None and instance["State"]["Name"] != "terminated":
        if instance["State"]["Name"] != "shutting-down" and not state.get("runner_terminate_requested"):
            state.update(runner_terminate_requested=True, cleanup_verified=False)
            save(state)
            ec2.terminate_instances(InstanceIds=[state["instance_id"]])
        return checkpoint("terminating runner", pending=True)
    state["runner_terminated"] = True

    # Redis subscription teardown destroys its VPC. Our custom group must be
    # removed first, after EC2 and its ENIs have stopped using it.
    if group is not None:
        interfaces = ec2.describe_network_interfaces(Filters=[
            {"Name": "group-id", "Values": [state["security_group_id"]]}])
        if interfaces.get("NextToken") or interfaces["NetworkInterfaces"]:
            return checkpoint("waiting for security group interfaces", pending=True)
        if state.get("security_group_dependency_failures", 0) >= 3:
            return checkpoint("security group dependencies require action", action_required=True)
        if state.get("security_group_delete_requested"):
            return checkpoint("verifying security group deletion", pending=True)
        state.update(security_group_delete_requested=True, cleanup_verified=False)
        save(state)
        try:
            ec2.delete_security_group(GroupId=state["security_group_id"])
        except Exception as exc:
            if error_code(exc) == "DependencyViolation":
                state["security_group_delete_requested"] = False
                state["security_group_dependency_failures"] = state.get("security_group_dependency_failures", 0) + 1
                return checkpoint("security group dependencies remain", pending=True)
            if error_code(exc) != "InvalidGroup.NotFound":
                raise
        return checkpoint("verifying security group deletion", pending=True)
    state["security_group_deleted"] = True

    # Do not interpret accepted/processing-completed tasks as deletion. Observe
    # the resource on later invocations, and never resubmit a persisted task.
    for resource, obj, resource_path in (("database", db, path + "/databases/" + str(state["database_id"])),
                                          ("subscription", sub, path)):
        if obj is None:
            state[resource + "_deleted"] = True
            continue
        state[resource + "_deleted"] = False
        if resource == "subscription" and obj.get("numberOfDatabases") != 0:
            return checkpoint("subscription is not empty", action_required=True)
        current = obj.get("status")
        if current in ("destroy_error", "delete-error", "deletion-error"):
            return checkpoint(resource + " deletion requires support", action_required=True)
        task_id = state.get(resource + "_delete_task")
        if task_id:
            task = api("/tasks/" + task_id)
            if task is None or task.get("status") == "processing-error":
                return checkpoint(resource + " deletion task requires action", action_required=True)
            return checkpoint("verifying " + resource + " deletion", pending=True)
        if current in ("pending-delete", "deleting"):
            return checkpoint("verifying " + resource + " deletion", pending=True)
        if current != "active":
            return checkpoint(resource + " is not active", action_required=True)
        if state.get(resource + "_delete_requested"):
            return checkpoint(resource + " deletion request uncertain", action_required=True)
        state[resource + "_delete_requested"] = True
        state["cleanup_verified"] = False
        save(state)
        task = api(resource_path, "DELETE")
        if not task or not task.get("taskId"):
            return checkpoint(resource + " deletion request uncertain", action_required=True)
        state[resource + "_delete_task"] = task["taskId"]
        return checkpoint("deleting " + resource, pending=True)

    state.pop("cleanup_action_reason", None)
    state.update(cleanup_verified=True, cleanup_verified_utc=now.isoformat(),
                 cleanup_phase="verified", phase="cleanup-verified")
    save(state)
    return {"verified": True}


def redis_api(credentials):
    def call(path, method="GET"):
        req = urllib.request.Request("https://api.redislabs.com/v1" + path, method=method,
            headers={"x-api-key": credentials["key"], "x-api-secret-key": credentials["secret"],
                     "Accept": "application/json", "User-Agent": "clickbench-budget-cleanup"})
        try:
            with urllib.request.urlopen(req, timeout=40) as response:
                data = response.read()
                return json.loads(data) if data else {}
        except urllib.error.HTTPError as exc:
            if exc.code == 404:
                return None
            raise RuntimeError("Redis API status " + str(exc.code)) from None
    return call


def handler(event, context):
    import boto3
    region = os.environ["AWS_REGION"]
    bucket = os.environ["BUCKET"]
    key = "setup/deployment-state.json"
    s3 = boto3.client("s3", region_name=region)
    state = json.loads(s3.get_object(Bucket=bucket, Key=key)["Body"].read())
    if state["region"] != region:
        raise ValueError("Cleanup region differs")
    status = None
    try:
        status = json.loads(s3.get_object(Bucket=bucket, Key="results/status.json")["Body"].read())
    except s3.exceptions.NoSuchKey:
        pass
    credentials = json.loads(boto3.client("secretsmanager", region_name=region).get_secret_value(
        SecretId=os.environ["SECRET_ARN"])["SecretString"])

    def save(value):
        s3.put_object(Bucket=bucket, Key=key, Body=json.dumps(value).encode(), ContentType="application/json")

    return step(boto3.client("ec2", region_name=region), redis_api(credentials), state, save,
                status=status, verify_only=bool(event.get("verify_only")))
