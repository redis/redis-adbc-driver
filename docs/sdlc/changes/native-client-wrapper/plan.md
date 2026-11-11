# Native client wrapper implementation plan

Date: 2026-11-11. Author: Codex. Status: initial implementation verified locally;
awaiting human review.
Request: [intent](intent.md). Contract: [spec](spec.md).

1. Factor concrete connection creation out of the ADBC builder without changing
   its initialization or ownership; reuse it from the native reader.
2. Add the Go reader using existing snapshot scopes, cancellable transport,
   descriptor proofs and direct-row visibility. Expose HASH methods and explicit
   shared-view acquisition/release.
3. Add a versioned handle-based C boundary in the existing shared library and a
   Python wrapper with explicit table binding, raw-byte semantics and dedicated
   snapshot contexts.
4. Verify native row semantics, lifecycle failures and Python ABI behavior on
   Redis Software with active verified 4x QPF; run unit and focused existing
   shared-snapshot regressions. Review ownership, authorization and limits.
5. Document the initial scope and evidence. Application-key mappings and native
   Search remain separate follow-up work within #385.

Actual results: [verification](verification.md).
