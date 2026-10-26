# Repair the DuckDB 2.0 registry build

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Base: c737cb8fff88855bc1c12e0d48f3b4dd3c46a364. Status: implementation complete;
remote CI and maintainer review pending.

[Community PR #3157](https://github.com/duckdb/community-extensions/pull/3157)
failed in [Linux amd64 job 118910590495](https://github.com/duckdb/community-extensions/actions/runs/39503761743/job/118910590495).
The registry enables vcpkg manifest mode and points it at the extension root.
The missing vcpkg.json stops configuration before any extension compilation.

Provide a valid manifest even though the extension has no vcpkg dependencies,
and exercise this build path in source CI. Keep DuckDB 2.0 as the target.
See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
