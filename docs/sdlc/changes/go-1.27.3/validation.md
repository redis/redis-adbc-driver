# Validation: Go 1.27.3 upgrade

Date: 2026-11-03. Baseline: `95d6583`. Status: implemented and locally validated.

Official Go release metadata identifies Go 1.27.3 as the latest stable version.
All four CMake archive SHA-256 values came from that metadata. The macOS arm64
archive was downloaded to a temporary directory, its SHA-256 independently
verified, and its executable reported `go version go1.27.3 darwin/arm64`.

## Actual checks

- `env -u REDIS_URI GOTOOLCHAIN=local <verified-go>/bin/go test -count=1 -p 1
  -json ./...` passed all three test packages on macOS arm64: 296 passing
  test/subtest events, zero failures and 456 skips. Of these, 445 skips request
  `REDIS_URI`; the rest require dedicated profile, pause or Flex environments.
- The shared library (`make -B -C go build`) and embedded C archive
  (`go -C go build -buildmode=c-archive -tags driverlib,duckdb -trimpath ./pkg`)
  built with the verified Go 1.27.3 executable, macOS deployment target 15.0
  and matching C compiler/linker minimum-version flags.
- Built `adbc_driver_redis-0.1.0.dev0-py3-none-macosx_15_0_arm64.whl` locally,
  verified its WHEEL compatibility tag, installed it into a temporary environment
  without dependencies, inspected the installed library with `otool` and confirmed
  `minos 15.0`. `ctypes.CDLL` loaded the native library and resolved
  `AdbcDriverInit`. This was a native-load check, not a DB-API query test.
- The builder rejected a requested `macosx_14_0_arm64` tag with exit code 2.
- All four CMake checksums and the module/CMake version pins independently
  matched official Go download metadata. Changed workflow YAML parsed;
  local documentation links resolved; `git diff --check` passed.

## Review and limits

Self-review under `REVIEW.md` found no outstanding actionable findings. Go
dependency versions and `go.sum` are unchanged. Active documentation, release
compiler flags, CMake targets, wheel builder and publication platform set agree
on macOS 15; the DuckDB CI runner is now macOS 15. Go's own host-toolchain
minimum is macOS 13, which is distinct from this project's macOS 15 policy.
Dated benchmark and historical validation toolchain versions were preserved.

Not run: Redis integration on the new toolchain, Linux/cross-architecture
builds, full DuckDB extension linking/runtime tests or remote CI. Local DuckDB
submodule sources and CMake are unavailable; the embedded Go C-archive path was
built directly. No runtime test was performed on a physical macOS 15 host.
No SIMD flags were enabled; no query speedup is claimed from this upgrade.
This self-review is not maintainer approval.
