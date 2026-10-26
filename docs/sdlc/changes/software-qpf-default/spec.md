# Contract

Date: 2026-10-22. Status: verified locally; PR review pending.

The owned Software RAM and Flex setup paths request active scaling factor 4. Reused databases are corrected if necessary. Before returning readiness, the management API must report an active database, active 4x QPF, six Search workers, mnp routing, and 32 connections. A partially applied or drifting configuration must time out rather than silently continue. TLS preparation reapplies and verifies the same configuration, and includes it in fixture evidence.

Default development and CI use Redis Software; unsupported providers are not added to compatibility testing. ClickBench must use Redis Software at 4x QPF, verify distribution and settings, and reuse a compatible verified backup when testing query performance. Dedicated experiments at other QPF settings require an explicit request and separate labeling.

Regression checks cover upgrading a standard database, reusing an already-correct database without mutation, and refusing mismatched worker counts, routing, connection counts, lifecycle state, or QPF factor. Live fixture validation must be reported separately from these mock management-API checks. No performance gain is implied by configuring a factor.

See [intent](intent.md) and [plan](plan.md).
