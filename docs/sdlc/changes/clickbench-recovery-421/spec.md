# Specification

Linked [intent](intent.md), [plan](plan.md), issue #421. Status: implementation
and operational verification in progress, 2026-10-21.

- Supplemental scans pass only with all nine independently valid pairs and all
  18 successful measurements. Finished attempts are recorded separately.
- Worker failures retain causes after redacting connection URLs/credentials.
- Backup preflight uses CLUSTER INFO and records node, expected/actual document
  counts, indexing and loading failures. Restore and teardown gates stay closed
  for incomplete backups or unvalidated scan passes.
- Parallel top-K retains final output fields and sort keys when bypassing late
  projection. Residual predicates and exact HASH values remain authoritative.
- Managed row-ID scans use at most 65,536 IDs in one Search query, including
  servers with unrestricted result caps. LIMIT/OFFSET, ordering, readiness,
  cursor cleanup and ownership semantics remain global and unchanged.
- Redis RSS is reported alongside logical allocation; RSS headroom is against
  this deployment's explicit 27 GiB container limit.
- Recovery artifacts and rerun binaries/results have separate provenance.
  Original failures and unresolved oracle semantics remain visible.


2026-10-21 scope update from Albert: rerun only incorrect, failed or mismatched queries. Stop the full fixed sweep and preserve its completed trials. Run Q24/Q25/Q27 three times each (nine trials), independently validate all nine outputs, then rerun the previously failed 18 supported-scan measurements. Retain original full evidence separately; do not claim a complete fixed-binary sweep or mix binary timings into a full score. Complete backup, approved restore checks and cleanup remain required.


Scan timeout recovery: a full-data statement scan reached its1,200-second cap while making progress without memory pressure. Preserve that failed attempt and partial Arrow output, let its already-started partition partner finish unchanged, then resume only missing/failed measurements with a1,800-second cap. Verify source/binary/case identity on resume, retain successful measurements and independently revalidate each pair. The overall scan deadline and backup/restore reserve are unchanged.
