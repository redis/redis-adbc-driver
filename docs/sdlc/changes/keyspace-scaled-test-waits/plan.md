# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. On an isolated Software fixture holding the 1.94M-key TPC-H subset, measure a new index's
   initial-scan time and SCAN `COUNT 100` / `COUNT 1000` rates per key.
2. Add `keyspaceWait` and `clearPrefix`; use them in the retained-store cleanup, `appIndex`
   and the live index restore wait. Add `dropStaleLive` to `newHashFixture`.
3. Show the failures on main's helpers (through `go test -overlay`): the readiness timeout,
   and `already registered` after an interrupted live test.
4. Run the four tests named in #653 with the change on the same fixture, then the live-HASH,
   partition and retained suites, and the unit-only pass.
5. Open a PR that fixes #653 with the measurements.
