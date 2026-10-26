# Implementation plan

Date: 2026-10-20. Author: Codex. Status: implementation complete;
verification recorded separately; remote CI and review pending.
See [intent](intent.md), [spec](spec.md), and [validation](validation.md).

1. Inspect the exact failed registry job and identify its first fatal error.
2. Use an isolated checkout to preserve ongoing user edits in the main workspace.
3. Add vcpkg.json and enable the pinned toolchain in existing Linux amd64 CI.
4. Reproduce configuration failure without the manifest, then configure and
   compile the extension with the manifest against the registry's DuckDB 2.0.
5. Open a source PR and update community PR #3157 to its verified source commit.
