# Publication metadata integrity admission

Status: implemented; focused and complete local snapshot/native checks pass.
The exact production-source hosted Test, Release and DuckDB workflows pass.
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
and comment changes do not change execution. The complete local follow-up below
adds snapshot/native and actual-principal evidence. Hosted/platform checks remain
a separate exact-source requirement.

## Complete local follow-up at `0f707f34`

- Full `-race -count=1 -p 1 -timeout 30m -v -run Snapshot ./...`: **536 pass
  events / 40 explicit skips**, root **391.138 seconds**, all six executable
  packages pass, no data-race report. Skips retain absent actual Cluster, special
  permission/reader-model and profile gates; the actual Cluster cases above are
  separate positive evidence, not inferred from these skips.
- Native C/ADBC snapshot suite: **19 passed, no skips, 11.505 seconds**. Host
  Python 3.13.16, PyArrow 26.0.0, driver manager 1.13.0.
- Full official native validation: **565 tests plus three subtests passed / eight
  capability skips**, **84.87 seconds**. Python 3.14.10, pytest 9.2.0, PyArrow
  26.0.1 and manager 1.13.0; official validation commit
  `47a5ddf3f2a617ac88da3891848e000c74d55b0e`.
- Actual Software-principal SQL SELECT/RBAC races: **21 pass events, zero skips**,
  root **26.206 seconds**, including captured-payload privacy, independent readers,
  grant lifetime and DROP/recreate isolation. No command/key grant is broadened.

The versioned host library is built with `make -C go build`, macOS ARM64 / Go
1.27.3. Embedded VCS reports clean `0f707f340d2b410085a6dd185c224466da2b6798`;
SHA-256 `9f81b90751554098b8d7dc733291774656bc3cea70dc8e8b599d2be0db2f8b9a`.
Both native suites use that exact artifact. The normal owned Software 8.8 fixture
reverifies strict TLS, effective QPF4 and noeviction before each suite; database
suites are serialized. No matched performance benchmark is run.

At exact production source `0f707f34`, all three workflows are terminal SUCCESS:
[Test](https://github.com/redis/redis-adbc-driver/actions/runs/42178643433),
[Release](https://github.com/redis/redis-adbc-driver/actions/runs/42178643525),
[DuckDB](https://github.com/redis/redis-adbc-driver/actions/runs/42178643441).
Both Software architectures pass ordinary Go, snapshot regressions, the actual
SQL SELECT grant step, native validation and independent proxy-TLS/RBAC checks.
The conditional failure-only “Require SQL SELECT grant validation” steps skip
because their actual authorization steps succeeded. ARM-only optional Ray/Airflow
and partition-measurement skips remain visible; they are not capability passes.
Release passes macOS, both Linux builds, ten distro jobs and all three installed
wheel-client jobs. Intentional release-upload/in-workflow DuckDB skips do not
replace the separate successful DuckDB workflow.

The local combined evidence was held until that matrix completed to avoid
canceling it with a documentation push. The subsequent
[test-only reference/checkpoint follow-up](recovery-reference-audit-validation.md)
has separate focused evidence and leaves production code unchanged; these
hosted results retain their exact `0f707f34` source, not a later-head label.

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
