# Contract

Date: 2026-10-21. Status: implemented; human approval pending.
See [intent](intent.md), [plan](plan.md), and issue #395.

Before mutating Search configuration, read and save each primary's original cap.
For each cap (137, 11, 3, 137), set all primaries before consuming the existing
partition descriptor and asserting exactly 500 original IDs. Restore each saved
value on success or failure with a five-second cleanup context, reporting errors.
Single-server cleanup uses the same path. Never mutate non-fixture deployments.

OSS CI enables REDIS_PARTITION_CAP_TEST and unsets the fixed driver cap so actual
runtime discovery is exercised. Software retains its explicit cap setting. No
assertion or capability is skipped to avoid the isolation failure.
