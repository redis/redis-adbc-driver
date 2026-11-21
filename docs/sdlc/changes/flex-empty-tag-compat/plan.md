# Flex empty-tag compatibility: plan

Date: 2026-11-21. Base: `139350b7`. Status: implementation, bounded indexed
pilot and same-build pilot backup/restore validation complete.
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

Implementation, focused native lifecycle checks, independent source review and
the canonical 2,013,006-row indexed pilot passed. The native GC comparison used
different write orders; original-order crash avoidance and full-scale success
remain unverified. Record actual results and exclusions in `validation.md`.

Before another full load, export the retained pilot through the documented
Software backup API and restore it into a separate, verified empty Software
Flex database with active 4× QPF. Verify artifacts and restored membership,
schema and typed samples. Preserve the source and never replay an uncertain
export/import request.

Full-scale loading remains separate: the pilot's 32 GiB logical capacity is not
a full-dataset capacity claim, and the earlier six-hour statement deadline is
inadequate for the observed input plus indexing/proof work. Establish logical,
working-memory and backup-storage admission and a finite operation deadline
before starting another full input stream.
