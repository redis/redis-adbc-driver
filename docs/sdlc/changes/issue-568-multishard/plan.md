# Multi-shard managed ingestion: plan and evidence

Date: 2026-11-20. Base: `bc6780e9`. User continuation recorded in [intent](intent.md).
Status: revised contract and executable model delivered; real topology proof pending.
[Specification](spec.md) · [Design](design.md)

## Completed in this revision

- Re-read #568 and the current single-slot layout, central publication/reference
  helpers, reader-retention and fenced row-reclamation code.
- Corrected the earlier claim that runtime progress necessarily waits for a
  supported coordinated live-view API. Managed immutable data can use one central
  manifest/reference authority; a custom live distributed snapshot is unnecessary.
- Specified monotonic central commit/abort/retirement, sealed participant retention,
  permanent local deletion fences and delayed exact-identity cleanup grants.
- Rechecked official Redis Software replication/persistence and WAIT/WAITAOF
  documentation on 2026-11-20. Those facilities are candidates to investigate;
  their documentation does not itself prove this protocol's rollback safety.
- Checked documentation links and diff whitespace. This revision changes only
  the four #568 records; no production files, servers or cloud resources change.

## Executable model delivered

Built a bounded reference model as a separately reviewable change. It models two
participants, a central manifest decision, a reader pin, preparation references,
local sealing and delayed reclamation grants. Enumerate explicitly bounded
interleavings of prepare/seal reports, publish/abort, writer expiry, pin/release,
GC admission, late initialization/writes, delayed/duplicated/lost delivery and
crash/recovery. Describe the actual bounds and any fairness/liveness limits.

Required checks: no partial publication; retained/pinned generations cannot be
reclaimed; terminal central retirement forbids later publication/reference;
local deletion fencing prevents resurrection; lost replies do not replay source;
and terminal outcomes remain discoverable. Include a negative trace where central
retirement rolls back after remote deletion, demonstrating the model assumption
rather than claiming that arbitrary failover is solved. The finite model and 14 tests pass; see [evidence](evidence.md) for exact bounds,
review findings and counterexamples. This is not a Redis/Lua/runtime or performance
proof.

## Subsequent implementation and verification

1. Add explicitly fenced experimental placement/participant records and bounded
   local batch/seal operations under a new format boundary. Keep production
   protocol 17 unchanged. Exercise narrow prepare/seal/private-output behavior
   before routing ordinary SQL/Arrow publication through it.
2. Establish exact Software/Flex product/version, persistence, eligible failover
   participants and connection/routing semantics for durability barriers. Test
   central-decision rollback, participant loss and stale cleanup grants; reject
   unavailable/unsupported cases explicitly. Address local terminal-evidence growth.
3. Integrate central publication/pins and remote cleanup using the proven state
   transitions. Native tests use real multi-primary Software/Flex with verified
   active QPF4; inspect actual physical distribution. No cross-slot script shortcut.
4. Verify concurrent DML/DDL, exact reads, overlay/tombstone/provenance handling,
   global merges, cancellation/unknown outcomes, GC/backpressure, promotion,
   restart, topology change and complete compatible manifest-backed restore.
5. Measure 1/2/4 writers and physical primaries with controlled total resources,
   CPU/Search/client/I/O attribution, backlog and validated acknowledged completion.
   Independently fetch/validate all 43 canonical ClickBench queries. No linear or
   all-core scaling promise; no full-scale reload without an authorized load run.

The open issue's real-topology and full correctness/performance acceptance remains
incomplete. No native tests or operational ingestion were run for this revision.
