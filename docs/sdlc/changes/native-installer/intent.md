# Intent: managed native installation and live diagnostics

Date: 2026-10-29. Base: `b1d0613`. Requestor: user in this chat; implementation:
Codex. Status: implemented locally; no release or merge approval recorded.
Inputs: merged [PR #240](https://github.com/redis/redis-adbc-driver/pull/240).
See [spec](spec.md), [plan](plan.md) and [validation](validation.md).

## Problem and scope

Users still select and unpack native archives manually, keep libraries in fragile
locations, and lack upgrade/uninstall ownership tracking. Complete the requested
next steps: inspect CI and exercise doctor on Software/TLS, automate native
download/registration, and support upgrade/uninstall without stale paths.

## Constraints

Use supported published targets and stable GitHub release assets. Preserve wheel
and legacy tarball installation paths. Do not alter user-managed files, weaken TLS
verification, publish releases, or modify another chat's test fixture. Registry
distribution and new database connectors remain outside this change.
