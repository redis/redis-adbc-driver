# Contract

[Intent](intent.md), [plan](plan.md), [validation](validation.md). November14,2026.

Join the fixture connection's background worker before making staged output
reclaimable. Preserve the exact600-row pre-deletion assertion, explicit first
collector barrier, independent second connection/recovery, stale collector fencing,
current-root/source values and logical-name reuse checks. No production behavior,
retry or skip changes. Release PR path selection must include these rekey tests
so native macOS validates future scheduling changes.
