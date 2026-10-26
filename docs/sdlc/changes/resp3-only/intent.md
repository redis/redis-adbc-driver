# RESP3-only intent

Date: 2026-10-22. Source: `6508626`. Requestor: repository user.
Status: implemented; prepared for the accompanying pull request. No issue created.

Require RESP3 for the Redis ADBC driver. The user explicitly requested that
RESP2 not be supported, superseding the previous compatibility policy.
Consumers using `protocol=2` must receive an error. Historical measurements
must retain their actual protocol labels.

See [specification](spec.md), [plan and validation](plan.md).
