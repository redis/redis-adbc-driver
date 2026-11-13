# Managed Arrow publication measurements

Date: 2026-11-13. Requestor: repository user; recorded by Codex.
Status: evaluated and rejected; production defaults unchanged. Baseline:
`ba855f7995b2685182ed1331bcf6703d73089c4a`; unapplied candidate:
`f81b7a048741e181895f0f35ee557953cf849c3d`.

## Baseline usage control

Owned Redis Software, active and effective 4x QPF, six Search workers,
TLS/RESP3, noeviction, 4 CPU / 8 GiB Docker limit, 4 GiB database.
Native Go 1.27.3 shared driver, Python 3.15.2, PyArrow 26.0.1,
ADBC manager 1.13.0. Each trial appends 10,000 rows / 105 mixed typed columns
from the same independently prepared Arrow input. Source SHA-256:
`a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2`.
Each of four configurations has one warmup and three measured trials; all
16 trials passed full independent fetch/type/value/multiplicity validation,
with zero caller retries, serialization conflicts or discarded samples.
The 1,000-row ceiling block preceded the 10,000-row block; writer-count order
alternated within repetitions. Medians:

| Maximum rows per public call | Writers | Seconds |
|---|---:|---:|
| 1,000 | 1 | 7.620 |
| 1,000 | 4 | 16.595 |
| 10,000 | 1 | 1.614 |
| 10,000 | 4 | 2.422 |

This is a call-size control, **not a driver optimization**. A larger call
changes the publication/atomicity unit; internal row and conversion bounds
remain unchanged. Four writers each append 2,500 rows, so the larger ceiling
produces four calls, compared with twelve at the smaller ceiling.

## Separate diagnostic attribution

`TestManagedPublicationNextProfile`, opt-in via
`REDIS_MANAGED_PUBLICATION_NEXT_PROFILE=true`, passed six samples: three at
each call size. These use synthetic 105-column nullable BIGINT input, including
exact values above 2^53, rather than the native mixed input. Timing excludes
full result validation. Foreground and maintenance hooks were installed before
work started, record categories rather than commands/values, and are settled
outside the timing interval. Maintenance operations can overlap foreground
operations and can finish after the append window. Their accumulated durations
are **not** exclusive wall time, server CPU or measured admission wait.

The single 10,000-row call took about 1.71 seconds: forty complete-image row
batches accounted for about 0.69 seconds and forty final index-notification
batches about 0.69 seconds. Ten 1,000-row calls took 7.95–8.00 seconds; the
ninth call took about 4.6 seconds, consistent with reaching the eight-segment
admission bound. The maintenance hook observed 68–70 bounded copy batches.
This evidence motivates eliminating the redundant notification pass only when
rows are provably complete first inserts into an initially empty private index.
It does not justify weakening admission, source-index validation or readiness.

Operational logs, connection settings and infrastructure identities remain
private. No 100-million-row rerun was performed.

## Candidate result and disposition

Candidate `f81b7a048741e181895f0f35ee557953cf849c3d`, built as
`v0.1.0-dev.publication-candidate`, is **rejected and unapplied** on this evidence
branch. Source, toolchain, driver/input/helper SHA-256 hashes and every one of
32 native trials (including warmups) and 12 diagnostic samples are recorded in
[results.json](results.json). Baseline native source is `ba855f79`, built as
`v0.1.0-dev.publication-base`; the common diagnostic helper is `c2776bda`.
Go toolchain: 1.27.3 darwin/arm64. Software Redis 8.8.0, Search module 80808.
All native and diagnostic samples fully validated; no caller retries,
serialization conflicts, failed runs, discarded samples or restarts.

| Public call ceiling | Writers | Baseline median s | Candidate median s | Candidate elapsed change |
|---|---:|---:|---:|---:|
| 1,000 | 1 | 7.620 | 8.417 | +10.5% |
| 1,000 | 4 | 16.595 | 19.236 | +15.9% |
| 10,000 | 1 | 1.614 | 1.627 | +0.8% |
| 10,000 | 4 | 2.422 | 2.555 | +5.5% |

Each median has three measured trials plus a separate warmup. Every measured
1K candidate sample exceeded its corresponding baseline range. The whole
baseline block preceded the whole candidate block, separated by other isolated
correctness/read experiments on the same fixture; no other heavy workload ran
during either measurement block. Within each variant the 1K block preceded 10K,
and case order alternated per repetition. This is not randomized crossover or
statistical significance evidence. Larger calls change publication units and
remain a usage control, not a new optimization.

The unchanged six-sample Go harness confirmed zero candidate notification calls
versus forty in baseline. Median observed client-hook totals:

| Call ceiling | Phase | Baseline s | Candidate s |
|---|---|---:|---:|
| 1,000 | Actual append wall | 8.001 | 8.378 |
| 1,000 | Foreground row-batch hook total | 0.957 | 1.487 |
| 1,000 | Foreground notification hook total | 0.691 | 0 |
| 1,000 | Actual ninth-call wall | 4.617 | 4.967 |
| 10,000 | Actual append wall | 1.712 | 1.755 |
| 10,000 | Foreground row-batch hook total | 0.688 | 1.347 |
| 10,000 | Foreground notification hook total | 0.688 | 0 |

These are client-observed durations, **not isolated server CPU**. Foreground
and maintenance categories overlap across pools. In particular, maintenance
copy/control totals do not measure foreground admission wait. The ninth-call
number is directly timed wall time for the whole call. The larger-call row
batch increase nearly offsets the removed notification total. A server-internal
reason or exact cause of the default-call regression is not established.

## Candidate correctness and review

The initial selected run had one leaf test-observer failure: typed-ACL ingest
succeeded but the new scan counter watched only the maintenance pool. Fixing
the observer to watch both existing transports produced 47 passes, zero
skips/failures, with unchanged production source. A related lifecycle selection
passed 35 with six skips: two required actual journal-ACL principals and four
required a separate verified Cluster URI. Both attempts remain private.

A subsequent combined `go test -race -count=1 -p 1` selection passed **59 tests /
subtests, zero skips/failures**, in 27.4 seconds (54.5 seconds with ACL setup and
cleanup). Four temporary Software principals independently denied EVALSHA,
FT.SEARCH, HDEL and ZREM. Each principal's positive PING/EVAL and intended denial
were verified over strict data TLS/RESP3. Actual EVAL-only Arrow and SQL-cache
fallback, write-only Arrow publication, import preflight, complete 513×105 values,
unknown CREATE/row reply, GC, overwrite invalidation, cancellation/reader errors,
late TAG/NaN facts and source import/provenance identity passed. Non-Cluster
retained prepare created no index; resume retained deferred notifications and
consumed no further Arrow input. Cluster-only API/topology scenarios are not
claimed. All temporary principal resources were removed and effective 4x QPF
reverified before timing. Parent source review found no blocker; correctness
success does not override the negative performance result.

## Evidence checks

The unapplied patch SHA-256 is
`bf4f4d13bfe6176146a08f6a98324eee0eb26cc4b8f24e5baf3e5dd8ee40c7dd`.
`git apply --check --whitespace=error-all candidate.patch` passed against exact
parent `c2776bda` before adding this report. Apply only in a separate checkout
at that parent; this evidence report intentionally changes patch-target docs.
Scoped repository whitespace checking excludes the nested patch's context lines:
`git diff --check -- . ':(exclude)docs/sdlc/changes/managed-arrow-first-insert-index/candidate.patch'`.

## Cleanup

The owned Software fixture was stopped and removed after all measurement
processes/clients had exited. Before/after Docker inventories confirmed removal
of exactly its one container and one network; unrelated resources were
unchanged. Its generated environment/TLS private-key files were removed, and
generated credentials were redacted from private controller/operational files.
All 32 native and 12 diagnostic samples, including warmups and the earlier
correctness-observer failure, remain retained privately. No shared datasets,
prior samples, license material or other fixtures were removed. The four
temporary ACL users/mappings/rules/roles had already been removed before timing.
