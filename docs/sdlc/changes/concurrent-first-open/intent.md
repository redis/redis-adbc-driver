# Concurrent first-open initialization

Date: November 17, 2026. Base: v0.0.28 (`b63a010d`). Status: implemented and locally validated.
Requester: user investigating the dbt adapter dependency refresh failure.
Maintainer approval is not recorded here; the pull request carries review status.

Parallel dbt workers opening a fresh database can read an absent snapshot
catalog, then reject another worker's successful initialization as incompatible
storage. Make this supported startup sequence reliable while preserving the
refusal to migrate legacy data or accept unsupported formats.

Scope: driver startup validation and regression coverage. No SQL/input replay,
storage migration, format change, or new adapter retry policy.

See [specification](spec.md) and [plan](plan.md).
