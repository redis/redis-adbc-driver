# Shared snapshot release readiness: specification

Date: 2026-11-10. Source baseline: `018e0368`. Status: implemented and locally validated.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [plan](plan.md), [acceptance](acceptance.md),
[verification](verification.md), [proposed release notes](release-notes.md).

The standard native pytest suite must call the existing shared-snapshot contract
validator with its `driver_path` and `db_kwargs` fixtures. These fixtures select
the built library or installed wheel used by existing Test/Release workflows.
The helper must not replace explicit TLS/database options with a separate
endpoint or resolve another source library. Its standalone command remains
compatible with `--driver` and REDIS_URI.

The check completes metadata and two sequential table results under one view,
compares exact BIGINT/NULL/duplicate values and schemas against the original
generation after independent publication, and releases the binding on failure.
No new skip, allowlist, persisted format or runtime API is introduced.

Release notes must identify the acquire/status/release API, finite lifetime,
protocol-17 compatibility, authorization, retry and unsupported transport limits.
They must keep the current connector's rejection visible and describe where its
native integration must acquire before metadata planning and release afterward.
Candidate wheel testing is local evidence, not a published release.
