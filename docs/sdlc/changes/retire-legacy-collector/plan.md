# Implementation and verification

See [intent](intent.md) and [specification](spec.md).

1. Remove the legacy compute, IAM, security group, Elastic IP, FinOps exception, AMI lookup, user-data template, variables and outputs from `analytics/deploy/terraform/`.
2. Add `removed` blocks for the deleted instance and its instance-state resource.
3. Make the serverless module unconditional with a `moved` block, and declare the DNS record as an alias only.
4. Update the deployment README and [SERVERLESS.md](../../../../analytics/deploy/SERVERLESS.md#terraform-state-reconciliation).

Validation (2026-11-26):

- `terraform fmt -check` and `terraform validate` (offline init with the locked AWS provider 6.73.0): passed.
- `terraform plan` against a local copy of the shared state, with a local backend override, `-lock=false`, read-only AWS calls and the operator's private variables and deployed package: `0 to add, 0 to change, 6 to destroy`. It forgets `aws_instance.collector` and `aws_ec2_instance_state.legacy[0]`. It deletes `aws_security_group.collector`, `aws_iam_role.collector`, `aws_iam_role_policy.backup_writer`, `aws_iam_role_policy_attachment.ssm`, `aws_iam_instance_profile.collector` and `aws_iam_user_policy.approved_collector_exception[0]`. All 37 module resources move with no changes, including the three Lambda functions. The DNS record and bucket have no changes, and only the `instance_id` output is removed. The state copy and plan file were deleted after the run.
- The plan warned that the private operator variables still set six now-undeclared variables; they must be removed from that private file.
- No plan or apply ran against the shared state. No AWS resource was changed.

This records agent verification, not independent human approval.
