# Contract

Date: 2026-10-20. Author: Codex. Status: implemented; review pending.
See [intent](intent.md), [plan](plan.md), and [validation](validation.md).

- Supply a root vcpkg.json with valid package metadata and an empty dependency
  list. No third-party library is needed from vcpkg.
- Run the existing Linux amd64 build/tests with the registry's pinned vcpkg
  toolchain, cd61e1e26a038e82d6550a3ebbe0fbbfe7da78e3.
- Include manifest edits in the DuckDB workflow's path filter.
- Reproduce the missing-manifest failure with the real toolchain and verify
  configuration and extension compilation with the new manifest.
- Preserve the existing DuckDB 2.0 source pin, runtime interfaces and tests.
  No Redis storage behavior or release permissions change.
