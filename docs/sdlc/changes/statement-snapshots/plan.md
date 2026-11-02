# Statement snapshots for managed tables: implementation plan

Date: 2026-11-02. Source baseline: `bb83527f305b75ea731696f55f78eb75121bbbc0`.
Requestor: Albert Wong. Status: runtime implemented; snapshot validation passed.
Issue: [#113](https://github.com/redis/redis-adbc-driver/issues/113).
Related: [intent](intent.md), [specification](spec.md), [validation](validation.md).

## Implemented work

1. `go/snapshot.go`: bounded catalog protocol, atomic acquisition/pins,
   server-time leases, root CAS plus durable outcomes, server-side mutation
   fences, namespace isolation, full-catalog copying, index readiness validation
   and resumable reclamation. Copied and final row images receive completed
   initial index scans. Rows and metadata occupy one hash slot. Shared
   per-table generations and cross-slot publication were deferred.
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

## Deferred work

The first version copies the entire catalog for every write and retains old
catalogs while readers use them. It is intentionally bounded. Later work may
introduce per-table sharing, delta generations, finer conflict detection,
configurable limits and ALTER support. Upgrade/migration work is excluded by
user direction. Cross-slot publication,
Active-Active, failover certification and throughput claims require separate
design and runtime evidence.
