# Owned HMGET result experiment

November 13, 2026. Albert Wong requested parallel performance improvements and
updates to existing upstream issues. Extend the isolated go-redis investigation
with a typed consumer that owns fresh result slots, rather than relying on the
prior reusable-slot prototype's changed result lifetime.

No production driver or public go-redis command API is introduced. See
[contract](spec.md), [plan](plan.md) and [validation](validation.md).
