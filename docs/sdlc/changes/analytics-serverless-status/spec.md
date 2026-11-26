# Documentation contract

See [intent](intent.md) and [plan](plan.md).

- `analytics/deploy/README.md` opens with the current serverless deployment. Every
  stated resource, setting and schedule was read from the live AWS account, not
  inferred from source.
- Dashboard access names the hostname and the SSM SecureString parameter that holds
  the read token. The new sections never contain the token, AWS keys, API IDs,
  function ARNs, instance IDs or IAM identities. Identifiers already published in
  the retired EC2 record are left unchanged.
- The README links the deployed source and Terraform module, and a warning states
  that the shared state must not be planned or applied until the deleted legacy
  instance is reconciled.
- Rollback status reports what actually remains: no host or disk, the last legacy
  RDB backup and its expiry, and the migration baseline. It does not claim a
  rollback path that no longer exists.
- The retired EC2 instructions are kept verbatim under a historical heading, with
  headings demoted one level.
- `analytics/README.md` no longer describes the hosted collector as an nginx-fronted
  single Redis server. Self-hosted Redis collector guidance is unchanged.
- No driver behavior, telemetry payload, endpoint or infrastructure changes.
