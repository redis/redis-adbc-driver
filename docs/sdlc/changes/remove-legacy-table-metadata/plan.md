# Plan and validation

Date: 2026-11-17. See [intent](intent.md) and [specification](spec.md).

1. Delete metadata migration, identifier fallback/marker and older-binary probes.
2. Remove compaction initial-scan compatibility; retain current queue eligibility
   until publication so a discarded private output is rebuilt from pinned data.
3. Rename the public source-type field, update current docs and test exact JSON.
4. Test on an exclusively owned Redis Software 8.8.0 three-shard fixture, effective
   active 4x QPF/six Search workers/mnp32/noeviction verified via make redis-up
   before live checks. Keep operational artifacts private. Run all unit packages
   separately with Redis URIs unset; those skips are not live coverage.

## Checks so far

- Initial identifier/migration subset passed. Expanded checks found two test/
  recovery requirements: untagged fixture keys need a nontransactional pipeline
  across shards; a discarded full-merge output needs its candidate retained.
- After those corrections, the focused recovery/import/inspection subset passes,
  including exact final rows, no initial-scan backfill, current preindex restart,
  source_type JSON and unchanged old metadata/application HASHes on refusal.
- Final expanded live compaction/selective-compaction/import/inspection/identifier
  suite passed (98.665 seconds) on the owned verified Software fixture. The
  focused recovery subset passed (5.852 seconds). All Go unit packages passed
  with Redis URIs unset; expected integration skips do not substitute for live
  coverage. Public JSON checks require source_type and reject in_place_type.
- No new performance evidence or GC-policy change. Historical reports retain
  their original names/settings; current report consumers must use source_type.

Author review checked current queue publication/retirement, source reference
retention, no initial-scan backfill, exact SQL identifier semantics and the
intentional public field break. This is not maintainer approval.
