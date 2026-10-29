# Prefect integration test intent

Date: 2026-10-29. Source commits: initial test `95d6583`, public PR base `34ae626`. Status: implemented and tested locally.
Requestor: repository user. Issue/PR: none created in this task.

Test the comparison's unverified custom Python task path using the Redis ADBC
driver. A successful result requires actual Prefect execution and fully fetched,
asserted Redis results. Exclude Flight SQL, prefect-redis, remote workers and
Cloud deployments. See [spec](spec.md) and [plan/evidence](plan.md).
