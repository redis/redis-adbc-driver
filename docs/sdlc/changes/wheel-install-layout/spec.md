# Specification: wheel library location validation

Date: 2026-10-29. Inputs: [intent](intent.md). Execution: [plan](plan.md).

Accept exactly one bundled native library at the package root or the matching
wheel distribution/version's `.data/purelib` or `.data/platlib` path. Retain the
intentional .so suffix for all platforms, including macOS Mach-O. Reject absent,
ambiguous or unrelated paths; retain format/architecture and checksum gates.
Delegate library location validation to the existing native validator rather
than duplicating a contradictory root-only assertion in the publisher workflow.
Do not rewrite tags, regenerate wheels or relax the platform set.
