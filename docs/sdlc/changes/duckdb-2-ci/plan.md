# Plan and verification

Date: 2026-10-21. Status: implementation merged in PR #382; local checks passed.
See [intent](intent.md) and [spec](spec.md).

1. Inspect failed Actions jobs and reproduce Linux native extension loading.
2. Select the 2.0 patch series and build the scanner from its upstream source
   against the installed engine tag, with compiler caching on both architectures.
3. Preserve read/write/transaction coverage and diagnose/fix native loading.
4. Run focused integration and SQL tests, inspect the final diff, and record
   actual verification results and limitations here before opening a PR.

Confirmed: PyPI currently offers 2.0.0 as the newest stable 2.0.x release.
The upstream v2.0 artifact is stamped `v2.0-cyanoptera`, so it cannot be loaded
in the released v2.0.0 wheel. A source build with the exact engine tag is needed.

Local checks completed:

- Built the pinned scanner with its upstream vcpkg toolchain against the exact
  v2.0.0 source, reusing an existing matching static DuckDB engine archive.
- DuckDB 2.0.0 + source-built scanner + disposable Redis 8.6.2: Python suite
  **25 passed, 2 skipped** (Ray not installed; Superset uses a separate environment).
  The DuckDB read/write/join/transaction test passed without skips.
- `actionlint` 1.7.12 and YAML/bash syntax checks passed for both workflows.
- Reproduced Linux arm64 direct loading failure in a Debian/glibc container:
  `cannot allocate memory in static TLS block`. ELF inspection identifies a
  2,704-byte jemalloc TLS variable sharing a module with Go's initial-exec TLS.
  Disabling `ENABLE_JEMALLOC` in the extension configuration selects DuckDB's
  system allocator.
- Rebuilt the full native extension on Linux arm64 without jemalloc. Direct
  loading succeeded with no `LD_PRELOAD` or loader tunables; ELF TLS shrank from
  2,792 bytes to 88 bytes. Offline SQLLogicTests: **1 passed, 0 skipped**.
- Native Linux arm64 Redis runtime: **13 CLI scenarios passed**, including
  physical types and every value in 10,003 rows. The fixture used the current
  checkout's Go driver, built inside the disposable Linux container.
- Rebuilt the current checkout's macOS fixture with Go 1.26.6 and reran Python
  analytics: **25 passed, 2 expected skips**, with the DuckDB integration passing.
- Remote Linux amd64/arm64 and macOS CI results remain pending; local Linux
  uses Debian/glibc, while hosted native CI uses Ubuntu 24.04.

PR #382 merged as 8f4c5f5763dbef7fa4f096b6da2d19218e6fb9d3 during verification.
The registry still pins e3d138291944904299ca8d05a2cf51c5d7ea261b; a separate
descriptor PR is needed to publish binaries containing the TLS fix. The
follow-up source change installs scanner build tools explicitly on hosted Linux.

Review: the system allocator changes allocation implementation, not row or Arrow
ownership contracts. The source scanner remains a separate C++ extension using
the driver library. No transaction assertions or skip guards were weakened.
