# Specification

Date: 2026-10-20. Status: implementation prepared.
See [intent](intent.md) and [plan](plan.md).

Publish the existing release workflow's wheel artifacts only after every build,
platform check, and GitHub asset upload succeeds. Skip PRs and old releases
without Python wheels. Isolate OIDC permission in a dedicated publishing job;
bind PyPI trust to `alberttwong/redis-adbc-driver`, `release.yml`, environment
`pypi`. Pin the publishing action. Permit retries of already-published files.

Acceptance: workflow validation, selection of wheels without tarballs/checksums,
visible PyPI publisher registration, and successful OIDC authentication. A fresh
PyPI installation must load the native library and construct the Dagster manager.
