# Specification

- PRs run DuckDB extension validation in its standalone workflow. Release and
  manually dispatched binary builds retain the reusable DuckDB build.
- The Test workflow owns Linux PR Go source-suite coverage. Release PRs retain
  native builds, installed-wheel ADBC validation, TLS and installer checks;
  release/manual runs also retain the original Go and source ADBC suites.
- Routine PR distribution coverage is AlmaLinux 8 (minimum glibc) on amd64 and
  arm64. ABI/toolchain, dependency, fixture, validation, installer, execution
  lifetime and workflow changes retain all five distributions on both arches.
  Non-PR events and historical release tags without the selector retain all ten.
  Selection examines the complete base/head diff, includes rename sources, and
  fails on a missing or invalid diff.
- Linux and macOS wheel-client gates share a reusable workflow, but depend only
  on their respective artifact producers. Publication requires both gates and
  all other existing release gates to succeed. Permissions are unchanged.
- Remove unreachable OSS-cluster and Prefect branches from Test, and retire
  the completed SIMD workflow. Preserve all experiment evidence and commands.

No production driver behavior changes. Reduced routine PR coverage trades some
per-PR distribution redundancy for speed; the complete release matrix remains
mandatory. No fixed speedup is promised before the changed workflows run.
See [intent](intent.md) and [plan and validation](plan.md).
