# Validation

Date: November 13, 2026. Candidate acceptance and native comparison pending.
Parent serialized the focused correctness slot; no native timing has run yet.

## Focused correctness

Endpoint-free `go test -count=1 -race -p 1` selection for control-script
compatibility and compaction source selection passed 30 test/subtest events,
with four explicit unset-endpoint integration skips and no failure/race report.

A private Go overlay replaced only the new acquisition helper with the preceding
separate `compact_acquire` plus `acquireCompactionCatalog` sequence, retaining the
new tests and surrounding compiled code. It compiled, then failed exactly the
production-step regression that refuses a separate catalog acquire. This is a
baseline-equivalent round-trip regression, not a native throughput comparison;
the actual overlay and complete expected-failure output are retained privately.

On the parent's explicitly loaned owned Redis Software fixture, the verifier
confirmed active/effective 4x QPF, six Search workers, mnp/32 connections, one
4-GiB noeviction RAM shard and certificate-verified data TLS/RESP3 before live
execution. Its owner provisioned through `make -C go redis-up`; this subagent
did not substitute or independently provision another server.

Focused live `go test -count=1 -race -p 1` passed 69 test/subtest events with two
explicit skips, zero failures and no race reports. New scenarios cover the
combined acquire path, bounded copy checkpoint, one pin release before rows,
unknown successful combined reply without replay, failed pin cleanup with a
bounded lease and subsequent expired-pin pruning, foreign-owner and 128-reader
refusal without job-lease mutation, and corrupt metadata proof without copied
rows or leaked pins. Existing atomic copy/outcome, stale-owner/tail, source
directory/index, DROP/TRUNCATE, publication/write-intent, queue and Close
recovery checks passed in the same selection.

The two skips were the new actual Software EVAL-only principal scenario (URI
not provided in this initial loan) and the existing opt-in combined-directory
timing diagnostic. Neither is claimed as tested. An actual ACL correctness gap
and a matched native/diagnostic comparison remain pending before adoption.

After all subprocesses and clients exited, the loaned database was reset only
within the owner's authorization. DBSIZE zero and an empty FT._LIST were
asserted; the owner verifier rechecked effective QPF/TLS/RESP3 and emptiness.
The shared container/network/CA remain owned by the parent; this subagent did
not tear them down. Endpoint identifiers, credentials and raw logs stay private.

## Pending evaluation

Compare exact pinned baseline and committed candidate native libraries, with
explicit GetInfo versions, complete independent result validation and all
attempts/warmups retained. Client-hook totals are overlapping observations,
not server CPU or measured admission wait. No performance gain is claimed yet.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
