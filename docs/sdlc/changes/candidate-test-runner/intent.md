# Candidate test runner intent

- Date: 2026-10-30
- Source commit: `22fee6883b9440c0d9999e9f967bc390bb0fd0e3`
- Status: implementation complete; PR review pending
- Requestor: user
- PR: [#248](https://github.com/redis/redis-adbc-driver/pull/248)

## Problem

The repository contains integration suites for multiple data engineering
applications, but rerunning the candidate paths requires manual Redis setup,
native driver configuration, and remembering the relevant test modules.

## Desired outcome

Provide one documented command to start or reuse Redis Software with verified
4x QPF and run selected local candidate integration suites or the complete set.

## Scope

Aggregate the runnable native integration tests and add local notebook,
application, plotting, model, Spark, SDK, orchestration, R, and document checks.
Keep the runner local: Flight SQL, hosted products, Databricks, and remote
orchestration workers are outside its scope. Custom Python compositions are
identified separately from dedicated database connectors.

The follow-up request covers all runnable direct-ADBC candidates. The complete
launcher isolates conflicting runtime dependencies and records every comparison
row's tested path or missing integration. Flight SQL is explicitly excluded by
the user's scope correction. [Complete native sweep](native-sweep.md).

## Links

- Specification: [spec.md](spec.md)
- Plan and evidence: [plan.md](plan.md)
- Workflow validation: [validation.md](validation.md)
