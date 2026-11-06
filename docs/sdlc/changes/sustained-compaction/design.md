# Batch immutable compaction directory reads

Date: 2026-11-06. See [intent](intent.md), [spec](spec.md), [plan](plan.md).
Preceding production: `c6baae32d77fd13575f0fc46defaaf761f699df4`.

The full-source controlled profile merges four 20,000-row versions, retaining
another unrelated 20,000-row table. It observes 314 copy steps. The original
copy path performs each source's rank query and first bounded directory-page
read separately. The source objects are immutable and retained by the durable
job, so the rank reads are independent; after resolving ranks, the page reads
are independently batchable too.

Use supported [Redis pipelining](https://redis.io/docs/latest/develop/using-commands/pipelining/):
one bounded rank pipeline for nonempty checkpoints, then one bounded first-page
pipeline. Each contains at most eight existing commands. No rank/range command,
page-size limit, permission, storage field or mutation script changes. The
ordinary scanner and compaction share the unchanged page-acceptance/cardinality
check. Row identities, newest-version/tombstone selection and physical-image
fetches still pass through the existing merge. Both aggregate pipeline errors
and individual command errors stop the batch before any copy commit.

This avoids new protocol infrastructure. Native read views and typed export
remain upstream requests ([Search #12219](https://github.com/RediSearch/RediSearch/issues/12219),
[Redis #16048](https://github.com/redis/redis/issues/16048), both open when checked).
Their proposal status does not establish a supported Software capability or
permit removing the driver's existing ownership/visibility/retention proofs.

The existing worker's 5 ms active pacing, per-step reclamation, lease and index
readiness phases remain. The source count, 64-row / 1 MiB copy target, maximum
single-row allowance, eight-segment admission gate and generation limit remain.
No foreground copy, SQL/input retry, cached cross-stage state, Search-index
rebuild or new worker scheduling protocol is introduced.

## Controlled observations

The same owned Software fixture, test bytes and non-race profile mode are used
sequentially. Each case independently checks every final exact BIGINT/payload
and all unrelated rows. Setup and final validation are outside the profile.

| Observation | Preceding | Batched |
|---|---:|---:|
| Paced complete job ms | 4,386.954 | 3,749.054 |
| Unpaced complete job ms | 2,009.996 | 1,706.274 |
| Paced copy step call ms | 2,515.891 | 1,895.249 |
| Unpaced copy step call ms | 1,926.309 | 1,620.536 |
| Copy-phase client calls | 7,226 | 5,345 |
| Copy steps | 314 | 314 |

Rank commands remain 1,252 but use 313 pipelines. The first 1,256 directory
range commands use 314 pipelines. Another 628 individual ZRANGE calls are the
existing metadata-GC queue probes, not unbatched source pages. Total copy-phase
calls fall by 1,881. These are single controlled observations per pacing mode;
native sustained comparisons, storage observations and broad correctness checks
are separate acceptance evidence, not implied by these timings.

## Regression

A new controlled observer refuses individual source directory reads. The
unchanged baseline fails in 0.37 s. Candidate focused races pass 16 events with
no skips, covering two bounded read pipelines at a resumed checkpoint, both
pipeline-only and command-only errors in either phase, unchanged durable output
and checkpoint on a read failure, all final rows, atomic/lost-reply copy cases,
tombstones and bounded directory pages. The existing bounded-page observer now
inspects both individual and pipelined reads; its assertion remains unchanged.

Operational logs and identities remain private. Numeric raw profiles and final
source-specific validation will be retained with the delivery evidence.
