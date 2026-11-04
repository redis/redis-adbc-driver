# Private managed-import capture journal

Date: 2026-11-03. Parent source a9a5a78. Storage protocol remains 14;
no migration. [Full parity](plan.md) remains incomplete.

## Reproduction and implementation

A foreign index containing 100,001 application HASHes failed with the retained
execution resource budget error before this change (race reproduction, 18.174
seconds). HASH adoption Apply also had a whole-operation 200,000-candidate limit.
The new integration cases import 100,001 foreign rows and 200,001 HASH rows.

Managed imports now capture into two reference-owned private metadata values:
a HASH of original source keys to native encoded row payloads and a score-zero
ZSET lexical directory. These reuse existing metadata registration, fencing,
reference counts and abort/expiry reclamation. No new live storage format is
introduced. First-observed keys win across repeated SCAN pages and overlapping
prefixes. Payloads preserve NULL versus empty, raw string/key bytes, records,
arrays, exact integers/decimals and floating-point special values.

Capture finishes before any destination row HASH is emitted. This prevents a
broad application index from capturing newly produced destination rows during
the same operation. The journal lives in the excluded control namespace.
Foreign source schema, topology and membership count are checked before/after
capture. External writes still do not participate in the managed snapshot
protocol; this is an observed scan, not an atomic external point-in-time view.

Journal commands and source pages use bounded groups of 128. Apply inspects
payload sizes before fetching, targets 1 MiB groups and retains the 64 MiB
single-value ceiling. Destination images use the existing bounded row-batch
outcome mechanism. Only acknowledged output permits consuming its journal
payloads/directory members. The final catalog publication remains atomic.
Successful imports prune both private journal values from published metadata.

Capture and consumption commands are separately fenced, not an atomic journal
page transaction. Any uncertain journal mutation aborts the import without
replaying the command or source input. Registered partially captured/consumed
values remain collectible. There is no public operation handle, persisted source
plan, input-complete checkpoint or process-resume contract in this change.
HASH adoption planning still has an operation-sized deduplication map. Foreign
source decode is bounded by pages rather than a whole-import budget; exceptionally
wide source pages can allocate before byte admission rejects them. Statement
timeouts, metadata/utility admission and retained partition quotas remain.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries behind the proxy,
Search, noeviction and active effective 4x QPF verified using make -C go redis-up.
Database-mutating checks ran sequentially on macOS ARM64. Operational artifacts,
credentials and infrastructure identifiers remain private.

- Full snapshot race suite: 202 pass events, 12 explicit skips, zero failures,
  300.446 seconds. Separate Cluster and permission-principal cases are skipped;
  this is not a current Cluster/failover validation of the new journal.
- Unit-only packages: 914 pass events, 782 expected integration skips, zero
  failures. This preceded the final interruption test and naming assertion edit.
- Focused large imports: foreign 100,001 rows passed in 48.66 seconds; HASH
  adoption 200,001 rows passed in 35.30 seconds. Exact counts/ranges and untouched
  application values are checked. These are capacity checks, not throughput
  comparisons against a successful old implementation above its limits.
- Codec regression covers raw bytes, NULL/empty, nested record/array values,
  exact numeric values, special floats and corrupt/truncated/trailing packets.
- Refresh tests verify first-observed deduplication across capture chunks,
  overlapping prefixes, no destination rows before capture completes, unchanged
  old root/values after a late source error, and journal reclamation on success
  and abort. Existing imported-key/DML/ALTER/partition checks passed.
- Eight added before/after-execution interruption cases cover journal HSET,
  ZADD, HDEL and ZREM: nine pass events including the parent, zero failures,
  3.176 seconds. Each verifies one input/command attempt, unchanged committed
  root/values, no published target and eventual private journal removal.
- Ordinary adoption/managed import/foreign-index race selection: 31 pass
  events, three ACL skips, zero failures, 11.513 seconds. The initial run
  reproduced stale legacy-name assertions in TestSQLForeignIndexName. Replacing
  those with canonical generation names, matching data/index identities,
  non-reuse and application namespace isolation passes while preserving the
  existing external index/HASH checks.
- Versioned native build completed. Native C/ADBC snapshot suite: 13 passed,
  zero skips/failures, 13.136 seconds. Python IPC: nine passed, 1.55 seconds.
  TLS Ray datasource: 17 passed, 13.54 seconds, three upstream deprecation
  warnings, zero interface skips. Changed Go formatting, Python syntax, local
  evidence links, benchmark JSON and git diff --check passed.

## Comparative sample

Parent/current libraries were built independently using the versioned make
target. [Complete samples and binary hashes](import-journal-benchmark.json) are
recorded for explicit refresh of 10,000 application HASHes, one warmup and three
measured repetitions. Input changes before every refresh; complete ordered
source-key/value results and all original HASH fields are independently checked
after every sample. Timing includes capture through publication; setup and
post-write validation are excluded. No SQL retries or ClickBench run are used.

| Case | Parent median ms | Journal median ms | Change |
|---|---:|---:|---:|
| Explicit foreign refresh | 4174.970 | 4380.075 | +4.9% |

The journal removes the reproduced capture ceiling but this sample shows a
throughput regression. It does not establish import performance parity or
bounded whole-process memory. The initial runner attempt failed before timing
because RESP3 FT.INFO returned a map; the runner now accepts map/list replies.

## Review and remaining work

Reviewed source ownership, first-image deduplication, exact native serialization,
private value registration, single-attempt mutations, payload size admission,
directory completeness, acknowledgement ordering and reclamation. Self-review
is not maintainer approval. Pre-existing managed rows under a broad external
prefix remain a source-ownership edge; capture-before-output only prevents this
operation from feeding on its newly produced rows.

Durable process/input resume, import overhead reductions, bounded adoption
planning, sustained compaction measurements, the complete feature/platform/native
Cluster matrix and broader pre-snapshot comparisons remain open.
