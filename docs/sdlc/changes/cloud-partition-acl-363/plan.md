# Implementation and validation

Date: 2026-10-21. See [intent](intent.md) and [spec](spec.md).

1. Add a Cloud Pro ACL management adapter and explicit owned-fixture preparation.
2. Reuse the TLS partition checker with a mutually exclusive Cloud backend.
3. Test management error redaction, no POST replay, ownership before mutation,
   database-scoped roles and partial-failure cleanup.
4. Provision a dedicated Pro fixture, run the native checks, archive non-secret
   results and delete all owned ACL/database/subscription resources.
5. Check existing backend behavior, document commands/limits, review and open PR.

## Outcome

Cloud Pro: 16 checks passed with Python 3.13 / ADBC 1.12 native and another
16 with Python 3.12 / installed-wheel ADBC 1.8. OSS regression: 15 passed.
Management fault tests: 13 passed, now wired into regular CI. Actual setup
rejections and corrections, final management trust-context replay and cleanup
are retained in [evidence](evidence.md). Database/subscription absence and ACL
cleanup were verified; private fixtures removed. No Go runtime source, assertions
or skip policy changed. Hosted checks and human review remain pending.
