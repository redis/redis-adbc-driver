# Validation

Date: November 13, 2026. Validation/evaluation complete; parent review pending.
The demonstrated result is fewer control calls. Local native medians were lower,
but diagnostic elapsed differences were small; no broad speedup is established.
Measured source: `86a27f2ba772cde03765cefa1d963edbc24c3490`, production changes
at `3ad8188f`. Later additions are tests/evidence only.

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

The two initial skips were the new actual Software EVAL-only principal scenario
(URI not provided in this initial loan) and the existing opt-in combined-directory
timing diagnostic. The actual ACL gap was closed in the separate block below;
the skipped timing diagnostic is not performance evidence.

## Actual Software ACL check

A later parent-granted correctness gap used the existing SoftwarePrincipals
creation/cleanup lifecycle for one temporary operator with EVALSHA denied.
Positive PING/EVAL and native EVALSHA denial were verified over strict data
TLS/RESP3, and active/effective 4x QPF was rechecked after mapping changes.
The new combined acquisition/valid/release lifecycle and existing generic
EVAL-only control lifecycle both passed under `-race`: two pass events, zero
skips/failures/race reports. The new path retained exactly one top-level refusal,
three successful EVAL control calls and no no-retry-marker violation.

The complete controller, including principal setup and teardown, took 8.7
seconds; this is a correctness duration, not a throughput sample. Its created
user/role/ACL/mapping resources were removed, effective QPF reverified, and the
owned loaned database returned empty/index-free. No certificate, topology or
shared fixture ownership changes were made.

After all subprocesses and clients exited, the loaned database was reset only
within the owner's authorization. DBSIZE zero and an empty FT._LIST were
asserted; the owner verifier rechecked effective QPF/TLS/RESP3 and emptiness.
The shared container/network/CA remain owned by the parent; this subagent did
not tear them down. Endpoint identifiers, credentials and raw logs stay private.

## Late-original ordering check

A separate post-measurement `-race -count=1 -p 1` check passed the new late
original test: one pass, zero skips/failures/race reports. The hook retains one
original no-retry request without sending it, lets the driver return unknown and
perform its single cleanup, then a raw test client delivers that original frame
for its first server execution. This is a controlled ordering simulation, not a
real network fault or driver mutation replay. No row copy, checkpoint advance
or root publication occurs. Its resulting read pin has a server-time lease no
longer than two minutes. Advancing only that owned test pin's expiry allows the
next ordinary step to prune it; full 257-row recovery validates exact values.

This makes the cleanup limit explicit: one best-effort release does not promise
immediate removal when an unknown original can execute later. Production code
was not changed after measured source `86a27f2b`. The shared fixture was returned
empty/index-free and effective QPF/TLS/RESP3 reverified after the check.

## Native comparison

The archived clean `5f759300c12f5191a36d2f2b5a29555ef14ef486` baseline library/CLI
hashes and Go 1.27.3 build identity were verified. Candidate `86a27f2b` was built
through the Makefile as `v0.1.0-dev.compaction-control`; both binaries recorded
`vcs.modified=false`. Baseline GetInfo version was
`v0.1.0-dev.merged-perf-2`. All build/source/input/harness hashes and every sample
are retained in [results.json](results.json).

The unchanged table-mode workers appended 10K rows × 105 mixed typed columns
plus the ordinal, in 1K public Arrow-call units. One writer makes ten calls;
four writers make twelve total admitted calls, which serialize. Each cell has
one warmup and three measured trials. All 16 native trials passed independent
complete physical-type/value/count/multiplicity validation, with no conflicts,
caller retries, failed trials or discarded samples.

| Writers | Baseline median s | Candidate median s | Observed elapsed reduction |
|---|---:|---:|---:|
| 1 | 8.368 | 7.969 | 4.8% |
| 4 | 18.899 | 18.106 | 4.2% |

The baseline block preceded candidate on the same fixture, with alternating
case order within repetitions. It was not a randomized crossover. Three samples
per cell and overlapping baseline/candidate ranges limit inference: the one-writer
candidate range was 7.821–8.371s, versus 8.361–8.375s baseline; four-writer ranges
were 17.721–18.485s versus 18.327–19.219s. Do not promise these percentages on
other workloads or call this a demonstrated parallel managed-write speedup.

Table/index setup is outside the existing-write timer; fresh worker startup,
source loading/conversion, connections, append calls and shutdown are included.
Independent final fetch validation is outside timing. Maximum individual worker
RSS is retained per trial, not a total process-tree/Go-heap memory measurement.

## Separate unchanged diagnostics

The existing publication helper passed all twelve samples: three at each 1K/10K
call ceiling for each variant. Its synthetic nullable BIGINT input differs from
the native mixed source. Median actual append wall was 8.056→8.013s for 1K calls
and 1.805→1.790s for one 10K call. Ninth-call ranges overlap. Foreground and
background hook sums overlap and do not measure isolated server CPU or admission
wait. The larger call changes publication units and remains a usage control.

The unchanged dense compaction helper passed all twelve jobs: paced/unpaced,
three observations each per variant, validating all 20K exact BIGINT/payload
rows and the unrelated table checks. Every job used 314 copy steps. Per copy
phase, command executions changed 30,995→30,681 and client round-trip calls
5,032→4,718: exactly 314 fewer calls (6.2%) and commands (1.0%). The 314 separate
catalog acquisitions were removed, replaced alongside the original 314 job
acquisitions by 314 combined acquisitions; all 314 pin-release calls remained.
Existing proof/row-copy calls and bounds remain in place.

| Dense mode | Baseline median wall s | Candidate median wall s |
|---|---:|---:|
| Paced | 8.362 | 8.335 |
| Unpaced | 8.209 | 8.149 |

These 0.3%/0.7% elapsed differences are essentially neutral local observations,
with overlapping ranges. The supported claim is the structural call reduction,
not a large compaction-throughput gain. Setup, pacing, observer reads and complete
final validation have their existing separate timing boundaries; do not add
phase-hook durations together as server CPU or compound percentages with earlier
compaction experiments. Native SQL throughput was not measured in this control.

## Environment, corrections and audit

Owned Software 8.2.6 / Redis 8.8.0 / Search 80808, active/effective 4x QPF,
six workers, mnp/32 connections, one 4-GiB noeviction RAM shard; actual four-CPU /
eight-GiB container limits verified before timing. Data TLS certificate/hostname
verification and RESP3 were required. API trust remained the existing fixture
bootstrap self-signed mode. Go 1.27.3, Apple M5 Max / 18 logical CPUs / 36 GiB,
macOS 26.7.3; native GOMAXPROCS unset, separate Go diagnostics GOMAXPROCS=4.
Python/library versions are retained in native metadata. The parent serialized
all task builds/tests/measurements, while light source/docs work continued on a
shared workstation; this does not prove the whole host was otherwise idle.

One private postprocessing attempt failed because Go test JSON splits long Logf
payloads across Output events. Reassembling those chunks per test recovered the
already successful baseline publication samples. A continuation guard also
needed to allow already-complete work directories. All original controller
stages/raw events are retained privately with hashes. No native or baseline
publication subprocess/sample was rerun, and no measurement failure was hidden.
Remaining declared diagnostics then completed without source tuning.

The audit recomputes every native cell, diagnostic identity and distribution,
checks source/version/input/validation/QPF, and verifies exact copy-step/control
counts. Reproduce the focused native control with
`benchmarks/table-modes/compaction_control_compare.py`, the same pinned libraries
and private fixture/path environment; run each variant in a fresh private work
directory and retain warmups. Audit this evidence with
`python3 benchmarks/table-modes/compaction_control_audit.py`.

The complete audit, artifact privacy/local-link checks and `git diff --check`
passed. Both production files are byte-identical to measured `86a27f2b` after
the test-only late-ordering addition. Parent source review and any GitHub CI
remain separate from these local observations; no human approval is inferred.

Shared fixture ownership remains with the parent. Every loan ended with clients
closed, DBSIZE zero and FT._LIST empty; no shared container/network/CA was removed
by this subagent. Raw events, operational identifiers and credentials stay
private. No ClickBench, Flex or alternate provider was run.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
