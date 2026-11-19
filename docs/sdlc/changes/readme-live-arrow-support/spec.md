# Specification

Date: 2026-11-19. Status: implemented documentation update. PR: [#533](https://github.com/redis/redis-adbc-driver/pull/533).
See [intent](intent.md), [plan](plan.md), and [validation](validation.md).

- Both capability tables say supported within the linked live write contract.
- Describe create, append, create_append and replace without changing ownership.
- Creation needs an explicitly configured empty prefix; replacement needs exact
  target/index consent and stopped application/driver writers, removes old HASHes
  and TTLs, and resets the index. State partial success and no whole-call rollback.
- Include Arrow in live creation/registration routes. Retain source-build scope for live tables;
  make no new release, performance, server-compatibility or atomicity claim.

- Order comparison modes Managed, Live, Unmanaged; explicitly label Managed as
  the default. Keep README mode descriptions in the same order.
- Separate Redis-style reads from direct writes. Link the experimental managed
  NativeClient; state its v0.0.29 availability, internal-row-ID and read-only scope.
  Ordinary physical-key reads do not resolve managed snapshot visibility.
