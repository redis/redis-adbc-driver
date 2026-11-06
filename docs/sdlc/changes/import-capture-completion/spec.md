# Import capture completion specification

Date: 2026-11-06. Status: implementation under review in PR #332.
See [intent](intent.md) and [plan](plan.md).

- Begin capture before source iteration or destination output.
- Completion closes input mutation and checks exact journal/directory membership
  in checkpoints of at most 128 entries with a 1 MiB key-byte target and the
  existing 64 MiB single-value ceiling. Failed validation cannot reopen capture.
- Output requires a ready capture for the same private data generation. Each
  atomic batch consumes captured keys and decrements the durable remaining count
  with its complete row images and outcome. Ordinary row mutation cannot bypass
  capture consumption.
- Seal and publication reject incomplete or unconsumed capture. The existing
  metadata-publication proof remains required alongside the capture proof.
- Unknown completion replies fail without replay. Unknown output replies retain
  the existing durable batch-outcome resolution. Abort/expiry use existing GC.
- Preserve binary source keys, exact native values, empty imports, deduplication,
  permissions and retained original-key provenance.

Acceptance uses focused Software QPF4 import and Cluster API regressions,
including incomplete capture admission, bounded validation, frozen input,
mismatched membership and lost replies. This is private statement state;
no detached operation ownership or public resume API is introduced.
