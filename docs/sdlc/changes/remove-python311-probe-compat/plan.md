# Plan and validation

Date: 2026-11-17. See [intent](intent.md) and [specification](spec.md).

1. Remove custom extraction compatibility code from both builders; require
   Python 3.14 and document it for new runs.
2. Pin the historical artifact auditor to its original committed harness bytes.
3. Build both private libraries on Linux with Python 3.14; run the server-free
   basic ownership probe and the historical artifact audit. No Redis tests or new
   latency comparison is needed for this extraction-only change.
4. Submit one follow-up PR for the two merged probe PRs. Keep old evidence intact.

## Validation and review

- Both updated builders compile their private Linux/aarch64 libraries using
  Python 3.14.10, Go 1.27.4 and source `d14eaf4e`. Ownership build uses unchanged
  GC with checkptr=2/cgocheck2; all seven original server-free groups pass with
  zero checked allocator bytes. Native proof library also builds with checkptr=2.
- Python 3.14 syntax compilation and the historical artifact audit pass. An
  initial validation attempt stopped at writing py_compile's cache to a read-only
  source mount; the corrected invocation places caches in its private directory.
  Neither attempt contacted Redis or collected new performance evidence.
- Historical observations and recorded Python 3.11 labels/hashes are unchanged;
  the artifact auditor verifies the original merged harness commit explicitly.
- Source review confirms both custom extraction loops are gone, both builders
  require Python 3.14+, and the standard data filter is explicit. Go selection,
  shipping code and public package support declarations are unchanged.
- [Other compatibility layers](compatibility-audit.md) answers the follow-up audit
  request with code locations and declared consumers. Those layers need separate
  scope/consumer review before removal.
