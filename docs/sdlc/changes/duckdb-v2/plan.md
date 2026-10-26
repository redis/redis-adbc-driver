# Implementation plan

Date: 2026-10-20. Author: Codex. Status: steps 1–4 complete; submission update
and remote review pending.
See [intent](intent.md), [spec](spec.md), and [verification](validation.md).

1. Read the maintainer comment and confirm the registry's source/tool versions.
2. Update pinned submodules and make the native CI matrix build/test 2.0.
   Remove the redundant compile-only job and use the existing compiler cache.
3. Use the 2.0 test runner's missing-extension failure flag and enhance CLI
   runtime verification to assert physical types. Update build/test guidance.
4. Build and run local 2.0 verification on a disposable Redis with Search.
5. Open the source PR, then update community PR #3157's pinned ref and body
   to state explicitly that the submission targets 2.0.
