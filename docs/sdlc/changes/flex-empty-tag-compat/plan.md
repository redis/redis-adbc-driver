# Flex empty-tag compatibility: plan

Date: 2026-11-21. Base: `139350b7`. Status: in progress.
See [intent](intent.md) and [specification](spec.md).

1. Reproduce or narrow the native failure on a dedicated verified Software Flex
   fixture; compare the original empty-posting option with its omission.
2. Change only plain-index generation and conservative empty-candidate handling.
   Audit shared constructors, capability probes and old physical layouts.
3. Verify empty/NULL and normalized-string semantics through native managed
   lifecycle tests. Keep normal RAM behavior and unmanaged index policy intact.
4. Run focused tests and applicable existing Flex contracts, then review the
   production diff and update the public compatibility documentation.
5. Record actual results, exclusions and unresolved native coverage. Do not
   relabel the earlier failed benchmark as a success or restart the full load
   before the bounded checks establish a viable path.

Validation and review results will be recorded in `validation.md` as completed.
