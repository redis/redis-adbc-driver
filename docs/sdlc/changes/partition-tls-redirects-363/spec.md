# Specification

Date: 2026-10-21. Source: `70a0d43`. Status: implemented; review pending. See [intent](intent.md) and [plan](plan.md).

Only the declared owned loopback TLS Cluster fixture may migrate a slot. Confirm
all three primary identities again, choose a slot containing an existing fixture
row, and refuse any unrelated key or unbounded key list before administrative
mutation. Use the fixture administrator to move owned keys; workers retain read-only
ACLs. Keep RESP3 and certificate/hostname verification enabled.

Two fresh worker processes open persistent native connections and read the same
pre-planned direct-HASH descriptor before migration. Observe an actual ASK response
from the source while the slot is migrating. Denying ASKING must explicitly fail;
after granting ASKING the existing worker must return the exact original Arrow
schema and values. Finalize ownership and observe an actual MOVED response. Both
connections must read the original descriptor again; one must not have read since
its pre-migration warmup. Recheck every broad Search result and each physical row
owner after migration. No updates/inserts/deletes run during this experiment.

Bound child responses to 30 seconds and close/kill child processes on every exit.
Do not log credentials, URIs, descriptors or raw child errors. Finalization must
refuse remaining source keys. Failures stay visible; normal caller cleanup removes
the entire owned container/private fixture even if migration cannot complete.

This is one legacy manual slot migration with fixed data, not general resharding,
replica promotion, transparent mid-stream retry or stable replay support.
