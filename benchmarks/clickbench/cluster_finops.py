"""Verify API termination protection and independent cleanup for owned EC2 nodes.

This is an explicit self-hosted alternative to the Cloud runner's IAM deny.
It does not claim an IAM exemption: a principal able to change the instance
attribute can remove protection. Scheduled cleanup deliberately does so at expiry.
"""
import datetime as dt
import json
import re


def checked_config(config, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    if config.get("protection_mode") != "ec2-api-termination-protection":
        raise ValueError("Explicit cluster protection mode required")
    expiry = dt.datetime.fromisoformat(config["expires_at"].replace("Z", "+00:00"))
    if expiry.tzinfo is None or not now < expiry <= now + dt.timedelta(hours=24):
        raise ValueError("Cluster expiry must be in the next 24 hours")
    ids = config["instance_ids"]
    if not 1 <= len(ids) <= 65 or len(ids) != len(set(ids)):
        raise ValueError("Cluster IDs must be unique and bounded")
    if any(not re.fullmatch(r"i-[0-9a-f]{17}", value) for value in ids):
        raise ValueError("Invalid EC2 instance identity")
    if config["instance_id"] not in ids:
        raise ValueError("Runner must be included in protected instances")
    if not re.fullmatch(r"\d{12}", config["account_id"]):
        raise ValueError("Invalid account")
    if not re.fullmatch(r"[a-z]{2}-[a-z]+-\d", config["region"]):
        raise ValueError("Invalid region")
    if not re.fullmatch(r"[a-z0-9-]{1,64}", config["run_tag"]):
        raise ValueError("Invalid run identity")
    return expiry


def cleanup_payload(config):
    return {key: config[key] for key in ("account_id", "region", "run_tag",
        "instance_ids", "security_group_id", "expires_at")}


def check_tags(config, tags):
    values = {t["Key"]: t["Value"] for t in tags}
    required = {"BenchmarkRun": config["run_tag"], "owner": "albert.wong@redis.com",
        "RedisLabsIdentifier": config["run_tag"], "skip_deletion": "true",
        "ExpiresAt": config["expires_at"]}
    if any(values.get(key) != value for key, value in required.items()):
        raise ValueError("Required ownership, reaper-exclusion or expiry tags differ")
    return values


def verify(config, ec2, events, functions, iam, now=None):
    expiry = checked_config(config, now)
    actual = [i for r in ec2.describe_instances(InstanceIds=config["instance_ids"])["Reservations"]
              for i in r["Instances"]]
    if {i["InstanceId"] for i in actual} != set(config["instance_ids"]):
        raise ValueError("Cluster inventory differs")
    checks = []
    for instance in actual:
        tags = check_tags(config, instance.get("Tags", []))
        if instance["State"]["Name"] != "running" or tags.get("BenchmarkRun") != config["run_tag"]:
            raise ValueError("Node state or ownership differs")
        if instance["InstanceType"] != "c6a.4xlarge" or tags.get("owner") != "albert.wong@redis.com":
            raise ValueError("Node hardware or required owner tag differs")
        if not ec2.describe_instance_attribute(InstanceId=instance["InstanceId"],
                Attribute="disableApiTermination")["DisableApiTermination"]["Value"]:
            raise ValueError("Node API termination protection is disabled")
        checks.append({"instance_id": instance["InstanceId"], "protected": True, "tags": tags})
    volumes = []
    for instance in actual:
        devices = instance.get("BlockDeviceMappings", [])
        if not devices or any(not d.get("Ebs", {}).get("DeleteOnTermination") for d in devices):
            raise ValueError("Every node requires owned root volumes with DeleteOnTermination")
        volumes.extend(d["Ebs"]["VolumeId"] for d in devices)
    actual_volumes = ec2.describe_volumes(VolumeIds=volumes)["Volumes"]
    if {v["VolumeId"] for v in actual_volumes} != set(volumes):
        raise ValueError("Root volume inventory differs")
    for volume in actual_volumes:
        check_tags(config, volume.get("Tags", []))
    groups = ec2.describe_security_groups(GroupIds=[config["security_group_id"]])["SecurityGroups"]
    if len(groups) != 1:
        raise ValueError("Owned security group differs")
    check_tags(config, groups[0].get("Tags", []))
    rule = events.describe_rule(Name=config["cleanup_rule_name"])
    if rule.get("State") != "ENABLED" or rule.get("ScheduleExpression") != "rate(5 minutes)":
        raise ValueError("Cleanup schedule is not enabled at the expected interval")
    targets = events.list_targets_by_rule(Rule=config["cleanup_rule_name"])
    if targets.get("NextToken") or len(targets["Targets"]) != 1 or targets["Targets"][0]["Arn"] != config["cleanup_function_arn"]:
        raise ValueError("Cleanup target differs")
    target = targets["Targets"][0]
    if "InputTransformer" in target or json.loads(target.get("Input", "{}")) != {}:
        raise ValueError("Cleanup target must not suppress deadline cleanup")
    function = functions.get_function_configuration(FunctionName=config["cleanup_function_arn"])
    if function["Role"] != config["cleanup_principal"] or function["CodeSha256"] != config["cleanup_code_sha256"]:
        raise ValueError("Cleanup code or principal differs")
    if json.loads(function["Environment"]["Variables"]["RUN_CONFIG"]) != cleanup_payload(config):
        raise ValueError("Cleanup ownership or deadline differs")
    # The cleanup role must be able to clear the flag and then terminate nodes.
    permissions = []
    for instance in actual:
        reply = iam.simulate_principal_policy(PolicySourceArn=config["cleanup_principal"],
            ActionNames=["ec2:ModifyInstanceAttribute", "ec2:TerminateInstances"],
            ResourceArns=[f"arn:aws:ec2:{config['region']}:{config['account_id']}:instance/{instance['InstanceId']}"])
        if len(reply["EvaluationResults"]) != 2 or any(
                v.get("MissingContextValues") or v["EvalDecision"] != "allowed" for v in reply["EvaluationResults"]):
            raise ValueError("Independent cleanup cannot remove protection and terminate every node")
        permissions.append({"instance_id": instance["InstanceId"], "cleanup_allowed": True})
    return {"verified": True, "protection_mode": config["protection_mode"],
        "verified_at": (now or dt.datetime.now(dt.timezone.utc)).isoformat(),
        "expires_at": expiry.isoformat(), "run_tag": config["run_tag"],
        "instances": checks, "cleanup_permissions": permissions,
        "tagged_root_volumes": volumes, "tagged_security_group": config["security_group_id"],
        "cleanup_rule_arn": rule["Arn"], "cleanup_function_arn": function["FunctionArn"],
        "limitation": "EC2 API protection, not an IAM deny; authorized attribute changes, AWS events and OS shutdown can still terminate nodes"}


def verify_live(config, credentials=None):
    import boto3
    session = boto3.Session(**(credentials or {}))
    account = session.client("sts").get_caller_identity()["Account"]
    if account != config["account_id"]:
        raise ValueError("AWS account differs")
    return verify(config, session.client("ec2", region_name=config["region"]),
        session.client("events", region_name=config["region"]),
        session.client("lambda", region_name=config["region"]), session.client("iam"))
