# Specification

2026-10-21; baseline `95d6365`; requestor alberttwong; issue #374.
See [intent](intent.md), [plan](plan.md). Status: implemented and locally validated; awaiting maintainer review.

- Both APIs construct their cursor/context/Arrow reader through one helper.
  Ordinary execution retains first-fill materialization, row count and statement
  plus connection ownership. Partition readers retain immediate connection-only
  ownership and independent authentication.
- A shared internal row-ID range source intersects safe Search predicate
  pushdown with the exact inclusive row-ID window and rechecks residuals.
  Direct-key ordinary queries retain direct HASH lookup. Partition descriptors
  are not serialized/deserialized for ordinary execution.
- When the existing ordinary lowered-cap HASH fallback is selected, bounded
  ranges replace it only for permanent managed stable-prefix numeric-ID tables,
  IDs below 2^53 and <=2^20 subranges. The plan must be row-local and require
  implicit insertion order or explicit ascending __rowid order. Each range sorts
  by unique exact internal ID; sequential ranges preserve the total order.
- LIMIT/OFFSET run once outside the range source. Other ordering, blocking plans,
  temporary/adopted/foreign/legacy/large-ID layouts and unavailable/nonpositive
  fresh caps retain their existing ordinary fallbacks/errors. No snapshot is
  added; the bounded source captures allocated IDs when preparing the scan.
- Cap discovery handles RESP2 alternating field/value lists and RESP3 maps.
  Ranges refresh caps before each cursor and keep the existing contract against
  concurrent lowering below an open cursor's bound. Ordinary initial capability
  caching/hidden-setting defaults are otherwise unchanged.
- Before opening a subsequent range, revalidate prefix ownership/generation.
  A truncate/drop/rekey must return the existing ordinary stale-read IO error,
  never silently switch to another prefix. Release/cancel opens no new ranges.

Verification compares ordered exact values/schema with the previous complete
HASH fallback, including sparse IDs, bindings, NULL/decimal projections and
cross-window LIMIT/OFFSET. Native and existing SQL tests cover public contracts;
measurements record command/memory tradeoffs without a universal speedup claim.
