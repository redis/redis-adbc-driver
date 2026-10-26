"""Install and verify a run-scoped, expiring EC2 deletion exemption.

Only the configured cleanup principals are covered. IAM simulation is a
preflight check, not proof of all organization policies or future callers.
"""
import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import re


def utc(value):
    result = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if result.tzinfo is None:
        raise ValueError("Expiry requires a timezone")
    return result.astimezone(dt.timezone.utc)


def policy(config):
    if not re.fullmatch(r"\d{12}", config["account_id"]) or not re.fullmatch(r"[a-z]{2}-[a-z]+-\d", config["region"]):
        raise ValueError("Invalid AWS account or region")
    if not config["run_tag"] or any(c in config["run_tag"] for c in "*?"):
        raise ValueError("Run tag must be nonempty and literal")
    expiry = utc(config["expires_at"]).isoformat()
    return {"Version": "2012-10-17", "Statement": [{"Effect": "Deny", "Action": "ec2:TerminateInstances",
        "Resource": f"arn:aws:ec2:{config['region']}:{config['account_id']}:instance/*",
        "Condition": {"StringEquals": {"ec2:ResourceTag/BenchmarkRun": config["run_tag"]},
                      "DateLessThan": {"aws:CurrentTime": expiry}}}]}


def policy_name(config):
    return "benchmark-protection-" + hashlib.sha256(config["run_tag"].encode()).hexdigest()[:20]


def principal(config, arn):
    match = re.fullmatch(r"arn:aws:iam::" + config["account_id"] + r":(user|role)/(.+)", arn)
    if not match:
        raise ValueError("Use an IAM user or role ARN in the benchmark account")
    return match[1], match[2].split("/")[-1]


def mutate(iam, config, delete=False):
    expected = policy(config)
    principals = config["finops_principals"]
    if not principals or config["cleanup_principal"] in principals:
        raise ValueError("Independent budget cleanup must not receive the exemption")
    if delete:
        if dt.datetime.now(dt.timezone.utc) < utc(config["expires_at"]):
            raise ValueError("Remove exemptions only after their deadline")
    elif dt.datetime.now(dt.timezone.utc) >= utc(config["expires_at"]):
        raise ValueError("Cannot install an expired exemption")
    for arn in principals:
        kind, name = principal(config, arn)
        kwargs = {kind.title() + "Name": name, "PolicyName": policy_name(config)}
        if delete:
            existing = getattr(iam, f"get_{kind}_policy")(**kwargs)["PolicyDocument"]
            if existing != expected:
                raise ValueError("Refusing to delete a policy with different contents")
            getattr(iam, f"delete_{kind}_policy")(**kwargs)
        else:
            # Do not overwrite a similarly named policy from another run.
            try:
                existing = getattr(iam, f"get_{kind}_policy")(**kwargs)["PolicyDocument"]
            except iam.exceptions.NoSuchEntityException:
                existing = expected
            if existing != expected:
                raise ValueError("Existing policy differs; refusing to overwrite")
            getattr(iam, f"put_{kind}_policy")(**kwargs, PolicyDocument=json.dumps(expected))


def decision(iam, config, arn, tag, now):
    contexts = [{"ContextKeyName": "aws:CurrentTime", "ContextKeyValues": [now.isoformat()], "ContextKeyType": "date"},
                {"ContextKeyName": "aws:RequestedRegion", "ContextKeyValues": [config["region"]], "ContextKeyType": "string"}]
    if tag is not None:
        contexts.append({"ContextKeyName": "ec2:ResourceTag/BenchmarkRun", "ContextKeyValues": [tag], "ContextKeyType": "string"})
    reply = iam.simulate_principal_policy(PolicySourceArn=arn, ActionNames=["ec2:TerminateInstances"],
        ResourceArns=[f"arn:aws:ec2:{config['region']}:{config['account_id']}:instance/{config['instance_id']}"],
        ContextEntries=contexts)
    results = reply["EvaluationResults"]
    if len(results) != 1 or results[0].get("MissingContextValues"):
        raise ValueError("IAM simulation has missing context or unexpected results")
    return results[0]["EvalDecision"]


def verify(iam, ec2, config, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    expiry = utc(config["expires_at"])
    if now >= expiry:
        raise ValueError("Benchmark protection expired")
    expected = policy(config)
    if not config["finops_principals"] or config["cleanup_principal"] in config["finops_principals"]:
        raise ValueError("Independent cleanup principal required")
    instance = ec2.describe_instances(InstanceIds=[config["instance_id"]])["Reservations"][0]["Instances"][0]
    tags = {t["Key"]: t["Value"] for t in instance.get("Tags", [])}
    if instance["State"]["Name"] not in ("running", "pending") or tags.get("BenchmarkRun") != config["run_tag"]:
        raise ValueError("Runner state/run tag differs")
    checks = []
    for arn in config["finops_principals"]:
        kind, name = principal(config, arn)
        actual = getattr(iam, f"get_{kind}_policy")(**{kind.title()+"Name": name, "PolicyName": policy_name(config)})["PolicyDocument"]
        if actual != expected:
            raise ValueError("Effective exemption policy differs")
        for case, tag, when, want in [
            ("protected", config["run_tag"], now, "explicitDeny"),
            ("unprotected", config["run_tag"] + "-unrelated", now, "allowed"),
            ("untagged", None, now, "allowed"),
            ("expired", config["run_tag"], expiry + dt.timedelta(seconds=1), "allowed")]:
            got = decision(iam, config, arn, tag, when)
            checks.append({"principal": arn, "case": case, "decision": got})
            if got != want:
                raise ValueError(f"Unexpected {case} deletion decision for {arn}: {got}")
    principal(config, config["cleanup_principal"])
    for when in (now, expiry + dt.timedelta(seconds=1)):
        got = decision(iam, config, config["cleanup_principal"], config["run_tag"], when)
        if got != "allowed":
            raise ValueError("Budget cleanup cannot terminate the runner")
        checks.append({"principal": config["cleanup_principal"], "case": "budget_cleanup", "decision": got})
    # Preserve exact ownership/deadline evidence for the independently
    # scheduled Redis teardown. Provisioning must verify this schedule itself.
    for key in ("cleanup_schedule_evidence", "redis_subscription_id", "redis_database_id"):
        if not config.get(key):
            raise ValueError(f"Missing independent cleanup evidence: {key}")
    return {"verified": True, "verified_at": now.isoformat(), "expires_at": expiry.isoformat(),
            "run_tag": config["run_tag"], "instance_id": config["instance_id"], "policy": expected,
            "checks": checks, "cleanup_schedule_evidence": config["cleanup_schedule_evidence"],
            "redis_subscription_id": config["redis_subscription_id"], "redis_database_id": config["redis_database_id"],
            "limitation": "Configured principals simulated; external caller source and organization controls require separate investigation"}


def verify_live(config):
    import boto3
    config = dict(config)
    events = boto3.client("events", region_name=config["region"])
    rule = events.describe_rule(Name=config["cleanup_rule_name"])
    if rule.get("State") != "ENABLED" or not rule.get("ScheduleExpression"):
        raise ValueError("Independent cleanup schedule is not enabled")
    targets = events.list_targets_by_rule(Rule=config["cleanup_rule_name"])
    if targets.get("NextToken") or not any(t["Arn"] == config["cleanup_function_arn"] for t in targets["Targets"]):
        raise ValueError("Cleanup schedule target differs or needs pagination")
    function = boto3.client("lambda", region_name=config["region"]).get_function_configuration(
        FunctionName=config["cleanup_function_arn"])
    if function["Role"] != config["cleanup_principal"]:
        raise ValueError("Scheduled cleanup uses a different principal")
    config["cleanup_schedule_evidence"] = {"rule_arn": rule["Arn"], "schedule": rule["ScheduleExpression"],
        "function_arn": function["FunctionArn"], "role": function["Role"]}
    return verify(boto3.client("iam"), boto3.client("ec2", region_name=config["region"]), config)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("config", type=Path)
    ap.add_argument("--output", type=Path, required=True)
    group = ap.add_mutually_exclusive_group()
    group.add_argument("--install", action="store_true")
    group.add_argument("--remove-expired", action="store_true")
    args = ap.parse_args()
    import boto3
    config = json.loads(args.config.read_text())
    iam = boto3.client("iam")
    evidence = {"verified": False}
    try:
        if args.install or args.remove_expired:
            mutate(iam, config, delete=args.remove_expired)
        evidence = {"removed": True} if args.remove_expired else verify_live(config)
    finally:
        args.output.write_text(json.dumps(evidence, indent=2) + "\n")


if __name__ == "__main__":
    main()
