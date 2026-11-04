# Intent: upgrade Go to 1.27.3

Date: 2026-11-03. Baseline: `95d6583`. Requestor: repository user in this chat.
Status: implemented and locally validated.

Upgrade the driver and embedded DuckDB toolchain from Go 1.26.6 to the current
stable Go 1.27.3, including current security fixes and a foundation for future
SIMD experiments. This does not implement SIMD kernels or enable experimental
compiler flags. The user also requested the oldest macOS branch in Apple's
current security update batch: macOS 15, based on the October 26, 2026 releases.
See [specification](spec.md) and [plan](plan.md).
