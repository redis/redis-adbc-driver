# Shared snapshot metadata discovery: intent

Date: 2026-11-10. Source baseline: `2c50655c` (includes PR #369).
Requestor: Albert Wong. Status: implemented and locally validated; awaiting review.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [specification](spec.md), [plan](plan.md), [validation](validation.md).

## Problem

The merged shared-view implementation binds schema/table enumeration callbacks,
but GetObjects can return catalog-only or empty filtered metadata without invoking
those callbacks. An unavailable bound view can therefore appear to succeed.
Independently binding callbacks also gives no single ownership boundary for the
whole metadata operation.

## Requested outcome and boundaries

Acquire one view before any GetObjects discovery, including catalog-only and
nonmatching-filter requests; hold it until complete Arrow metadata construction.
Reuse the published #369 API, retention limits and parent/reader ownership rules.
Add automated Python full-refresh coverage and document the existing API in the
option reference. Release and connector evidence remains outstanding in #365.
