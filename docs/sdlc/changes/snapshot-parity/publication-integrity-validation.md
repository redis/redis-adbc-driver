# Publication metadata integrity admission

Status: implemented, focused validation complete; broader combined checks remain.
This is core snapshot correctness work, not a deferred feature or a demonstrated
explanation of the intermittent RDB failure. See [minimal scope](core-spec.md).

## Reproduced gap and correction

Previously, control-level seal/publication could accept a private root containing
a mapped bucket with a missing HASH, missing field directory, wrong expected
count, or missing bucket-reference proof. The focused administrative-corruption
fixture reproduces all eight operation/corruption combinations against unchanged
production source: eight failing subcases, parent 1.68 seconds. This is a protocol
admission invariant test, not a claim that ordinary SQL itself corrupts metadata.

Seal and publication now check each mapped bucket's identity, positive reference
proof, expected count, HASH/directory cardinalities and absence of an unfinished
construction marker before admission. Merge publication checks its replacement
map before transferring references or replacing the original map. The seal
wrapper supplies its actual metadata map, just as publication does. The first
candidate omitted that key-list change: four publication cases passed but four
seal cases still failed; that intermediate run is not counted as green.

The guard checks map count before HGETALL, limits traversal to at most 256 mapped
buckets, fetches only identities/counts and emits sanitized numeric diagnostics.
It does not copy values/catalog payloads, rebuild indexes, change leases/GC,
replay SQL/input or add storage fields/protocol changes. Empty registered buckets
with count zero remain valid even when Redis has removed their empty containers.
This checks bounded mapped-bucket proofs, not all logical membership/value edges
or arbitrary corruption. Additional count reads add foreground work; no speedup
or performance-parity claim accompanies this correctness guard.

## Actual validation

- Focused actual Cluster guard selection: **13 pass events, no skips**. Covers
  eight corruption refusals, valid empty/nonempty seal/publication, and refusal
  of an incomplete replacement map while preserving root/original ownership.
- Complete normal `^TestSnapshotCluster` selection: **20 pass events, two explicit
  orchestrator-only skips**, no failures. Three physical primaries/three replicas,
  strict TLS, Software `8.2.0-78.18` / database Redis 8.6.2, verified effective QPF4,
  noeviction and fresh RDB policy. Managed data still uses one slot.
- Fresh per-primary RDB restart with the new guard: **52.36 seconds**, passes
  exact SQL, sentinel, private batch/outcome, recovered compaction and all four
  metadata audits. This is not a performance/availability benchmark.
- Neighboring metadata, independent-publication/rebase, compaction-publication
  and cached-control races: **54 pass events / two capability skips**, root
  **14.446 seconds**, all six executable packages pass without a race report.
  Separate owned Software 8.8 fixture, strict TLS, verified effective QPF4 and
  noeviction. Commands use `-race -count=1 -p 1 -timeout 15m`.
- All unit packages pass, root **27.152 seconds**, six executable packages.
  Live endpoints are unset; integration/profile skips are not deployment proof.

Production sources in these guard runs are the same; subsequent documentation
and comment changes do not change execution. Full combined snapshot/native and
hosted/platform checks are not yet passing evidence for this guard revision.

## Review and original failure boundary

Author review checked zero-count buckets, count-before-enumeration bounds,
pre-mutation refusal, merge ownership transfer, token/lease checks and unknown
outcome handling. No role grants or key families are broadened; the native
commands already belong to the trusted-writer profile. This is not human
maintainer approval.

Two further pre-guard promotion/RDB cycles passed (6.74/46.34 and 6.62/43.41
seconds), without reproducing the original incomplete-bucket error. Source
comparison confirms that its relevant production code otherwise differs only by
numeric error details. Artifact timestamps alone do not establish interfering
workloads. Neither those passes nor this new guard explain the original failure.
It remains open; do not describe the guard as its proven root-cause fix.
