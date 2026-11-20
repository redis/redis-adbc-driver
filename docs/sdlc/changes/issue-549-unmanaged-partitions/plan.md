# Implementation and evidence plan

Date: 2026-11-20. Base: `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implemented and locally verified; awaiting human review; no release recorded.

See [intent](intent.md), [specification](spec.md), and [verification](verification.md).

1. Add the explicit source selector and strict version-5 hash-bucket descriptor.
2. Reuse the catalog partition TTL/child-pin lifecycle without freezing source rows.
3. Reuse SCAN/INKEYS discovery on Software RAM and Flex, adding assigned-key/work
   credits and complete source-definition checks; keep all SQL predicates.
4. Add a scoped strict HASH pipeline reader for composed-error safety.
5. Test worker-count-independent exact values, duplicate/overlap coverage,
   binary/NUL keys, source values/TTL/unknown-field preservation, independent
   processes, source changes, expiry, typed parameters, failures and cancellation.
6. Run focused race checks on verified 4x QPF Software RAM and actual Flex, unit
   checks and relevant existing regressions. Measure a labeled synthetic comparison
   and document its scale/limits without assuming speedup.
7. Update current source coverage docs, review lifecycle/ownership, open a PR.

No historical measurements or credentials are copied into these records.
