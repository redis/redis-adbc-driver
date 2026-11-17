# Specification: repair the DuckDB toolchain bootstrap

Date: 2026-11-17. Source: `2b6634e0`. Status: implemented; native archive validation passed.
See [intent](intent.md) and [plan/evidence](plan.md).

The extension bootstrap must download Go 1.27.4 for Linux and macOS on amd64
and arm64, with SHA-256 checksums from the
[official release metadata](https://go.dev/dl/?mode=json).
Keep local-only Go toolchain selection and checksum verification. Update the
extension build instructions to identify the selected version accurately.

Acceptance: CMake downloads and verifies the native toolchain, the downloaded
binary reports Go 1.27.4, and the embedded driver builds as a C archive with
`GOTOOLCHAIN=local`. GitHub validates the Linux DuckDB build and SQL tests.
No Redis behavior changes; runtime checks require Software with verified 4x QPF.
