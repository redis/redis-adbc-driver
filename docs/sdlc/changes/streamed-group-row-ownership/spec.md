# Specification

Date: 2026-11-24. Status: clean public follow-up candidate, stacked on PR619.
See [intent](intent.md), [plan](plan.md), [verification](verification.md).

A serial aggregate may attach a request-local ownership-marker pointer when its
plan is eligible for star streaming. Eligibility itself never grants ownership.
Only the visitor invoked by a successfully executing star stream sets the marker,
for exactly that synchronous coordinator callback, restoring its prior value with
defer on success, error, cancellation or panic. Workers never touch the marker;
nested native/UNION/memory source requests do not inherit it. No executor/global
or persisted ownership state is introduced.

After every aggregate input has consumed a first group row, prune the existing
owned native/stream map directly. Unmarked memory-backed rows retain the original
clone-before-prune behavior. Preserve group/aggregate values, first-row semantics,
source/CTE/dimension maps, independent leaf maps and all output fields needed
later. Unsupported/refused streams and retained/private contexts never advertise
owned callbacks. No new source scan, query rewrite or visibility change occurs.

The separate [panic cleanup fix](../parallel-star-panic-join/spec.md) cancels the
child and joins workers before grant release. Ownership tests also run through
that actual parallel panic path and verify the marker is restored afterward.
