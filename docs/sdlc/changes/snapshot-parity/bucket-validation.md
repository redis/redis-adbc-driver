# Validation: bounded metadata bucket construction

Date: 2026-11-03. Parent checkpoint `6834041`. Status: locally verified;
[full parity](plan.md) remains incomplete. Comparative timing is recorded below.

## Layout and ownership

Storage protocol 11 adds an exact score-zero sorted-set field directory beside
each metadata bucket HASH. Point metadata reads use HGET, with bucket count,
directory count and reference proofs; they no longer load a whole bucket. Root
maps remain bounded to 256 references. Earlier formats, including 10, are rejected
without modification; no migration is introduced.

A touched bucket copies at most 64 references per Lua call, targeting 1 MiB of
copied logical-field/owner bytes. One oversized field can exceed the target,
subject to server key limits and the existing staging bound. Each field is fetched
individually from the ordered directory, so COUNT hints cannot turn a page into
an entire bucket. Checkpoints persist the last logical key, not a Redis SCAN cursor.
Every page validates the workspace lease, source proof and object references.
Only completed buckets accept native-value mutations or merge entries. Native
values are still copied only when their logical key is changed.

The partial target is registered in its root immediately; a construction edge
takes over that root's reference to the source bucket. Completion releases the
source edge. Abort/expiry retires the partial target and source edge; ordinary
bucket GC releases the references actually copied. Incomplete work cannot seal
or publish, including merge workspaces. Root retirement visits bounded maps;
bucket retirement remains destructive and does not retain Redis SCAN cursors.
Field directories update atomically with own-value registration, logical merge,
pruning and collection. Source values remain live even after their original
manifest disappears.

An attempted construction source is also tracked locally when a page reply is
uncertain, allowing explicit private construction resumption to consult the
server checkpoint. Pages are sent once; SQL, Arrow input and uncertain commands
are not automatically replayed. Resuming a bucket is not resuming a SQL statement
or reconstructing its planning dependencies after process loss.

## Regressions

- Before: a point query read two entire buckets; after: zero whole-bucket reads.
- Before: one touched 193-field bucket copied all 193 references in one call;
  after: batches of at most 64, with every unchanged value preserved.
- Lost local progress resumes from a partial 64-field target, drains the persisted
  construction count and retains every native object exactly once per bucket.
- Direct publication of incomplete work is rejected. A stopped base pin and
  retired original manifest do not revoke the construction source; abort then
  collects both partial target and obsolete source bucket/directories.
- Expired/reclaimed writers cannot advance or resurrect partial construction.
- A lost successful step reply is not automatically replayed. Explicit metadata
  resumption completes without duplicate references; abort restores exact counts.
- Missing field-directory entries fail explicitly. A supported wide-field case
  uses 65 approximately 16 KiB logical keys and copies at most 63 per call, proving
  the 1 MiB target applies before the 64-reference limit.

An initial synthetic 300 KiB-key fixture failed during setup. Raw Software SET
also rejected keys around 64 KiB and larger, while 16 KiB keys succeeded. The byte
case therefore uses supported 16 KiB keys. The invalid fixture is excluded from
passing evidence; no broader key-size support is claimed.

## Checks

Owned Software proxy fixture: three primary shards, strict TLS, active verified
4x QPF and noeviction. Mutating suites ran sequentially. Native builds used
`make -C go build` and its version linker flags on macOS ARM64.

- Complete snapshot race suite: **117 pass events, seven expected Cluster API
  skips, zero failures**, 120.115 seconds.
- All Go unit packages: **895 pass events, 697 expected integration skips, zero
  failures**.
- Native C/ADBC snapshot validation: **12 passed**, 13.162 seconds, including
  concurrent independent Arrow streams and shared-generation readers.
- Native IPC module: **nine passed**, 1.06 seconds.
- gofmt and diff whitespace checks passed.

Author self-review checked source-edge transfer, partial-target registration,
count/directory proofs, checkpoint idempotence, late-writer fences, publication
admission, per-key merge/prune integration and exact native values. This is not
maintainer approval.

Bounded construction limits each call; it still copies all references in a
touched bucket over its complete operation. No constant total construction cost,
write-throughput or full performance parity claim follows. Large atomic
operations, sustained compaction/dirty-lineage scheduling, full feature/CI/platform
and final Cluster API/failover/AOF checks, broader comparisons and owned cleanup
remain required.

## Comparative small writes

[All raw samples](bucket-benchmark.json) retain both the initial implementation
`1a72c1a` and the final pipelined lookup `1c8a49a`, each versus published checkpoint
`6834041` (formats 10 → 11). Both candidates were measured in both orders at
32 and 128 unrelated tables. Each process uses a fresh disposable database, one
initial row, two initial segments, one warmup and five timed samples per case.
The table pools all ten samples per version/catalog size from the final pair of
orders; no samples are discarded. The same macOS ARM64 host, verified Software
fixture, version flags and native dependencies were used. Background maintenance
stays active. Timing includes publication and complete RETURNING fetch;
independent complete-result validation follows each write outside the timer.

| Unrelated tables | Case | Baseline median ms | Final median ms | Baseline/final |
|---:|---|---:|---:|---:|
| 32 | Metadata COMMENT | 15.06 | 18.30 | 0.82× |
| 32 | One-row UPDATE RETURNING | 35.09 | 33.15 | 1.06× |
| 32 | One-row Arrow append | 41.25 | 35.94 | 1.15× |
| 128 | Metadata COMMENT | 25.87 | 26.66 | 0.97× |
| 128 | One-row UPDATE RETURNING | 72.10 | 80.14 | 0.90× |
| 128 | One-row Arrow append | 80.07 | 80.43 | 1.00× |

Results are mixed. The initial 32-table UPDATE median regressed from 33.09 to
42.77 ms across both orders. Lookup and bucket proofs were separate round trips;
pipelining them removes one round trip for the first field in a bucket while
preserving all checks. The final table still shows slower COMMENT and 128-table
UPDATE cases. Bounded work is established; general latency/throughput parity is
not. The comparison does not isolate all directory, GC or background maintenance
costs, and catalog-wide compaction discovery still scales with catalog size.

## Full-feature CI gaps inspected

The completed workflows for parent `6834041` passed DuckDB extension checks but
failed Software Test and native release validation jobs. Reported failures include
ACL helpers reading the legacy metadata namespace, adoption/sweep expectations,
wide-fetch setup using legacy metadata/raw row mutation, and Ray expecting old
live descriptors to observe a later SQL UPDATE. These failures remain open here;
this checkpoint does not replace their assertions with broad skips or weaker
ACL grants. Validate schema/table key isolation against snapshot physical names,
retain wide-fetch optimization checks, and reconcile managed-copy/shared-generation
contracts before claiming full feature/platform parity. The next pushed head
must be checked separately; these are parent-workflow observations.
