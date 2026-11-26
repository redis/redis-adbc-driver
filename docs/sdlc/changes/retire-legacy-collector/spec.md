# Contract and boundaries

See [intent](intent.md) and [plan](plan.md).

- Terraform forgets, and never deletes, `aws_instance.collector` and `aws_ec2_instance_state.legacy` through `removed` blocks with `destroy = false`. Their AWS objects no longer exist.
- The legacy security group, instance role, its inline policy and SSM attachment, instance profile and FinOps lifecycle exception are no longer declared. An approved apply deletes them. Nothing else in AWS changes.
- The serverless module is always present. `serverless_package` is required; a `moved` block maps `module.serverless[0]` to `module.serverless` with no resource changes.
- The public DNS record is always the API Gateway alias. The legacy A-record branch, Elastic IP, AMI lookup, user data and migration-only variables are removed.
- The backup bucket, its protection and lifecycle rules, and the legacy RDB backups are unchanged.
- No plan or apply touches the shared state as part of this change. Any apply needs a fresh reviewed plan against the shared state and the owner's explicit approval.
