# Validation evidence

2026-10-21. Base `455acb7` (#418), Codex for Albert Wong. Runtime Go source
remains #416 (`4b0fdb5`); no production Go code changed. See [intent](intent.md),
[spec](spec.md), [plan](plan.md), and [hashes/summaries](evidence/validation.json).

## Reproduced regression and correction

PR #418's [Linux ARM64 cluster job](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39691875576/job/119539821225)
failed its later exact live-value assertion after migration; its [result](evidence/hosted-arm64-failure.json)
and [successful ACL cleanup](evidence/hosted-arm64-failure-cleanup.json) are preserved.
Local repeated runs also intermittently omitted the migrated fixture row (id 0)
in a post-migration native Search read: 1,098 instead of 1,099 rows, exact schema,
no extra/duplicate IDs. [Reproduction](evidence/reproduced-missing-migrated-row.json)
and [cleanup](evidence/reproduced-failure-cleanup.json). Direct-HASH ASK/MOVED reads
passed. Raw Redis probes performed after the native failure contained the row;
the exact internal race and whether it belongs to Redis or the driver are not
attributed conclusively. A correct global count is insufficient readiness proof.

The [regression check failed before correction](evidence/count-only-readiness-before-fix.log):
readiness accepted a global count without exercising cursor membership. The
fixture now requires exact count plus exact cap-bounded cursor membership on
all three coordinators for three consecutive rounds, closes unexpected cursors,
and retains the deadline/unrelated-error failures. Native reads/writes are not
retried. New diagnostics contain only generated fixture IDs/counts, schema equality
and mutation counts; no field values, URIs, secrets or descriptors are logged.

**12 final native TLS runs / 228 checks passed**, alternating automatic and explicit
Cluster API routing, against the owned Redis 8.6.2/Search 8.6.0 three-primary
fixture with cap 137. Every run checked complete original and live Arrow
values/schema, all physical owners, and owned ACL absence on every primary.
[All reports and cleanup records](evidence/tls-repetitions.json.gz) are a gzip JSON
array; [summary](evidence/validation.json) retains attempt counts and source hashes.
This establishes these fixed-data checks after the stronger fixture readiness gate,
not transparent runtime resharding, stable snapshots or concurrent-write safety.

## Ownership and process-failure checks

- New Go tests cover adopted HASHes, application-owned indexes, foreign indexes
  and legacy metadata. Repeated rejected planning, statement release and forged
  descriptors preserve exact HASH bytes (including binary keys/values), metadata,
  index definitions and ownership. Unsafe layouts remain unsupported in live mode.
- A new native process test deliberately exits the planner with os._exit after
  publishing its descriptor, then kills a worker after its first partial batch.
  A fresh independently authenticated process retries the descriptor after
  update/delete/insert, checking exact schema/values and high-watermark exclusion.
  Retry retains live semantics; it is not immutable replay. CI now requires this case.
- All partition Go tests with race detector and dedicated cap mutation enabled:
  **21 passed, zero skips**, 3.446s. [Log](evidence/go-partitions-race.log).
- Native partition module: **10 passed**. [JUnit](evidence/native-partitions.xml).
- Full configured native validation: **372 passed, eight feature skips, three
  subtests passed** (6.63s). [Log](evidence/full-validation.log),
  [JUnit with separate skip reasons](evidence/full-validation.xml).
- Management/input/fault checks: **32 passed**. [Log](evidence/fault-tests.log).
  New cases reject false count-only readiness and require three complete cursor
  rounds, with unexpected-cursor deletion. Python compilation, formatting,
  exact JUnit collection, source/evidence/link/whitespace checks passed.

These local clients used macOS ARM64, Python 3.13.16, ADBC manager 1.12.0,
Arrow 26.0.0 and redis-py 8.1.0. The full/native Go suite server was separately
owned native Redis 8.10.2/Search 8.10.1. New Linux checks await this PR's CI.

## Completed hosted compatibility audit

[Release run 39690236286](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39690236286)
passed at head `28c64ed7572f3e7b7c005b340a4a2f7c7ca3f42f` (#417).
Downloaded artifacts were inspected rather than inferred from workflow definitions:
all **12** Linux x86_64/ARM64 and macOS ARM64 cells for Python 3.13/3.14 ×
ADBC manager 1.8/1.12 passed 15 verified-TLS checks each, with matching per-platform
library hashes. Seven full configured suites each had 371 passes/eight feature
skips, with all nine historical native partition cases passing. The focused
macOS minimum-client suite passed all nine cases. Six Linux installed-wheel TLS
backend checks passed: standalone 15, Software proxy 16, real Cluster owners 17
per architecture, including verified all-primary cleanup. [Audited summaries,
feature skips and original-file SHA-256](evidence/hosted-release-audit.json).
That historical run predates #418 and the process-death test; it supplies no
hosted result for either new check.

The existing [repeated benchmark evidence](../../../../benchmarks/partitions/evidence-2026-10-21/README.md)
retains 330 exact-value/schema trials across partition/worker counts, narrow/wide
rows, dense/sparse/skewed layouts, sampled RSS/server memory/network, command
provenance and limitations. It is historical reduced-data evidence, not a
current-revision performance claim or full-scale ClickBench ranking.

## Review, cleanup and remaining scope

Agent review followed REVIEW.md for lifecycle, ownership, failed-state handling,
exact evidence and compatibility scope. No human approval is claimed.
The owned native PID was stopped and exact label/identity-verified TLS container
removed. Both fixture directories, including all CA/private key/password material,
were removed. No paid or Cloud resources were created.

The user explicitly requested runtime features too: #359/#360/#362 remain open
for bounded retained results, global-query execution, additional layouts and
large IDs; published designs alone do not close them. #363 remains open while
this regression fix and the new runtime compatibility/performance checks proceed.
