# Plan and verification: README and Arrow feature matrix

Date: 2026-10-22. Author: Codex. Status: implemented; documentation checks complete.

1. Reviewed repository guidance, existing README and Arrow IPC/partition guides.
2. Checked type conversion, statement methods, metadata and native C bindings;
   consulted the official Arrow format and ADBC API specifications linked from
   the matrix.
3. Moved the detailed reference, added the matrix and updated incoming links.
4. Checked moved content preservation, local links and anchors, and whitespace.

Verification: content preservation and documentation link checks passed;
`git diff --check` passed. The original README and updated documentation have no missing local link
destinations or anchors in the checked Markdown links.
No runtime tests or benchmarks were run for this documentation-only change.
Review covered support claims, historical evidence labels and navigation.
Human review/approval remains unrecorded.

See the [intent](intent.md) and [specification](spec.md).

Conflict resolution: merged current `main`, preserved its eligible UNION ALL
streaming documentation in the moved reference and IPC guide, updated the matrix,
and rechecked section preservation against `origin/main` plus local links/anchors.
