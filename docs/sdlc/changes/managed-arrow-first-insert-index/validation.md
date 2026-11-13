# Managed Arrow publication measurements

Date: 2026-11-13. Requestor: repository user; recorded by Codex.
Status: baseline only; candidate acceptance pending. Source: `ba855f7995b2685182ed1331bcf6703d73089c4a`.

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
