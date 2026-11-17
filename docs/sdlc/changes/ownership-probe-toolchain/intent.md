# Keep ownership probes buildable on current source

Date: 2026-11-17. Source: `292ee254` (merged #462). Status: implementation.
Requestor: repository user continuing #446. Author: Codex.

The ownership builder forces Go 1.27.3, but current source requires 1.27.4. Its
archive extraction also assumes a tarfile API absent in the Linux image's Python
3.11. These prevent the unchanged-policy native safety probe from reaching its
actual checks. Follow the archived module's toolchain and safely support that
Python image, preserving old evidence and historical source selection.

No production bridge, dependency, GC or ownership-policy change. See
[specification](spec.md) and [plan/validation](plan.md).
