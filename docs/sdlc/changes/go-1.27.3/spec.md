# Specification: consistent Go toolchain upgrade

Date: 2026-11-03. Baseline: `95d6583`. Status: implemented.
See [intent](intent.md), [plan](plan.md) and [validation](validation.md).

- `go/go.mod` requires Go 1.27.3. Existing GitHub workflows read that file and
  therefore select the same version without duplicated workflow pins.
- DuckDB's CMake bootstrap downloads Go 1.27.3 and verifies official SHA-256
  checksums for Linux/macOS on amd64/arm64. Keep `GOTOOLCHAIN=local` in the
  embedded-driver build. Go 1.27 requires macOS 13+, while the user requested
  the oldest currently Apple-updated macOS branch, macOS 15. Raise C/C++/Go
  deployment targets, release wheel tags, publication verification and the
  DuckDB macOS CI runner together. Reject wheel tags older than macOS 15.
- Update active build requirements and guidance. Preserve dated historical
  benchmark and validation versions as evidence of their actual toolchains.
- Do not change module dependency versions, SQL behavior, Arrow ABI, public
  options or SIMD flags as part of the upgrade.

Sources: [official downloads](https://go.dev/dl/?mode=json),
[release history](https://go.dev/doc/devel/release),
[Go 1.27 release notes](https://go.dev/doc/go1.27).

The macOS baseline is a project policy based on
[Apple's October 26, 2026 update batch](https://support.apple.com/en-us/100100),
which includes macOS 15, 26 and 27. It is not an Apple guarantee of future
support duration. The deployment target and wheel compatibility tag are 15.0;
users should install current security patches within their macOS release.
