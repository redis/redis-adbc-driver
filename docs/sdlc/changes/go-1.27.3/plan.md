# Plan: Go 1.27.3 upgrade

Date: 2026-11-03. Baseline: `95d6583`. Status: implemented and locally validated.
See [intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Confirm current stable release and all four official archive hashes: complete.
2. Update module, CMake bootstrap and active documentation pins: complete.
3. Verify official local archive, run full unit-only suite and build both shared
   library and embedded C archive with Go 1.27.3: complete.
4. Raise the minimum macOS release to 15, including CMake, release compiler flags,
   wheel builder/publication tags, macOS CI runner and active documentation:
   complete. A local wheel passed Mach-O minimum-version and native load checks.
5. Check pin/hash consistency, YAML syntax and review diff: complete. Test skips
   and unavailable cross-platform/DuckDB checks are in [validation](validation.md).
