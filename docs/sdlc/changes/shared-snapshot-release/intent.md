# Shared snapshot release readiness: intent

Date: 2026-11-10. Requestor: Albert Wong. Source baseline: `018e0368`.
Status: local candidate validated; no release published.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [specification](spec.md), [plan](plan.md), [acceptance](acceptance.md),
[verification](verification.md), [proposed release notes](release-notes.md).

The reusable native view and GetObjects validation fix have merged in #369 and
#373. The remaining issue criterion requires a released driver version and
concrete connector integration steps. Standalone native validation currently
requires a manual invocation; the ordinary native/release-wheel suite must also
exercise the same contract before new distributions can claim support.

Add that gate using the suite's selected library and database options. Prepare
release notes and an evidence map without describing an unpublished candidate
as an available release. Preserve the existing unsupported Flight SQL boundary.
