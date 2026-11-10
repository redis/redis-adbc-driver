# Shared snapshot metadata discovery: plan

Date: 2026-11-10. Source baseline: `2c50655c`. Status: implemented and locally validated; awaiting review.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Preserve the merged #369 runtime/API while resolving concurrent branch changes.
2. Reproduce unavailable-view GetObjects success at catalog-only depth and with
   nonmatching catalog filters.
3. Wrap driverbase metadata construction in one captured scope and pin.
4. Add automated Python full-refresh coverage using the published option names,
   and add those options to the driver reference.
5. Validate focused/race, RBAC, native Python, unit and documentation checks;
   update PR #373 around the final scope and evidence.

Issue #365 remains open for its released-version and connector enablement evidence.
No release or merge approval is inferred by this change.
