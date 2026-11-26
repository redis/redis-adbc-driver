# Implementation and verification

See [intent](intent.md) and [specification](spec.md).

1. Remove the legacy compute, IAM, security group, Elastic IP, FinOps exception, AMI lookup, user-data template, variables and outputs from `analytics/deploy/terraform/`.
2. Add `removed` blocks for the deleted instance and its instance-state resource.
3. Make the serverless module unconditional with a `moved` block, and declare the DNS record as an alias only.
4. Update the deployment README and [SERVERLESS.md](../../../../analytics/deploy/SERVERLESS.md#terraform-state-reconciliation).

Validation (2026-11-26):

- `terraform fmt -check` and `terraform validate` (offline init with the locked AWS provider 6.73.0): passed.
- `terraform plan` against a local copy of the shared state, with a local backend override, `-lock=false`, read-only AWS calls and the operator's private variables and deployed package: `0 to add, 0 to change, 6 to destroy`. It forgets `aws_instance.collector` and `aws_ec2_instance_state.legacy[0]`. It deletes `aws_security_group.collector`, `aws_iam_role.collector`, `aws_iam_role_policy.backup_writer`, `aws_iam_role_policy_attachment.ssm`, `aws_iam_instance_profile.collector` and `aws_iam_user_policy.approved_collector_exception[0]`. All 34 module resources move with no changes, including the three Lambda functions. The DNS record and bucket have no changes, and only the `instance_id` output is removed. The state copy and plan file were deleted after the run.
- The plan warned that the private operator variables still set six now-undeclared variables; they must be removed from that private file.
- (Correction: an earlier version of this record said 37 moves. The 34 moves are correct; the other 7 unchanged resources are the root DNS record and the bucket resources.)

Apply (2026-11-26, Pacific): the owner approved the apply in a Claude Code session after PR #723 merged.

- A fresh plan of `main` (272c93cb) against the shared state, with the deployed package, matched the approved set exactly. It forgot the same two addresses, deleted the same six resources and made 34 `module.serverless[0]` to `module.serverless` moves. There were no other resource actions, and only the `instance_id` output was removed. A script checked the match before the saved plan was applied.
- Applying the saved plan gave `0 added, 0 changed, 6 destroyed`. State serial 17 became 18; the versioned bucket retains the previous version.
- After the apply, a new plan reported no changes. The state's root holds only the DNS record and six backup-bucket resources. The legacy IAM role, instance profile, security group and FinOps exception policy no longer exist.
- Public checks after the apply:
  - `GET /` and `/health`: 200.
  - `/api/summary`: 401 without a token or with a bad one, and 200 with the dashboard token, which was read in memory and not printed.
  - `POST /v1/usage` with an invalid payload: 400, so no report was stored.
- Not changed: the private operator variables still set the six undeclared variables (warnings only).

This records agent verification, not independent human approval.
