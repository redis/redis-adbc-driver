# Analytics deployment status after serverless cutover

2026-11-26 · Requestor: Albert Wong · Source: d65f3b52 · Status: documentation updated, not yet reviewed

The hosted analytics collector moved from an EC2 host to API Gateway, Lambda and S3
on 2026-11-19. The deployment README still described the retired EC2 host and
pointed operators at an on-host environment file for the dashboard token. Those
instructions now fail because the host no longer exists.

Update the analytics documentation to describe the deployed collector: how to open
the dashboard, where its read token is stored, how to check it and what is left
for rollback. Warn operators not to plan or apply the shared Terraform state until
the deleted legacy instance is reconciled. Keep the retired EC2 instructions as a
labeled historical record.

The serverless source was missing from git when this record was started. It was
recovered on `claude/serverless-collector-source` ([PR #721](https://github.com/redis/redis-adbc-driver/pull/721))
and merged into this branch, so the README now links to it instead of warning
that it is absent.

Out of scope: landing the serverless source, changing infrastructure, rotating the
token or repairing Terraform state.

See [specification](spec.md) and [plan and validation](plan.md). No issue or PR
has been created for this change.
