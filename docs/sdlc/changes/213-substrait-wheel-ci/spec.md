# Specification: header-independent native harness

Date: 2026-11-04. Baseline: `4fd0894`. Author: Codex.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

Compile against the repository's public adbc.h only. Dynamically load the exact
library supplied by the validation fixture and initialize its ADBC 1.1 vtable.
Keep null/nonzero pointer, oversized invalid address, SIZE_MAX and subsequent
successful SQL assertions unchanged. No generated headers, build filenames,
rpath or working-directory library lookup may be required. Link libdl on Linux.

Changes under go/validation/ must trigger the release workflow, whose installed-
wheel jobs exercise this layout on all three platforms. Workflow permissions
and publishing conditions stay unchanged.

Run the harness from a temporary directory against both the supplied library
and a headerless renamed copy. Report compiler stderr on failure. No driver,
packaging, feature behavior, permissions, assertions or skip conditions change.
