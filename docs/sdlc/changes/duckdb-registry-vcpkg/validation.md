# Verification

Date: 2026-10-20. Author: Codex. Status: local checks passed;
remote Linux build and human review pending.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

The failed registry job's first fatal error was vcpkg reading the absent
`/duckdb_build_dir/vcpkg.json`; the subsequent compiler/Ninja messages were
consequences of incomplete configuration. Its toolchain commit was
cd61e1e26a038e82d6550a3ebbe0fbbfe7da78e3.

Local verification used that actual vcpkg commit on macOS arm64 and DuckDB
2.0 source d54cbbe3df17e8e5ce68312f483b4155836112aa:

- Negative configuration with VCPKG_MANIFEST_MODE=ON and an empty manifest
  directory reproduced the same missing-file error and returned failure.
- Configuration with CMAKE_TOOLCHAIN_FILE pointing to the pinned vcpkg.cmake,
  VCPKG_BUILD=1 and VCPKG_MANIFEST_DIR pointing to the fixed checkout completed
  successfully. vcpkg reported all requested installations successful.
- `cmake --build ... --target redis_adbc_loadable_extension -j 4` succeeded,
  including the embedded Go archive, C++ compilation and extension linking.
- The matching DuckDB 2.0 CLI loaded the newly built extension and reported
  redis_adbc as loaded. Five offline cases from test/sql/redis_adbc.test passed:
  reject DDL, multiple statements, NULL URI, invalid scheme and disabled
  external access with their expected errors, before connecting to Redis.
- Manifest JSON and workflow YAML parsed; the Linux amd64 step enables the
  pinned vcpkg toolchain. The pinned extension tools translate its environment
  variable into CMAKE_TOOLCHAIN_FILE and the root VCPKG_MANIFEST_DIR.
- `git diff --check` passed.

The local build used cached copies of the exact DuckDB source and Go toolchain,
omitted unrelated built-in extensions, and built the loadable extension only.
No new Redis runtime or full engine build was required for this packaging-only
change. Linux amd64 CI runs the existing complete build and runtime tests with
vcpkg enabled; no unobserved CI success or maintainer approval is claimed.

Review against REVIEW.md: no SQL/storage/lifecycle or security behavior changed;
the manifest adds no dependencies and workflow permissions remain read-only.
The remaining platform uncertainty is the remote Linux build result.
