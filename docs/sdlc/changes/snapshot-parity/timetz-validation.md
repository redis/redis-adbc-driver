# TIMETZ authoritative storage checkpoint

Date: 2026-11-04. Parent cd98561. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The next fresh 106-name ordinary continuation passed 24 test events and explicitly
skipped the separately gated CLIENT PAUSE test, then stopped at TestTimeTZColumns'
unpinned metadata read (root 6.218 s). This follows the separate single-index and
segmented rounded-order checks; it is not a full ordinary pass.

The TIMETZ fixture now inspects metadata and complete authoritative directory
rows under one pin. It checks the exact multiplicity of all seven physical text
values, including the NULL/missing value, rather than looking only for three
examples and ignoring HGET errors. Every read error fails the test. The pin is
released before subsequent SQL writes, with deferred cleanup on failure.

## Validation and review

Owned strict-TLS Redis Software with noeviction and active effective 4x QPF
verified before testing. All nine selected TIMETZ race test events pass with
zero test skips/failures; root 4.471 s. CUDA has no applicable selected tests and
is skipped. Mutating selections run sequentially. Existing tests retain complete
SQL ordering/filter/group/join/window/function comparisons, precision and Arrow
type metadata, timezone behavior, UPDATE/CTAS/defaults, ingestion and binding.

Reviewed pin lifetime, complete directory iteration, EOF/error handling, exact
native text and NULL counts. No production, protocol or ACL change. Author review
is not independent maintainer approval. Full ordinary/platform/recovery,
restricted readers, public input/process resume, sustained compaction and
comparative performance remain open.
