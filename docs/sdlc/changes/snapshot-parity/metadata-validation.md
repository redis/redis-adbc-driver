# Validation: share metadata buckets and native values

Date: 2026-11-02. Parent checkpoint `f8e1352`. Status: implemented and verified
locally; the complete [parity plan](plan.md) remains open.

## Layout and ownership

Storage protocol **8** replaces full native metadata copies with a root map of
at most 256 bucket references. A write copies touched buckets' reference maps;
native Redis values are copied only when their logical key is mutated. Sets,
hashes, counters and JSON strings retain their existing Redis command semantics.
WATCH materializes the private object before watching it. Command key positions
are resolved; SQL literals, HASH values and set members are never rewritten.

Root-to-bucket and bucket-to-value reference counts have durable retirement
queues. Old manifest records can disappear while another root still refers to
native values created by that manifest. Root retirement detaches at most 256
references. Bucket retirement drains bounded pages without retaining SCAN cursors,
and value collection deletes only zero-reference registered objects. Private
mutation fences update presence proofs and exact native-key directories.

Deletion pruning removes absent logical mappings before publication; future
roots do not retain every deleted table/sequence name. Shared physical metadata
and unregistered private objects cannot be modified through a writer fence.
Canonical script keys remain explicit in the same metadata slot. Committed root,
bucket and object proofs reject missing or incomplete metadata. Formats 6 and 7
are explicitly tested as rejected without modification; no migration is added.

Snapshot UUID/data ownership replaces legacy global prefix/index/name registries.
Index sealing and readiness checks visit dirty tables. Reference enumeration
reads descriptor structure without opening unrelated row indexes. A damaged
unrelated index does not block healthy-table reads or writes. Foreign capture
continues to read its live external source; SQL/catalog metadata reads the managed
copy, including its private physical prefix.

## Regression evidence

- Before: one-row UPDATE copied seven unrelated table descriptors with seven
  DUMP and seven fenced RESTORE commands. After: zero unrelated value-copy
  commands; all table values/counts remain correct.
- An unchanged native descriptor retains the same physical key after its original
  manifest/work record and root map are retired. It is collected after its final
  reference is removed.
- A pinned old query still reads its original table after DROP; releasing that
  pin allows metadata/data reclamation.
- Root-map deletion, bucket-entry deletion and missing native objects fail
  explicitly. Forged shared-object mutation and bypass of metadata registration
  are rejected before changing data.
- Twenty CREATE/DROP cycles retain no deleted table/sequence mappings in the
  current buckets. Counter/metadata exactness and ordinary DML/DDL tests pass.
- Lost-publication-response injection remains active after adding the new declared
  root-map key. Its hook now locates the operation after the declared key count.

The full run caught an import regression in the metadata-helper split: live
foreign capture was subjected to managed-descriptor validation. Restoring the
source branch fixed it; a new catalog assertion checks the managed foreign copy.

## Checks

Owned Software proxy fixture: three primary shards, verified TLS, active verified
4x QPF and noeviction; host/toolchain as in [read evidence](validation.md).
Suites ran sequentially against the disposable database. Libraries used `make build`.

- Complete snapshot race suite: **89 pass events, seven expected Cluster API
  skips, zero failures**, 109.637 seconds.
- All Go unit packages: **894 pass events, 670 expected integration skips, zero
  failures**.
- Native snapshot validation: **11 passed**, 13.261 seconds.
- Native IPC module: **nine passed**, 1.11 seconds.
- Targeted lifetime, reclamation, corruption, physical-fence, churn, import,
  unrelated-index and prior-format checks passed under the race detector.
- `gofmt`, Python benchmark syntax/formatting and `git diff --check` passed.

Physical copy reduction is verified. The comparison below measures local write latency; it does not establish
all-workload or throughput parity.

## Review and remaining work

Author self-review checked lease/pin admission, explicit-key routing, graph edge
lifetimes, alias preservation, deletion pruning, idempotent retirement batches,
physical mutation guards and exact typed replies. This is not maintainer approval.

Publication still enumerates table descriptors and copies aggregate row-reference
lists. Bucket construction copies one complete touched bucket; bounded/resumable
construction is still needed for very large catalogs. Independent-writer rebasing,
row-reference graph work, large atomic operations and compaction throughput remain
open; see [the next reference/publication design](row-reference-design.md).
Full feature/platform/Cluster API recovery validation, broader comparisons and
owned cleanup remain required. No full parity claim is made.

## Comparative small-write results

[Raw samples](write-benchmark.json): `f8e1352` (format 7) versus `a4379dd`
(format 8), built with the same Go/native version flags. Baseline ran first,
then current, each in a separate process after resetting the same owned Software
fixture. One initial row, two initial segments and 32 unrelated empty tables;
one warmup and five timed samples per case. Writes include publication and
complete RETURNING fetch. Independent post-write full result comparison is
outside the timer and must pass before a sample is retained. Setup is excluded.
Background maintenance remains active.

| Case | Baseline median ms | Current median ms | Baseline/current |
|---|---:|---:|---:|
| Metadata COMMENT | 70.97 | 68.50 | 1.04× |
| One-row UPDATE RETURNING | 96.30 | 69.36 | 1.39× |
| One-row Arrow append | 105.05 | 98.54 | 1.07× |

Metadata COMMENT and Arrow append show only small differences in this run;
UPDATE improved, but the remaining catalog-wide reference/maintenance work is
visible in these latencies. No general latency or throughput parity claim is
made. The experiment does not isolate copying from dirty-table sealing,
reference validation or maintenance changes.

Reproduce with the read benchmark runner plus `--writes --rows 1`, using the
owned fixture API/proxy port flags and explicit `--reset-owned-database`.
The runner verifies QPF/noeviction/TLS and independently checks every write.
