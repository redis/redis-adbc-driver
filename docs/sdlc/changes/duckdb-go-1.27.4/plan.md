# Plan and evidence: repair the DuckDB toolchain bootstrap

Date: 2026-11-17. Source: `2b6634e0`. Author: Codex.
See [intent](intent.md) and [specification](spec.md).

1. Confirm current failure and inspect prior fixes on main: complete. The
   embedded archive build refuses Go 1.27.3 after the dependency refresh.
2. Update all four bootstrap checksums and the version URL/comment; update
   current extension build instructions: complete.
3. Verify official checksum metadata and CMake bootstrap; build the native
   embedded archive: pending.
4. Review diff, publish a fix PR, and inspect its Actions results: pending.

No Redis tests or benchmarks have been run for this change.
