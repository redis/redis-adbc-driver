# GetObjects table-type filtering: plan

Date: 2026-11-19. Source: `3e45955b`. Status: completed, awaiting review.
Inputs: [intent](intent.md), [specification](spec.md).
Issue: [#537](https://github.com/redis/redis-adbc-driver/issues/537).

## Files and order of work

1. Audit Redis's enumerator, native C shim, pinned driverbase filtering, and Python
   driver-manager list conversion. Completed: existing runtime supports filtering.
2. Enable only the table-type capability in `go/validation/tests/redis.py`.
3. Add native discovery/filter matrix in `test_getobjects_tabletypes.py` and extend
   the existing restricted-reader metadata subtest in `rbac_integration_test.go`.
4. Build the shared driver and run focused validation and Software RBAC tests.
   Record actual results in [validation](validation.md).

## Verification and review

Inspect actual nested objects and columns, consume streams completely, and keep
fixture changes scoped to generated test objects. Run on the exclusively leased
Redis Software fixture after its setup wrapper verifies 4x QPF and persistence
policy. No benchmark or release claim is made. Maintainer approval is pending.
