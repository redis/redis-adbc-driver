# Positional values for narrow snapshot projections

Date: 2026-11-05. Parent: 5a4604a, production c06e181.
Status: focused unit reproduction and actual API checks pass; native and
comparative performance checks remain pending. See [intent](intent.md), [spec](spec.md),
[plan](plan.md) and [implicit-order evidence](implicit-order-validation.md).

After exact snapshot visibility selects the winning physical HASH, a narrow
simple stream still builds an intermediate HASH map and decoded row map.
The existing positional HASH/Arrow path is limited to wide projections, even
though it already checks physical row identity, tombstones, missing values and
NULL markers. Extend that path to narrow directory/candidate reads without
changing ordinary live Search fetch eligibility or exposing a product option.

This changes eligibility after visibility resolution only. The existing simple
projection binder still rejects unsupported expressions. Residual predicates,
tie sorting, late projection, pushed limits, descending/other sort orders and
unmanaged HASHes retain their existing paths. Prefetch's separate width gate
is unchanged, so narrow streams do not acquire new prefetch workers.

The new operator-level unit test fails against c06e181 because the real snapshot
scan cannot bind positional values. After the change it checks duplicate/aliased
outputs, embedded NULs, exact BIGINT beyond 2^53, row identity, an older row's
missing-column default and its explicit NULL override. Seven boundary cases
retain map/materializing fallbacks. The focused race selection, including
existing map-versus-positional decoder comparisons, provenance, projection and
wide typed values, passes; root 1.972 s. An initial build had an unused local
after removing the wide-only eligibility check; it was corrected before this
passing run. Redis endpoints are unset for these unit checks; they do not prove
server compatibility.

The existing both-API projection integration case additionally requires
positional values on narrow shared-generation partitions, comparing every
returned value with the independent buffered path. Segmented ordinary streams
also require positional binding when the eligible path is selected.

The subsequent verified Software race selection passes 57 test events, zero
skips/failures, root 12.990 s. It includes the strengthened both-API case,
early-release/cancellation, HASH integrity, narrow boundaries, provenance,
implicit/explicit ordering, candidate visibility, prefetch, NULL-order proofs,
readiness transitions and Substrait visibility. The earlier c06e181 root snapshot
selection completes separately with 430 passes / 25 explicit skips / no failures,
964.963 s; it does not cover this later change.

Server checks are sequential on owned Redis Software 8.8.0, strict verified TLS,
active/effective 4x QPF and noeviction, three primaries behind its proxy; macOS
ARM64 / Go 1.27.3. These do not establish Cluster API or recovery compatibility.

Current production/server evidence remains attached to c06e181 until fresh
checks of this change finish. Its complete snapshot race process was already
compiled and running before these edits; do not relabel its result as coverage
of this new eligibility change. Keep matched native benchmarks isolated from
all local builds, tests and database probes, and retain both measured gains and
remaining mutable gaps. No performance gain is claimed yet for this change.
