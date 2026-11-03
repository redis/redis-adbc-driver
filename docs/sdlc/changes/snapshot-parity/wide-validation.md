# Validation: snapshot-aware wide HASH/Arrow fetching

Date: 2026-11-03. Baseline `c06ea05`; implementation `1943816`, followed by
planning improvement `f60de7c` and a diagnostic-preservation follow-up. Status: local compatibility/performance checkpoint;
[full parity](plan.md) remains incomplete. Storage protocol remains 12.

## Implementation and reproduced failures

The existing 5,000-row wide-fetch test first failed because its raw legacy
metadata lookup could not find the snapshot-managed table (`42P01`). Its setup
now uses a pinned catalog and writes the embedded-NUL string through SQL.
Published managed HASHes are no longer mutated by the fixture.

Preserving the original optimized-path assertions then exposed two real gaps:
the overlay branch never enabled positional HASH fetching, and live partition
readers added a redundant residual for bounds already enforced by exact directory
seeking. Wide scans now merge newest versions through exact directories or index
candidates before fetching winning HASHes positionally. Tombstones and exact row
identity are checked before Arrow projection; missing referenced HASHes fail
closed. Generic map readers use the same discovery helpers. No keys are guessed.

Eligible ascending row-ID scans use their existing exact merge order without
building an ordered reducer. The follow-up planner change also avoids constructing
visibility masks that this path never consumes. Other ordering/reduction paths
retain their mask proofs. Partition directory seeking still enforces descriptor
bounds exactly, while the caller's original predicate remains a residual.
Inexact boundaries, residual predicates, pushed LIMIT and descending order retain
the original fast-path rejection assertions.
The early planning return also preserves execution-plan diagnostics; the wide
test asserts that an eligible scan still records its plan.

The unmanaged discovery/fetch race checks remain intact on a separate physical
copy of all 4,998 visible fixture rows. They still verify vanished-row omission
and authoritative current HASH values for map and positional consumers. Managed
regular streams and partition descriptors separately retain the old complete
result across a later SQL UPDATE. Exact integers above 2^53 and at int64 limits,
NULLs, defaults, timestamp precision, embedded NUL strings, duplicate projected
columns, batching, cancellation and retained Arrow ownership remain checked.
A new unit case verifies tombstone suppression, missing/incorrect row identities,
diagnostic counts and cancellation in positional snapshot fetching.

## Local checks

- `make -C go redis-up`: dedicated Redis Software fixture, active verified 4x
  QPF, three primary shards, noeviction, strict TLS. Effective Search workers and
  routing verified before integration tests. Infrastructure identifiers and
  credentials remain private.
- `go test -count=1 -v -run '^TestSQLWideHashFetchBothAPIs$' .`: passed,
  53.768 seconds on `1943816` execution code. The test retains every original
  fast/fallback and value assertion.
- `go test -race -count=1 -v -run '^TestSQLWideHashFetchBothAPIs$' .`: passed,
  66.327 seconds on final execution code, including the diagnostic assertion.
  The preceding `f60de7c` run also passed (70.626 seconds).
- Full unit-only `go test -json -count=1 -p 1 ./...`: 896 pass events, 705
  expected integration skips, zero failures. The skips are not compatibility
  evidence.
- Full snapshot race suite on `f60de7c`: 126 pass events, seven expected
  Cluster-only skips, zero failures, 124.668 seconds. This followed an earlier
  complete pass on `1943816` (126.005 seconds).
- Final native `snapshot_validation.py`: 12 passed, 13.360 seconds. IPC suite:
  nine passed, 1.11 seconds. Final Ray suite: 17 passed, zero skips (including
  required private-CA TLS rejection). All used the rebuilt final library.
  Earlier `1943816` native/IPC runs also passed.
- `gofmt`, benchmark Python formatting/help and `git diff --check`: passed.

## Comparative timing

The [benchmark harness](../../../../go/validation/snapshot_read_benchmark.py)
adds `--wide` with 5,000 input rows and 64 columns. Updates replace one-quarter
of row images and DELETE adds tombstones for IDs divisible by seven. Independently
constructed expected Arrow tables validate every column, type, NULL, Unicode/NUL
string and exact int64 value for every warmup and timed sample. Visible output
is 4,286 rows; selective output is the surviving IDs in 100..200.

Each version runs in a separate process against a reset owned fixture. Both
measurement orders are retained, with one discarded warmup per case/run and five
timed samples per case/run: ten final samples per version/case. Timers include
execution, complete Arrow fetch and independent typed-result validation. Setup
is excluded. This synthetic test is not ClickBench and does not reuse or relabel
historical benchmark data.

Final pooled medians, `c06ea05` to `f60de7c`:

| Case | Before (ms) | After (ms) |
|---|---:|---:|
| Wide directory scan | 417.11 | 418.69 |
| Wide selective candidates | 98.02 | 85.51 |

Full-scan latency is approximately unchanged; selective reads improve in this
local comparison. The initial positional-only comparison (`1943816`) showed
410.38 to 412.71 ms and 100.05 to 99.45 ms respectively. Both initial and final
orders and all raw samples are retained in [wide-benchmark.json](wide-benchmark.json).
Do not infer a general speedup or pre-snapshot performance parity.

Environment: macOS ARM64, Python 3.13.16, PyArrow 26.0.0, ADBC driver manager
1.13.0; Redis Software image 8.2.0-78.18, Redis 8.6.2, verified QPF4. A catalog's
rows still occupy one hash slot; three configured primaries do not establish
distributed row placement or multi-shard throughput.

## Author review and remaining scope

Reviewed visibility before predicates/projection, exact row identity and tombstone
validation, sort/limit fallback selection, partition-bound enforcement, missing
data errors, cancellation, Arrow ownership and unmanaged data ownership. The
benchmark validates complete results and retains neutral/slower measurements.
Self-review is not maintainer approval. Current hosted/platform/Cluster API and
recovery reruns, ACL/adoption/sweep compatibility, large atomic operations,
sustained-compaction performance and broader comparisons remain open.
