# Statement snapshots for managed tables: implementation plan

Date: 2026-11-02. Source baseline: `bb83527f305b75ea731696f55f78eb75121bbbc0`.
Requestor: Albert Wong. Status: runtime implemented; snapshot validation passed.
Issue: [#113](https://github.com/redis/redis-adbc-driver/issues/113).
Related: [intent](intent.md), [specification](spec.md), [validation](validation.md).

## Implemented work

1. `go/snapshot.go`: bounded catalog protocol, atomic acquisition/pins,
   server-time leases, root CAS plus durable outcomes, server-side mutation
   fences, namespace isolation, metadata-manifest copying and delta-segment allocation, index readiness validation
   and resumable reclamation. Final changed/compacted row images receive completed
   initial index scans. Rows and metadata occupy one hash slot. Unchanged table generations are shared; catalog manifests and table data
   have separate reclamation protocols. Cross-slot publication is deferred.
2. `go/database.go`, `go/connection.go`: explicit format option, empty-catalog
   initialization, deployment checks, safe option restrictions, scoped discovery
   and timeout-client replacement retaining the namespace hook.
3. `go/exec.go`, `go/statement.go`, `go/ingest.go`: acquire before planning,
   one publication per supported write/input stream, rejection of unsupported
   statements and independent snapshots for parameter executions/scripts.
4. `go/store.go`, `go/query.go`, `go/aggregate.go`, `go/tags.go`: unique physical
   names, immutable expected row counts, preserved exact HASH values, missing
   cursor-row/partial-result errors and disabled mutable background tag checks.
5. `go/stream.go`: reader pin/deadline ownership, validation before returning a
   batch and cleanup on cancellation, exhaustion, release and handle close.
6. `go/hashscan.go`: reject legacy HASH tooling for snapshot catalogs.
7. `go/snapshot*_test.go`, `go/validation/snapshot_validation.py`: deterministic
   concurrency, fault-injection, lifetime and native C/ADBC value validation.
8. README and `docs/snapshots.md`: supported scope, automatic snapshots, fixed limits,
   required ACL commands, deployment constraints and reproduction commands.

The exploratory legacy integration run exposed a pre-existing seasonal TIMETZ
assertion, reproduced on the untouched baseline. No unrelated time-zone fix is
included in this PR.

## Verification and review

Run Go formatting, unit and existing integration suites, the snapshot suite under
the race detector, shared-library build and native snapshot validation. Use the
separate disposable Redis Software database with verified active 4x QPF. Record
actual pass/skip counts and source-review findings in [validation.md](validation.md).

Review publication/outcome ambiguity, pin/cleanup races, command key rewriting,
private mutation fencing, all writer entry points, context propagation, Arrow
ownership, option switching and external HASH ownership according to REVIEW.md.
No merge, release or human approval is implied by these working-tree changes.

## Delta refinement

The delta refinement is implemented in `go/snapshot_delta.go`, with sorted exact
row-ID directories, newest-version merging, direct lookups, complete replacement
images, tombstones and an eight-segment compaction threshold. The planner and
aggregate/join shortcuts reject unsafe physical-index pushdowns on overlays.
TRUNCATE clears history. Publication/reference tracking and reclamation include
all segment indexes, HASHes and directories. Fault tests cover failed compaction
and missing data, while native checks cover visibility and streamed compaction.

## Deferred delta work

Compaction is synchronous and bounded by existing budgets; incremental/background
compaction, visibility-aware predicate/aggregate pushdowns, finer conflict
detection, configurable limits and ALTER support remain deferred. Metadata
copying, index checks and some initial/cleanup scans still traverse the catalog
or database. Upgrade/migration work is excluded for all earlier draft formats.
Cross-slot publication, Active-Active, failover certification and throughput
claims require separate design and runtime evidence.
