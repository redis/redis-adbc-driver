# Protocol-16 Cluster API, recovery and native checkpoint

Date: 2026-11-05. Baseline production driver source `076c838`; the later
[failed pin-cleanup correction](pin-cleanup-recovery-validation.md) changes
runtime cleanup as described separately. Full [parity](spec.md) remains
incomplete. The [protocol-14 report](cluster14-validation.md) and older
[protocol-6 report](../snapshot-cluster/validation.md) remain historical.

## Deployment and scope

An owned, disposable three-node Redis Software deployment used the pinned
`redislabs/redis:8.2.0-78.18` image, database Redis 8.6.2, three primary shards
and three replicas. The existing orchestrator verified actual Cluster API
enablement, strict TLS, noeviction, shard roles/loading state and effective 4x
QPF (six Search workers, mnp, conns 32) before tests and after each fault.
Stable private addresses and persisted volumes survived all-node restart.
Credentials, license, endpoints, certificates and raw logs remain private.

This exercises Cluster API node routing, rather than the three-primary proxy
used by the ordinary Software CI jobs. Managed catalog rows and metadata still
share the `{meta}` slot. External source HASHes span multiple primary endpoints;
this does not prove distributed managed-catalog write throughput.

## Fixture corrections and retained failures

- Partition cleanup needs the raw administration URI. The orchestrator now
  supplies a seed URI without the Go driver's repeated `addr` parameters.
  The exact original binary reproduces the missing-URI failure and passes with
  this environment correction; no production fix was necessary.
- The old recovery fixture updates only one row. Selective compaction preserves
  the large base and copies one winning delta row, so its expectation of a
  64-row checkpoint was invalid even before a fault. The corrected fixture
  updates 128 rows and verifies 64 registered images plus a durable complete
  copy outcome before interruption. After recovery, source position, references,
  output identity, checkpoint and complete outcome must match exactly. No count
  assertion was weakened. `Rows` is finalized during indexing; copy progress is
  recorded in `CopyBatch` and the exact output directory.
- The RDB gate formerly accepted a snapshot from the same second as the final
  probe writes. Its whole-second INFO timestamp could precede those writes.
  The first restart lost a primary sentinel despite passing that gate. The gate
  now requires a strictly later second than the acknowledged final write.
  An offline regression fails against the original gate and passes against the
  correction. AOF enabled, unfinished/failed saves, old checkpoints and missing
  persistence fields are rejected.
- The first run with the stronger gate recovered all three sentinels but timed
  out in the 30-second compaction completion helper. That failed attempt remains
  part of the evidence; it does not prove a compaction latency bound. Recovery
  now explicitly checks private-writer abort errors before releasing its pin
  and entering compaction, rather than silently ignoring cleanup failure.
  A later unchanged repeat also timed out during metadata resolution. Explicit
  retirement is diagnostic hardening, not a demonstrated fix for this issue.
- A failing diagnostic then records 4,634 publication refusals. A writer pin's
  heartbeat and deferred release both fail during the outage; its later local
  release does not retry server cleanup. The [runtime correction and focused
  review regression](pin-cleanup-recovery-validation.md) preserve token ownership
  while retrying failed cleanup, including one shared concurrent attempt.
- A final promotion followed by the RDB test exposes another harness assumption:
  two physical primaries can share one node endpoint. Counting endpoint strings
  cannot prove that every physical shard was dirtied. The orchestrator now passes
  management-verified primary slot groups, and the checkpoint writer validates
  disjoint complete slot coverage before placing one sentinel in each group.
  The old endpoint test fails before any restart; it is not a persistence failure.

## Verified checks

- Normal Cluster API suite: seven passed, two explicit orchestrator-only skips
  (recovery and RDB checkpoint writes). Covers managed SQL/temporary objects,
  direct-node fences, no-replay bounded row-batch outcomes, shared metadata and
  rekey, late-index cleanup, multi-primary imports, auto detection and partitions.
- Actual catalog-primary promotion: recovery passed in 7.94 seconds. The pinned
  stream returned all exact values. SQL recovered without explicit read retries.
- AOF `appendfsync-always`: abrupt kill/start of all three server containers
  passed in 35.50 seconds. The independent client remained alive. This uses
  management CCS checkpoints separately from SQL database AOF, with no SQL RDB
  checkpoint used to establish the AOF result.
- RDB with AOF disabled: a separately verified frequent snapshot policy dirties
  all three routed primaries after the recovery client pauses. Fresh successful
  completed RDBs are verified on every shard before all-node kill/start. The
  explicit-retirement recovery run passed in 46.95 seconds, including all three
  exact sentinel values. The stream returned an explicit interruption error
  without replay; established and new SQL clients recovered.
  An argument-free command-category diagnostic run also passed in 46.78 seconds
  and finished compaction with short command round trips. This successful sample
  does not explain the intermittent continuation timeouts above.
- Both successful persistence paths retain a private 600-row generation and
  its exact last 88-row batch outcome, keep unpublished metadata invisible,
  recover the durable 64-image compaction checkpoint and complete it under a new
  owner. Final committed exact values and a new write are checked.
- Native C/ADBC through actual Cluster API: all 19 snapshot tests passed,
  zero skips, 7.350 seconds. Linux ARM64, Go 1.27.3, Python 3.14.10,
  PyArrow 26.0.1, manager 1.13.0, redis-py 8.1.0. The versioned shared library was
  built with `make build`; SHA-256:
  `74585f689cab4583c3df149660c8ec7bd505bfa523a4db0e5cfd787161fca16c`.
  The suite now accepts `REDIS_SNAPSHOT_CLUSTER_URI`, routes raw source HASH
  access with redis-py Cluster, broadcasts FLUSHDB to primaries, explicitly
  dispatches distributed Search inspection and requires multiple routed primary
  endpoints. Coverage includes Arrow ingestion/concurrency, exact schema/values,
  stream lifetime, managed external copies/refresh, and live/retained partitions.
- Single-endpoint native harness regression: all 19 passed, zero skips,
  13.354 seconds on the separate owned Software 8.8/QPF4/TLS fixture, using the
  previously validated `b70f24a` library (production-equivalent to `076c838`).
- Offline checkpoint gate: two tests pass using
  `python -m unittest discover -s scripts -p test_snapshot_software_cluster.py -v`
  in the Redis-enabled helper environment. The old gate has exactly one expected
  failure: accepting the pre-write checkpoint. Formatting and diff checks pass.

## Final cleanup implementation checks

The shared-attempt cleanup implementation at `1fb8208` passes the normal Cluster
suite (seven cases, two orchestrator skips) and actual promotion (7.22 seconds).
The physical-slot checkpoint helper then passes after promotion, including all
three exact primary sentinels. That RDB recovery attempt fails in the SQL query
after compaction publication with `snapshot metadata bucket is incomplete`
(53.01 seconds). This is a distinct unresolved metadata validation failure,
not counted as a successful final-code recovery. A subsequent diagnostic run
passes in 55.28 seconds; that pass does not explain or erase the failure.

The final runtime native C/ADBC Cluster suite passes all 19 cases, zero skips,
8.957 seconds. Same native environment as above; rebuilt library SHA-256:
`f4ff5dd062dd6b51e3af1c69965996af9a86b7fc4711a42c2594945c6612d233`.
The diagnostic executable only adds sanitized bucket-count inspection on failure;
it does not change production behavior or assertions. Further recovery evidence
is required before closing the metadata failure.

Further final-code checks:

- Initialized AOF files are verified on all six shards before abrupt restart,
  including enabled AOF, completed successful rewrite and no scheduled rewrite.
  Effective QPF4/noeviction/appendfsync-always are reverified. Recovery passes in
  38.86 seconds with the cleanup correction and bounded failure diagnostics.
- Three RDB diagnostic repetitions pass in 46.36, 47.28 and 47.39 seconds.
  Three subsequent complete promotion-then-RDB cycles pass: promotion 7.44,
  7.38 and 6.81 seconds; restart 47.81, 51.23 and 53.44 seconds. They do not
  reproduce the earlier metadata error, which remains unresolved. The error now
  includes numeric proof/reference/directory counts without identities or values;
  validation still fails closed and is not relaxed or retried.
- The anchored snapshot race selection passes 471 events / 61 explicit skips,
  root 410.908 seconds, all six executable packages, no races. Its first invocation
  omitted the raw administrator URI and fails six partition cleanup fixtures;
  that 397.604-second failed run is retained, not counted as a pass. The corrected
  invocation supplies `REDIS_ADMIN_URI`. The selection leaves ordinary-URI gates
  and names outside `^TestSnapshot` for a separately configured supplement.
- All final cleanup unit packages pass under the race detector, root 54.395
  seconds. This predates the numeric-only error detail and bounded failure
  inspection additions; no broader head claim follows from it.
- Actual-principal SQL SELECT/RBAC race coverage passes 21 events, no skips,
  root 24.851 seconds. A subsequent snapshot supplement is initially refused by
  the retained-control policy gate because the verified declaration was omitted;
  this is a failed configuration attempt, not passing retained coverage.

At integrated candidate `9036c90` (cleanup `b9813e3`, based on the published
`1664000` audit fixes), the remaining local checks finish:

- The correctly configured snapshot supplement passes 75 race events, zero
  skips, root 27.591 seconds. It covers the 20 families omitted by the anchored
  selection or their ordinary-URI gates, including retained publication lifetime,
  readiness, ordering/NULLs, schema rebinding, cancellation and statistics.
- Native macOS snapshot validation passes all 19 cases, zero skips, 11.921
  seconds (helper Python 3.13.16, Arrow 26.0.0, manager 1.13.0).
- Full official native validation passes 565 tests plus three subtests, eight
  explicit skips, 85.28 seconds. Python 3.14.10, Arrow 26.0.1, manager 1.13.0,
  pytest 9.2.0; framework revision
  `47a5ddf3f2a617ac88da3891848e000c74d55b0e`. The versioned macOS ARM64 library
  SHA-256 is `3a1f3109875183c7f0ac8e06538e44eae43bc04c268d3efb3311399899135170`.
  It was built before the commit/rebase from the equivalent production tree plus
  the numeric diagnostics; its embedded VCS is not a clean `9036c90` build.
- Complete ordinary Go suite: `go test -count=1 -p 1 -timeout=30m -v ./...`,
  2,157 pass events / 379 expected skips, all six executable packages, root
  1,240.876 seconds. The verified retained-control declaration and raw admin URI
  are present. Actual-principal SQL/RBAC is enabled. Snapshot-only fixtures remain
  separate; ordinary skips do not establish those capabilities.

These complete the named local candidate checks, not the separate final-head
hosted/platform gate. The intermittent RDB metadata failure remains open.

## Limits and remaining work

The Cluster Go binary is Linux AMD64, Go 1.27.3, CGO disabled; this is separate
from the native ARM64 C ABI run. Neither is a Cluster race/platform matrix.
Correctness-run durations are not availability or performance benchmarks.
The earlier RDB compaction timeout is retained and its surviving writer-intent
cause is covered by the separate cleanup correction; later passes do not erase
the failed attempts. Arbitrary faults, corruption and cross-region behavior remain unverified.
Public process/input resume, sustained compaction performance and the broader
comparative performance/platform gates remain open. Active-Active remains
excluded, noeviction required, autocommit only, and no storage migration is added.


## Minimal-scope stabilization follow-up

The user narrowed PR #284 to the [minimal atomic statement snapshot contract](core-spec.md).
No deferred import-capture/resume production code is merged. Production `272bbd4d`
has terminal SUCCESS in Test 42172244144, Release 42172244104 and DuckDB
42172244094; subsequent scope commits change documentation only.

A freshly compiled exact-core Linux/AMD64 recovery binary passes a further RDB
restart in **47.38 seconds**, with its per-primary checkpoint subcase passing in
0.22 seconds. Every physical-primary sentinel, committed exact values, private
rows and batch outcome, metadata isolation, recovered compaction and fresh/old
connection behavior pass. The interrupted reader returns an explicit error and
is not replayed. QPF4 and fresh RDB policy/checkpoints are verified. A private
preflight initially timed out by checking effective QPF in an unrequested-field
response; requesting the explicit QPF fields corrects that preflight, without
changing driver code. This additional pass does not reproduce or explain the
retained incomplete-bucket failure; it remains open.


## Recovery phase diagnostics (test-only)

The recovery test now audits mapped committed buckets at four serialized phase
boundaries: before interruption, after deployment/SQL recovery, before resumed
compaction, and immediately after compaction publication. Each audit has a
three-second deadline, visits at most 256 mapped buckets and checks numeric
reference/count proofs against HASH and directory cardinality. Diagnostics emit
phase and counts only, without logical keys, owner IDs or source values. The
fixture disables background compaction and serializes its writers; this helper
is not a general concurrent catalog verifier or production validation change.
Existing exact SQL, private-row/outcome, pin and sentinel assertions remain.

On the same owned three-physical-primary/three-replica Software Cluster with
strict TLS and verified effective QPF4, the original promotion-then-RDB sequence
passes: promotion **7.26 seconds**, fresh per-shard RDB restart **51.37 seconds**,
checkpoint helper **0.21 seconds**. All four audits report five valid mapped
buckets in each fault run, including after recovered compaction publication.
The promotion stream completes all exact values; the RDB stream fails explicitly
without replay, then established/fresh queries and every RDB sentinel pass.

Production code remains identical to `272bbd4d`; only recovery test diagnostics
change. The instrumented binary precedes a comment-placement clarification with
identical test execution. These additional passes do not reproduce or explain
the retained incomplete-bucket attempt. The failure remains a core completion
blocker, and no Redis defect, fix, resume support or performance gain is claimed.
