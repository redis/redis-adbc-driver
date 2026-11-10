# Shared read snapshot implementation plan

Date: 2026-11-10. Status: implementation and verification complete; PR review/release pending.
Issue: https://github.com/redis/redis-adbc-driver/issues/365.
Base: `85509485dbc07b2e2e0ea9e1b8e08bd11a290587`.

1. Add connection options and reference-owned binding in `go/shared_snapshot.go`.
2. Route snapshot acquisitions through it; retain existing server lease/quotas.
3. Refuse unsupported SQL, volatile writes, partition and special execution paths.
4. Add exact metadata/value, ownership, expiry, cancellation, quota and real
   process-death regressions; extend authenticated Software RBAC validation.
5. Build the native C library and validate its Python API/example.
6. Record checks and review findings; publish a PR. Maintainers select/release a
   driver version before enabling connector shared-snapshot mode.

See [intent](intent.md), [spec](spec.md), and [verification](verification.md).
